# 🐧 国粹棋牌 · 家里旧电脑 Linux / 云服务器部署教程

> 本教程面向 **标准 Linux 系统**（包括**家里旧电脑重装 Ubuntu/Debian**，或**购买的 Linux 云服务器**），与 Termux 手机端使用**同一份仓库代码**、完全一致的 `data/` 数据与 `.env` 配置。

---

## 一、 一键拉取与部署（两大场景通用）

在你的 Linux 主机（Ubuntu 22.04 / 24.04 或 Debian 12+）执行：

```bash
sudo apt-get update -y && sudo apt-get install -y git
git clone https://github.com/你的用户名/你的仓库名.git ~/chess-lobby
cd ~/chess-lobby
chmod +x deploy_linux.sh
./deploy_linux.sh
```

脚本会自动完成（**智能检测，已安装内容绝不重复安装**）：
1. 按需检测并安装系统工具、`Node.js 20 LTS` 与 `PM2`，已存在直接跳过且清理安装缓存；
2. 对比 `package.json` / `package-lock.json` 指纹，依赖未变化则跳过 `npm install`；
3. 对比前端源码指纹，仅在 `dist/` 缺失或源码更新时才执行 `npm run build`；
4. 初始化 `.env` 与 `data/` 目录，并用 PM2 启动或平滑重启 `chess-server`（监听 `0.0.0.0:3000`）。

设置**断电重启自动拉起服务**：
```bash
pm2 startup
# 将终端输出的那一行 sudo env PATH=... 命令复制执行一次
pm2 save
```

编辑环境配置（Telegram Bot Token、域名、管理员密钥）：
```bash
nano ~/chess-lobby/.env
pm2 restart chess-server
```

---

## 二、 场景 A：家里旧电脑改造成 Linux 服务器

适合：利用淘汰台式机/笔记本接入家里路由器网线，24 小时低成本挂机。

1. **安装推荐系统**：Ubuntu Server LTS（无桌面版最省内存）或 Debian，安装时插上网线，并在路由器后台为这台电脑绑定 **静态局域网 IP**（如 `192.168.1.50`）。
2. **公网接入方式选择**（按延迟从低到高排序）：
   - **① 公网直连（延迟最低，20～60ms）**：若家里宽带支持公网 IPv4（或独立公网 IPv6 + 域名 AAAA 解析），在路由器做好端口映射，配合 Caddy/Nginx 自动签发 HTTPS 证书即可直连，**延迟通常 30ms 内**。
   - **② 轻量中转（推荐无公网 IP 时，60～120ms）**：用一台低配香港/境内小机跑 `frp` 或 `rathole`，把家里电脑的 `3000` 端口安全映射到公网，数据全留在家中电脑，同时彻底绕开跨洋延迟。
   - **③ Cloudflare Tunnel（零公网端口门槛）**：直接安装官方 `cloudflared` 指向 `http://127.0.0.1:3000`。插网线的 Linux 电脑不会出现手机 Wi-Fi 休眠抖动，稳定性远高于手机。
3. **局域网本地玩**：同 Wi-Fi 下直接在客户端填 `http://192.168.1.50:3000`，延迟 **1～10ms**。

---

## 三、 场景 B：购买 Linux 云服务器部署（终极低延迟）

适合：希望所有外部牌友都稳定保持 **20～80ms 极速延迟**、免折腾穿透。

1. **服务器选择建议**：
   - **国内轻量应用服务器**（延迟 20～50ms，1核2G/2核2G 足够；需注意域名备案及 Telegram 连通性）；
   - **香港/日本优化线路云服务器**（免备案，国内直连通常 40～90ms，且原生直连 Telegram Bot API）。
2. **防火墙放行端口**：在云厂商控制台安全组放行 `80`、`443`（若使用 Caddy/Nginx 域名反代，**建议不要直接对公网暴露 3000**；若暂不配域名测试，需放行 `3000`）。
3. **域名 HTTPS + WebSocket 反向代理（以极简的 Caddy 为例）**：
   ```bash
   sudo apt-get install -y caddy
   ```
   编辑 `/etc/caddy/Caddyfile`，写入：
   ```caddyfile
   你的域名.com {
       reverse_proxy 127.0.0.1:3000
   }
   ```
   重载生效（自动申请并续期免费 HTTPS 证书，原生支持 `/ws` WebSocket）：
   ```bash
   sudo systemctl reload caddy
   ```
4. **更新域名配置给客户端/APK**：
   修改项目根目录 `server_url.txt` 为 `https://你的域名.com` 并提交，重新运行 `npm run build` 或触发 APK 构建即可。

---

## 四、 若已有老数据需要从手机搬到 Linux（可选）

如果未来想把手机 Termux 上跑出来的老玩家账号与牌局进度保留到新 Linux 主机：
只需将手机项目目录下的 **`data/` 整个文件夹** 和 **`.env`** 复制覆盖到 Linux 主机对应的 `~/chess-lobby/` 目录，然后执行 `pm2 restart chess-server` 即可原样接管全部数据。

---

## 五、 Linux 日常增量更新与维护命令

拉取代码更新后直接重新运行部署脚本即可（会自动跳过已装软件包和未变依赖，只按需重新编译 `dist/` 并重启服务）：
```bash
cd ~/chess-lobby
git pull
./deploy_linux.sh
```
- 强制重新编译前端：`./deploy_linux.sh --force-build`
- 强制重新安装依赖：`./deploy_linux.sh --force-deps`
- 查看日志：`pm2 logs chess-server --lines 50`
- 查看进程与内存：`pm2 status`
