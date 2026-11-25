#!/usr/bin/env bash
# ==============================================================================
# 🀄 国粹棋牌全功能服务 - Termux Ubuntu (proot-distro) 增量部署与守护脚本
# 适用环境: Android 手机 Termux 内安装的 proot-distro Ubuntu (glibc 环境)
# 优化特性:
#   1. 检测系统软件包、Node.js、PM2 是否已安装，已安装直接跳过，不重复下载
#   2. 不再删除 node_modules 与 package-lock.json，仅在依赖变更时执行 npm install
#   3. 仅在 dist/ 缺失或源码更新时执行 npm run build，并自动清理安装缓存节省空间
# 可选参数:
#   --force-deps   强制重新安装 npm 依赖 (如怀疑 Rollup 原生绑定异常时使用)
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
echo -e "${AMBER}  🀄 国粹棋牌 Termux Ubuntu 部署/增量更新 (智能检测 · 省空间)${NC}"
echo -e "${AMBER}==================================================================${NC}"

# 1. 检查是否在 Ubuntu/Debian 体系中
if ! command -v apt-get >/dev/null 2>&1; then
  echo -e "${RED}[错误] 未检测到 apt 包管理器！请确保您已执行 'proot-distro login ubuntu' 进入了 Ubuntu 系统。${NC}"
  exit 1
fi

echo -e "\n${CYAN}[1/7] 检测基础编译与网络软件包...${NC}"
REQUIRED_PKGS=(curl git build-essential pkg-config python3 ca-certificates jq nano iproute2 lsof)
MISSING_PKGS=()
for pkg in "${REQUIRED_PKGS[@]}"; do
  if ! dpkg -s "$pkg" >/dev/null 2>&1; then
    MISSING_PKGS+=("$pkg")
  fi
done

if [ ${#MISSING_PKGS[@]} -gt 0 ]; then
  echo -e "${AMBER}  -> 缺少系统软件包: ${MISSING_PKGS[*]}，正在按需安装...${NC}"
  apt-get update -y
  apt-get install -y --no-install-recommends "${MISSING_PKGS[@]}"
  apt-get clean >/dev/null 2>&1 || true
  echo -e "${GREEN}  -> 缺失软件包安装完成，并已清理 apt 缓存${NC}"
else
  echo -e "${GREEN}  -> 所有基础系统软件包均已安装，跳过 apt-get update / install${NC}"
fi

# 2. 检查并按需安装 Node.js LTS
echo -e "\n${CYAN}[2/7] 检测 Node.js 运行时环境...${NC}"
NODE_OK=false
if command -v node >/dev/null 2>&1; then
  NODE_VER=$(node -v | tr -d 'v' | cut -d'.' -f1)
  if [ "$NODE_VER" -ge 18 ]; then
    NODE_OK=true
    echo -e "${GREEN}  -> 检测到已就绪的 Node.js: $(node -v) (满足 >= 18.x，跳过安装)${NC}"
  fi
fi

if [ "$NODE_OK" = false ]; then
  echo -e "${AMBER}  -> 正在安装 Node.js 20.x LTS...${NC}"
  curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
  apt-get install -y nodejs
  apt-get clean >/dev/null 2>&1 || true
  echo -e "${GREEN}  -> Node.js 安装成功: $(node -v), npm: $(npm -v)${NC}"
fi

# 3. 按需安装 PM2
echo -e "\n${CYAN}[3/7] 检测后台守护工具 PM2...${NC}"
if ! command -v pm2 >/dev/null 2>&1; then
  echo -e "${AMBER}  -> 未检测到 PM2，正在安装...${NC}"
  npm install -g pm2
  npm cache clean --force >/dev/null 2>&1 || true
else
  echo -e "${GREEN}  -> PM2 已就绪: $(pm2 -v) (跳过安装)${NC}"
fi

# 4. 检测并按需安装 Node.js 依赖（不再暴力删除 node_modules）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "\n${CYAN}[4/7] 检测 Node.js 项目依赖...${NC}"
CURRENT_DEPS_HASH=$(compute_deps_hash)
SAVED_DEPS_HASH=""
[ -f "node_modules/.deps-hash" ] && SAVED_DEPS_HASH=$(cat "node_modules/.deps-hash" 2>/dev/null || true)

if [ "$FORCE_DEPS" = false ] && [ -d "node_modules/vite" ] && [ -d "node_modules/tsx" ] && [ -d "node_modules/express" ] && [ -f "node_modules/.deps-hash" ] && [ "$CURRENT_DEPS_HASH" = "$SAVED_DEPS_HASH" ]; then
  echo -e "${GREEN}  -> 依赖无变化且核心模块完整，跳过 npm install${NC}"
else
  echo -e "${AMBER}  -> 检测到依赖缺失或 package.json 变更，正在安装 Node.js 依赖...${NC}"
  npm install --include=optional --no-audit --no-fund
  mkdir -p node_modules
  printf '%s' "$CURRENT_DEPS_HASH" > node_modules/.deps-hash
  npm cache clean --force >/dev/null 2>&1 || true
  echo -e "${GREEN}  -> 依赖安装完成，并已清理 npm 缓存${NC}"
fi

# 5. 按需编译前端生产静态资源 (Vite + Tailwind CSS v4)
echo -e "\n${CYAN}[5/7] 检测前端生产包构建状态 (dist/)...${NC}"
CURRENT_BUILD_HASH=$(compute_build_hash)
SAVED_BUILD_HASH=""
[ -f "dist/.build-hash" ] && SAVED_BUILD_HASH=$(cat "dist/.build-hash" 2>/dev/null || true)

if [ "$FORCE_BUILD" = true ] || [ ! -f "dist/index.html" ] || [ -z "$SAVED_BUILD_HASH" ] || [ "$CURRENT_BUILD_HASH" != "$SAVED_BUILD_HASH" ]; then
  echo -e "${AMBER}  -> 检测到 dist/ 缺失或源码已更新，正在执行 npm run build...${NC}"
  npm run build
  mkdir -p dist
  printf '%s' "$CURRENT_BUILD_HASH" > dist/.build-hash
  echo -e "${GREEN}  -> 前端生产包 dist/ 构建成功${NC}"
else
  echo -e "${GREEN}  -> 前端源码无变化且 dist/index.html 已存在，跳过 npm run build${NC}"
fi

# 6. 配置 .env 环境变量与 data 目录
echo -e "\n${CYAN}[6/7] 检查环境变量 (.env) 与数据目录...${NC}"
if [ ! -f ".env" ]; then
  if [ -f ".env.example" ]; then
    cp .env.example .env
    echo -e "${GREEN}  -> 已从 .env.example 复制生成 .env 基础配置文件${NC}"
  else
    cat << 'EOF' > .env
URL=
APP_URL=
BOT_MODE=auto
TELEGRAM_BOT_TOKEN=
ADMIN_USERNAMES=admin_username
TELEGRAM_WEBHOOK_SECRET=
PORT=3000
HOST=0.0.0.0
NODE_ENV=production
EOF
    echo -e "${GREEN}  -> 已自动创建标准 .env 配置文件${NC}"
  fi
  echo -e "${AMBER}  -> 提示: nano .env 填写 TELEGRAM_BOT_TOKEN 即可激活机器人${NC}"
else
  echo -e "${GREEN}  -> 检测到已有 .env 配置文件${NC}"
fi

mkdir -p data
if [ ! -f "data/game_database.json" ]; then
  echo '{"profiles":{},"records":[],"authorizedPhones":[],"adminPhones":[],"revokedPhones":[],"adminLogs":[],"systemAnnouncements":[]}' > data/game_database.json
fi

# 7. 平滑重启或启动服务
echo -e "\n${CYAN}[7/7] 启动或平滑重启 PM2 常驻对战服务...${NC}"
if pm2 describe chess-server >/dev/null 2>&1; then
  pm2 restart chess-server --update-env
else
  if command -v fuser >/dev/null 2>&1; then
    fuser -k 3000/tcp 2>/dev/null || true
  elif command -v lsof >/dev/null 2>&1; then
    OLD_PID=$(lsof -t -i:3000 2>/dev/null || true)
    [ -n "$OLD_PID" ] && kill -9 $OLD_PID 2>/dev/null || true
  fi
  pm2 start "npx tsx server.ts" --name "chess-server" --max-memory-restart 300M
  pm2 save
fi

LAN_IP=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{print $7}' | head -n 1 || hostname -I | awk '{print $1}')

echo -e "\n${GREEN}==================================================================${NC}"
echo -e "${GREEN}  🎉 棋牌全栈服务已在 Termux Ubuntu 完成增量部署/启动！${NC}"
echo -e "${GREEN}  📱 手机本机访问:   http://127.0.0.1:3000${NC}"
echo -e "${GREEN}  🏠 局域网好友对战: http://${LAN_IP:-192.168.1.x}:3000${NC}"
echo -e "${AMBER}  🛠️ 常用指令:${NC}"
echo -e "     - 代码更新增量刷新: git pull && bash deploy_ubuntu.sh"
echo -e "     - 强制重编前端:     bash deploy_ubuntu.sh --force-build"
echo -e "     - 强制重装依赖:     bash deploy_ubuntu.sh --force-deps"
echo -e "     - 查看实时日志:     pm2 logs chess-server"
echo -e "${GREEN}==================================================================${NC}"
