#!/usr/bin/env bash
# ARTEX Linux 免 Docker 生产部署：预编译二进制 + 系统 PostgreSQL + systemd 常驻。
#
# 用法:
#   sudo ./deploy-linux.sh                 # 安装/升级到最新 Release
#   sudo VER=v0.3.16 ./deploy-linux.sh     # 指定版本
#   sudo ARTX_DIR=/opt/artex ./deploy-linux.sh
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
PG_SUPER="${PG_SUPER:-postgres}"   # 本机 psql 管理员用户(pee认证)

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

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
  info "配置 PostgreSQL…"
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

# ── systemd 单元 ─────────────────────────────────
cat > "/etc/systemd/system/${SERVICE}.service" <<UNIT
[Unit]
Description=ARTEX - AI Autonomous Penetration Testing System
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
WorkingDirectory=${ARTX_DIR}
ExecStart=${ARTX_DIR}/start.sh -addr ${LISTEN_ADDR} -proxy ${PROXY_ADDR}
Restart=always
RestartSec=5
# 一键更新(start.sh 内 exit 75 换装流程)依赖可执行目录可写;数据目录卷已隔离
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now "$SERVICE" 2>/dev/null || systemctl restart "$SERVICE"

# ── 健康检查 ─────────────────────────────────────
info "等待服务就绪…"
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
