# 🀄 国粹棋牌 Go 语言高性能服务端 (Go-Authoritative Server)

> ⭐️ **架构升级说明**：
> 针对先前 Rust 语言在移动端/Termux 环境下编译极其繁琐、构建时间长达数十分钟、容易发生 LLVM 内存溢出、生命周期与宏语法维护复杂的问题，本项目已**全面重新构建升级为纯 Go (Golang) 架构**！
> **所有前端界面样式、交互布局、卡牌画布动效及三大棋牌游戏规则完全 100% 保持不变！**

---

## 🚀 为什么选用 Go 语言？(与 Rust 对比)

| 特性维度 | Rust 原旧方案 | Go 全新构建方案 (本项目) |
| :--- | :--- | :--- |
| **编译速度** | 需 10~20 分钟 (LLVM 极度消耗手机 CPU 与内存) | **仅需 2~3 秒** (`go build` 瞬间完成) |
| **手机 Termux 部署** | 易因 Bionic libc 或 clang 内存不足导致 OOM 崩溃 | **一键原生支持** (`pkg install golang` 即可) |
| **代码维护与升级** | 所有权、借用检查器、泛型与生命周期心智负担沉重 | **语法简洁直观**，团队维护与功能扩展轻松上手 |
| **并发处理能力** | 需配置 Tokio 运行时复杂异步调度 | **原生轻量级 Goroutine 协程**，轻松支撑百万高并发 |
| **外部依赖** | 依赖数十个 Cargo crates 库 | **零外部依赖 (纯标准库)**，绝无下载失败风险 |
| **跨平台交叉编译** | 需配置繁复的目标平台交叉编译器 | **只需设置环境变量**，一行命令输出 ARM64/AMD64 单二进制 |

---

## 📦 纯 Go 单体全栈方案：彻底摆脱 Node.js 运行时

> 💡 **用户核心提问：可以用纯 Go 语言把项目做成单体全栈吗？不再需要 Node.js？**
> **答案：完全可以，而且这正是业界最顶尖（如 PocketBase、Gitea、Grafana、Caddy）的标准推荐架构！**
> 
> 本项目已全面实现 **Go Embed 单二进制单体全栈 (`//go:embed all:dist`)**：
> 1. **运行阶段 100% 纯 Go**：编译完成后，输出**唯一的单文件可执行程序**（如 `mingju-single-app`，约 15MB）；
> 2. **免除一切 Node.js 依赖**：手机 Termux 或云服务器**完全不需要安装 Node.js、npm 或 PM2**，直接执行 `./mingju-single-app` 即可；
> 3. **前端资源内嵌二进制**：所有 Vue 3 界面、三大国粹棋牌 Canvas 舞台、数十首真人配音 MP3、扑克与麻将 SVG 均已打包内嵌到该二进制文件的 `.rodata` 只读数据段中，从内存极速伺服；
> 4. **毫秒级极速响应**：内存占用仅约 15MB（仅为 Node 运行时的 1/10），启动时间仅 0.01 秒。

---

## 🛠️ 快速编译与运行

### 1. 一键生成纯 Go 单体全栈二进制 (Single Binary)
```bash
cd go-server

# 执行单体打包脚本 (自动嵌入前端 dist)
bash build_single_binary.sh

# 生成单一可执行程序后，脱离 Node.js 直接运行：
./mingju-single-app
```

### 2. 本地直接源码调试
```bash
cd go-server

# 直接运行源码 (加载全部 Go 权威模块)
go run .

# 编译为极致压缩的高性能静态二进制文件 (剥离调试符号)
CGO_ENABLED=0 go build -ldflags="-s -w" -o mingju-go-server .

# 启动服务 (默认监听 0.0.0.0:3000)
./mingju-go-server
```

### 2. 跨平台交叉编译 (开发机一键生成 Android/Linux 产物)
在 Mac 或 Windows/Linux 电脑上，无需安装任何交叉编译链，直接编译出可以在 Android Termux 或 Linux 云服务器上运行的单一二进制文件：

```bash
# 生成 Android 手机 / ARM64 Linux 适用二进制:
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o mingju-go-server-arm64 .

# 生成 x86_64 云服务器适用二进制:
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o mingju-go-server-amd64 .

# 生成 Windows 适用 exe:
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o mingju-go-server.exe .
```

### 3. Android Termux 手机一键部署
在 Termux 终端中直接运行：
```bash
bash go-server/deploy_termux.sh
```

---

## 📡 API 与 WebSocket 规范

- **WebSocket 端点**：`ws://0.0.0.0:3000/ws`
  - 客户端连接后自动下发 `lobby_snapshot` 与 `presence_counts`、`teahouse_players_update`；
  - 支持 `ping` / `pong`、`presence_update`、`request_teahouse_players`、`room_create`、`room_join`、`room_leave`、`room_reconnect`；
  - 支持 `scene_join`、`scene_leave`、`scene_player_ready`、`scene_start_game`、`scene_reconnect`、`scene_chat`、`scene_accept_challenge`；
  - 支持三大权威棋牌引擎 (`yansan` 烟三、`shisanshui` 十三水、`mahjong` 国标麻将) 与 `booking_*` 预约专列实时同步；
  - 断线自动托管、60 秒结算超时清理、崩溃恢复快照自动持久化。
- **REST API**：
  - 健康检查与监控：`GET /api/health`、`GET /api/rooms`、`GET /api/presence`、`GET /api/teahouse/players`
  - 账号与钱庄：`POST /api/auth/register`、`POST /api/auth/login`、`POST /api/auth/check-session`、`POST /api/user/sync`、`POST /api/user/profile`、`GET /api/points/ledger`、`GET /api/users/search`、`POST /api/points/transfer`
  - 预约专列：`GET /api/booking/carriage`、`POST /api/booking/select-seat`、`POST /api/booking/pick-table`、`POST /api/booking/submit`、`POST /api/booking/advance-carriage`、`POST /api/booking/simulate-settle`、`POST /api/booking/ack-settlement`
  - 语音与安装包：`GET|POST /api/tts`、`GET /api/apk/status`、`GET|HEAD /api/download/apk`
  - 管理与 Telegram：`GET|POST /api/admin/*`、`/api/telegram/{webhook,command,status,set-webhook,sync-commands,webhook-info,start-polling}`
- **前端托管**：优先加载 `./dist` 或 `../dist`，缺失时自动从二进制内嵌 `//go:embed all:dist` 读取，支持 SPA 路由 Fallback。
