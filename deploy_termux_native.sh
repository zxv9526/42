#!/data/data/com.termux/files/usr/bin/bash
# ==============================================================================
# 🀄 国粹棋牌全功能服务 - Android Termux 原生直连一键部署 / 增量更新脚本
# 适用环境: Android 手机原生 Termux (直接 git clone 后运行即可，无需任何 Linux 容器)
# 特性:
#   - 自动检测已安装的 Termux 软件包，已安装则跳过 pkg update/install 并清理缓存
#   - 对比 package.json / package-lock.json 指纹，依赖无变化跳过 npm install
#   - 智能检测 dist/ 静态包与源码变更，按需拉取最新 dist.tar.gz
#   - 更新代码重启后端时尽量保持已有 Cloudflare Tunnel 隧道不断线
# 可选参数:
#   --force-deps   强制重新安装 npm 依赖
#   --update-dist  强制重新检查并拉取 GitHub Releases 最新 dist.tar.gz
#   --restart-all  强制重建 tmux 会话（同时重启服务与隧道）
# ==============================================================================

set -e

GREEN='\033[0;32m'
AMBER='\033[1;33m'
CYAN='\033[0;36m'
RED='\033[0;31m'
NC='\033[0m'

FORCE_DEPS=false
UPDATE_DIST=false
RESTART_ALL=false

for arg in "$@"; do
  case "$arg" in
    --force-deps|--reinstall-deps) FORCE_DEPS=true ;;
    --update-dist|--force-build|--rebuild) UPDATE_DIST=true ;;
    --restart-all) RESTART_ALL=true ;;
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

compute_frontend_hash() {
  local files
  files=$(find index.html vite.config.ts tsconfig.json package.json server_url.txt src public -type f 2>/dev/null | LC_ALL=C sort)
  if [ -z "$files" ]; then
    echo "empty"
    return
  fi
  while IFS= read -r f; do
    [ -f "$f" ] && printf '%s ' "$f" && calc_text_hash < "$f"
  done <<< "$files" | calc_text_hash
}

echo -e "${AMBER}==================================================================${NC}"
echo -e "${AMBER}  📱 Android Termux 原生直跑部署 (智能检测 · 增量更新)${NC}"
echo -e "${AMBER}==================================================================${NC}"

# 0. 环境校验
if [ -z "$PREFIX" ] || [[ "$PREFIX" != *"com.termux"* ]]; then
  echo -e "${RED}[提示] 当前似乎不在 Termux 终端中。若是 Linux 电脑或云服务器，请改用 deploy_linux.sh！${NC}"
fi

# 1. 开启唤醒锁防止息屏断网
echo -e "\n${CYAN}[1/6] 申请 Termux 唤醒锁...${NC}"
if command -v termux-wake-lock >/dev/null 2>&1; then
  termux-wake-lock 2>/dev/null || true
  echo -e "${GREEN}  -> 已激活 termux-wake-lock 唤醒守护${NC}"
else
  echo -e "${AMBER}  -> 未检测到 termux-wake-lock，请在系统设置中关闭 Termux 电池优化${NC}"
fi

# 2. 检测并按需安装原生运行环境与 cloudflared (已安装则不重复更新/安装)
echo -e "\n${CYAN}[2/6] 检测 Termux 原生软件包...${NC}"
PKG_CHECKS=(
  "node:nodejs-lts"
  "git:git"
  "curl:curl"
  "jq:jq"
  "tmux:tmux"
  "lsof:lsof"
  "tar:tar"
  "cloudflared:cloudflared"
)
MISSING_PKGS=()
for entry in "${PKG_CHECKS[@]}"; do
  cmd="${entry%%:*}"
  pkg="${entry##*:}"
  if ! command -v "$cmd" >/dev/null 2>&1 && ! dpkg -s "$pkg" >/dev/null 2>&1; then
    MISSING_PKGS+=("$pkg")
  fi
done

if [ ${#MISSING_PKGS[@]} -gt 0 ]; then
  echo -e "${AMBER}  -> 缺少软件包: ${MISSING_PKGS[*]}，正在按需安装...${NC}"
  pkg update -y
  pkg install -y "${MISSING_PKGS[@]}"
  pkg clean >/dev/null 2>&1 || true
  echo -e "${GREEN}  -> 缺失软件包安装完成，并已清理 pkg 缓存${NC}"
else
  echo -e "${GREEN}  -> 所有必备软件包均已安装，跳过 pkg update / pkg install${NC}"
fi
echo -e "${GREEN}  -> Node.js: $(node -v) | npm: $(npm -v)${NC}"
if command -v cloudflared >/dev/null 2>&1; then
  echo -e "${GREEN}  -> cloudflared: $(cloudflared --version | head -n 1)${NC}"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 3. 检测并按需安装纯服务端运行依赖 (--omit=dev)
echo -e "\n${CYAN}[3/6] 检测服务端 Node.js 依赖 (npm install --omit=dev)...${NC}"
CURRENT_DEPS_HASH=$(compute_deps_hash)
SAVED_DEPS_HASH=""
[ -f "node_modules/.deps-hash" ] && SAVED_DEPS_HASH=$(cat "node_modules/.deps-hash" 2>/dev/null || true)

if [ "$FORCE_DEPS" = false ] && [ -d "node_modules/tsx" ] && [ -d "node_modules/express" ] && [ -d "node_modules/ws" ] && [ -n "$CURRENT_DEPS_HASH" ] && [ "$CURRENT_DEPS_HASH" = "$SAVED_DEPS_HASH" ]; then
  echo -e "${GREEN}  -> package.json / package-lock.json 无变化且核心依赖完整，跳过 npm install${NC}"
else
  echo -e "${AMBER}  -> 检测到依赖需要初始化或更新，正在安装纯服务端依赖...${NC}"
  npm install --omit=dev --no-audit --no-fund
  mkdir -p node_modules
  printf '%s' "$CURRENT_DEPS_HASH" > node_modules/.deps-hash
  npm cache clean --force >/dev/null 2>&1 || true
  echo -e "${GREEN}  -> 服务端依赖安装完成，并已清理 npm 缓存${NC}"
fi

# 4. 智能检查前端静态包 dist/ 是否需要拉取更新
echo -e "\n${CYAN}[4/6] 检测前端静态资源 (dist/index.html)...${NC}"
CURRENT_FRONTEND_HASH=$(compute_frontend_hash)
SAVED_FRONTEND_HASH=""
[ -f "dist/.frontend-hash" ] && SAVED_FRONTEND_HASH=$(cat "dist/.frontend-hash" 2>/dev/null || true)

NEED_DIST_CHECK=false
if [ "$UPDATE_DIST" = true ] || [ ! -f "dist/index.html" ]; then
  NEED_DIST_CHECK=true
elif [ -n "$SAVED_FRONTEND_HASH" ] && [ "$CURRENT_FRONTEND_HASH" != "$SAVED_FRONTEND_HASH" ]; then
  echo -e "${AMBER}  -> 检测到前端源码较上次部署有更新，检查是否有新的 dist.tar.gz 发布包...${NC}"
  NEED_DIST_CHECK=true
elif [ -z "$SAVED_FRONTEND_HASH" ] && [ -f "dist/index.html" ]; then
  # 兼容首次接入指纹：检查是否有比 dist/index.html 更新的前端源文件
  NEWER_SRC=$(find index.html vite.config.ts server_url.txt src public -type f -newer dist/index.html 2>/dev/null | head -n 1 || true)
  if [ -n "$NEWER_SRC" ]; then
    echo -e "${AMBER}  -> 检测到前端源码比现有 dist/ 更新，检查是否有新的 dist.tar.gz 发布包...${NC}"
    NEED_DIST_CHECK=true
  fi
fi

if [ "$NEED_DIST_CHECK" = true ]; then
  REMOTE_URL=$(git config --get remote.origin.url 2>/dev/null || true)
  REPO_SLUG=""
  if [[ "$REMOTE_URL" =~ github\.com[:/]([^/]+/[^/.]+)(\.git)?$ ]]; then
    REPO_SLUG="${BASH_REMATCH[1]}"
  fi
  if [ -n "$REPO_SLUG" ]; then
    DL_URL="https://github.com/${REPO_SLUG}/releases/download/dist-latest/dist.tar.gz"
    REMOTE_ETAG=$(curl -fsSLI --connect-timeout 10 "$DL_URL" 2>/dev/null | awk 'tolower($1)=="etag:"{print $2}' | tr -d '\r' | tail -n 1 || true)
    SAVED_ETAG=""
    [ -f "dist/.release-etag" ] && SAVED_ETAG=$(cat "dist/.release-etag" 2>/dev/null || true)

    if [ "$UPDATE_DIST" = false ] && [ -f "dist/index.html" ] && [ -n "$REMOTE_ETAG" ] && [ "$REMOTE_ETAG" = "$SAVED_ETAG" ]; then
      echo -e "${AMBER}  -> 远端 dist-latest 发布包版本与本地一致 (ETag 未变更)，跳过重复下载。${NC}"
      echo -e "${AMBER}     (若刚推送源码，请等待 GitHub Actions 构建完成，或手动从电脑同步 dist/)${NC}"
    else
      echo -e "${AMBER}  -> 正在下载最新前端静态包 dist.tar.gz...${NC}"
      if curl -fsSL --connect-timeout 15 "$DL_URL" -o dist.tar.gz; then
        rm -rf dist
        tar -xzf dist.tar.gz
        rm -f dist.tar.gz
        mkdir -p dist
        printf '%s' "$CURRENT_FRONTEND_HASH" > dist/.frontend-hash
        [ -n "$REMOTE_ETAG" ] && printf '%s' "$REMOTE_ETAG" > dist/.release-etag
        echo -e "${GREEN}  -> 前端静态包下载并解压成功！${NC}"
      else
        rm -f dist.tar.gz
        echo -e "${RED}  -> 下载 dist.tar.gz 失败。请在电脑执行 npm run build 后将 dist/ 上传至 ${SCRIPT_DIR}/dist/${NC}"
      fi
    fi
  else
    if [ ! -f "dist/index.html" ]; then
      echo -e "${RED}  -> 未识别到 GitHub 仓库地址，请将预构建的 dist/ 目录放入 ${SCRIPT_DIR}/dist/${NC}"
    else
      echo -e "${GREEN}  -> 保留本地已有 dist/index.html${NC}"
    fi
  fi
else
  mkdir -p dist
  [ -z "$SAVED_FRONTEND_HASH" ] && printf '%s' "$CURRENT_FRONTEND_HASH" > dist/.frontend-hash
  echo -e "${GREEN}  -> 前端静态包 dist/index.html 已是最新，跳过下载${NC}"
fi

# 5. 初始化 Termux 根目录 ~/.env 与 data 目录
echo -e "\n${CYAN}[5/6] 检查环境变量 (优先 Termux 根目录 ~/.env) 与数据目录...${NC}"
TERMUX_ENV="$HOME/.env"
LOCAL_ENV="$SCRIPT_DIR/.env"

if [ -f "$TERMUX_ENV" ]; then
  echo -e "${GREEN}  -> 已检测到 Termux 根目录配置: ~/.env${NC}"
elif [ -f "$LOCAL_ENV" ]; then
  echo -e "${GREEN}  -> 已检测到项目目录配置: $LOCAL_ENV${NC}"
  echo -e "${AMBER}  -> 提示: 建议执行 mv \"$LOCAL_ENV\" \"$TERMUX_ENV\" 移至 Termux 根目录，删库重拉也不会丢失配置${NC}"
else
  if [ -f ".env.example" ]; then
    cp .env.example "$TERMUX_ENV"
  else
    cat << 'EOF' > "$TERMUX_ENV"
URL=""
APP_URL=""
CLOUDFLARED_TUNNEL_TOKEN=""
CLOUDFLARED_PROTOCOL="http2"
CLOUDFLARED_EDGE_IP="4"
CLOUDFLARED_HA_CONNECTIONS="2"
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
  echo -e "${AMBER}  -> 已在 Termux 根目录生成配置文件: ~/.env${NC}"
  echo -e "${AMBER}  -> 请执行 nano ~/.env 填写 CLOUDFLARED_TUNNEL_TOKEN、URL 与机器人密钥${NC}"
fi

mkdir -p data
if [ ! -f "data/game_database.json" ]; then
  echo '{"profiles":{},"records":[],"authorizedPhones":[],"adminPhones":[],"revokedPhones":[],"adminLogs":[],"systemAnnouncements":[]}' > data/game_database.json
fi

# 6. 使用 tmux 启动/平滑重启服务 (尽量不打断已有隧道连接)
echo -e "\n${CYAN}[6/6] 配置并启动 tmux 常驻守护 (chess-native)...${NC}"

cat << 'EOF' > "$SCRIPT_DIR/.run_termux_server.sh"
#!/data/data/com.termux/files/usr/bin/bash
termux-wake-lock 2>/dev/null || true
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
while true; do
  echo "[$(date '+%F %T')] 🚀 启动国粹棋牌 Node.js 服务端 (端口 3000)..."
  npx tsx server.ts
  echo "[$(date '+%F %T')] ⚠️ 服务端退出，2 秒后自动拉起..."
  sleep 2
done
EOF
chmod +x "$SCRIPT_DIR/.run_termux_server.sh"

OLD_TUNNEL_HASH=""
[ -f "$SCRIPT_DIR/.run_termux_tunnel.sh" ] && OLD_TUNNEL_HASH=$(calc_text_hash < "$SCRIPT_DIR/.run_termux_tunnel.sh")

cat << 'EOF' > "$SCRIPT_DIR/.run_termux_tunnel.sh"
#!/data/data/com.termux/files/usr/bin/bash
termux-wake-lock 2>/dev/null || true
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 按与 server.ts 一致的优先级加载环境变量 (Termux 根目录 ~/.env 优先)
for env_file in "$SCRIPT_DIR/.env" "$SCRIPT_DIR/../.env" "$HOME/.env"; do
  if [ -f "$env_file" ]; then
    set -a
    . "$env_file"
    set +a
  fi
done

TOKEN="${CLOUDFLARED_TUNNEL_TOKEN:-$TUNNEL_TOKEN}"
CF_PROTO="${CLOUDFLARED_PROTOCOL:-http2}"
CF_EDGE_IP="${CLOUDFLARED_EDGE_IP:-4}"
CF_HA="${CLOUDFLARED_HA_CONNECTIONS:-2}"

if [ -n "$TOKEN" ]; then
  while true; do
    echo "[$(date '+%F %T')] 🌐 读取 ~/.env Token 启动 Cloudflare Tunnel (协议: ${CF_PROTO}, IPv${CF_EDGE_IP}, 连接数: ${CF_HA})..."
    cloudflared tunnel --edge-ip-version "$CF_EDGE_IP" --protocol "$CF_PROTO" --ha-connections "$CF_HA" run --token "$TOKEN"
    echo "[$(date '+%F %T')] ⚠️ 隧道断开，3 秒后自动重连..."
    sleep 3
  done
elif [ -f "$HOME/.cloudflared/config.yml" ]; then
  while true; do
    echo "[$(date '+%F %T')] 🌐 启动配置文件模式 Cloudflare Tunnel (协议: ${CF_PROTO}, IPv${CF_EDGE_IP}, 连接数: ${CF_HA})..."
    cloudflared tunnel --edge-ip-version "$CF_EDGE_IP" --protocol "$CF_PROTO" --ha-connections "$CF_HA" run
    echo "[$(date '+%F %T')] ⚠️ 隧道断开，3 秒后自动重连..."
    sleep 3
  done
else
  echo "[提示] 未在 ~/.env 中检测到 CLOUDFLARED_TUNNEL_TOKEN，且不存在 ~/.cloudflared/config.yml。"
  echo "请编辑 ~/.env 填入 CLOUDFLARED_TUNNEL_TOKEN=\"你的Token\" 后执行："
  echo "  ./deploy_termux_native.sh --restart-all"
  echo "或直接在此窗口运行临时测试域名："
  echo "  cloudflared tunnel --edge-ip-version 4 --protocol http2 --url http://127.0.0.1:3000"
  exec bash
fi
EOF
chmod +x "$SCRIPT_DIR/.run_termux_tunnel.sh"
NEW_TUNNEL_HASH=$(calc_text_hash < "$SCRIPT_DIR/.run_termux_tunnel.sh")

if [ "$RESTART_ALL" = false ] && tmux has-session -t chess-native 2>/dev/null && [ "$OLD_TUNNEL_HASH" = "$NEW_TUNNEL_HASH" ]; then
  echo -e "${GREEN}  -> 检测到隧道会话已在运行，仅热重启 Node.js 后端服务（保持隧道不断线）...${NC}"
  if command -v lsof >/dev/null 2>&1; then
    OLD_PID=$(lsof -t -i:3000 2>/dev/null || true)
    if [ -n "$OLD_PID" ]; then
      kill $OLD_PID 2>/dev/null || true
      sleep 1
      STILL_PID=$(lsof -t -i:3000 2>/dev/null || true)
      [ -n "$STILL_PID" ] && kill -9 $STILL_PID 2>/dev/null || true
    fi
  else
    tmux respawn-window -k -t chess-native:server "bash \"$SCRIPT_DIR/.run_termux_server.sh\"" 2>/dev/null || true
  fi
else
  if command -v lsof >/dev/null 2>&1; then
    OLD_PID=$(lsof -t -i:3000 2>/dev/null || true)
    [ -n "$OLD_PID" ] && kill -9 $OLD_PID 2>/dev/null || true
  fi
  tmux kill-session -t chess-native 2>/dev/null || true
  tmux new-session -d -s chess-native -n server "bash \"$SCRIPT_DIR/.run_termux_server.sh\""
  tmux new-window -t chess-native -n tunnel "bash \"$SCRIPT_DIR/.run_termux_tunnel.sh\""
fi

LAN_IP=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{print $7}' | head -n 1 || true)

echo -e "\n${GREEN}==================================================================${NC}"
echo -e "${GREEN}  🎉 国粹棋牌已在 Termux 原生部署/增量更新完成！${NC}"
echo -e "${GREEN}  📱 手机本机访问:   http://127.0.0.1:3000${NC}"
echo -e "${GREEN}  🏠 局域网直连对战: http://${LAN_IP:-192.168.1.x}:3000${NC}"
echo -e "${AMBER}  🛠️ 日常守护与更新指令:${NC}"
echo -e "     - 代码更新增量刷新: git pull && ./deploy_termux_native.sh"
echo -e "     - 强制重拉前端包:   ./deploy_termux_native.sh --update-dist"
echo -e "     - 进入实时控制台:   tmux attach -t chess-native"
echo -e "     - 切换窗口(服务/隧道): Ctrl+b 后按 n"
echo -e "     - 退出面板保持后台: Ctrl+b 后按 d"
echo -e "     - 查看隧道连线机房: tmux capture-pane -pt chess-native:tunnel -S -50 | grep location"
echo -e "${GREEN}==================================================================${NC}"
