#!/usr/bin/env bash
# ==============================================================================
# 🀄 国粹棋牌全功能服务 - 标准 Linux PC / 云服务器一键部署 & 增量更新脚本
# 适用环境: 家里旧电脑改 Linux (Ubuntu/Debian) 或 通用 Linux 云服务器 (x86_64 / ARM64)
# 特点:
#   1. 自动检测系统软件包、Node.js、PM2，已安装则直接跳过，不重复 apt-get update
#   2. 对比 package.json / package-lock.json 指纹，依赖无变化跳过 npm install
#   3. 对比前端源码指纹，仅在 dist/ 缺失或源码变动时才执行 npm run build
#   4. 安装后自动清理 apt 与 npm 缓存，避免磁盘空间浪费
# 可选参数:
#   --force-deps   强制重新安装 npm 依赖
#   --force-build  强制重新编译前端 dist/
# ==============================================================================

set -e

GREEN='\033[0;32m'
AMBER='\033[1;33m'
CYAN='\033[0;36m'
RED='\033[0;31m'
NC='\033[0m'

FORCE_DEPS=false
FORCE_BUILD=false

for arg in "$@"; do
  case "$arg" in
    --force-deps|--reinstall-deps) FORCE_DEPS=true ;;
    --force-build|--rebuild) FORCE_BUILD=true ;;
  esac
done

calc_text_hash() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 | awk '{print $1}'
  else
    cksum | awk '{print $1"-"$2}'
  fi
}

compute_deps_hash() {
  {
    [ -f "package.json" ] && cat "package.json"
    [ -f "package-lock.json" ] && cat "package-lock.json"
  } | calc_text_hash
}

compute_build_hash() {
  local files
  files=$(find index.html vite.config.ts tsconfig.json package.json package-lock.json server_url.txt src public -type f 2>/dev/null | LC_ALL=C sort)
  if [ -z "$files" ]; then
    echo "empty"
    return
  fi
  while IFS= read -r f; do
    [ -f "$f" ] && printf '%s ' "$f" && calc_text_hash < "$f"
  done <<< "$files" | calc_text_hash
}

echo -e "${AMBER}==================================================================${NC}"
echo -e "${AMBER}  🐧 Linux 主机 / 云服务器部署国粹棋牌 (智能检测 · 增量更新)${NC}"
echo -e "${AMBER}==================================================================${NC}"

if ! command -v apt-get >/dev/null 2>&1; then
  echo -e "${RED}[错误] 当前脚本面向 Ubuntu / Debian 系统 (需要 apt-get)。其他发行版请手动安装 Node.js 20 LTS 与 PM2 后执行 npm install && npm run build && pm2 start \"npx tsx server.ts\" --name chess-server${NC}"
  exit 1
fi

SUDO=""
if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
  SUDO="sudo"
fi

# 1. 检测基础编译与网络工具（已安装则跳过）
echo -e "\n${CYAN}[1/6] 检测基础系统软件包...${NC}"
REQUIRED_PKGS=(curl git build-essential ca-certificates jq lsof)
MISSING_PKGS=()
for pkg in "${REQUIRED_PKGS[@]}"; do
  if ! dpkg -s "$pkg" >/dev/null 2>&1; then
    MISSING_PKGS+=("$pkg")
  fi
done

if [ ${#MISSING_PKGS[@]} -gt 0 ]; then
  echo -e "${AMBER}  -> 缺少系统软件包: ${MISSING_PKGS[*]}，正在按需安装...${NC}"
  $SUDO apt-get update -y
  $SUDO apt-get install -y --no-install-recommends "${MISSING_PKGS[@]}"
  $SUDO apt-get clean >/dev/null 2>&1 || true
  echo -e "${GREEN}  -> 缺失软件包安装完成，并已清理 apt 缓存${NC}"
else
  echo -e "${GREEN}  -> 所有基础系统软件包均已安装，跳过 apt-get update / install${NC}"
fi

# 2. 检查并按需安装 Node.js 20 LTS
echo -e "\n${CYAN}[2/6] 检测 Node.js 运行时...${NC}"
NODE_OK=false
if command -v node >/dev/null 2>&1; then
  NODE_VER=$(node -v | tr -d 'v' | cut -d'.' -f1)
  if [ "$NODE_VER" -ge 18 ]; then
    NODE_OK=true
    echo -e "${GREEN}  -> Node.js 已就绪: $(node -v) (满足 >= 18.x，跳过安装)${NC}"
  fi
fi

if [ "$NODE_OK" = false ]; then
  echo -e "${AMBER}  -> 正在安装 Node.js 20.x LTS...${NC}"
  curl -fsSL https://deb.nodesource.com/setup_20.x | $SUDO -E bash -
  $SUDO apt-get install -y nodejs
  $SUDO apt-get clean >/dev/null 2>&1 || true
  echo -e "${GREEN}  -> Node.js 安装完成: $(node -v)${NC}"
fi

# 3. 检查并按需安装 PM2
echo -e "\n${CYAN}[3/6] 检测 PM2 进程守护...${NC}"
if ! command -v pm2 >/dev/null 2>&1; then
  echo -e "${AMBER}  -> 未检测到 PM2，正在全局安装...${NC}"
  $SUDO npm install -g pm2
  npm cache clean --force >/dev/null 2>&1 || true
else
  echo -e "${GREEN}  -> PM2 已就绪: $(pm2 -v) (跳过安装)${NC}"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 4. 检测并按需安装 Node.js 依赖
echo -e "\n${CYAN}[4/6] 检测项目 Node.js 依赖...${NC}"
CURRENT_DEPS_HASH=$(compute_deps_hash)
SAVED_DEPS_HASH=""
[ -f "node_modules/.deps-hash" ] && SAVED_DEPS_HASH=$(cat "node_modules/.deps-hash" 2>/dev/null || true)

if [ "$FORCE_DEPS" = false ] && [ -d "node_modules/vite" ] && [ -d "node_modules/tsx" ] && [ -d "node_modules/express" ] && [ -f "node_modules/.deps-hash" ] && [ "$CURRENT_DEPS_HASH" = "$SAVED_DEPS_HASH" ]; then
  echo -e "${GREEN}  -> package.json / package-lock.json 无变化且依赖完整，跳过 npm install${NC}"
else
  echo -e "${AMBER}  -> 检测到依赖缺失或有更新，正在执行 npm install...${NC}"
  npm install --no-audit --no-fund
  mkdir -p node_modules
  printf '%s' "$CURRENT_DEPS_HASH" > node_modules/.deps-hash
  npm cache clean --force >/dev/null 2>&1 || true
  echo -e "${GREEN}  -> 项目依赖安装完成，并已清理 npm 缓存${NC}"
fi

# 5. 按需编译前端生产静态资源 (dist/)
echo -e "\n${CYAN}[5/6] 检测前端构建状态 (dist/)...${NC}"
CURRENT_BUILD_HASH=$(compute_build_hash)
SAVED_BUILD_HASH=""
[ -f "dist/.build-hash" ] && SAVED_BUILD_HASH=$(cat "dist/.build-hash" 2>/dev/null || true)

if [ "$FORCE_BUILD" = true ] || [ ! -f "dist/index.html" ] || [ -z "$SAVED_BUILD_HASH" ] || [ "$CURRENT_BUILD_HASH" != "$SAVED_BUILD_HASH" ]; then
  echo -e "${AMBER}  -> 检测到 dist/ 缺失或源码已更新，正在执行 npm run build...${NC}"
  npm run build
  mkdir -p dist
  printf '%s' "$CURRENT_BUILD_HASH" > dist/.build-hash
  echo -e "${GREEN}  -> 前端生产包 dist/ 编译完成${NC}"
else
  echo -e "${GREEN}  -> 前端源码无变化且 dist/index.html 已就绪，跳过 npm run build${NC}"
fi

# 6. 检查 .env 与 data 目录
echo -e "\n${CYAN}[6/6] 检查环境变量 (.env)、数据目录并重启服务...${NC}"
if [ -f "$HOME/.env" ]; then
  echo -e "${GREEN}  -> 检测到用户根目录配置: ~/.env${NC}"
elif [ -f ".env" ]; then
  echo -e "${GREEN}  -> 检测到项目目录已有 .env 配置文件${NC}"
else
  if [ -f ".env.example" ]; then
    cp .env.example .env
  else
    cat << 'EOF' > .env
URL=""
APP_URL=""
CLOUDFLARED_TUNNEL_TOKEN=""
TELEGRAM_BOT_TOKEN=""
ADMIN_USERNAMES="admin_username"
TELEGRAM_API_BASE=""
BOT_MODE="webhook"
WEBHOOK_URL=""
TELEGRAM_WEBHOOK_SECRET=""
ADMIN_API_SECRET=""
PORT=3000
HOST="0.0.0.0"
NODE_ENV="production"
EOF
  fi
  echo -e "${AMBER}  -> 已生成 .env，请编辑填入 TELEGRAM_BOT_TOKEN、URL 及 ADMIN_API_SECRET${NC}"
fi

mkdir -p data
if [ ! -f "data/game_database.json" ]; then
  echo '{"profiles":{},"records":[],"authorizedPhones":[],"adminPhones":[],"revokedPhones":[],"adminLogs":[],"systemAnnouncements":[]}' > data/game_database.json
fi

if pm2 describe chess-server >/dev/null 2>&1; then
  pm2 restart chess-server --update-env
else
  pm2 start "npx tsx server.ts" --name "chess-server" --cwd "$SCRIPT_DIR"
  pm2 save
fi

LAN_IP=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{print $7}' | head -n 1 || hostname -I | awk '{print $1}')

echo -e "\n${GREEN}==================================================================${NC}"
echo -e "${GREEN}  🎉 国粹棋牌已在 Linux 系统完成部署/增量更新！${NC}"
echo -e "${GREEN}  🖥️  本机访问地址:   http://127.0.0.1:3000${NC}"
echo -e "${GREEN}  🏠 局域网访问地址: http://${LAN_IP:-192.168.1.x}:3000${NC}"
echo -e "${AMBER}  🛠️ 常用管理与增量更新指令:${NC}"
echo -e "     - 拉取更新后重编: git pull && ./deploy_linux.sh"
echo -e "     - 强制重编前端:   ./deploy_linux.sh --force-build"
echo -e "     - 查看实时日志:   pm2 logs chess-server"
echo -e "     - 设置开机自启:   pm2 startup (按终端提示复制命令执行一次即可)"
echo -e "${GREEN}==================================================================${NC}"
