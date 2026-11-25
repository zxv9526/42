import {
  loadDatabase,
  saveDatabase,
  recordAdminLog,
  recordServerPointsTx,
  generateSecureSessionToken,
  type UserProfileData,
  hashPassword,
  verifyPassword,
} from './serverStorage.ts';
import fs from 'fs';
import path from 'path';
import { exec } from 'child_process';
import { presenceTracker } from './presenceTracker.ts';
import {
  ackServerUnreadSettlements,
  advanceServerBookingCarriage,
  getServerBookingState,
  pickServerBookingTable,
  selectServerBookingSeat,
  simulateServerBookingSettlement,
  submitServerBookingHand,
} from './games/shisanshui/serverBookingService.ts';
import {
  getGithubRepoName,
  saveEnvVariable,
  normalizePhone,
  getAllowedAndAdminPhones,
  findUser,
  ensureUserProfile,
  parseJsonBody,
  sendJson,
  sendHtml,
  broadcastTeahousePlayersRoster,
  sanitizeProfileForClient,
  safeTokenEqual,
  authenticateRequest,
  authenticateAdmin,
  applyCorsHeaders,
} from './server/apiHelpers.ts';
import {
  exportPlayersPointsTxt,
  triggerDebouncedTxtBackup,
  executeTelegramUpdate,
} from './server/telegramAdminHandlers.ts';

export {
  withBookingCarriagesLock,
  loadBookingCarriagesFromDisk,
  saveBookingCarriagesToDisk,
  getOrInitServerCarriage,
} from './server/apiHelpers.ts';
export {
  sanitizeTelegramMarkdown,
  getStoredAdminChatId,
  storeAdminChatId,
  escapeHtml,
  buildTelegramAdminKeyboard,
  notifyPointsTxtToTelegram,
  evaluateTextQuality,
  decodeTxtBuffer,
  handleIncomingTelegramDocument,
} from './server/telegramAdminHandlers.ts';
export {
  getGithubRepoName,
  saveEnvVariable,
  normalizePhone,
  getAllowedAndAdminPhones,
  ensureUserProfile,
  parseJsonBody,
  broadcastTeahousePlayersRoster,
  exportPlayersPointsTxt,
  triggerDebouncedTxtBackup,
  executeTelegramUpdate,
};

let isRebuildingApk = false;

export async function handleApiRoute(
  req: any,
  res: any,
  broadcast?: (payload: any) => void
): Promise<boolean> {
  const urlObj = new URL(req.url, 'http://localhost');
  const pathname = urlObj.pathname;
  const method = req.method?.toUpperCase();

  // 0. Universal Cross-Origin Resource Sharing (CORS) & Preflight Headers
  applyCorsHeaders(req, res);

  // Handle HTTP OPTIONS Preflight requests immediately with 204 No Content
  if (method === 'OPTIONS') {
    res.statusCode = 204;
    res.end();
    return true;
  }

  // 1. Health check
  if (pathname === '/api/health' && method === 'GET') {
    sendJson(res, 200, {
      status: 'ok',
      engine: 'Telegram-Authoritative-Engine',
      timestamp: Date.now(),
    });
    return true;
  }

  // 1b. APK Status & Metadata
  if (pathname === '/api/apk/status' && method === 'GET') {
    try {
      const isDebug = urlObj.searchParams.get('variant') === 'debug';
      const fileName = isDebug ? 'chess-game-debug.apk' : 'chess-game-release.apk';
      const repo = getGithubRepoName();

      let diskBuildInfo: any = null;
      try {
        const infoFile = path.resolve(process.cwd(), 'apk', 'apk-build-info.json');
        if (fs.existsSync(infoFile)) {
          diskBuildInfo = JSON.parse(fs.readFileSync(infoFile, 'utf-8'));
        }
      } catch {}

      let fileSize = Number(diskBuildInfo?.fileSize) >= 4200000 ? Number(diskBuildInfo.fileSize) : 6394293;
      let isLocal = false;

      // Check if authentic local APK exists
      const localCandidates = [
        path.resolve(process.cwd(), 'apk', fileName),
        path.resolve(process.cwd(), 'apk', 'app-latest.apk'),
      ];
      for (const lc of localCandidates) {
        if (fs.existsSync(lc)) {
          try {
            const st = fs.statSync(lc);
            if (st.size >= 4200000) {
              fileSize = st.size;
              isLocal = true;
              break;
            }
          } catch {}
        }
      }

      if (!isLocal) {
        try {
          const headUrl = `https://ghproxy.net/https://github.com/${repo}/releases/download/latest-apk/${fileName}`;
          const headResp = await fetch(headUrl, { method: 'HEAD', signal: AbortSignal.timeout(2500) });
          if (headResp.ok) {
            const cl = headResp.headers.get('content-length');
            if (cl) {
              const sz = parseInt(cl, 10);
              if (sz >= 4200000) fileSize = sz;
            }
          }
        } catch {}
      }

      const fileSizeMb = `${(fileSize / (1024 * 1024)).toFixed(2)} MB`;

      sendJson(res, 200, {
        success: true,
        repo,
        apkExists: true,
        fileSize,
        fileSizeMb,
        isLocal,
        mtime: new Date().toISOString(),
        buildInfo: diskBuildInfo || {
          version: '1.0.13',
          buildTime: 'GitHub Actions 官方全量编译 (正式签名版)',
          fileSizeMb,
          fileSize,
          description: '三大经典国粹棋牌 - Android 官方最新全量发布版 (包含全套真人离线配音与实时语音)',
        },
      });
      return true;
    } catch (e: any) {
      sendJson(res, 500, { success: false, error: String(e.message || e) });
      return true;
    }
  }

  // 1c. APK On-Demand Rebuild (前端静态资源重新构建)
  if ((pathname === '/api/apk/rebuild' || pathname === '/api/download/apk/rebuild') && (method === 'POST' || method === 'GET')) {
    const db = loadDatabase();
    if (!authenticateAdmin(req, db)) {
      sendJson(res, 403, { success: false, error: '需要管理员权限方可执行构建' });
      return true;
    }
    if (isRebuildingApk) {
      sendJson(res, 409, { success: false, error: '前端资源正在构建中，请稍候...' });
      return true;
    }
    if (!fs.existsSync(path.resolve(process.cwd(), 'node_modules', 'vite'))) {
      sendJson(res, 400, {
        success: false,
        error: '当前为纯生产运行时模式（--omit=dev），未安装前端编译工具链，请在 PC/CI 构建 dist 后同步更新',
      });
      return true;
    }
    isRebuildingApk = true;
    try {
      console.log('🔄 [APK API] Rebuilding web assets asynchronously with npm run build...');
      await new Promise<void>((resolve, reject) => {
        exec('npm run build', { cwd: process.cwd(), maxBuffer: 1024 * 1024 * 10 }, (err) => {
          if (err) reject(err);
          else resolve();
        });
      });
      sendJson(res, 200, {
        success: true,
        message: '最新前端代码已重新构建完成！',
      });
      return true;
    } catch (e: any) {
      console.error('❌ [APK API] Build error:', e);
      sendJson(res, 500, { success: false, error: '重新构建失败: ' + String(e.message || e) });
      return true;
    } finally {
      isRebuildingApk = false;
    }
  }

  // 1d. APK Direct Download (同源免代理高速下载专线，优先分发本地 4.49MB 正式安装包，杜绝 4.09MB 损坏旧包)
  if ((pathname === '/api/download/apk' || pathname === '/api/apk/release' || pathname === '/api/apk/debug') && (method === 'GET' || method === 'HEAD')) {
    try {
      const isDebug = pathname === '/api/apk/debug' || urlObj.searchParams.get('variant') === 'debug';
      const fileName = isDebug ? 'chess-game-debug.apk' : 'chess-game-release.apk';
      const repo = getGithubRepoName();

      // 1. Check if authentic local APK (>= 4.2 MB) exists in dist / apk / public
      const localCandidates = [
        path.resolve(process.cwd(), 'dist', fileName),
        path.resolve(process.cwd(), 'dist', 'app-latest.apk'),
        path.resolve(process.cwd(), 'apk', fileName),
        path.resolve(process.cwd(), 'apk', 'app-latest.apk'),
        path.resolve(process.cwd(), 'public', fileName),
        path.resolve(process.cwd(), 'public', 'apk', fileName),
      ];
      for (const lc of localCandidates) {
        if (fs.existsSync(lc)) {
          try {
            const st = fs.statSync(lc);
            if (st.size >= 4200000) {
              console.log(`🚀 [APK Download] 直接从本地极速分发正版完整安装包: ${lc} (${(st.size / 1048576).toFixed(2)} MB)`);
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
              if (method === 'HEAD') {
                res.end();
              } else {
                fs.createReadStream(lc).pipe(res);
              }
              return true;
            }
          } catch {}
        }
      }

      // 2. If no local valid APK exists, redirect to GitHub Releases 4.49MB official APK via high-speed mirror
      const releaseMirrorUrl = `https://ghproxy.net/https://github.com/${repo}/releases/download/latest-apk/${fileName}`;
      console.log(`🚀 [APK Download Redirect] 本地暂无安装包，重定向至 GitHub Releases 4.49MB 最新安装包加速直链: ${releaseMirrorUrl}`);
      res.writeHead(302, {
        'Location': releaseMirrorUrl,
        'Cache-Control': 'no-cache, no-store, must-revalidate',
      });
      res.end();
      return true;
    } catch (e) {
      const repo = getGithubRepoName();
      res.writeHead(302, {
        'Location': `https://ghproxy.net/https://github.com/${repo}/releases/download/latest-apk/chess-game-release.apk`,
      });
      res.end();
      return true;
    }
  }

  // 1e. High-Speed Chinese TTS Speech Proxy with Server Memory Caching (100% 免翻墙·国内节点极速秒发)
  if (pathname === '/api/tts' && (method === 'GET' || method === 'POST')) {
    try {
      let text = urlObj.searchParams.get('text') || '';
      let isFemale = urlObj.searchParams.get('female') === 'true';
      if (method === 'POST') {
        const body = await parseJsonBody(req);
        if (body.text) text = body.text;
        if (body.female !== undefined) isFemale = Boolean(body.female);
      }

      const cleanText = String(text || '').replace(/[^\u4e00-\u9fa5a-zA-Z0-9，。！、？]/g, '').trim();
      if (!cleanText) {
        sendJson(res, 400, { error: 'Text parameter required' });
        return true;
      }

      // 多音字"单"纠偏：出牌语音"单张九"替换为"丹张九"，防止 Baidu TTS 误读为 "shàn zhāng"
      const ttsSpeechText = cleanText.replace(/单张/g, '丹张').replace(/^单([0-9一二三四五六七八九十勾圈老K尖A-Z])/g, '丹$1');

      // 1. 服务端内存缓存 (0ms 极速命中，彻底解决重复网络请求)
      const cacheKey = `${cleanText}_${isFemale ? 'f' : 'm'}`;
      if (typeof globalThis !== 'undefined') {
        if (!(globalThis as any)._serverTtsCache) {
          (globalThis as any)._serverTtsCache = new Map<string, Buffer>();
        }
        const cache: Map<string, Buffer> = (globalThis as any)._serverTtsCache;
        if (cache.has(cacheKey)) {
          const buf = cache.get(cacheKey)!;
          res.writeHead(200, {
            'Content-Type': 'audio/mpeg',
            'Content-Length': buf.byteLength,
            'Access-Control-Allow-Origin': '*',
            'Cache-Control': 'public, max-age=604800, immutable',
          });
          res.end(buf);
          return true;
        }
      }

      const per = isFemale ? '0' : '1';
      const domesticTtsEndpoints = [
        // Baidu 官方通用中文高清晰语音 (按性别划分男声per=1，女声per=0)
        `https://fanyi.baidu.com/gettts?lan=zh&per=${per}&text=${encodeURIComponent(ttsSpeechText)}&spd=6&source=web`,
        // Baidu Text2Audio 备用节点
        `https://tts.baidu.com/text2audio?cuid=baike&lan=ZH&ctp=1&per=${per}&padd=&tp=1&tex=${encodeURIComponent(ttsSpeechText)}`,
      ];

      for (const ttsUrl of domesticTtsEndpoints) {
        try {
          const resp = await fetch(ttsUrl, {
            headers: {
              'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36',
              'Referer': 'https://fanyi.baidu.com/',
            },
            signal: AbortSignal.timeout(2000), // 超短 2s 超时，不卡顿
          });

          if (resp.ok) {
            const buf = Buffer.from(await resp.arrayBuffer());
            if (buf.byteLength > 300) {
              // 存入服务端内存缓存 (最多缓存 500 条常用语音)
              if (typeof globalThis !== 'undefined' && (globalThis as any)._serverTtsCache) {
                const cache: Map<string, Buffer> = (globalThis as any)._serverTtsCache;
                if (cache.size > 500) {
                  const firstKey = cache.keys().next().value;
                  if (firstKey) cache.delete(firstKey);
                }
                cache.set(cacheKey, buf);
              }

              res.writeHead(200, {
                'Content-Type': 'audio/mpeg',
                'Content-Length': buf.byteLength,
                'Access-Control-Allow-Origin': '*',
                'Cache-Control': 'public, max-age=604800, immutable',
              });
              res.end(buf);
              return true;
            }
          }
        } catch {}
      }

      // 3. 微软 Edge Speech 备用节点 (设置 1.5s 短超时)
      try {
        const voiceName = isFemale ? 'zh-CN-XiaoxiaoNeural' : 'zh-CN-YunxiNeural';
        const ssml = `<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='zh-CN'><voice name='${voiceName}'><prosody pitch='+0Hz' rate='+12%'>${cleanText}</prosody></voice></speak>`;
        const edgeResp = await fetch('https://speech.platform.bing.com/consumer/speech/synthesize/readaloud/single/v1?trustedclienttoken=6A5AA1D4EA2D4738320C0F880E4CB756', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0',
          },
          body: JSON.stringify({
            context: {
              synthesis: {
                audio: {
                  metadataversion: '1.0',
                  name: 'speech',
                  outputFormat: 'audio-24khz-48kbitrate-mono-mp3',
                  type: 'audio',
                },
              },
            },
            script: ssml,
          }),
          signal: AbortSignal.timeout(1800),
        });

        if (edgeResp.ok) {
          const buf = Buffer.from(await edgeResp.arrayBuffer());
          if (buf.byteLength > 300) {
            res.writeHead(200, {
              'Content-Type': 'audio/mpeg',
              'Content-Length': buf.byteLength,
              'Access-Control-Allow-Origin': '*',
              'Cache-Control': 'public, max-age=604800, immutable',
            });
            res.end(buf);
            return true;
          }
        }
      } catch {}

      sendJson(res, 500, { error: 'TTS synthesis temporary failure' });
      return true;
    } catch (e: any) {
      sendJson(res, 500, { error: String(e.message || e) });
      return true;
    }
  }

  // 1f. Booking Carriages API (水镜十三水预约专列车厢赛牌桌数据与提交 - 权威多端同步)
  if (pathname === '/api/booking/carriage' && method === 'GET') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      const sceneMode = (urlObj.searchParams.get('sceneMode') === 'eight-booking') ? 'eight-booking' : 'four-booking';
      const carriageId = urlObj.searchParams.get('carriageId') || undefined;
      const carriageNumParam = urlObj.searchParams.get('carriageNumber');
      const requestedCarriageNumber = carriageNumParam ? parseInt(carriageNumParam, 10) : undefined;
      let userId = urlObj.searchParams.get('playerId') || urlObj.searchParams.get('userId') || 'guest';
      if (authUser) {
        userId = authUser.id;
      } else if (userId && db.profiles[userId]?.password) {
        sendJson(res, 401, { success: false, error: '请先登录后再访问个人预约专列进度' });
        return true;
      }
      const playerName = authUser?.name || urlObj.searchParams.get('playerName') || '茶客';
      const avatar = authUser?.avatar || urlObj.searchParams.get('avatar') || '👑';
      const state = await getServerBookingState({
        sceneMode,
        userId,
        playerName,
        avatar,
        requestedCarriageId: carriageId,
        requestedCarriageNumber,
      });
      sendJson(res, 200, { success: true, ...state });
      return true;
    } catch (err: any) {
      sendJson(res, 500, { success: false, error: String(err?.message || err) });
      return true;
    }
  }

  if (pathname === '/api/booking/select-seat' && method === 'POST') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      const payload = await parseJsonBody(req);
      let userId = payload.playerId || payload.userId || 'guest';
      if (authUser) {
        userId = authUser.id;
      } else if (userId && db.profiles[userId]?.password) {
        sendJson(res, 401, { success: false, error: '会话失效，请重新登录后选座' });
        return true;
      }
      const sceneMode = payload.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
      const resData = await selectServerBookingSeat({
        sceneMode,
        userId,
        playerName: authUser?.name || payload.playerName || '茶客',
        avatar: authUser?.avatar || payload.avatar || '👑',
        seat: payload.seat || '东',
        carriageId: payload.carriageId || undefined,
        carriageNumber: payload.carriageNumber ? Number(payload.carriageNumber) : undefined,
      });
      sendJson(res, resData.ok ? 200 : 400, { success: resData.ok, ...resData });
      return true;
    } catch (err: any) {
      sendJson(res, 400, { success: false, error: String(err?.message || err) });
      return true;
    }
  }

  if (pathname === '/api/booking/pick-table' && method === 'POST') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      const payload = await parseJsonBody(req);
      let userId = payload.playerId || payload.userId || 'guest';
      if (authUser) {
        userId = authUser.id;
      } else if (userId && db.profiles[userId]?.password) {
        sendJson(res, 401, { success: false, error: '会话失效，请重新登录' });
        return true;
      }
      const sceneMode = payload.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
      const resData = await pickServerBookingTable({
        sceneMode,
        userId,
        carriageId: payload.carriageId || undefined,
        carriageNumber: payload.carriageNumber ? Number(payload.carriageNumber) : undefined,
        specificTableIndex: payload.tableIndex ? Number(payload.tableIndex) : undefined,
      });
      sendJson(res, resData.ok ? 200 : 400, { success: resData.ok, ...resData });
      return true;
    } catch (err: any) {
      sendJson(res, 400, { success: false, error: String(err?.message || err) });
      return true;
    }
  }

  if (pathname === '/api/booking/submit' && method === 'POST') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      const payload = await parseJsonBody(req);
      let userId = payload.playerId || payload.userId || 'guest';
      if (authUser) {
        userId = authUser.id;
      } else if (userId && db.profiles[userId]?.password) {
        sendJson(res, 401, { success: false, error: '会话失效，请重新登录后提交手牌' });
        return true;
      }
      const sceneMode = payload.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
      const resData = await submitServerBookingHand({
        sceneMode,
        userId,
        playerName: authUser?.name || payload.playerName || '茶客',
        avatar: authUser?.avatar || payload.avatar || '👑',
        carriageId: payload.carriageId || undefined,
        carriageNumber: payload.carriageNumber ? Number(payload.carriageNumber) : undefined,
        tableIndex: payload.tableIndex ? Number(payload.tableIndex) : undefined,
        playerSeat: payload.playerSeat,
        handTiers: payload.playerHand,
        isGraceSubmission: Boolean(payload.isGraceSubmission),
      });
      sendJson(res, resData.ok ? 200 : 400, { success: resData.ok, ...resData });
      return true;
    } catch (err: any) {
      sendJson(res, 400, { success: false, error: String(err?.message || err) });
      return true;
    }
  }

  if (pathname === '/api/booking/advance-carriage' && method === 'POST') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      const payload = await parseJsonBody(req);
      let userId = payload.playerId || payload.userId || 'guest';
      if (authUser) {
        userId = authUser.id;
      } else if (userId && db.profiles[userId]?.password) {
        sendJson(res, 401, { success: false, error: '会话失效，请重新登录' });
        return true;
      }
      const sceneMode = payload.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
      const resData = await advanceServerBookingCarriage({
        sceneMode,
        userId,
        playerName: authUser?.name || payload.playerName || '茶客',
        avatar: authUser?.avatar || payload.avatar || '👑',
        createFresh: Boolean(payload.createFresh),
      });
      sendJson(res, 200, { success: true, ...resData });
      return true;
    } catch (err: any) {
      sendJson(res, 400, { success: false, error: String(err?.message || err) });
      return true;
    }
  }

  if (pathname === '/api/booking/simulate-settle' && method === 'POST') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      const payload = await parseJsonBody(req);
      let userId = payload.playerId || payload.userId || 'guest';
      if (authUser) {
        userId = authUser.id;
      } else if (userId && db.profiles[userId]?.password) {
        sendJson(res, 401, { success: false, error: '会话失效，请重新登录' });
        return true;
      }
      const sceneMode = payload.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
      const resData = await simulateServerBookingSettlement({
        sceneMode,
        userId,
        carriageId: payload.carriageId || undefined,
        carriageNumber: payload.carriageNumber ? Number(payload.carriageNumber) : undefined,
      });
      sendJson(res, resData.ok ? 200 : 400, { success: resData.ok, ...resData });
      return true;
    } catch (err: any) {
      sendJson(res, 400, { success: false, error: String(err?.message || err) });
      return true;
    }
  }

  if (pathname === '/api/booking/ack-settlement' && method === 'POST') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      const payload = await parseJsonBody(req);
      const userId = authUser?.id || payload.playerId || payload.userId;
      if (userId) {
        await ackServerUnreadSettlements(userId);
      }
      sendJson(res, 200, { success: true });
      return true;
    } catch (err: any) {
      sendJson(res, 400, { success: false, error: String(err?.message || err) });
      return true;
    }
  }

  // 2. Database Export (管理员鉴权 + 密码与令牌脱敏)
  if (pathname === '/api/database/export' && method === 'GET') {
    const db = loadDatabase();
    if (!authenticateAdmin(req, db)) {
      sendJson(res, 403, { success: false, error: '需要管理员权限方可导出数据' });
      return true;
    }
    const sanitizedProfiles: Record<string, any> = {};
    for (const [k, v] of Object.entries(db.profiles || {})) {
      sanitizedProfiles[k] = sanitizeProfileForClient(v);
    }
    sendJson(res, 200, { ...db, profiles: sanitizedProfiles });
    return true;
  }

  // 3. User Sync (需持有有效会话令牌，绝不信任客户端余额或覆盖全局记录)
  if (pathname === '/api/user/sync' && method === 'POST') {
    try {
      const payload = await parseJsonBody(req);
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      if (!authUser) {
        sendJson(res, 401, { success: false, error: '会话无效或未登录，请重新登录' });
        return true;
      }

      if (payload.profile?.id && payload.profile.id !== authUser.id) {
        sendJson(res, 403, { success: false, error: '无权同步他人账号数据' });
        return true;
      }

      const pPhone = normalizePhone(authUser.phone || '');
      const { allowedPhones, adminPhones } = getAllowedAndAdminPhones();
      const isRevoked = pPhone && (db.revokedPhones || []).some((ph: any) => {
        const np = normalizePhone(ph);
        return np && (np === pPhone || np.endsWith(pPhone) || pPhone.endsWith(np));
      });
      const isAllowed = !pPhone ||
        allowedPhones.some((p) => p === pPhone || p.endsWith(pPhone) || pPhone.endsWith(p)) ||
        adminPhones.some((p) => p === pPhone || p.endsWith(pPhone) || pPhone.endsWith(p));

      if (isRevoked || !isAllowed) {
        delete db.profiles[authUser.id];
        saveDatabase(db);
        sendJson(res, 200, {
          success: false,
          notFound: true,
          revoked: true,
          error: '该账号手机号授权已被管理员移除，已注销',
        });
        return true;
      }

      if (authUser.isBanned) {
        sendJson(res, 403, { success: false, error: '该账号已被封禁，禁止同步操作' });
        return true;
      }

      if (payload.profile?.name) {
        const cleanSyncName = String(payload.profile.name).trim();
        if (cleanSyncName.length >= 1 && cleanSyncName.length <= 3) {
          authUser.name = cleanSyncName;
        }
      }
      if (payload.profile?.avatar) authUser.avatar = String(payload.profile.avatar).slice(0, 12);
      if (typeof payload.profile?.rating === 'number' && Number.isFinite(payload.profile.rating)) {
        const curRating = typeof authUser.rating === 'number' ? authUser.rating : 1800;
        const delta = Math.max(-200, Math.min(200, Math.round(payload.profile.rating) - curRating));
        authUser.rating = Math.max(1000, Math.min(9999, curRating + delta));
      }
      if (typeof payload.profile?.level === 'number' && Number.isFinite(payload.profile.level)) {
        const curLevel = typeof authUser.level === 'number' ? authUser.level : 1;
        const nextLevel = Math.max(1, Math.min(20, Math.round(payload.profile.level)));
        if (Math.abs(nextLevel - curLevel) <= 2) {
          authUser.level = nextLevel;
        }
      }
      if (payload.profile?.stats && typeof payload.profile.stats === 'object') {
        const nextStats: any = { ...(authUser.stats || {}) };
        for (const gk of ['yansan', 'shisanshui', 'mahjong'] as const) {
          const incoming = payload.profile.stats[gk];
          if (incoming && typeof incoming === 'object') {
            nextStats[gk] = {
              played: Math.max(0, Math.min(999999, Math.floor(Number(incoming.played) || 0))),
              won: Math.max(0, Math.min(999999, Math.floor(Number(incoming.won) || 0))),
              highestScore: Math.max(0, Math.min(9999999, Math.floor(Number(incoming.highestScore) || 0))),
            };
          }
        }
        authUser.stats = nextStats;
      }
      authUser.updatedAt = Date.now();

      if (payload.records && Array.isArray(payload.records)) {
        const existingIds = new Set((db.records || []).map((r) => r.id));
        const newEntries = payload.records
          .filter((r: any) => r && r.id && !existingIds.has(r.id))
          .slice(0, 20)
          .map((r: any) => ({ ...r, playerId: authUser.id }));
        if (newEntries.length > 0) {
          db.records = [...newEntries, ...(db.records || [])].slice(0, 500);
        }
      }
      saveDatabase(db);
      sendJson(res, 200, {
        success: true,
        coins: authUser.coins,
        rating: authUser.rating,
        level: authUser.level,
        profile: sanitizeProfileForClient(authUser),
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 3b. User Profile Update (昵称与头像修改直落服务器)
  if (pathname === '/api/user/profile' && method === 'POST') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      if (!authUser) {
        sendJson(res, 401, { success: false, error: '请先登录后再修改资料' });
        return true;
      }
      const { name, avatar } = await parseJsonBody(req);
      const cleanName = typeof name === 'string' ? name.trim() : '';
      if (cleanName.length > 3) {
        sendJson(res, 400, { success: false, error: '昵称长度不能超过3个字' });
        return true;
      }
      if (cleanName) authUser.name = cleanName;
      if (typeof avatar === 'string' && avatar.trim()) authUser.avatar = avatar.trim().slice(0, 12);
      authUser.updatedAt = Date.now();
      saveDatabase(db);
      if (broadcast) {
        try {
          broadcastTeahousePlayersRoster(broadcast, db);
        } catch {}
      }
      sendJson(res, 200, { success: true, profile: sanitizeProfileForClient(authUser) });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 3c. Server Points Ledger (云端积分流水账单)
  if (pathname === '/api/points/ledger' && method === 'GET') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      if (!authUser) {
        sendJson(res, 401, { success: false, error: '请先登录查看账单流水' });
        return true;
      }
      const transactions = db.pointsLedger?.[authUser.id] || [];
      sendJson(res, 200, { success: true, ledger: transactions, transactions });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 4. Auth Register
  // 4. Auth Register (移除账号名，仅需授权手机号、6位密码与昵称头像，以手机号为唯一凭证)
  if (pathname === '/api/auth/register' && method === 'POST') {
    try {
      const { phone, password, nickname, avatar } = await parseJsonBody(req);
      const cleanPhone = normalizePhone(phone);
      if (!cleanPhone || !password) {
        sendJson(res, 200, { success: false, error: '请填写完整的手机号和密码' });
        return true;
      }
      if (String(password).trim().length !== 6) {
        sendJson(res, 200, { success: false, error: '密码必须为六位数' });
        return true;
      }
      if (cleanPhone.length < 5) {
        sendJson(res, 200, { success: false, error: '手机号格式不正确' });
        return true;
      }
      if (nickname && String(nickname).trim().length > 3) {
        sendJson(res, 200, { success: false, error: '昵称长度不能超过3个字' });
        return true;
      }

      const { allowedPhones, adminPhones } = getAllowedAndAdminPhones();
      const isAuthorized = allowedPhones.some((p) => p === cleanPhone || p.endsWith(cleanPhone) || cleanPhone.endsWith(p)) ||
                           adminPhones.some((p) => p === cleanPhone || p.endsWith(cleanPhone) || cleanPhone.endsWith(p));

      if (!isAuthorized) {
        sendJson(res, 200, {
          success: false,
          error: '该手机号未获得 Telegram Bot 授权白名单！必须先由管理员在 Bot 中发送 /authphone 授权后方可注册。',
        });
        return true;
      }

      const isAdmin = adminPhones.some((p) => p === cleanPhone || p.endsWith(cleanPhone) || cleanPhone.endsWith(p));
      const db = loadDatabase();

      let existingProfile: any = null;
      for (const p of Object.values(db.profiles)) {
        const existingPhone = normalizePhone(p.phone);
        if (existingPhone && (existingPhone === cleanPhone || existingPhone.endsWith(cleanPhone) || cleanPhone.endsWith(existingPhone))) {
          if (p.password) {
            sendJson(res, 200, { success: false, error: '该授权手机号已注册过账号，请直接使用手机号登录' });
            return true;
          }
          existingProfile = p;
          break;
        }
      }

      if (existingProfile) {
        // 白名单预建建档用户首次在前端输入密码完成注册激活
        const finalName = nickname && String(nickname).trim() ? String(nickname).trim() : (existingProfile.name || `茶客${cleanPhone.slice(-4)}`);
        existingProfile.password = hashPassword(String(password).trim());
        existingProfile.name = finalName;
        existingProfile.avatar = avatar || existingProfile.avatar || '👑';
        existingProfile.isAdmin = isAdmin;
        existingProfile.updatedAt = Date.now();
        const sessionToken = generateSecureSessionToken();
        existingProfile.sessionToken = sessionToken;

        saveDatabase(db);
        triggerDebouncedTxtBackup(`激活白名单新茶客 (${existingProfile.name})`, db);
        recordAdminLog('USER_REGISTER', 'Telegram Auth', cleanPhone, `白名单预建用户激活注册成功: ${existingProfile.name} (📱 ${cleanPhone}, UID: ${existingProfile.id})，当前预设筹码: ${existingProfile.coins || 0}`, 0);

        sendJson(res, 200, { success: true, profile: sanitizeProfileForClient(existingProfile), sessionToken });
        return true;
      }

      const newId = `USER-${Math.floor(1000 + Math.random() * 9000)}`;
      const finalName = nickname && String(nickname).trim() ? String(nickname).trim() : `茶客${cleanPhone.slice(-4)}`;
      const newProfile: UserProfileData = {
        id: newId,
        username: cleanPhone,
        password: hashPassword(String(password).trim()),
        name: finalName,
        avatar: avatar || '👑',
        phone: cleanPhone,
        isAdmin,
        isBanned: false,
        coins: 0,
        rating: 1800,
        level: 1,
        lastCheckinDate: '',
        registeredAt: Date.now(),
        updatedAt: Date.now(),
        stats: {
          yansan: { played: 0, won: 0, highestScore: 0 },
          shisanshui: { played: 0, won: 0, highestScore: 0 },
          mahjong: { played: 0, won: 0, highestScore: 0 },
        },
      };

      const sessionToken = generateSecureSessionToken();
      newProfile.sessionToken = sessionToken;

      db.profiles[newId] = newProfile;
      saveDatabase(db);
      triggerDebouncedTxtBackup(`新茶客注册 (${newProfile.name})`, db);

      recordAdminLog('USER_REGISTER', 'Telegram Auth', cleanPhone, `新用户注册: ${newProfile.name} (📱 ${cleanPhone}, UID: ${newId})，初始筹码: 0`, 0);

      if (broadcast) {
        try {
          const freshPlayers = presenceTracker.getAllPlayers(db.profiles || {});
          broadcast({ type: 'teahouse_players_update', count: freshPlayers.length, onlineCount: freshPlayers.filter(p => p.isOnline).length, players: freshPlayers });
        } catch {}
      }

      sendJson(res, 200, { success: true, profile: sanitizeProfileForClient(newProfile), sessionToken });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 5. Auth Login (仅支持手机号和密码登录，取消用户名登录)
  if (pathname === '/api/auth/login' && method === 'POST') {
    try {
      const { phone, username, password } = await parseJsonBody(req);
      const cleanPhone = normalizePhone(phone || username || '');
      if (!cleanPhone || !password) {
        sendJson(res, 200, { success: false, error: '请输入手机号和密码' });
        return true;
      }

      const db = loadDatabase();
      let foundProfile: any = null;
      for (const p of Object.values(db.profiles)) {
        const profPhone = normalizePhone(p.phone);
        if (profPhone && (profPhone === cleanPhone || profPhone.endsWith(cleanPhone) || cleanPhone.endsWith(profPhone))) {
          foundProfile = p;
          break;
        }
      }
      // 手机号未建档，检查是否在白名单中
      if (!foundProfile) {
        const { allowedPhones, adminPhones } = getAllowedAndAdminPhones();
        const isAuth = allowedPhones.some((p) => p === cleanPhone || p.endsWith(cleanPhone) || cleanPhone.endsWith(p)) ||
                       adminPhones.some((p) => p === cleanPhone || p.endsWith(cleanPhone) || cleanPhone.endsWith(p));
        if (isAuth) {
          sendJson(res, 200, {
            success: false,
            needRegister: true,
            error: '该手机号已在白名单授权，但尚未激活注册。请先点击【注册账号】设置您的6位密码与牌桌昵称！',
          });
          return true;
        }
        sendJson(res, 200, { success: false, error: '该手机号尚未注册或密码错误，请先注册' });
        return true;
      }
      if (foundProfile.isBanned) {
        sendJson(res, 200, { success: false, error: `该账号已被管理员封禁: ${foundProfile.bannedReason || '违规操作'}` });
        return true;
      }

      // Check whether user's bound phone is still authorized in whitelist
      const { allowedPhones, adminPhones } = getAllowedAndAdminPhones();
      const userPhone = normalizePhone(foundProfile.phone);
      const isStillAuthorized = !userPhone ||
        allowedPhones.some((p) => p === userPhone || p.endsWith(userPhone) || userPhone.endsWith(p)) ||
        adminPhones.some((p) => p === userPhone || p.endsWith(userPhone) || userPhone.endsWith(p));

      if (!isStillAuthorized) {
        delete db.profiles[foundProfile.id];
        saveDatabase(db);
        sendJson(res, 200, {
          success: false,
          error: '该账号绑定的手机号授权已被管理员移除，账号已注销。如需使用请联系管理员重新授权。',
        });
        return true;
      }

      // 核心安全漏洞修复：白名单预建档手机号尚未在前端注册设置密码，严禁以任意密码直接登录！
      if (!foundProfile.password || !String(foundProfile.password).trim()) {
        sendJson(res, 200, {
          success: false,
          needRegister: true,
          error: '该手机号已在白名单授权，但尚未激活注册。请先点击【注册账号】设置您的6位密码与牌桌昵称！',
        });
        return true;
      }

      const isValid = verifyPassword(String(password).trim(), foundProfile.password);
      if (!isValid) {
        sendJson(res, 200, { success: false, error: '手机号或密码不正确' });
        return true;
      }
      // Upgrade legacy plaintext password to secure hash upon successful login
      if (!foundProfile.password.includes(':')) {
        foundProfile.password = hashPassword(String(password).trim());
      }
      const sessionToken = generateSecureSessionToken();
      foundProfile.sessionToken = sessionToken;
      saveDatabase(db);

      sendJson(res, 200, { success: true, profile: sanitizeProfileForClient(foundProfile), sessionToken });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // Check Session (Single-device session enforcement & authorization validation)
  if (pathname === '/api/auth/check-session' && method === 'POST') {
    try {
      const { userId, sessionToken } = await parseJsonBody(req);
      const db = loadDatabase();
      const user = Object.values(db.profiles).find((p) => p.id === userId);
      if (!user) {
        sendJson(res, 200, { success: true, valid: false, sessionExpired: true, error: '用户不存在或已被清理' });
        return true;
      }

      const { allowedPhones, adminPhones } = getAllowedAndAdminPhones();
      const userPhone = normalizePhone(user.phone || '');
      const isStillAuthorized = !userPhone ||
        allowedPhones.some((p) => p === userPhone || p.endsWith(userPhone) || userPhone.endsWith(p)) ||
        adminPhones.some((p) => p === userPhone || p.endsWith(userPhone) || userPhone.endsWith(p));

      const isRevoked = userPhone && (db.revokedPhones || []).some((ph: any) => {
        const np = normalizePhone(ph);
        return np && (np === userPhone || np.endsWith(userPhone) || userPhone.endsWith(np));
      });

      if (!isStillAuthorized || isRevoked) {
        delete db.profiles[user.id];
        saveDatabase(db);
        sendJson(res, 200, { success: true, valid: false, sessionExpired: true, error: '该账号手机号授权已被管理员移出白名单，账号已注销' });
        return true;
      }

      if (user.sessionToken && !safeTokenEqual(user.sessionToken, String(sessionToken || ''))) {
        sendJson(res, 200, { success: true, valid: false, sessionExpired: true, error: '您的账号已在另一台设备登录，当前会话已失效' });
        return true;
      }
      sendJson(res, 200, {
        success: true,
        valid: true,
        coins: user.coins,
        rating: user.rating,
        level: user.level,
        name: user.name,
        profile: sanitizeProfileForClient(user),
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 6. User Search (需登录后检索茶客)
  if (pathname === '/api/users/search' && method === 'GET') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      if (!authUser) {
        sendJson(res, 401, { success: false, error: '请先登录后再查找茶客' });
        return true;
      }
      const rawPhone = String(urlObj.searchParams.get('phone') || '').trim();
      const phone = normalizePhone(rawPhone) || rawPhone.replace(/\D/g, '');
      if (!phone) {
        sendJson(res, 400, { success: false, error: '请输入要搜索的手机号' });
        return true;
      }
      let found = findUser(phone, db);
      if (!found && phone.length >= 7) {
        const { allowedPhones, adminPhones } = getAllowedAndAdminPhones();
        const isAllowed = allowedPhones.some((p) => p === phone || p.endsWith(phone) || phone.endsWith(p)) ||
                          adminPhones.some((p) => p === phone || p.endsWith(phone) || phone.endsWith(p));
        if (isAllowed) {
          found = ensureUserProfile(phone, db, 0);
          saveDatabase(db);
        }
      }
      if (!found) {
        sendJson(res, 200, { success: false, error: '未找到该手机号对应的茶客，请确认手机号是否正确或已获白名单授权' });
        return true;
      }
      sendJson(res, 200, {
        success: true,
        user: {
          id: found.id,
          name: found.name,
          username: found.username,
          phone: found.phone,
          coins: found.coins,
          rating: found.rating,
          level: found.level,
          avatar: found.avatar,
          isAdmin: found.isAdmin,
          isBanned: found.isBanned,
        },
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 7. Points Transfer (严格会话鉴权，绝不接受客户端余额，双边写入云端流水)
  if (pathname === '/api/points/transfer' && method === 'POST') {
    try {
      const db = loadDatabase();
      const authUser = authenticateRequest(req, db);
      if (!authUser) {
        sendJson(res, 401, { success: false, error: '请先登录后再赠送筹码' });
        return true;
      }
      const { senderId, recipientId, recipientPhone, amount } = await parseJsonBody(req);
      if (senderId && senderId !== authUser.id) {
        sendJson(res, 403, { success: false, error: '身份校验失败，禁止操作他人账户' });
        return true;
      }
      const transferAmount = Math.floor(Number(amount) || 0);
      if (!Number.isSafeInteger(transferAmount) || transferAmount <= 0 || transferAmount > 10_000_000) {
        sendJson(res, 400, { success: false, error: '赠送筹码数量无效 (需为 1 - 10,000,000 之间的整数)' });
        return true;
      }
      const sender = authUser;
      if (sender.isBanned) {
        sendJson(res, 403, { success: false, error: '当前账号已被封禁，禁止划转筹码' });
        return true;
      }

      if (sender.coins < transferAmount) {
        sendJson(res, 200, { success: false, error: `持有筹码不足，当前仅有 ${sender.coins} 分，无法赠送` });
        return true;
      }

      let recipient = (recipientId && db.profiles[recipientId]) || (recipientPhone && findUser(recipientPhone, db));
      if (!recipient && recipientPhone) {
        const cleanRec = normalizePhone(recipientPhone);
        if (cleanRec && cleanRec.length >= 7) {
          const { allowedPhones, adminPhones } = getAllowedAndAdminPhones();
          const isWhitelisted =
            allowedPhones.some((p) => p === cleanRec || p.endsWith(cleanRec) || cleanRec.endsWith(p)) ||
            adminPhones.some((p) => p === cleanRec || p.endsWith(cleanRec) || cleanRec.endsWith(p));
          if (isWhitelisted) {
            recipient = ensureUserProfile(cleanRec, db, 0);
          }
        }
      }

      if (!recipient) {
        sendJson(res, 200, { success: false, error: '未找到该接收手机号对应的已授权茶客' });
        return true;
      }
      if (recipient.isBanned) {
        sendJson(res, 200, { success: false, error: '对方账号当前处于封禁状态，无法接收筹码' });
        return true;
      }

      const cleanSenderPhone = normalizePhone(sender.phone || '');
      const cleanRecipientPhone = normalizePhone(recipient.phone || '');
      if (recipient.id === sender.id || (cleanSenderPhone && cleanRecipientPhone && cleanSenderPhone === cleanRecipientPhone)) {
        sendJson(res, 200, { success: false, error: '不能向自己的手机号赠送筹码' });
        return true;
      }

      sender.coins -= transferAmount;
      recipient.coins += transferAmount;
      sender.updatedAt = Date.now();
      recipient.updatedAt = Date.now();

      recordServerPointsTx(
        db,
        sender.id,
        'transfer_out',
        `向 ${recipient.name} (${recipient.phone || recipient.id}) 赠送筹码`,
        -transferAmount,
        sender.coins
      );
      recordServerPointsTx(
        db,
        recipient.id,
        'transfer_in',
        `收到 ${sender.name} (${sender.phone || sender.id}) 赠送筹码`,
        transferAmount,
        recipient.coins
      );

      saveDatabase(db);
      triggerDebouncedTxtBackup(`互赠筹码 (${sender.name} -> ${recipient.name})`, db);

      recordAdminLog(
        'POINTS_TRANSFER',
        sender.name,
        recipient.name,
        `互赠筹码 ${transferAmount} 分 (发起人: ${sender.phone || sender.name} -> 接收人: ${recipient.phone || recipient.name})`,
        transferAmount
      );

      if (typeof broadcast === 'function') {
        try {
          broadcast({
            type: 'user_coins_updated',
            userId: sender.id,
            phone: sender.phone,
            username: sender.username,
            name: sender.name,
            coins: sender.coins,
            rating: sender.rating,
            level: sender.level,
            delta: -transferAmount,
          });
          broadcast({
            type: 'user_coins_updated',
            userId: recipient.id,
            phone: recipient.phone,
            username: recipient.username,
            name: recipient.name,
            coins: recipient.coins,
            rating: recipient.rating,
            level: recipient.level,
            delta: transferAmount,
          });
          broadcastTeahousePlayersRoster(broadcast, db);
          broadcast({
            type: 'chat',
            message: {
              id: 'gift_' + Date.now(),
              sender: '钱庄管家',
              avatar: '🪙',
              text: `🎉 茶客【${sender.name}】向【${recipient.name}】赠送了 ${transferAmount.toLocaleString()} 分筹码！`,
              time: new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
              channel: 'world',
              action: 'gift',
            },
          });
        } catch {}
      }

      sendJson(res, 200, {
        success: true,
        transferAmount,
        senderCoins: sender.coins,
        recipientCoins: recipient.coins,
        senderProfile: sanitizeProfileForClient(sender),
        recipientProfile: sanitizeProfileForClient(recipient),
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 7.4 Real-time Teahouse Registered & Active Players Roster Endpoint
  if (pathname === '/api/teahouse/players' && method === 'GET') {
    try {
      const db = loadDatabase();
      const players = presenceTracker.getAllPlayers(db.profiles || {});
      sendJson(res, 200, {
        success: true,
        count: players.length,
        onlineCount: players.filter((p) => p.isOnline).length,
        players,
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 7.5 Real-time Presence Counts & Players Endpoint
  if (pathname === '/api/presence' && method === 'GET') {
    try {
      const counts = presenceTracker.getRealGamePresence();
      const db = loadDatabase();
      const players = presenceTracker.getAllPlayers(db.profiles || {});
      sendJson(res, 200, {
        success: true,
        ...counts,
        playersCount: players.length,
        players,
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 7.6 Active Rooms List Endpoint
  if (pathname === '/api/rooms' && method === 'GET') {
    try {
      const activeRooms = [
        {
          id: 'RM-202',
          title: '风雷堂 · 经典烟三四人赛',
          gameType: 'yansan',
          status: 'playing',
          playersCount: 4,
          spectators: 9,
          baseStake: 10,
          modeLabel: '方块四首出 · 四人争锋',
        },
        {
          id: 'RM-204',
          title: '水镜阁 · 水镜十三水',
          gameType: 'shisanshui',
          status: 'playing',
          playersCount: 4,
          spectators: 18,
          baseStake: 10,
          modeLabel: '头中尾三道 · 智能理牌',
        },
        {
          id: 'RM-205',
          title: '雀神楼 · 经典国标麻将',
          gameType: 'mahjong',
          status: 'playing',
          playersCount: 4,
          spectators: 12,
          baseStake: 10,
          modeLabel: '国标经典 · 雀神东风圈',
        },
      ];
      sendJson(res, 200, { success: true, rooms: activeRooms });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // ==========================================
  // TELEGRAM ADMIN APIS (管理员专属控制接口)
  // ==========================================
  if (pathname.startsWith('/api/admin/')) {
    const db = loadDatabase();
    if (!authenticateAdmin(req, db)) {
      sendJson(res, 403, { success: false, error: '管理员权限校验失败' });
      return true;
    }
  }

  // 8. Admin: List All Users
  if (pathname === '/api/admin/users' && method === 'GET') {
    try {
      const db = loadDatabase();
      const users = Object.values(db.profiles).map((p) => ({
        id: p.id,
        name: p.name,
        username: p.username || '',
        avatar: p.avatar,
        phone: p.phone || '未绑定',
        isAdmin: Boolean(p.isAdmin),
        isBanned: Boolean(p.isBanned),
        bannedReason: p.bannedReason || '',
        coins: p.coins,
        rating: p.rating,
        level: p.level,
        updatedAt: p.updatedAt,
        stats: p.stats,
      }));
      sendJson(res, 200, { success: true, users });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 9. Admin: Adjust User Points
  if (pathname === '/api/admin/points/adjust' && method === 'POST') {
    try {
      const { targetId, phone, delta, type = 'add', reason = '管理员调整', operator = 'Telegram Admin' } = await parseJsonBody(req);
      const db = loadDatabase();
      const user = (targetId && db.profiles[targetId]) || (phone && findUser(phone, db));
      if (!user) {
        sendJson(res, 404, { success: false, error: '未找到目标用户' });
        return true;
      }

      const num = Math.floor(Number(delta) || 0);
      const oldCoins = user.coins;

      if (type === 'set') {
        user.coins = Math.max(0, num);
      } else if (type === 'subtract') {
        user.coins = Math.max(0, user.coins - Math.abs(num));
      } else {
        user.coins = Math.max(0, user.coins + Math.abs(num));
      }
      user.updatedAt = Date.now();
      const netDelta = user.coins - oldCoins;
      if (netDelta !== 0) {
        recordServerPointsTx(
          db,
          user.id,
          netDelta >= 0 ? 'admin_grant' : 'admin_deduct',
          `${reason || '管理员调整筹码'} (${netDelta >= 0 ? '+' : ''}${netDelta})`,
          netDelta,
          user.coins
        );
      }

      saveDatabase(db);
      triggerDebouncedTxtBackup(`Web后台调整筹码 (${user.name})`, db);
      recordAdminLog(
        `POINTS_${type.toUpperCase()}`,
        operator,
        user.name,
        `${reason}: ${oldCoins} -> ${user.coins} (变动: ${netDelta >= 0 ? '+' : ''}${netDelta})`,
        netDelta
      );

      if (typeof broadcast === 'function') {
        broadcast({
          type: 'user_coins_updated',
          userId: user.id,
          phone: user.phone,
          username: user.username,
          name: user.name,
          coins: user.coins,
          rating: user.rating,
          level: user.level,
          delta: netDelta,
          operator,
          timestamp: Date.now(),
        });
      }

      sendJson(res, 200, {
        success: true,
        user: sanitizeProfileForClient(user),
        oldCoins,
        newCoins: user.coins,
        delta: netDelta,
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 9b. Admin: Export All Players Points TXT Document (网页端一键下载 TXT 灾备文档)
  if ((pathname === '/api/admin/export-points-txt' || pathname === '/api/admin/points/export-txt') && (method === 'GET' || method === 'POST')) {
    try {
      const db = loadDatabase();
      const { txtContent, fileName, safeFileName } = exportPlayersPointsTxt(db);
      const fileBuffer = Buffer.from(txtContent, 'utf-8');

      res.writeHead(200, {
        'Content-Type': 'text/plain; charset=utf-8',
        'Content-Disposition': `attachment; filename="${safeFileName}"; filename*=UTF-8''${encodeURIComponent(fileName)}`,
        'Cache-Control': 'no-cache, no-store, must-revalidate',
      });
      res.end(fileBuffer);
      return true;
    } catch (e: any) {
      sendJson(res, 500, { success: false, error: String(e.message || e) });
      return true;
    }
  }

  // 10. Admin: Batch Adjust Points for All Users
  if (pathname === '/api/admin/points/batch' && method === 'POST') {
    try {
      const { amount, reason = '全服福利金发放', operator = 'Telegram Admin' } = await parseJsonBody(req);
      const num = Math.floor(Number(amount) || 0);
      if (num === 0) {
        sendJson(res, 400, { success: false, error: '批量调整数量不能为0' });
        return true;
      }
      const db = loadDatabase();
      let affectedCount = 0;
      for (const user of Object.values(db.profiles)) {
        const oldCoins = user.coins;
        user.coins = Math.max(0, user.coins + num);
        user.updatedAt = Date.now();
        const delta = user.coins - oldCoins;
        if (delta !== 0) {
          recordServerPointsTx(
            db,
            user.id,
            delta >= 0 ? 'admin_grant' : 'admin_deduct',
            `${reason} (${delta >= 0 ? '+' : ''}${delta})`,
            delta,
            user.coins
          );
        }
        affectedCount++;
      }
      saveDatabase(db);
      recordAdminLog(
        'POINTS_BATCH',
        operator,
        `全服玩家 (${affectedCount}人)`,
        `${reason}: 每人发放 ${num > 0 ? '+' : ''}${num} 分`,
        num
      );
      if (broadcast) {
        broadcast({
          type: 'chat_broadcast',
          roomId: 'ALL',
          message: {
            id: `sys-${Date.now()}`,
            sender: '茶楼通告',
            avatar: '📢',
            text: `【全服通告】${reason}：所有茶客每人到账 ${num} 分筹码！`,
            timestamp: new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
          },
        });
      }
      sendJson(res, 200, { success: true, affectedCount, amount: num });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 11. Admin: Get Whitelist
  if (pathname === '/api/admin/whitelist' && method === 'GET') {
    try {
      const { allowedPhones, adminPhones } = getAllowedAndAdminPhones();
      sendJson(res, 200, {
        success: true,
        authorizedPhones: allowedPhones,
        adminPhones,
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 12. Admin: Add Whitelist Phone
  if (pathname === '/api/admin/whitelist/add' && method === 'POST') {
    try {
      const { phones, operator = 'Telegram Admin' } = await parseJsonBody(req);
      const rawList = Array.isArray(phones)
        ? phones
        : String(phones || '').split(/[\s,;]+/).map((s) => s.trim()).filter(Boolean);
      const list = rawList.map((p: any) => normalizePhone(String(p))).filter(Boolean);

      if (list.length === 0) {
        sendJson(res, 400, { success: false, error: '请输入有效的手机号' });
        return true;
      }

      const db = loadDatabase();
      const current = new Set((db.authorizedPhones || []).map((p: any) => normalizePhone(p)).filter(Boolean));
      const added: string[] = [];

      for (const p of list) {
        if (!current.has(p)) {
          current.add(p);
          added.push(p);
        }
      }

      // If phone was in revokedPhones, restore it
      if (db.revokedPhones && Array.isArray(db.revokedPhones)) {
        db.revokedPhones = db.revokedPhones.filter((p: string) => !list.includes(normalizePhone(p)));
      }

      db.authorizedPhones = Array.from(current);
      saveDatabase(db);

      recordAdminLog(
        'WHITELIST_ADD',
        operator,
        added.join(', '),
        `将 ${added.length} 个手机号加入 Telegram 授权白名单: ${added.join(', ')}`
      );

      sendJson(res, 200, {
        success: true,
        added,
        totalAuthorized: db.authorizedPhones,
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 13. Admin: Remove Whitelist Phone & Purge Associated Profile Residues
  if (pathname === '/api/admin/whitelist/remove' && method === 'POST') {
    try {
      const { phone, operator = 'Telegram Admin' } = await parseJsonBody(req);
      const cleanPhone = normalizePhone(phone || '');
      if (!cleanPhone) {
        sendJson(res, 400, { success: false, error: '请提供要移出的手机号' });
        return true;
      }
      const db = loadDatabase();
      const current = new Set((db.authorizedPhones || []).map((p: any) => normalizePhone(p)).filter(Boolean));
      current.delete(cleanPhone);
      db.authorizedPhones = Array.from(current);

      const currentAdmin = new Set((db.adminPhones || []).map((p: any) => normalizePhone(p)).filter(Boolean));
      currentAdmin.delete(cleanPhone);
      db.adminPhones = Array.from(currentAdmin);

      if (!db.revokedPhones) db.revokedPhones = [];
      if (!db.revokedPhones.includes(cleanPhone)) {
        db.revokedPhones.push(cleanPhone);
      }

      // Purge registered profiles associated with this phone
      const deletedUsers: string[] = [];
      for (const [pId, profile] of Object.entries(db.profiles)) {
        const pPhone = normalizePhone(profile.phone || '');
        if (pPhone === cleanPhone) {
          deletedUsers.push(`${profile.name} (${pId})`);
          delete db.profiles[pId];
        }
      }

      saveDatabase(db);

      recordAdminLog(
        'WHITELIST_REMOVE',
        operator,
        cleanPhone,
        `将手机号 ${cleanPhone} 移出 Telegram 授权白名单，彻底清理注销账号: ${deletedUsers.length > 0 ? deletedUsers.join(', ') : '无注册用户'}`
      );

      sendJson(res, 200, {
        success: true,
        removed: cleanPhone,
        deletedUsers,
        totalAuthorized: db.authorizedPhones,
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 13b. Admin: Delete User Profile & Purge Authorization
  if (pathname === '/api/admin/user/delete' && method === 'POST') {
    try {
      const { userId, phone, operator = 'Telegram Admin' } = await parseJsonBody(req);
      const db = loadDatabase();
      const user = (userId && db.profiles[userId]) || (phone && findUser(phone, db));
      if (!user) {
        sendJson(res, 404, { success: false, error: '未找到指定用户' });
        return true;
      }

      const pId = user.id;
      const userName = user.name;
      const userPhone = normalizePhone(user.phone || '');
      delete db.profiles[pId];

      if (userPhone) {
        const current = new Set((db.authorizedPhones || []).map((p: any) => normalizePhone(p)).filter(Boolean));
        current.delete(userPhone);
        db.authorizedPhones = Array.from(current);

        const currentAdmin = new Set((db.adminPhones || []).map((p: any) => normalizePhone(p)).filter(Boolean));
        currentAdmin.delete(userPhone);
        db.adminPhones = Array.from(currentAdmin);

        if (!db.revokedPhones) db.revokedPhones = [];
        if (!db.revokedPhones.includes(userPhone)) db.revokedPhones.push(userPhone);
      }

      saveDatabase(db);
      recordAdminLog('USER_DELETE', operator, userName, `管理员彻底删除并注销用户: ${userName} (${pId}, 📱 ${userPhone || '未绑'})`);

      sendJson(res, 200, { success: true, deletedUser: userName, userId: pId });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 14. Admin: Ban / Unban User
  if (pathname === '/api/admin/user/ban' && method === 'POST') {
    try {
      const { userId, phone, isBanned, reason = '违反茶楼守则', operator = 'Telegram Admin' } = await parseJsonBody(req);
      const db = loadDatabase();
      const user = (userId && db.profiles[userId]) || (phone && findUser(phone, db));
      if (!user) {
        sendJson(res, 404, { success: false, error: '未找到指定用户' });
        return true;
      }
      user.isBanned = Boolean(isBanned);
      user.bannedReason = isBanned ? reason : '';
      user.updatedAt = Date.now();
      saveDatabase(db);

      recordAdminLog(
        isBanned ? 'USER_BAN' : 'USER_UNBAN',
        operator,
        user.name,
        isBanned ? `封禁账号 (${user.phone}): ${reason}` : `解封账号 (${user.phone})`
      );

      sendJson(res, 200, { success: true, user });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 15. Admin: Broadcast Announcement
  if (pathname === '/api/admin/broadcast' && method === 'POST') {
    try {
      const { text, author = 'Telegram 管理员' } = await parseJsonBody(req);
      const cleanText = String(text || '').trim();
      if (!cleanText) {
        sendJson(res, 400, { success: false, error: '公告内容不能为空' });
        return true;
      }

      const db = loadDatabase();
      const announcement = {
        id: `ann-${Date.now()}`,
        text: cleanText,
        author,
        timestamp: new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
      };

      db.systemAnnouncements = [announcement, ...(db.systemAnnouncements || []).slice(0, 19)];
      saveDatabase(db);

      recordAdminLog('ANNOUNCEMENT_BROADCAST', author, '全服房间', cleanText);

      if (broadcast) {
        broadcast({
          type: 'chat_broadcast',
          roomId: 'ALL',
          message: {
            id: `broadcast-${Date.now()}`,
            sender: `📢 ${author}`,
            avatar: '🏮',
            text: cleanText,
            timestamp: announcement.timestamp,
          },
        });
      }

      sendJson(res, 200, { success: true, announcement });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 16. Admin: Get Operation Logs
  if (pathname === '/api/admin/logs' && method === 'GET') {
    try {
      const db = loadDatabase();
      sendJson(res, 200, {
        success: true,
        logs: db.adminLogs || [],
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 17. Telegram Command Processor (Direct interactive CLI & Webhook)
  const isWebhookPath = (pathname === '/api/telegram/webhook' || pathname === '/api/telegram/webhook/');
  if ((pathname === '/api/telegram/command' || isWebhookPath) && method === 'POST') {
    try {
      if (!isWebhookPath) {
        const db = loadDatabase();
        if (!authenticateAdmin(req, db)) {
          sendJson(res, 403, { success: false, error: '需要管理员权限方可执行机器人指令' });
          return true;
        }
      }

      // If Telegram Webhook Secret Token is configured in .env, verify header
      const rawSecret = (process.env.TELEGRAM_WEBHOOK_SECRET || '').trim().replace(/^['"]+|['"]+$/g, '');
      if (isWebhookPath && rawSecret) {
        const secretHeader = String(req.headers ? req.headers['x-telegram-bot-api-secret-token'] || '' : '');
        if (!safeTokenEqual(secretHeader, rawSecret)) {
          console.warn('⚠️ [Telegram Webhook] Secret Token 不匹配');
          sendJson(res, 403, { success: false, error: '非法请求：Telegram Webhook 密钥校验失败' });
          return true;
        }
      }

      const body = await parseJsonBody(req);
      const result = await executeTelegramUpdate(body, broadcast, { isWebhook: isWebhookPath });

      // Telegram Webhook Delivery Engine:
      // 极速 100% 键盘菜单交付引擎：
      // 在 Webhook HTTP 200 响应体中直接以 JSON 格式原装返回 method: "sendMessage" + 5 排快捷键盘 + safeText，
      // 100% 避免由于 Termux 本地无法直连 api.telegram.org 导致发信超时卡死、彻底消除消息丢包与键盘遗漏！
      if (isWebhookPath) {
        const messageObj = body.message || body.edited_message || body.channel_post || body.callback_query?.message;
        const targetChatId = messageObj?.chat?.id || body.callback_query?.from?.id || result.chatId;

        const { refreshEnvFromDisk, getTelegramApiBase, syncTelegramBotCommands } = await import('./telegramBotService.ts');
        refreshEnvFromDisk();
        const botToken = (process.env.TELEGRAM_BOT_TOKEN || '').trim().replace(/^['"]+|['"]+$/g, '');

        if (targetChatId && result.reply) {
          const plainText = result.reply.replace(/[*_`\[\]]/g, '');
          console.log(`⚡ [Telegram Webhook] 正在通过 HTTP 200 响应体即时交付回复与 5 排快捷键盘 (chatId: ${targetChatId})`);

          // 若为 /start 或 /help 指令，自动触发后台同步注册官方 [/] 指令菜单与 ChatMenuButton
          if (result.command.startsWith('/start') || result.command.startsWith('/help')) {
            syncTelegramBotCommands(botToken).catch(() => {});
          }

          // 同步毫秒级返回 Webhook JSON 载荷给 Telegram 官方服务器，Telegram 官方秒级渲染键盘与消息 (排他性返回，杜绝消息重复)
          sendJson(res, 200, {
            method: 'sendMessage',
            chat_id: targetChatId,
            text: plainText,
            reply_markup: result.replyMarkup,
          });
          return true;
        }

        sendJson(res, 200, { ok: true, processed: true });
        return true;
      }

      sendJson(res, 200, result);
    } catch (e) {
      console.error('[Telegram Webhook Error]', e);
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 17.5. Telegram: Explicit Command & Keyboard Menu Synchronization Endpoint (管理员鉴权)
  if ((pathname === '/api/telegram/sync-commands' || pathname === '/api/telegram/sync') && (method === 'POST' || method === 'GET')) {
    try {
      const db = loadDatabase();
      if (!authenticateAdmin(req, db)) {
        sendJson(res, 403, { success: false, error: '需要管理员权限方可同步机器人菜单' });
        return true;
      }
      const { syncTelegramBotCommands } = await import('./telegramBotService.ts');
      const syncResult = await syncTelegramBotCommands();
      sendJson(res, 200, {
        success: syncResult.success,
        message: syncResult.success
          ? '🟢 成功向 Telegram 官方注册 14 条下拉菜单指令与 5 排快捷键盘菜单 (setMyCommands & setChatMenuButton)！'
          : `⚠️ 同步 Telegram 键盘菜单提示: ${syncResult.error || '未知错误'}`,
        error: syncResult.error,
      });
    } catch (e: any) {
      sendJson(res, 500, { success: false, error: String(e.message || e) });
    }
    return true;
  }

  // 18. Telegram: Set Webhook Helper Endpoint & One-Click Activation (管理员鉴权，禁止回显明文 Token)
  if ((pathname === '/api/telegram/set-webhook' || pathname === '/api/telegram/register') && (method === 'POST' || method === 'GET')) {
    try {
      const db = loadDatabase();
      if (!authenticateAdmin(req, db)) {
        sendJson(res, 403, { success: false, error: '需要管理员权限方可配置或激活 Telegram Webhook' });
        return true;
      }

      const { refreshEnvFromDisk, normalizePublicUrl, stopTelegramPolling, getTelegramApiBase, initTelegramBotService, syncTelegramBotCommands } = await import('./telegramBotService.ts');
      refreshEnvFromDisk();

      const body = method === 'POST' ? await parseJsonBody(req).catch(() => ({})) : {};
      const queryToken = urlObj.searchParams.get('token');
      const providedToken = (queryToken || body.token || '').trim().replace(/^['"]+|['"]+$/g, '');

      if (providedToken) {
        saveEnvVariable('TELEGRAM_BOT_TOKEN', providedToken);
        process.env.TELEGRAM_BOT_TOKEN = providedToken;
        console.log(`🤖 [Telegram] 自动更新并保存 TELEGRAM_BOT_TOKEN: ${providedToken.slice(0, 5)}...`);
      }

      const token = (process.env.TELEGRAM_BOT_TOKEN || providedToken || '').trim().replace(/^['"]+|['"]+$/g, '');

      const queryUrl = urlObj.searchParams.get('url');
      const targetAppUrl = normalizePublicUrl(queryUrl || body.url || process.env.URL || process.env.APP_URL || process.env.WEBHOOK_URL || '');
      const webhookUrl = targetAppUrl ? `${targetAppUrl}/api/telegram/webhook` : '';
      const dropPending = urlObj.searchParams.get('drop_pending') !== 'false';

      const rawSecret = (process.env.TELEGRAM_WEBHOOK_SECRET || '').trim().replace(/^['"]+|['"]+$/g, '');
      const validSecret = /^[A-Za-z0-9_-]{1,256}$/.test(rawSecret) ? rawSecret : '';
      const secretParam = validSecret ? `&secret_token=${encodeURIComponent(validSecret)}` : '';

      let serverOk = false;
      let serverData: any = null;
      let serverErrorMsg: string | null = null;
      let botMe: any = null;

      if (token && targetAppUrl) {
        try {
          const apiBase = getTelegramApiBase();
          const serverFetchUrl = `${apiBase}/bot${token}/setWebhook?url=${encodeURIComponent(
            webhookUrl
          )}${secretParam}&drop_pending_updates=${dropPending}&allowed_updates=${encodeURIComponent(
            JSON.stringify(['message', 'edited_message', 'channel_post', 'callback_query'])
          )}`;

          const tgResp = await fetch(serverFetchUrl, { signal: AbortSignal.timeout(6000) });
          serverData = await tgResp.json().catch(() => ({}));
          if (serverData && serverData.ok) {
            serverOk = true;
            stopTelegramPolling();
            const meRes = await fetch(`${apiBase}/bot${token}/getMe`, { signal: AbortSignal.timeout(4000) });
            const meData = await meRes.json().catch(() => ({}));
            if (meData?.ok) botMe = meData.result;
            initTelegramBotService(broadcast).catch(() => {});
            syncTelegramBotCommands(token, apiBase).catch(() => {});
          } else {
            serverErrorMsg = serverData?.description || 'Telegram 官方返回未成功';
          }
        } catch (err: any) {
          serverErrorMsg = err?.message || String(err);
        }
      }

      const isHtmlRequest = Boolean(req.headers?.accept?.includes('text/html'));

      if (isHtmlRequest) {
        const pageHtml = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Telegram Bot Webhook 全自动激活中枢</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0f172a; color: #f8fafc; margin: 0; padding: 24px; display: flex; justify-content: center; align-items: center; min-height: 100vh; box-sizing: border-box; }
    .card { background: #1e293b; border: 1px solid #334155; border-radius: 20px; max-width: 680px; width: 100%; padding: 32px; box-shadow: 0 25px 30px -5px rgba(0, 0, 0, 0.6); }
    h1 { margin-top: 0; font-size: 22px; color: #38bdf8; display: flex; align-items: center; gap: 8px; }
    .status-badge { display: inline-block; padding: 4px 12px; border-radius: 9999px; font-size: 13px; font-weight: 700; }
    .status-success { background: #065f46; color: #34d399; }
    .status-pending { background: #854d0e; color: #fde047; }
    .url-box { background: #0f172a; border: 1px solid #334155; padding: 12px; border-radius: 10px; font-family: monospace; font-size: 12px; word-break: break-all; color: #94a3b8; margin: 10px 0; }
    .input-field { width: 100%; box-sizing: border-box; background: #0f172a; border: 1px solid #475569; border-radius: 10px; padding: 12px; font-size: 14px; color: #f8fafc; font-family: monospace; margin: 8px 0 16px 0; }
    .btn { display: inline-block; padding: 12px 24px; border-radius: 10px; font-weight: 700; cursor: pointer; font-size: 14px; border: none; background: #059669; color: #fff; width: 100%; }
  </style>
</head>
<body>
  <div class="card">
    <h1>🤖 Telegram Bot Webhook 激活中枢</h1>
    <p>
      当前激活状态:
      ${serverOk
        ? `<span class="status-badge status-success">🟢 Webhook 已激活 (账号: @${botMe?.username || '国粹棋牌Bot'})</span>`
        : (token
            ? '<span class="status-badge status-pending">⚡ 已配置 Token（待服务端直连注册）</span>'
            : '<span class="status-badge status-pending">🔑 尚未配置 Bot Token</span>')}
    </p>
    <form method="POST" action="/api/telegram/set-webhook" style="margin: 20px 0; padding: 18px; background: #141f32; border-radius: 14px; border: 1px solid #25334d;">
      <label style="font-size: 13px; font-weight: 600; color: #cbd5e1;">🔑 更新 Telegram Bot Token (不回显已有密钥):</label>
      <input type="password" name="token" class="input-field" placeholder="粘贴新 Token 以覆盖更新..." required />
      <button type="submit" class="btn">🚀 保存并激活 Webhook</button>
    </form>
    <p style="font-size: 12px; color: #94a3b8; margin-bottom: 4px;">当前绑定的回调入口 (Webhook URL):</p>
    <div class="url-box">${webhookUrl || '请先在 .env 中设置 URL'}</div>
  </div>
</body>
</html>`;
        sendHtml(res, 200, pageHtml);
        return true;
      }

      sendJson(res, 200, {
        success: serverOk,
        mode: serverOk ? 'server_registered' : 'pending_server_registration',
        tokenConfigured: Boolean(token),
        message: serverOk
          ? 'Webhook 已成功在 Telegram 官方服务器注册并生效！'
          : '服务端直连注册未完成，请确认服务器公网 URL 与网络/反代可达性。',
        configuredWebhookUrl: webhookUrl,
        serverFetchError: serverErrorMsg,
      });
    } catch (e) {
      sendJson(res, 200, {
        success: false,
        error: String(e),
      });
    }
    return true;
  }

  // 19. Telegram: Get Webhook Info Helper Endpoint (管理员鉴权 + 密钥脱敏)
  if (pathname === '/api/telegram/webhook-info' && method === 'GET') {
    try {
      const db = loadDatabase();
      if (!authenticateAdmin(req, db)) {
        sendJson(res, 403, { success: false, error: '需要管理员权限方可查看 Webhook 诊断信息' });
        return true;
      }

      const { refreshEnvFromDisk, normalizePublicUrl, getTelegramApiBase } = await import('./telegramBotService.ts');
      refreshEnvFromDisk();

      const token = (process.env.TELEGRAM_BOT_TOKEN || '').trim().replace(/^['"]+|['"]+$/g, '');
      if (!token) {
        sendJson(res, 400, {
          success: false,
          error: '环境变量未配置 TELEGRAM_BOT_TOKEN，请先在 .env 中设置',
        });
        return true;
      }

      const envRawUrl = (process.env.URL || process.env.APP_URL || process.env.WEBHOOK_URL || '').trim();
      const normalizedEnvUrl = normalizePublicUrl(envRawUrl);
      const expectedWebhookUrl = normalizedEnvUrl ? `${normalizedEnvUrl}/api/telegram/webhook` : '';

      let serverFetchOk = false;
      let tgData: any = null;

      try {
        const apiBase = getTelegramApiBase();
        const tgResp = await fetch(`${apiBase}/bot${token}/getWebhookInfo`, {
          signal: AbortSignal.timeout(3500),
        });
        tgData = await tgResp.json().catch(() => ({}));
        if (tgData && tgData.ok) {
          serverFetchOk = true;
        }
      } catch {}

      if (serverFetchOk && tgData) {
        const info = tgData.result || {};
        const isRegistered = Boolean(info.url && info.url.length > 0);
        const isUrlMatched = expectedWebhookUrl ? (info.url === expectedWebhookUrl) : false;

        let diagnosis = '正常';
        if (!isRegistered) {
          diagnosis = 'Telegram 官方未注册任何 Webhook 回调地址。请访问 /api/telegram/set-webhook 激活！';
        } else if (!isUrlMatched) {
          diagnosis = `URL 不匹配：Telegram 登记的地址为 [${info.url}]，而当前配置的是 [${expectedWebhookUrl}]。请访问 /api/telegram/set-webhook 重新登记！`;
        } else if (info.last_error_message) {
          diagnosis = `Telegram 官方上报错误: "${info.last_error_message}"。若为 502/Bad Gateway 说明隧道未连通本地 3000 端口；若为 Connection timeout 说明隧道中断。`;
        } else {
          diagnosis = '🟢 Webhook 挂载状态完全正常，Telegram 官方可毫秒级直推消息！';
        }

        sendJson(res, 200, {
          success: true,
          envUrl: envRawUrl,
          normalizedEnvUrl,
          expectedWebhookUrl,
          registeredWebhookUrl: info.url || '未注册 (为空)',
          isUrlMatched,
          hasPendingUpdates: info.pending_update_count || 0,
          lastErrorDate: info.last_error_date ? new Date(info.last_error_date * 1000).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }) : null,
          lastErrorMessage: info.last_error_message || null,
          diagnosis,
          webhookInfo: info,
        });
      } else {
        sendJson(res, 200, {
          success: false,
          networkNotice: '本地服务器直连 Telegram 官方受限，请检查公网反代或网络连通性。',
          expectedWebhookUrl,
          envUrl: envRawUrl,
        });
      }
    } catch (e) {
      sendJson(res, 200, { success: false, error: String(e) });
    }
    return true;
  }

  // 20. Telegram: Bot Status Endpoint (公开基础状态，连通性自检需管理员权限)
  if (pathname === '/api/telegram/status' && method === 'GET') {
    try {
      const { getTelegramBotStatus, getTelegramApiBase } = await import('./telegramBotService.ts');
      const apiBase = getTelegramApiBase();
      const status = getTelegramBotStatus();

      let connectionTest: any = null;
      if (urlObj.searchParams.get('test') === '1') {
        const db = loadDatabase();
        if (!authenticateAdmin(req, db)) {
          sendJson(res, 403, { success: false, error: '需要管理员权限方可执行机器人连通性测试' });
          return true;
        }
        const token = (process.env.TELEGRAM_BOT_TOKEN || '').trim().replace(/^['"]+|['"]+$/g, '');
        if (token) {
          try {
            const t0 = Date.now();
            const meRes = await fetch(`${apiBase}/bot${token}/getMe`, { signal: AbortSignal.timeout(8000) });
            const meData = await meRes.json().catch(() => ({}));
            connectionTest = {
              ok: meData?.ok || false,
              latencyMs: Date.now() - t0,
              username: meData?.result?.username || undefined,
            };
          } catch (tErr: any) {
            connectionTest = {
              ok: false,
              error: tErr?.message || String(tErr),
            };
          }
        }
      }

      sendJson(res, 200, {
        success: true,
        bot: status,
        connectionTest,
      });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  // 21. Telegram: Manually start Long Polling (管理员鉴权)
  if ((pathname === '/api/telegram/start-polling' || pathname === '/api/telegram/polling') && (method === 'POST' || method === 'GET')) {
    try {
      const db = loadDatabase();
      if (!authenticateAdmin(req, db)) {
        sendJson(res, 403, { success: false, error: '需要管理员权限方可切换长轮询模式' });
        return true;
      }
      const token = process.env.TELEGRAM_BOT_TOKEN;
      if (!token) {
        sendJson(res, 400, { success: false, error: '未配置 TELEGRAM_BOT_TOKEN' });
        return true;
      }
      const { startTelegramPolling } = await import('./telegramBotService.ts');
      await startTelegramPolling(token, broadcast);
      sendJson(res, 200, { success: true, message: '已成功启动长轮询模式 (Long Polling)' });
    } catch (e) {
      sendJson(res, 500, { success: false, error: String(e) });
    }
    return true;
  }

  return false;
}
