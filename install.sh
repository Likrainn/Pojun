#!/usr/bin/env bash
# ARTEX 安装 / 部署脚本（唯一入口）
#
# 运行后选择部署形态，再走对应流程；也可用 --mode 直接指定。
#
#   sudo ./install.sh                     交互选择（二进制 / Docker）
#   sudo ./install.sh --mode=binary       二进制：单文件 + systemd + 系统 PostgreSQL
#   sudo ./install.sh --mode=docker       Docker Compose：容器 + 容器内 PostgreSQL
#   sudo ./install.sh --mode=binary --with-tools
#
#   curl -fsSL .../install.sh | sudo bash -s -- --mode=binary   # 管道场景必须显式给 --mode
#
# 幂等：重复执行 = 升级程序，保留数据（config.json / jwt.key / data / 数据库 / .env）。
#
# 卸载：二进制 systemctl disable --now artex && rm -rf /opt/artex
#       Docker   cd /opt/artex && docker compose down -v && rm -rf /opt/artex
set -euo pipefail

REPO="chu0119/ARTEX"
INSTALL_DIR="/opt/artex"
SERVICE="artex"
RUN_USER="artex"
VERSION="latest"
MODE="${MODE:-}"
WITH_TOOLS=0
LISTEN_ADDR="${LISTEN_ADDR:-:8787}"
PROXY_ADDR="${PROXY_ADDR:-127.0.0.1:8788}"
PG_SUPER="${PG_SUPER:-postgres}"
# 安装期出网代理（apt / curl / go 下载走它）。留空 = 直连。
# 只作用于「装东西」，与运行期 Agent 的目标代理、LLM 代理互不相干。
INSTALL_PROXY="${INSTALL_PROXY:-${HTTPS_PROXY:-}}"
IMAGE="ghcr.io/${REPO,,}"
ASSUME_YES=0
# 是否安装特权助手 artex-hostd(提供「系统 → 宿主环境」页的一键安装)。
# 默认装;不想让 Web 端具备装包能力时用 --no-installer 关掉。
WITH_INSTALLER=1

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

usage(){
  cat <<EOF
ARTEX 安装脚本

用法: $0 [选项]

部署形态:
  --mode=binary|docker   二进制（默认，推荐）或 Docker。不给则交互选择
  --with-tools           顺装常用渗透工具（Docker 形态下工具随镜像固定，见说明）

通用:
  --version=VER          版本，默认 latest（如 v0.3.20）
  --dir=PATH             安装目录（默认 /opt/artex）
  --listen=ADDR          监听地址（默认 :8787）
  --proxy=ADDR           流量记录代理地址（默认 127.0.0.1:8788）
  --install-proxy=URL    安装期出网代理，如 http://10.0.0.14:7897（默认取 \$HTTPS_PROXY）
  -y, --yes              非交互，全部用默认值
  -h, --help             显示帮助

二进制形态专用:
  --user=NAME            运行用户（默认 artex）
  --pg-super=NAME        PostgreSQL 超级用户（默认 postgres）
  --no-installer         不安装特权助手 artex-hostd（关掉 Web 端的一键安装能力）

Docker 形态专用:
  --image=REF            镜像名（默认 $IMAGE）

示例:
  sudo ./install.sh --mode=binary --with-tools
  sudo ./install.sh --mode=docker --version=v0.3.20
  sudo ./install.sh --mode=binary --install-proxy=http://10.0.0.14:7897 --with-tools
EOF
}

# ── 参数 ─────────────────────────────────────────
for arg in "$@"; do
  case "$arg" in
    --mode=*)          MODE="${arg#--mode=}" ;;
    --with-tools)      WITH_TOOLS=1 ;;
    --version=*)       VERSION="${arg#--version=}" ;;
    --dir=*)           INSTALL_DIR="${arg#--dir=}" ;;
    --listen=*)        LISTEN_ADDR="${arg#--listen=}" ;;
    --proxy=*)         PROXY_ADDR="${arg#--proxy=}" ;;
    --install-proxy=*) INSTALL_PROXY="${arg#--install-proxy=}" ;;
    --user=*)          RUN_USER="${arg#--user=}" ;;
    --pg-super=*)      PG_SUPER="${arg#--pg-super=}" ;;
    --image=*)         IMAGE="${arg#--image=}" ;;
    --no-installer)    WITH_INSTALLER=0 ;;
    -y|--yes)          ASSUME_YES=1 ;;
    -h|--help)         usage; exit 0 ;;
    *) die "未知参数: $arg（用 --help 查看帮助）" ;;
  esac
done

# ── 前置检查 ─────────────────────────────────────
[ "$(uname -s)" = "Linux" ] || die "仅支持 Linux"
[ "$(id -u)" = "0" ] || die "请用 root/sudo 运行"

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  PKG_ARCH="linux-amd64" ;;
  aarch64) PKG_ARCH="linux-arm64" ;;
  *) die "不支持的架构: $ARCH（支持 x86_64/aarch64）" ;;
esac

# curl 是两种形态的公共依赖
if ! command -v curl >/dev/null 2>&1; then
  command -v apt-get >/dev/null 2>&1 && { apt-get update -qq || true; apt-get install -y curl >/dev/null || true; }
  command -v dnf >/dev/null 2>&1 && dnf install -y curl >/dev/null || true
fi
command -v curl >/dev/null 2>&1 || die "curl 安装失败，请手动安装"

CURL=(curl -fsSL --retry 3)
[ -n "$INSTALL_PROXY" ] && { CURL+=(-x "$INSTALL_PROXY"); info "安装期使用代理: $INSTALL_PROXY"; }

# ── 选择部署形态 ─────────────────────────────────
choose_mode(){
  if [ -n "$MODE" ]; then
    case "$MODE" in binary|docker) return ;; *) die "--mode 只能是 binary 或 docker"; esac
  fi
  if [ "$ASSUME_YES" = "1" ] || [ ! -t 0 ]; then
    MODE="binary"
    info "非交互模式，默认使用二进制部署（--mode 可显式指定）"
    return
  fi
  printf '\n请选择部署形态：\n'
  printf '  1) 二进制部署（推荐）  单文件 + systemd + 系统 PostgreSQL，运行时零依赖\n'
  printf '  2) Docker 部署         容器编排，工具随镜像固定\n'
  printf '选择 [1]: '
  local pick=""
  read -r -t 60 pick || true
  case "${pick:-1}" in
    2|docker|D) MODE="docker" ;;
    *)          MODE="binary" ;;
  esac
  printf '\n'
}

choose_mode
ok "部署形态: $MODE  目标目录: $INSTALL_DIR"

# ── 解析版本 ─────────────────────────────────────
resolve_version(){
  [ "$VERSION" = "latest" ] || return
  VERSION="$("${CURL[@]}" "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -oE '"tag_name": *"[^"]+"' | head -1 | cut -d'"' -f4)" || VERSION=""
  [ -n "$VERSION" ] || die "无法获取最新版本号（检查网络或用 --install-proxy 指定代理）"
}

# ── 下载并校验 Release 包（两种形态都要：二进制用程序，Docker 用 skills）──
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fetch_release(){
  resolve_version
  ok "目标版本: $VERSION"
  local zip="artex-${VERSION#v}-${PKG_ARCH}.zip"
  info "下载 $zip"
  # 用原名落盘：SHA256SUMS 里写的就是这个名字，改名保存会让 sha256sum -c 找不到文件。
  "${CURL[@]}" "https://github.com/$REPO/releases/download/$VERSION/$zip" -o "$TMP/$zip"
  if "${CURL[@]}" "https://github.com/$REPO/releases/download/$VERSION/SHA256SUMS" -o "$TMP/SHA256SUMS" 2>/dev/null; then
    ( cd "$TMP" && grep " $zip\$" SHA256SUMS | sha256sum -c - ) || die "SHA256 校验失败"
    ok "SHA256 校验通过"
  else
    warn "未取到 SHA256SUMS，跳过校验"
  fi
  unzip -oq "$TMP/$zip" -d "$TMP/unpack" || die "解压失败（需要 unzip）"
}

# ══════════════════════════════════════════════════
# 二进制部署
# ══════════════════════════════════════════════════
deploy_binary(){
  command -v systemctl >/dev/null 2>&1 || die "未检测到 systemd"
  command -v unzip   >/dev/null 2>&1 || { apt-get install -y unzip >/dev/null 2>&1 || dnf install -y unzip >/dev/null 2>&1 || die "需要 unzip"; }

  fetch_release
  local src="$TMP/unpack/artex-${VERSION#v}-${PKG_ARCH}"
  [ -d "$src" ] || die "发行包结构异常: 找不到 $src"

  mkdir -p "$INSTALL_DIR"
  # 程序/skills 覆盖；config.json / jwt.key / data 不在 zip 里，天然不被动
  cp -r "$src"/. "$INSTALL_DIR/"
  chmod +x "$INSTALL_DIR/artex" "$INSTALL_DIR/start.sh"
  ok "程序就绪: $INSTALL_DIR/artex"

  # 运行用户
  if ! id "$RUN_USER" >/dev/null 2>&1; then
    useradd --system --home "$INSTALL_DIR" --shell /usr/sbin/nologin "$RUN_USER"
    ok "已创建系统用户 $RUN_USER"
  fi
  chown -R "$RUN_USER:$RUN_USER" "$INSTALL_DIR"

  # PostgreSQL + config.json（仅首次）
  if [ ! -f "$INSTALL_DIR/config.json" ]; then
    info "配置 PostgreSQL..."
    if ! command -v psql >/dev/null 2>&1; then
      if command -v apt-get >/dev/null 2>&1; then
        apt-get update -qq && apt-get install -y postgresql
      elif command -v dnf >/dev/null 2>&1; then
        dnf install -y postgresql-server && postgresql-setup --initdb
      else
        die "未识别的包管理器，请手动安装 PostgreSQL"
      fi
      systemctl enable --now postgresql
    fi
    systemctl is-active --quiet postgresql || systemctl start postgresql
    local DB_PASS
    DB_PASS="$(head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24)"
    sudo -u "$PG_SUPER" psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='artex'" | grep -q 1 \
      || sudo -u "$PG_SUPER" psql -c "CREATE ROLE artex LOGIN PASSWORD '$DB_PASS'"
    sudo -u "$PG_SUPER" psql -tAc "SELECT 1 FROM pg_database WHERE datname='artex'" | grep -q 1 \
      || sudo -u "$PG_SUPER" createdb -O artex artex
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
    ok "数据库 artex 就绪（密码已写入 config.json，权限 600）"
  else
    ok "沿用既有 config.json（升级不覆盖）"
  fi

  # systemd 单元（加固版）
  # 注意 ProtectSystem=strict 与 ReadWritePaths 必须成对：strict 把整个文件系统挂成
  # 只读，缺 ReadWritePaths 时服务写不了自己的 data 目录——流量索引(SQLite)、任务产物、
  # 证据、transcripts 会全部静默失败（日志刷 "attempt to write a readonly database"），
  # 页面「一键更新」也换不动二进制。
  cat > "/etc/systemd/system/${SERVICE}.service" <<UNIT
[Unit]
Description=ARTEX - AI Autonomous Penetration Testing System
Documentation=https://github.com/${REPO}
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
ReadWritePaths=${INSTALL_DIR}
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
  ok "systemd 服务已启动（${SERVICE}）"

  if [ "$WITH_INSTALLER" = "1" ]; then install_hostd; fi
}

# ── 特权安装助手 artex-hostd ─────────────────────
# 主服务单元带 NoNewPrivileges + ProtectSystem=strict,进程无法提权、/usr 只读,
# 所以「一键安装」做不了——必须在外面用一个 root 助手。它只监听一个 Unix socket,
# 且只认 hosttools 里的白名单工具 id,不接受自由命令。
install_hostd(){
  if [ ! -f "$INSTALL_DIR/artex-hostd" ]; then
    warn "发行包里没有 artex-hostd（旧版本包），跳过；体检仍可用，一键安装不可用"
    return 0
  fi
  install -m 0755 "$INSTALL_DIR/artex-hostd" /usr/local/bin/artex-hostd
  cat > "/etc/systemd/system/${SERVICE}-hostd.service" <<UNIT
[Unit]
Description=ARTEX host tool installer (privileged helper)
Documentation=https://github.com/${REPO}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/artex-hostd -socket /run/artex/hostd.sock -socket-group ${RUN_USER}
Restart=always
RestartSec=5
# /run/artex 由 systemd 建好并随停止清理;socket 本身被助手下调成 0660 root:${RUN_USER}
RuntimeDirectory=artex
RuntimeDirectoryMode=0755
# 助手要调 apt/dnf、写 /usr/local/bin,所以不能像主服务那样把根挂成只读。
# 其余收紧:无原始套接字(只要 AF_INET/INET6/UNIX)、无新特权、私有 /tmp。
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectClock=true
ProtectHostname=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictNamespaces=true
RestrictRealtime=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
LockPersonality=true
SystemCallFilter=@system-service
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable --now "${SERVICE}-hostd" 2>/dev/null || systemctl restart "${SERVICE}-hostd"
  ok "安装助手已启动（${SERVICE}-hostd）→ socket /run/artex/hostd.sock"
}

# ══════════════════════════════════════════════════
# Docker 部署
# ══════════════════════════════════════════════════
deploy_docker(){
  command -v docker >/dev/null 2>&1 || die "未检测到 docker（安装见 https://docs.docker.com/engine/install/）"
  docker compose version >/dev/null 2>&1 || die "未检测到 docker compose 插件（安装 docker-compose-plugin）"
  docker info >/dev/null 2>&1 || die "Docker 守护进程未运行（systemctl start docker）"
  command -v unzip >/dev/null 2>&1 || { apt-get install -y unzip >/dev/null 2>&1 || dnf install -y unzip >/dev/null 2>&1 || die "需要 unzip"; }

  # skills 要挂进容器：空目录挂载会覆盖镜像内置的 /app/skills，所以先从发行包取一份
  fetch_release
  local src="$TMP/unpack/artex-${VERSION#v}-${PKG_ARCH}"
  mkdir -p "$INSTALL_DIR/data" "$INSTALL_DIR/skills"
  if [ ! -f "$INSTALL_DIR/skills/.keep" ] && [ -z "$(ls -A "$INSTALL_DIR/skills" 2>/dev/null)" ]; then
    [ -d "$src/skills" ] && cp -r "$src/skills/." "$INSTALL_DIR/skills/"
  fi
  touch "$INSTALL_DIR/skills/.keep"

  # .env（首次生成随机 PG 密码；升级不覆盖）
  if [ ! -f "$INSTALL_DIR/.env" ]; then
    local PG_PASS
    PG_PASS="$(head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24)"
    cat > "$INSTALL_DIR/.env" <<ENV
POSTGRES_USER=artex
POSTGRES_PASSWORD=$PG_PASS
POSTGRES_DB=artex
ARTEX_IMAGE=$IMAGE
ARTEX_TAG=$VERSION
ENV
    chmod 600 "$INSTALL_DIR/.env"
    ok "已生成 $INSTALL_DIR/.env（含随机数据库密码，权限 600）"
  else
    ok "沿用既有 .env（升级不覆盖）"
  fi

  local PORT="${LISTEN_ADDR##*:}" PROXY_PORT="${PROXY_ADDR##*:}"
  cat > "$INSTALL_DIR/docker-compose.yml" <<COMPOSE
# 由 install.sh 生成。数据在 ./data，skills 在 ./skills，数据库在卷 pgdata。
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: \${POSTGRES_USER:-artex}
      POSTGRES_PASSWORD: \${POSTGRES_PASSWORD:?请在 .env 设置 POSTGRES_PASSWORD}
      POSTGRES_DB: \${POSTGRES_DB:-artex}
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U \${POSTGRES_USER:-artex} -d \${POSTGRES_DB:-artex}"]
      interval: 5s
      timeout: 3s
      retries: 10
    restart: unless-stopped

  artex:
    image: \${ARTEX_IMAGE}:\${ARTEX_TAG}
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      ARTEX_PG_DSN: postgres://\${POSTGRES_USER:-artex}:\${POSTGRES_PASSWORD}@postgres:5432/\${POSTGRES_DB:-artex}?sslmode=disable
      ANTHROPIC_API_KEY: \${ANTHROPIC_API_KEY:-}
      OPENAI_API_KEY: \${OPENAI_API_KEY:-}
      ARTEX_LLM_PROVIDER: \${ARTEX_LLM_PROVIDER:-}
      ARTEX_LLM_MODEL: \${ARTEX_LLM_MODEL:-}
      ARTEX_LLM_BASE_URL: \${ARTEX_LLM_BASE_URL:-}
      ARTEX_LLM_PROXY: \${ARTEX_LLM_PROXY:-}
    ports:
      - "${PORT}:8787"
      - "127.0.0.1:${PROXY_PORT}:8788"
      # 回连端口映射（按需放开）：容器内 MSF/listener 监听的端口要映射到宿主，
      # agent 才能收到 reverse shell。示例：
      # - "4444-4450:4444-4450"
    volumes:
      - ./data:/app/data
      - ./skills:/app/skills
    restart: unless-stopped

volumes:
  pgdata:
COMPOSE

  ok "已生成 $INSTALL_DIR/docker-compose.yml"
  info "拉取镜像 $IMAGE:$VERSION ..."
  ( cd "$INSTALL_DIR" && docker compose pull )
  ( cd "$INSTALL_DIR" && docker compose up -d )
  ok "容器已启动"
}

# ── --with-tools：宿主渗透工具 ───────────────────
install_tools(){
  if [ "$MODE" = "docker" ]; then
    warn "Docker 形态下工具固化在镜像里，不在运行时安装。"
    warn "需要更多工具时：基于官方镜像自建（见 README「Docker 部署」），或改用 --mode=binary。"
    return 0
  fi
  info "安装常用渗透工具（系统源内）..."
  local APT_PROXY=() DNF_PROXY=()
  if [ -n "$INSTALL_PROXY" ]; then
    APT_PROXY=(-o "Acquire::http::Proxy=$INSTALL_PROXY" -o "Acquire::https::Proxy=$INSTALL_PROXY")
    DNF_PROXY=(--setopt=proxy="$INSTALL_PROXY")
  fi
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get "${APT_PROXY[@]}" update -qq || warn "apt update 失败，继续尝试"
    apt-get "${APT_PROXY[@]}" install -y nmap masscan sqlmap hydra whatweb nikto gobuster john \
      netcat-openbsd dnsutils whois unzip seclists >/dev/null 2>&1 \
      || apt-get "${APT_PROXY[@]}" install -y nmap masscan sqlmap hydra whatweb nikto gobuster john \
           netcat-openbsd dnsutils whois unzip >/dev/null \
      || warn "部分 apt 工具未装上"
  elif command -v dnf >/dev/null 2>&1; then
    dnf "${DNF_PROXY[@]}" install -y nmap masscan sqlmap hydra whatweb nikto john nmap-ncat bind-utils unzip \
      || warn "dnf 部分包缺失，已装可用项"
  else
    warn "未识别的包管理器，跳过系统源工具安装"
  fi
  ok "系统源工具安装完成"

  # 非系统源工具（Go 系 / GitHub Release），装不上的在 Web「系统 → 宿主环境」页按需装
  install_go_tools
}

# Go 系工具优先用 `go install`（有 Go 工具链时），否则回退到 GitHub Release 单文件。
install_go_tools(){
  local GOBIN_DIR="/usr/local/bin"
  if command -v go >/dev/null 2>&1; then
    local GOENV=()
    [ -n "$INSTALL_PROXY" ] && GOENV=(HTTPS_PROXY="$INSTALL_PROXY" HTTP_PROXY="$INSTALL_PROXY")
    info "检测到 Go 工具链，安装 httpx / subfinder ..."
    for pkg in "github.com/projectdiscovery/httpx/cmd/httpx@latest" \
               "github.com/projectdiscovery/subfinder/v2/cmd/subfinder@latest"; do
      env "${GOENV[@]}" GOBIN="$GOBIN_DIR" go install "$pkg" >/dev/null 2>&1 \
        && ok "  $(basename "${pkg%%@*}") ✓" || warn "  $(basename "${pkg%%@*}") 安装失败"
    done
  else
    warn "未检测到 Go 工具链，httpx / subfinder 未安装（可在 Web「宿主环境」页装，或装 Go 后重跑）"
  fi

  # nuclei 不在系统源，用 GitHub Release 单文件（带版本号资产名）
  if ! command -v nuclei >/dev/null 2>&1; then
    info "安装 nuclei（projectdiscovery）..."
    local NTAG=""
    NTAG="$("${CURL[@]}" https://api.github.com/repos/projectdiscovery/nuclei/releases/latest \
      | grep -oE '"tag_name": *"[^"]+"' | head -1 | cut -d'"' -f4)" || NTAG=""
    if [ -n "$NTAG" ]; then
      local NURL=""
      case "$ARCH" in
        x86_64)  NURL="https://github.com/projectdiscovery/nuclei/releases/download/${NTAG}/nuclei_${NTAG#v}_linux_amd64.zip" ;;
        aarch64) NURL="https://github.com/projectdiscovery/nuclei/releases/download/${NTAG}/nuclei_${NTAG#v}_linux_arm64.zip" ;;
      esac
      if "${CURL[@]}" "$NURL" -o "$TMP/nuclei.zip" \
         && unzip -oq "$TMP/nuclei.zip" -d "$TMP/nuclei" && [ -x "$TMP/nuclei/nuclei" ]; then
        mv "$TMP/nuclei/nuclei" /usr/local/bin/nuclei && chmod +x /usr/local/bin/nuclei
        ok "nuclei $NTAG → /usr/local/bin/nuclei"
      else
        warn "nuclei 下载失败（可加 --install-proxy 走代理）"
      fi
    else
      warn "无法解析 nuclei 最新版本（GitHub API 受限，可加 --install-proxy）"
    fi
  else
    ok "nuclei 已存在"
  fi

  info "其余缺项见 Web「系统 → 宿主环境」页，可一键安装"
}

# ── 执行部署 ─────────────────────────────────────
case "$MODE" in
  binary) deploy_binary ;;
  docker) deploy_docker ;;
  *) die "未知部署形态: $MODE" ;;
esac

if [ "$WITH_TOOLS" = "1" ]; then install_tools; fi

# ── 健康检查 ─────────────────────────────────────
PORT="${LISTEN_ADDR##*:}"
for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null "http://127.0.0.1:${PORT}/api/health" 2>/dev/null; then
    HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
    ok "服务已就绪 → http://${HOST_IP:-127.0.0.1}:${PORT}（首次访问设置管理员密码）"
    if [ "$MODE" = "binary" ]; then
      systemctl --no-pager status "$SERVICE" | head -5
    else
      ( cd "$INSTALL_DIR" && docker compose ps )
    fi
    exit 0
  fi
  sleep 2
done
if [ "$MODE" = "binary" ]; then
  warn "60s 内未就绪，查看日志: journalctl -u $SERVICE -n 50"
else
  warn "60s 内未就绪，查看日志: cd $INSTALL_DIR && docker compose logs --tail 50"
fi
exit 1
