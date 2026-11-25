# 📱 国粹棋牌 · Android Termux 原生直接部署完整教程

> 本教程面向 **全新直接部署在 Android 手机 Termux** 中使用：**无需安装任何 Ubuntu/Linux 容器**，拉取同一套仓库代码即可直接运行。  
> 环境变量统一存放在 **Termux 根目录 (`~/.env`)**，不仅服务端与 Cloudflare Tunnel 会自动读取，而且后续 `git pull` 更新或重拉仓库代码时配置完全不会丢失。

---

## 一、 手机与 Termux 前期准备

1. **安装最新版 Termux**：请从 **F-Droid** 或 **GitHub Releases** 下载安装最新版 Termux（切勿使用 Google Play 停止维护的旧版）。
2. **关闭系统省电限制**（避免锁屏断网与延迟飙升）：
   - 手机系统设置 → 应用管理 → Termux → 电池策略设为 **“无限制 / 允许完全后台运行”**；
   - 在手机多任务卡片界面将 Termux **锁定**；
   - 建议连接 **5GHz Wi-Fi 并保持插电** 运行（相比移动流量可大幅降低无线基站休眠唤醒带来的延迟抖动）。

---

## 二、 第一步：拉取仓库代码

打开 Termux（此时默认就在 Termux 根目录 `~`，即 `/data/data/com.termux/files/home`），依次执行：

```bash
termux-wake-lock
pkg update -y && pkg upgrade -y
pkg install -y git
git clone https://github.com/你的用户名/你的仓库名.git ~/chess-lobby
```

---

## 三、 第二步：在 Termux 根目录配置 `.env` 文件

将环境变量文件固定放在 **Termux 根目录 (`~/.env`)**，服务端（`server.ts`）与隧道守护脚本都会自动优先加载它。

### 方式 1：从仓库模板一键复制并编辑（推荐）
```bash
cp ~/chess-lobby/.env.example ~/.env
nano ~/.env
```

### 方式 2：直接新建完整 `.env` 模板
如果你习惯直接创建，可在 Termux 根目录执行以下整段命令生成 `~/.env`（先改好引号里的内容再粘贴）：

```bash
cat << 'EOF' > ~/.env
# ==============================================================================
# 🀄 国粹棋牌服务 - Termux 根目录环境变量配置 (~/.env)
# ==============================================================================

# 1. 公网访问 HTTPS 域名 (填写你在 Cloudflare Tunnel 绑定的域名，末尾不要加 /)
URL="https://你的域名.com"
APP_URL="https://你的域名.com"

# 2. Cloudflare Tunnel 隧道 Token (只粘贴 eyJ... 开头的纯 Token 字符串)
CLOUDFLARED_TUNNEL_TOKEN="你的Cloudflare_Tunnel_Token"

# 隧道协议调优 (针对手机/运营商禁用 QUIC UDP 场景，强制 IPv4 + HTTP/2)
CLOUDFLARED_PROTOCOL="http2"
CLOUDFLARED_EDGE_IP="4"
CLOUDFLARED_HA_CONNECTIONS="2"

# 3. Telegram Bot 机器人与管理员设置
TELEGRAM_BOT_TOKEN="你的Telegram_Bot_Token"
ADMIN_USERNAMES="你的TG用户名"
TELEGRAM_API_BASE=""

# 运行模式: webhook (有公网域名推荐) / polling (无公网域名备用)
BOT_MODE="webhook"
WEBHOOK_URL=""
TELEGRAM_WEBHOOK_SECRET=""

# 4. 后台管理 API 密钥 (用于管理接口与导出鉴权，建议设置强密码)
ADMIN_API_SECRET="请设置一个高强度随机密钥"

# 5. 服务监听端口与环境
PORT=3000
HOST="0.0.0.0"
NODE_ENV="production"
EOF
```

### 关键配置项获取说明
| 变量名 | 说明与获取方法 |
| :--- | :--- |
| `CLOUDFLARED_TUNNEL_TOKEN` | 登录 [Cloudflare Zero Trust](https://one.dash.cloudflare.com/) → **Networks** → **Tunnels** → 创建或打开隧道，在安装命令中复制 `--token` 后面的 `eyJhIjoi...` 长字符串（不要带前面命令）。并在隧道 **Public Hostname** 中将你的域名指向 `http://127.0.0.1:3000`。 |
| `URL` / `APP_URL` | 上一步绑定的公网 HTTPS 域名（如 `https://m.my66.cc.cd`），用于 Telegram Webhook 回调与客户端连接。 |
| `TELEGRAM_BOT_TOKEN` | 在 Telegram 中向 `@BotFather` 发送 `/newbot` 获取的机器人 Token。 |
| `ADMIN_USERNAMES` | 你的 Telegram 用户名（不带 `@`），多个管理员用英文逗号分隔。 |
| `ADMIN_API_SECRET` | 自定义管理密钥，防止管理与数据导出接口被未授权调用。 |

> 使用 `nano ~/.env` 编辑完成后：按 `Ctrl + O` 回车保存，再按 `Ctrl + X` 退出。

---

## 四、 第三步：一键原生部署并同时拉起服务与隧道

进入仓库目录并运行原生部署脚本：

```bash
cd ~/chess-lobby
chmod +x deploy_termux_native.sh
./deploy_termux_native.sh
```

脚本会自动完成以下全部工作（**内置智能检测，已安装内容绝不重复安装或浪费缓存**）：
1. **按需检查 Termux 软件包**：逐项检测 `nodejs-lts`、`cloudflared`、`tmux`、`jq`、`curl`、`lsof`、`tar`，已安装直接跳过 `pkg update/install`，安装缺失项后自动执行 `pkg clean` 清理缓存。
2. **指纹比对安装后端依赖**：对比 `package.json` / `package-lock.json` 指纹，仅安装服务端纯运行依赖（`--omit=dev`），跳过手机端不兼容的前端编译器；依赖无变化时直接跳过，安装后自动清理 `npm` 缓存。
3. **自动同步前端静态资源**：检查 `dist/`，首次缺少或前端源码更新时自动从 GitHub Releases (`dist-latest`) 拉取最新 `dist.tar.gz`（也可手动上传电脑构建好的 `dist/`）。
4. **自动读取 `~/.env` 启动双窗口守护 (`chess-native`)**：
   - `server` 窗口：常驻运行 `npx tsx server.ts`（意外退出 2 秒自动拉起）；
   - `tunnel` 窗口：自动读取 `~/.env` 中的 `CLOUDFLARED_TUNNEL_TOKEN`，以低延迟优化参数 `--edge-ip-version 4 --protocol http2 --ha-connections 2` 自动连接 Cloudflare 隧道（断线 3 秒自动重连）。

---

## 五、 隧道排查与延迟优化

### 1. 查看隧道实际连线机房
执行以下命令查看 `location=` 后的机房代号：
```bash
tmux capture-pane -pt chess-native:tunnel -S -80 | grep -i "location="
```
- 若返回 `lax`、`sjc` 等美西节点，约 700～900ms 的延迟主要来自跨境绕行美西机房。
- 本脚本默认启用 `--edge-ip-version 4 --protocol http2 --ha-connections 2`，专门解决手机端 QUIC/UDP 被阻断导致的握手失败与队头卡顿问题。
- 若牌友与主机在同一路由器 Wi-Fi 下，可在客户端右上角【服务器配置】切换为页面提示的局域网 IP（如 `http://192.168.x.x:3000`），延迟通常仅 **5～30ms**。

### 2. 修改 `~/.env` 后如何生效
- 如果修改了机器人 Token、密钥或 `URL`：直接执行 `./deploy_termux_native.sh` 热重启后端即可；
- 如果修改了 `CLOUDFLARED_TUNNEL_TOKEN` 或隧道参数：执行以下命令让隧道窗口同步重启生效：
  ```bash
  cd ~/chess-lobby && ./deploy_termux_native.sh --restart-all
  ```

### 3. 激活 Telegram Bot Webhook（解决国内网络下机器人无反应）
由于国内手机无法直连 `api.telegram.org`，后端开机自动注册 Webhook 时可能会超时，**必须手动一次性激活**：
1. 确保 `~/.env` 已填好 `URL="https://你的域名.com"` 和 `TELEGRAM_BOT_TOKEN` 并已启动服务；
2. 用**能正常打开 Telegram 的浏览器**访问：`https://你的域名.com/api/telegram/set-webhook`；
3. 点击页面蓝色按钮 **「👉 在当前浏览器中直连官方激活 Webhook」**，浏览器弹出 `{"ok":true,"result":true,"description":"Webhook was set"}` 即激活成功！
4. 激活后 Telegram 服务器会通过你的 Cloudflare 域名主动把指令推送给 Termux，**手机无需挂代理即可秒级回复管理员菜单**（若仍无反应，可访问 `https://你的域名.com/api/telegram/webhook-info` 查看错误诊断）。

---

## 六、 日常运维与代码增量更新

### 1. 查看运行日志与控制台
```bash
# 打开 tmux 实时控制台
tmux attach -t chess-native

# 在控制台内切换 server / tunnel 窗口：先按 Ctrl+b，松开后再按 n
# 退出控制台但保持后台继续运行（切勿直接 Ctrl+C）：先按 Ctrl+b，松开后再按 d
```

### 2. 仓库代码更新后一键刷新（不重装软件、不断开隧道）
```bash
cd ~/chess-lobby
git pull
./deploy_termux_native.sh
```
> 增量更新说明：
> - 存放在 `~/.env` 的密钥与隧道 Token 完全不受 `git pull` 影响；
> - 已安装的系统软件和未变更的 `node_modules` 会自动跳过；
> - 若更新只改了后端，仅热重启 `server` 进程，现有 `tunnel` 隧道保持不断线；
> - 如需强制重新拉取最新前端压缩包：`./deploy_termux_native.sh --update-dist`；
> - 如需强制重装依赖：`./deploy_termux_native.sh --force-deps`。

### 3. 停止全部后台服务
```bash
tmux kill-session -t chess-native
```
