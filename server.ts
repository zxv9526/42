import dotenv from 'dotenv';
import express from 'express';
import { createServer } from 'http';
import { WebSocketServer } from 'ws';
import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';
import { handleApiRoute } from './src/serverApi.ts';
import { applyCorsHeaders } from './src/server/apiHelpers.ts';
import { initTelegramBotService } from './src/telegramBotService.ts';
import { createRealtimeHub, type AuthoritativeBoardState } from './src/serverRealtimeHub.ts';

// Auto-detect and load .env from project directory, parent directory (~/.env), or HOME
const possibleEnvPaths = [
  path.resolve(process.cwd(), '.env'),
  path.resolve(process.cwd(), '..', '.env'),
  process.env.HOME ? path.resolve(process.env.HOME, '.env') : '',
].filter(Boolean);

for (const p of possibleEnvPaths) {
  if (fs.existsSync(p)) {
    try {
      const content = fs.readFileSync(p, 'utf-8');
      const parsed = dotenv.parse(content);
      for (const [k, v] of Object.entries(parsed)) {
        if (typeof v === 'string' && v.trim() !== '') {
          process.env[k] = v.trim();
        }
      }
    } catch {}
  }
}

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Purge only stale/corrupted python-repackaged 4.09 MB APK files (< 4.2MB), preserving genuine >= 4.2 MB official release APKs
try {
  const candidateFiles = [
    path.resolve(process.cwd(), 'apk', 'chess-game-release.apk'),
    path.resolve(process.cwd(), 'apk', 'chess-game-debug.apk'),
    path.resolve(process.cwd(), 'apk', 'app-latest.apk'),
    path.resolve(process.cwd(), 'public', 'chess-game-release.apk'),
  ];
  for (const cf of candidateFiles) {
    if (fs.existsSync(cf)) {
      try {
        const stats = fs.statSync(cf);
        // Stale python-repackaged file is ~4,093,258 bytes. Authentic Gradle release APK is > 4.4 MB (4,600,000+ bytes)
        if (stats.size < 4200000) {
          fs.unlinkSync(cf);
          console.log(`🧹 [Server] 已自动清理旧版未签名的损坏安装包: ${path.basename(cf)} (${(stats.size / 1048576).toFixed(2)} MB)`);
        }
      } catch {}
    }
  }

  // Purge obsolete 4.09MB build info json
  const infoCandidates = [
    path.resolve(process.cwd(), 'apk', 'apk-build-info.json'),
    path.resolve(process.cwd(), 'public', 'apk-build-info.json'),
  ];
  for (const ic of infoCandidates) {
    if (fs.existsSync(ic)) {
      try {
        const raw = fs.readFileSync(ic, 'utf-8');
        if (raw.includes('4093258') || raw.includes('3.90 MB') || raw.includes('4.09')) {
          fs.unlinkSync(ic);
        }
      } catch {}
    }
  }
} catch {}

export type GameType = 'yansan' | 'shisanshui' | 'mahjong';
export type { AuthoritativeBoardState };

const app = express();

// Universal CORS Middleware for Android Capacitor Native App (https://localhost / capacitor://localhost) & Web Clients
app.use((req, res, next) => {
  applyCorsHeaders(req, res);

  if (req.method === 'OPTIONS') {
    return res.status(204).end();
  }
  next();
});

app.use(express.json({ limit: '10mb' }));
app.use(express.urlencoded({ extended: true, limit: '2mb' }));

const server = createServer(app);
server.keepAliveTimeout = 65000;
server.headersTimeout = 66000;
server.on('connection', (socket) => {
  socket.setNoDelay(true);
  socket.setKeepAlive(true, 30000);
});

const wss = new WebSocketServer({ server, path: '/ws', perMessageDeflate: false });

wss.on('error', (err) => {
  console.warn('[WS Server Warning]', err);
});

const broadcast = createRealtimeHub(wss);

// Unified API Route Dispatcher
app.use(async (req, res, next) => {
  if (req.url && req.url.startsWith('/api/')) {
    try {
      const handled = await handleApiRoute(req, res, broadcast);
      if (handled) return;
    } catch (err) {
      console.error('[API Error]', err);
      return res.status(500).json({ error: String(err) });
    }
  }
  next();
});

// Explicit APK Direct Download Route (Ensures correct MIME, attachment filename & CORS headers)
app.get(['/chess-game-release.apk', '/app-latest.apk', '/*.apk'], (req, res, next) => {
  const reqPath = req.path;
  const fileName = path.basename(reqPath) || 'chess-game-release.apk';
  const candidates = [
    path.join(__dirname, 'dist', fileName),
    path.join(__dirname, 'dist', 'chess-game-release.apk'),
    path.join(__dirname, 'apk', fileName),
    path.join(__dirname, 'apk', 'chess-game-release.apk'),
    path.join(__dirname, 'public', fileName),
  ];
  for (const c of candidates) {
    if (fs.existsSync(c)) {
      try {
        const st = fs.statSync(c);
        if (st.size >= 4200000) {
          res.writeHead(200, {
            'Content-Type': 'application/vnd.android.package-archive',
            'Content-Length': st.size,
            'Content-Disposition': `attachment; filename="${fileName}"`,
            'Access-Control-Allow-Origin': '*',
            'Access-Control-Expose-Headers': 'Content-Length, Content-Disposition',
            'Cache-Control': 'no-cache, no-store, must-revalidate',
            'Pragma': 'no-cache',
            'Expires': '0',
          });
          return fs.createReadStream(c).pipe(res);
        }
      } catch {}
    }
  }
  next();
});

// Static file serving or automated Vite dev middleware fallback
const distHtml = path.join(__dirname, 'dist', 'index.html');

if (fs.existsSync(distHtml)) {
  // Production mode: immutable caching ONLY for hashed Vite assets; always revalidate HTML & manifests
  app.use(
    express.static(path.join(__dirname, 'dist'), {
      setHeaders: (res, filePath) => {
        const normalized = filePath.replace(/\\/g, '/');
        if (normalized.includes('/assets/')) {
          res.setHeader('Cache-Control', 'public, max-age=31536000, immutable');
        } else {
          res.setHeader('Cache-Control', 'no-cache, no-store, must-revalidate');
          res.setHeader('Pragma', 'no-cache');
          res.setHeader('Expires', '0');
        }
      },
    })
  );
  app.get('*', (_req, res) => {
    res.setHeader('Cache-Control', 'no-cache, no-store, must-revalidate');
    res.setHeader('Pragma', 'no-cache');
    res.setHeader('Expires', '0');
    res.sendFile(distHtml);
  });
} else {
  // Guidance fallback when dist has not been cloned or built
  console.warn('⚠️ [Server] 未在项目根目录检测到 dist/index.html 生产静态资源。');
  console.warn('💡 [提示] 请确保已将包含 dist 文件夹的仓库完整推送并在 Termux 中 pull。');

  app.get('*', (_req, res) => {
    res.status(200).send(`
      <!DOCTYPE html>
      <html lang="zh-CN">
      <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>国粹棋牌 · 提示</title>
        <style>
          body { background: #12100E; color: #F5EFE6; font-family: -apple-system, BlinkMacSystemFont, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 20px; text-align: center; }
          .box { background: #1C1815; border: 1px solid #D97706; border-radius: 20px; padding: 28px; max-width: 520px; box-shadow: 0 10px 40px rgba(0,0,0,0.8); }
          h2 { color: #FBBF24; margin-top: 0; }
          p { font-size: 14px; line-height: 1.6; color: #D6C7B2; }
          code { background: #26201B; padding: 4px 10px; border-radius: 6px; color: #34D399; font-family: monospace; font-size: 14px; font-weight: bold; }
        </style>
      </head>
      <body>
        <div class="box">
          <h2>⚠️ 尚未检测到前端 dist 静态包</h2>
          <p>若在手机 Termux 环境运行，推荐使用 <code>bash termux_install_ubuntu.sh</code> 在 Ubuntu 容器中执行 <code>npm run build</code>；或从 GitHub Releases 下载最新的 <code>dist.tar.gz</code> 解压即可畅玩！</p>
        </div>
      </body>
      </html>
    `);
  });
}

const PORT = process.env.PORT ? parseInt(process.env.PORT, 10) : 3000;

server.on('error', (err: any) => {
  if (err.code === 'EADDRINUSE') {
    console.error(`\n❌【端口冲突】端口 ${PORT} 已被后台旧服务占用！`);
    console.error('👉 解决办法：在 Termux 终端执行以下命令释放端口后再启动：');
    console.error('   pkill -9 -f "node|tsx|server"\n');
    process.exit(1);
  } else {
    console.error('[Server Error]', err);
  }
});

server.listen(PORT, '0.0.0.0', () => {
  console.log(`Server running on port ${PORT}`);
  // 自动调度 Telegram Bot (根据 .env 的 URL 配置智能选择 Webhook 或轮询启动)
  initTelegramBotService(broadcast).catch((err) => {
    console.error('⚠️ [Telegram Bot Init Error]', err);
  });
});
