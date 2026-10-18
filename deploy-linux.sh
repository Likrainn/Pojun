#!/usr/bin/env bash
# ARTEX Linux 免 Docker 生产部署:预编译二进制 + 系统 PostgreSQL + systemd 常驻。
#
# 用法:
#   sudo ./deploy-linux.sh                 # 安装/升级到最新 Release
#   sudo VER=v0.3.16 ./deploy-linux.sh     # 指定版本
#   sudo ARTX_DIR=/opt/artex ./deploy-linux.sh
#   sudo ./deploy-linux.sh --with-tools    # 顺装常用渗透工具(见下方清单)
#
# 幂等:重复执行 = 升级程序、保留数据(config.json/jwt.key/data/数据库不动)。
# 卸载:systemctl disable --now artex && rm -rf /opt/artex(数据库自行处置)。
set -euo pipefail

VER="${VER:-latest}"
ARTX_DIR="${ARTX_DIR:-/opt/artex}"
SERVICE="artex"
RUN_USER="artex"
REPO_URL="https://github.com/chu0119/ARTEX"
LISTEN_ADDR="${LISTEN_ADDR:-:8787}"
PROXY_ADDR="${PROXY_ADDR:-127.0.0.1:8788}"
PG_SUPER="${PG_SUPER:-postgres}"   # 本机 psql 管理用户(peer认证)
WITH_TOOLS=0

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

# ── 参数 ─────────────────────────────────────────
for arg in "$@"; do
  case "$arg" in
    --with-tools) WITH_TOOLS=1 ;;
    --help|-h)
      sed -n '2,11p' "$0"; exit 0 ;;
    *) die "未知参数: $arg(支持 --with-tools;版本/目录用 VER=/ARTX_DIR= 环境变量)" ;;
  esac
done

# ── 前置检查 ─────────────────────────────────────
[ "$(uname -s)" = "Linux" ] || die "仅支持 Linux"
[ "$(id -u)" = "0" ] || die "请用 root/sudo 运行"
command -v systemctl >/dev/null 2>&1 || die "未检测到 systemd"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  PKG_ARCH="linux-amd64" ;;
  aarch64) PKG_ARCH="linux-arm64" ;;
  *) die "不支持的架构: $ARCH(支持 x86_64/aarch64)" ;;
esac
command -v curl >/dev/null 2>&1 || { apt-get update >/dev/null 2>&1 || true; apt-get install -y curl >/dev/null; }
command -v curl >/dev/null 2>&1 || die "curl 安装失败,请手动安装"

# ── 解析版本号 ───────────────────────────────────
if [ "$VER" = "latest" ]; then
  VER="$(curl -fsSL "https://api.github.com/repos/${REPO_URL#https://github.com/}/releases/latest" | grep -oE '"tag_name": *"[^"]+"' | cut -d'"' -f4)"
  [ -n "$VER" ] || die "无法获取最新版本号(检查网络/代理)"
fi
ok "目标版本: $VER → $ARTX_DIR"

# ── 下载并换装(升级安全:先解到临时目录)─────────
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
info "下载 $REPO_URL/releases/download/$VER/artex-${VER#v}-$PKG_ARCH.zip"
curl -fsSL --retry 3 "$REPO_URL/releases/download/$VER/artex-${VER#v}-$PKG_ARCH.zip" -o "$TMP/artex.zip"
curl -fsSL --retry 3 "$REPO_URL/releases/download/$VER/SHA256SUMS" -o "$TMP/SHA256SUMS"
( cd "$TMP" && grep "artex-${VER#v}-$PKG_ARCH.zip" SHA256SUMS | sha256sum -c - ) || die "SHA256 校验失败"
mkdir -p "$ARTX_DIR"
unzip -oq "$TMP/artex.zip" -d "$TMP/unpack"
# 换装:程序/skills 覆盖,config.json/jwt.key/data 天然不在 zip 里、不被动
cp -r "$TMP/unpack/artex-${VER#v}-$PKG_ARCH"/. "$ARTX_DIR/"
chmod +x "$ARTX_DIR/artex" "$ARTX_DIR/start.sh"
ok "程序就绪: $ARTX_DIR/artex"

# ── 运行用户 ─────────────────────────────────────
if ! id "$RUN_USER" >/dev/null 2>&1; then
  useradd --system --home "$ARTX_DIR" --shell /usr/sbin/nologin "$RUN_USER"
  ok "已创建系统用户 $RUN_USER"
fi
chown -R "$RUN_USER:$RUN_USER" "$ARTX_DIR"

# ── PostgreSQL(仅首次)──────────────────────────
if [ ! -f "$ARTX_DIR/config.json" ]; then
  info "配置 PostgreSQL..."
  if ! command -v psql >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
      apt-get update && apt-get install -y postgresql
    elif command -v dnf >/dev/null 2>&1; then
      dnf install -y postgresql-server && postgresql-setup --initdb
    else
      die "未识别的包管理器,请手动安装 PostgreSQL"
    fi
    systemctl enable --now postgresql
  fi
  systemctl is-active --quiet postgresql || systemctl start postgresql
  DB_PASS="$(head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24)"
  sudo -u "$PG_SUPER" psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='artex'" | grep -q 1 \
    || sudo -u "$PG_SUPER" psql -c "CREATE ROLE artex LOGIN PASSWORD '$DB_PASS'"
  sudo -u "$PG_SUPER" psql -tAc "SELECT 1 FROM pg_database WHERE datname='artex'" | grep -q 1 \
    || sudo -u "$PG_SUPER" createdb -O artex artex
  # 本机 trust/peer 环境下密码登录需确保 pg_hba 含 md5/scram(主流默认已含)
  cat > "$ARTX_DIR/config.json" <<JSON
{
  "database": {
    "host": "127.0.0.1",
    "port": 5432,
    "user": "artex",
    "password": "$DB_PASS",
    "dbname": "artex",
    "sslmode": "disable"
  }
}
JSON
  chown "$RUN_USER:$RUN_USER" "$ARTX_DIR/config.json"
  chmod 600 "$ARTX_DIR/config.json"
  ok "数据库 artex 就绪(密码已写入 config.json,权限 600)"
else
  ok "沿用既有 config.json(升级不覆盖)"
fi

# ── systemd 单元 (加固版)─────────────────────────
cat > "/etc/systemd/system/${SERVICE}.service" <<UNIT
[Unit]
Description=ARTEX - AI Autonomous Penetration Testing System
Documentation=https://github.com/chu0119/ARTEX
After=network-online.target postgresql.service
Wants=network-online.target postgresql.service

[Service]
Type=simple
User=${RUN_USER}
WorkingDirectory=${ARTX_DIR}
ExecStart=${ARTX_DIR}/start.sh -addr ${LISTEN_ADDR} -proxy ${PROXY_ADDR}
Restart=always
RestartSec=5
# 安全加固
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true
RestrictNamespaces=true
RestrictRealtime=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallFilter=@system-service
SystemCallErrorAction=EPERM
CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_DAC_OVERRIDE CAP_CHOWN
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now "$SERVICE" 2>/dev/null || systemctl restart "$SERVICE"

# ── --with-tools:宿主渗透工具安装 ─────────────────
# 二进制部署形态的核心红利:工具装在系统里,ARTEX 经 Bash 直接调用,
# 「系统 → 宿主环境」页与 agent 的 graph_overview 都能看到这份清单。
install_tools(){
  info "安装常用渗透工具(apt/dnf 源内)..."
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq || warn "apt update 失败,继续尝试"
    # 系统源内即有的:扫描/爆破/口令/取证基础面(seclists 缺包时降级装基础项)
    apt-get install -y nmap masscan sqlmap hydra whatweb nikto gobuster john \
      netcat-openbsd dnsutils whois unzip seclists >/dev/null 2>&1 \
      || apt-get install -y nmap masscan sqlmap hydra whatweb nikto gobuster john \
           netcat-openbsd dnsutils whois unzip >/dev/null
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y nmap masscan sqlmap hydra whatweb nikto john nmap-ncat bind-utils unzip \
      || warn "dnf 部分包缺失,已装可用项"
  else
    warn "未识别的包管理器,跳过系统源工具安装"
  fi
  ok "系统源工具安装完成(缺项见 /system/hostenv 页)"

  # nuclei:不在系统源,GitHub Release 下载(GitHub API 解析最新 tag → 资产名带版本)
  if ! command -v nuclei >/dev/null 2>&1; then
    info "安装 nuclei(projectdiscovery)..."
    NTAG="$(curl -fsSL --retry 2 https://api.github.com/repos/projectdiscovery/nuclei/releases/latest \
      | grep -oE '"tag_name": *"[^"]+"' | head -1 | cut -d'"' -f4)" || NTAG=""
    if [ -n "$NTAG" ]; then
      case "$ARCH" in
        x86_64)  NURL="https://github.com/projectdiscovery/nuclei/releases/download/${NTAG}/nuclei_${NTAG#v}_linux_amd64.zip" ;;
        aarch64) NURL="https://github.com/projectdiscovery/nuclei/releases/download/${NTAG}/nuclei_${NTAG#v}_linux_arm64.zip" ;;
      esac
      if curl -fsSL --retry 2 "$NURL" -o "$TMP/nuclei.zip" \
         && unzip -oq "$TMP/nuclei.zip" -d "$TMP/nuclei" && [ -x "$TMP/nuclei/nuclei" ]; then
        mv "$TMP/nuclei/nuclei" /usr/local/bin/nuclei && chmod +x /usr/local/bin/nuclei
        ok "nuclei $NTAG → /usr/local/bin/nuclei"
      else
        warn "nuclei 下载失败,手动装: https://github.com/projectdiscovery/nuclei/releases"
      fi
    else
      warn "无法解析 nuclei 最新版本(GitHub API 受限),手动装: https://github.com/projectdiscovery/nuclei/releases"
    fi
  else
    ok "nuclei 已存在,跳过"
  fi

  info "其余 Go 系工具(httpx/subfinder/ffuf 等)按需安装,清单见 Web「系统 → 宿主环境」页"
}

if [ "$WITH_TOOLS" = "1" ]; then
  install_tools
fi

# ── 健康检查 ─────────────────────────────────────
for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null "http://127.0.0.1:${LISTEN_ADDR#*:}/api/health" 2>/dev/null \
     || curl -fsS -o /dev/null "http://127.0.0.1:${LISTEN_ADDR#*:}/" 2>/dev/null; then
    ok "服务已就绪 → http://$(hostname -I 2>/dev/null | awk '{print $1}'):${LISTEN_ADDR#*:}(首次访问 /setup 设置管理员密码)"
    systemctl --no-pager status "$SERVICE" | head -5
    exit 0
  fi
  sleep 2
done
warn "30×2s 内未就绪,查看日志: journalctl -u $SERVICE -n 50"
exit 1