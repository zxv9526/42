import { defineConfig, type Plugin } from 'vite';
import vue from '@vitejs/plugin-vue';
import tailwindcss from '@tailwindcss/vite';
import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';
import dotenv from 'dotenv';
import { WebSocketServer } from 'ws';
import { handleApiRoute } from './src/serverApi.ts';
import { applyCorsHeaders } from './src/server/apiHelpers.ts';
import { initTelegramBotService } from './src/telegramBotService.ts';
import { createRealtimeHub } from './src/serverRealtimeHub.ts';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Auto-load .env from project root, parent directory (Termux root ~/.env), or HOME
const envCandidates = [
  path.resolve(process.cwd(), '.env'),
  path.resolve(process.cwd(), '..', '.env'),
  process.env.HOME ? path.resolve(process.env.HOME, '.env') : '',
].filter(Boolean);

for (const p of envCandidates) {
  if (fs.existsSync(p)) {
    dotenv.config({ path: p, override: true });
  }
}

function chessWebSocketPlugin(): Plugin {
  return {
    name: 'chess-websocket-plugin',
    configureServer(server) {
      if (!server.httpServer) return;

      const wss = new WebSocketServer({ server: server.httpServer, path: '/ws', perMessageDeflate: false });

      wss.on('error', (err) => {
        console.warn('[Vite WS Server Warning]', err);
      });

      const broadcast = createRealtimeHub(wss);

      // REST API middlewares
      server.middlewares.use(async (req, res, next) => {
        applyCorsHeaders(req, res);

        if (req.method === 'OPTIONS') {
          res.statusCode = 204;
          res.end();
          return;
        }

        if (req.url && (req.url.startsWith('/chess-game-release.apk') || req.url.startsWith('/app-latest.apk'))) {
          const fileName = 'chess-game-release.apk';
          const candidates = [
            path.join(__dirname, 'dist', fileName),
            path.join(__dirname, 'apk', fileName),
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
                  fs.createReadStream(c).pipe(res);
                  return;
                }
              } catch {}
            }
          }
        }

        if (req.url && req.url.startsWith('/api/')) {
          try {
            const handled = await handleApiRoute(req, res, broadcast);
            if (handled) return;
          } catch (err) {
            console.error('[Vite API Error]', err);
            res.statusCode = 500;
            res.setHeader('Content-Type', 'application/json');
            res.end(JSON.stringify({ error: String(err) }));
            return;
          }
        }
        next();
      });

      // Auto-schedule Telegram Bot Service in Vite development server
      if (server.httpServer.listening) {
        initTelegramBotService(broadcast).catch((err) => {
          console.error('⚠️ [Vite Telegram Bot Init Error]', err);
        });
      } else {
        server.httpServer.once('listening', () => {
          initTelegramBotService(broadcast).catch((err) => {
            console.error('⚠️ [Vite Telegram Bot Init Error]', err);
          });
        });
      }
    },
  };
}

function getResolvedEnvUrl(): string {
  // 1. 最高优先级：专门用于随时更换隧道和域名的 server_url.txt 配置文件
  const serverFilePath = path.resolve(__dirname, 'server_url.txt');
  if (fs.existsSync(serverFilePath)) {
    try {
      const content = fs.readFileSync(serverFilePath, 'utf-8');
      for (const line of content.split('\n')) {
        const trimmed = line.trim();
        if (trimmed && !trimmed.startsWith('#') && (trimmed.startsWith('http://') || trimmed.startsWith('https://'))) {
          return trimmed.replace(/\/+$/, '');
        }
      }
    } catch {}
  }

  // 2. 备用读取当前正在运行的环境变量 SERVER_URL / URL / APP_URL / WEBHOOK_URL
  const envUrl = (process.env.SERVER_URL || process.env.URL || process.env.APP_URL || process.env.WEBHOOK_URL || '').trim().replace(/\/+$/, '');
  if (envUrl && (envUrl.startsWith('http://') || envUrl.startsWith('https://'))) {
    return envUrl;
  }

  return 'https://m.my66.cc.cd';
}

function appConfigPlugin(): Plugin {
  return {
    name: 'app-config-sync-plugin',
    buildStart() {
      const url = getResolvedEnvUrl();
      const cfg = {
        serverUrl: url,
        buildTime: new Date().toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }),
        description: 'Auto-generated from .env URL for Android APK and Web client',
      };
      const publicDir = path.resolve(__dirname, 'public');
      if (!fs.existsSync(publicDir)) fs.mkdirSync(publicDir, { recursive: true });
      fs.writeFileSync(path.join(publicDir, 'app-config.json'), JSON.stringify(cfg, null, 2), 'utf-8');
    },
    transformIndexHtml(html) {
      const url = getResolvedEnvUrl();
      return html.replace(
        '</head>',
        `  <script>window.__APP_ENV_URL__ = ${JSON.stringify(url)};</script>\n  </head>`
      );
    },
  };
}

export default defineConfig(() => {
  const resolvedUrl = getResolvedEnvUrl();
  return {
    define: {
      __APP_ENV_URL__: JSON.stringify(resolvedUrl),
    },
    plugins: [vue(), tailwindcss(), chessWebSocketPlugin(), appConfigPlugin()],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    build: {
      outDir: 'dist',
      emptyOutDir: true,
      sourcemap: false,
    },
    server: {
      port: process.env.PORT ? parseInt(process.env.PORT, 10) : 3000,
      host: '0.0.0.0',
      allowedHosts: true,
      hmr: process.env.DISABLE_HMR !== 'true',
      watch: process.env.DISABLE_HMR === 'true' ? null : {},
    },
    preview: {
      allowedHosts: true,
    },
  };
});
