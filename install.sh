#!/usr/bin/env bash
# ARTEX 一键安装/升级脚本
# 用法:
#   curl -fsSL https://github.com/chu0119/ARTEX/releases/latest/download/install.sh | sudo bash
#   curl -fsSL https://github.com/chu0119/ARTEX/releases/latest/download/install.sh | sudo bash -s -- --with-tools
#   sudo ./install.sh --with-tools --dir /opt/artex --version v0.3.18

set -euo pipefail

REPO="chu0119/ARTEX"
INSTALL_DIR="/opt/artex"
SERVICE="artex"
RUN_USER="artex"
VERSION="latest"
WITH_TOOLS=0
LISTEN_ADDR=":8787"
PROXY_ADDR="127.0.0.1:8788"
PG_SUPER="postgres"

info() { printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok()   { printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[!]\033[0m %s\n' "$*"; }
die()  { printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

# 解析参数
for arg in "$@"; do
  case "$arg" in
    --with-tools)     WITH_TOOLS=1 ;;
    --dir=*)          INSTALL_DIR="${arg#--dir=}" ;;
    --version=*)      VERSION="${arg#--version=}" ;;
    --listen=*)       LISTEN_ADDR="${arg#--listen=}" ;;
    --proxy=*)        PROXY_ADDR="${arg#--proxy=}" ;;
    --user=*)         RUN_USER="${arg#--user=}" ;;
    --pg-super=*)     PG_SUPER="${arg#--pg-super=}" ;;
    --help|-h)
      cat <<EOF
ARTEX 一键安装脚本

用法: $0 [选项]

选项:
  --with-tools          顺带安装常用渗透工具 (nmap, sqlmap, nuclei 等)
  --dir=PATH            安装目录 (默认: /opt/artex)
  --version=VER         指定版本 (默认: latest, 如 v0.3.18)
  --listen=ADDR         监听地址 (默认: :8787)
  --proxy=ADDR          代理地址 (默认: 127.0.0.1:8788)
  --user=NAME           运行用户 (默认: artex)
  --pg-super=NAME       PostgreSQL 超级用户 (默认: postgres)
  --help                显示帮助

示例:
  # 官方推荐方式
  curl -fsSL https://github.com/chu0119/ARTEX/releases/latest/download/install.sh | sudo bash

  # 带工具安装
  curl -fsSL https://github.com/chu0119/ARTEX/releases/latest/download/install.sh | sudo bash -s -- --with-tools

  # 自定义目录和版本
  sudo ./install.sh --dir /opt/artex --version v0.3.18 --with-tools
EOF
      exit 0 ;;
    *) die "未知参数: $arg (用 --help 查看帮助)" ;;
  esac
done

# 前置检查
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

# 解析版本
if [ "$VERSION" = "latest" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/chu0119/ARTEX/releases/latest" | grep -oE '"tag_name": *"[^"]+"' | cut -d'"' -f4)"
  [ -n "$VERSION" ] || die "无法获取最新版本号(检查网络/代理)"
fi
info "目标版本: $VERSION → $INSTALL_DIR"

# 下载并验证
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
info "下载 https://github.com/chu0119/ARTEX/releases/download/$VERSION/artex-${VERSION#v}-${PKG_ARCH}.zip"
curl -fsSL --retry 3 "https://github.com/chu0119/ARTEX/releases/download/$VERSION/artex-${VERSION#v}-${PKG_ARCH}.zip" -o "$TMP/artex.zip"
curl -fsSL --retry 3 "https://github.com/chu0119/ARTEX/releases/download/$VERSION/SHA256SUMS" -o "$TMP/SHA256SUMS"
( cd "$TMP" && grep "artex-${VERSION#v}-${PKG_ARCH}.zip" SHA256SUMS | sha256sum -c - ) || die "SHA256 校验失败"

# 解压并安装
mkdir -p "$INSTALL_DIR"
unzip -oq "$TMP/artex.zip" -d "$TMP/unpack"
cp -r "$TMP/unpack/artex-${VERSION#v}-${PKG_ARCH}"/. "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/artex" "$INSTALL_DIR/start.sh"
ok "程序就绪: $INSTALL_DIR/artex"

# 运行用户
if ! id "$RUN_USER" >/dev/null 2>&1; then
  useradd --system --home "$INSTALL_DIR" --shell /usr/sbin/nologin "$RUN_USER"
  ok "已创建系统用户 $RUN_USER"
fi
chown -R "$RUN_USER:$RUN_USER" "$INSTALL_DIR"

# PostgreSQL
if [ ! -f "$INSTALL_DIR/config.json" ]; then
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
  sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='artex'" | grep -q 1 \
    || sudo -u postgres psql -c "CREATE ROLE artex LOGIN PASSWORD '$DB_PASS'"
  sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='artex'" | grep -q 1 \
    || sudo -u postgres createdb -O artex artex
  cat > "$INSTALL_DIR/config.json" <<JSON
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
  chown "$RUN_USER:$RUN_USER" "$INSTALL_DIR/config.json"
  chmod 600 "$INSTALL_DIR/config.json"
  ok "数据库 artex 就绪"
else
  info "沿用既有 config.json"
fi

# systemd 服务 (加固版)
LISTEN_ADDR_VAL="${LISTEN_ADDR#:}"
cat > "/etc/systemd/system/${SERVICE}.service" <<UNIT
[Unit]
Description=ARTEX - AI Autonomous Penetration Testing System
Documentation=https://github.com/chu0119/ARTEX
After=network-online.target postgresql.service
Wants=network-online.target postgresql.service

[Service]
Type=simple
User=${RUN_USER}
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/start.sh -addr ${LISTEN_ADDR} -proxy ${PROXY_ADDR}
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

# 可选工具安装
if [ "$WITH_TOOLS" = "1" ]; then
  info "安装常用渗透工具..."
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq || warn "apt update 失败,继续尝试"
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
  ok "系统源工具安装完成"

  # nuclei
  if ! command -v nuclei >/dev/null 2>&1; then
    info "安装 nuclei..."
    NTAG="$(curl -fsSL --retry 2 https://api.github.com/repos/projectdiscovery/nuclei/releases/latest \
      | grep -oE '"tag_name": *"[^"]+"' | head -1 | cut -d'"' -f4)" || NTAG=""
    if [ -n "$NTAG" ]; then
      case "$(uname -m)" in
        x86_64)  NURL="https://github.com/projectdiscovery/nuclei/releases/download/${NTAG}/nuclei_${NTAG#v}_linux_amd64.zip" ;;
        aarch64) NURL="https://github.com/projectdiscovery/nuclei/releases/download/${NTAG}/nuclei_${NTAG#v}_linux_arm64.zip" ;;
      esac
      if curl -fsSL --retry 2 "$NURL" -o /tmp/nuclei.zip \
         && unzip -oq /tmp/nuclei.zip -d /tmp/nuclei && [ -x /tmp/nuclei/nuclei ]; then
        mv /tmp/nuclei/nuclei /usr/local/bin/nuclei && chmod +x /usr/local/bin/nuclei
        ok "nuclei $NTAG 已安装"
      else
        warn "nuclei 下载失败"
      fi
    else
      warn "无法解析 nuclei 最新版本"
    fi
  else
    ok "nuclei 已存在"
  fi
  info "其余 Go 系工具请按需安装,清单见 Web「系统 → 宿主环境」页"
fi

# 健康检查
for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null "http://127.0.0.1:${LISTEN_ADDR_VAL}/api/health" 2>/dev/null \
     || curl -fsS -o /dev/null "http://127.0.0.1:${LISTEN_ADDR_VAL}/" 2>/dev/null; then
    ok "服务已就绪 → http://\$(hostname -I 2>/dev/null | awk '{print \$1}'):${LISTEN_ADDR_VAL} (首次访问 /setup 设置管理员密码)"
    systemctl --no-pager status "$SERVICE" | head -5
    exit 0
  fi
  sleep 2
done
warn "30×2s 内未就绪,查看日志: journalctl -u $SERVICE -n 50"
exit 1