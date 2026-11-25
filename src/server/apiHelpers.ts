import fs from 'fs';
import path from 'path';
import crypto from 'crypto';
import { loadDatabase, type UserProfileData } from '../serverStorage.ts';
import { presenceTracker } from '../presenceTracker.ts';
import { initServerCarriageTables } from '../games/shisanshui/bookingTrain.ts';
import type { ServerBookingCarriage } from '../games/shisanshui/types.ts';

const BOOKING_FILE = path.resolve(process.cwd(), 'data', 'booking_carriages.json');

let bookingCarriageLockChain: Promise<void> = Promise.resolve();
export function withBookingCarriagesLock<T>(fn: () => Promise<T> | T): Promise<T> {
  const run = bookingCarriageLockChain.then(() => fn());
  bookingCarriageLockChain = run.then(
    () => undefined,
    () => undefined
  );
  return run;
}

export function loadBookingCarriagesFromDisk(): Record<string, ServerBookingCarriage> {
  try {
    if (fs.existsSync(BOOKING_FILE)) {
      const raw = fs.readFileSync(BOOKING_FILE, 'utf-8');
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed.carriages === 'object') {
        return parsed.carriages;
      }
    }
  } catch (err) {
    console.warn('[Booking Data] Read disk error:', err);
  }
  return {};
}

export function saveBookingCarriagesToDisk(carriages: Record<string, ServerBookingCarriage>): void {
  try {
    const dataDir = path.dirname(BOOKING_FILE);
    if (!fs.existsSync(dataDir)) {
      fs.mkdirSync(dataDir, { recursive: true });
    }
    const tempFile = `${BOOKING_FILE}.tmp.${Date.now()}_${Math.random().toString(36).slice(2, 6)}`;
    fs.writeFileSync(tempFile, JSON.stringify({ carriages, updatedAt: Date.now() }, null, 2), 'utf-8');
    fs.renameSync(tempFile, BOOKING_FILE);
  } catch (err) {
    console.error('[Booking Data] Save disk error:', err);
  }
}

export function getOrInitServerCarriage(sceneMode: 'four-booking' | 'eight-booking', carriageNumber: number = 1): ServerBookingCarriage {
  const carriages = loadBookingCarriagesFromDisk();
  const key = `${sceneMode}_car_${carriageNumber}`;

  if (!carriages[key] || !Array.isArray(carriages[key].tables) || carriages[key].tables.length === 0) {
    const newCarriage = initServerCarriageTables(carriageNumber, sceneMode);
    carriages[key] = newCarriage;
    saveBookingCarriagesToDisk(carriages);
  }

  return carriages[key];
}

function findEnvFilePath(): string | null {
  const candidates = [
    process.env.HOME ? path.resolve(process.env.HOME, '.env') : '',
    path.resolve(process.cwd(), '..', '.env'),
    path.resolve(process.cwd(), '.env'),
  ].filter(Boolean);

  for (const p of candidates) {
    if (fs.existsSync(p)) return p;
  }
  return null;
}

export function getGithubRepoName(): string {
  try {
    const gitConfigPath = path.resolve(process.cwd(), '.git', 'config');
    if (fs.existsSync(gitConfigPath)) {
      const content = fs.readFileSync(gitConfigPath, 'utf-8');
      const match = content.match(/github\.com[:\/]([^\s\/]+\/[^\s\/\.]+)/);
      if (match && match[1]) {
        return match[1].replace(/\.git$/, '');
      }
    }
  } catch {}
  return 'wen775866/yy';
}

export function saveEnvVariable(key: string, value: string): void {
  process.env[key] = value;
  const envPath = findEnvFilePath() || (process.env.HOME ? path.resolve(process.env.HOME, '.env') : path.resolve(process.cwd(), '.env'));
  try {
    let content = fs.existsSync(envPath) ? fs.readFileSync(envPath, 'utf-8') : '';
    const regex = new RegExp(`^${key}\\s*=.*$`, 'm');
    if (regex.test(content)) {
      content = content.replace(regex, `${key}="${value}"`);
    } else {
      content = content.trimEnd() + `\n${key}="${value}"\n`;
    }
    fs.writeFileSync(envPath, content, 'utf-8');
    console.log(`💾 [Env] 已将配置 ${key} 成功持久化写入 ${envPath}`);
  } catch (err) {
    console.warn(`[Env] 写入配置 ${key} 失败:`, err);
  }
}

function getAdminUsernamesFromEnv(): string[] {
  const candidates = [
    process.env.HOME ? path.resolve(process.env.HOME, '.env') : '',
    path.resolve(process.cwd(), '..', '.env'),
    path.resolve(process.cwd(), '.env'),
  ].filter(Boolean);

  try {
    for (const envPath of candidates) {
      if (!fs.existsSync(envPath)) continue;
      const content = fs.readFileSync(envPath, 'utf-8');
      const lines = content.split('\n');
      const usernames: string[] = [];
      for (const line of lines) {
        const match = line.match(/^([A-Z_]+)\s*=\s*(['"]?)([^'"]*)\2/i);
        if (match) {
          const key = match[1].toUpperCase();
          const val = match[3];
          if (key.includes('ADMIN') && (key.includes('USER') || key.includes('NAME'))) {
            const vals = val.split(/[\s,;]+/);
            for (const v of vals) {
              const cleaned = v.trim().replace(/^@/, '');
              if (cleaned && cleaned !== 'admin_username') usernames.push(cleaned);
            }
          }
        }
      }
      if (usernames.length > 0) return usernames;
    }
  } catch {}

  const envUsernames = process.env.ADMIN_USERNAMES || '';
  return envUsernames.split(/[\s,;]+/).map((s) => s.trim().replace(/^@/, '')).filter(Boolean);
}

export function normalizePhone(raw: string): string {
  let p = String(raw || '').trim().replace(/\D/g, '');
  if (p.length === 13 && p.startsWith('86')) {
    p = p.slice(2);
  }
  return p;
}

export function getAllowedAndAdminPhones(): { allowedPhones: string[]; adminPhones: string[]; adminUsernames: string[] } {
  const db = loadDatabase();
  const adminUsernames = getAdminUsernamesFromEnv();
  const revokedSet = new Set<string>((db.revokedPhones || []).map((p: any) => normalizePhone(p)));

  const allowedSet = new Set<string>(
    (db.authorizedPhones || [])
      .map((p: any) => normalizePhone(p))
      .filter((p: string) => p && !revokedSet.has(p))
  );

  const adminSet = new Set<string>(
    (db.adminPhones || [])
      .map((p: any) => normalizePhone(p))
      .filter((p: string) => p && !revokedSet.has(p))
  );

  return {
    allowedPhones: Array.from(allowedSet),
    adminPhones: Array.from(adminSet),
    adminUsernames,
  };
}

export function findUser(query: string, db = loadDatabase()): UserProfileData | null {
  const rawQ = String(query || '').trim();
  const normQ = normalizePhone(rawQ);
  const q = rawQ.toLowerCase();
  if (!q && !normQ) return null;
  for (const p of Object.values(db.profiles || {})) {
    if (p.id?.toLowerCase() === q) return p;
    if (p.phone) {
      const pNorm = normalizePhone(p.phone);
      if (pNorm && (pNorm === normQ || pNorm.endsWith(normQ) || normQ.endsWith(pNorm))) return p;
    }
    if (p.username && p.username.toLowerCase() === q) return p;
    if (p.name && p.name.toLowerCase() === q) return p;
  }
  return null;
}

export function ensureUserProfile(phone: string, db = loadDatabase(), initialCoins = 0): UserProfileData {
  const cleanPhone = normalizePhone(phone);
  if (!cleanPhone) {
    throw new Error('Invalid phone number');
  }

  if (!db.profiles) db.profiles = {};

  for (const p of Object.values(db.profiles)) {
    const pNorm = normalizePhone(p.phone || '');
    if (pNorm && (pNorm === cleanPhone || pNorm.endsWith(cleanPhone) || cleanPhone.endsWith(pNorm))) {
      return p;
    }
  }

  const { adminPhones } = getAllowedAndAdminPhones();
  const isAdmin = adminPhones.some((ap) => ap === cleanPhone || ap.endsWith(cleanPhone) || cleanPhone.endsWith(ap));

  const newId = `USER-${Math.floor(1000 + Math.random() * 9000)}`;
  const newProfile: UserProfileData = {
    id: newId,
    username: cleanPhone,
    phone: cleanPhone,
    name: `茶客${cleanPhone.slice(-4)}`,
    avatar: '👑',
    isAdmin,
    isBanned: false,
    coins: Math.max(0, initialCoins),
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

  db.profiles[newId] = newProfile;
  return newProfile;
}

export async function parseJsonBody(req: any): Promise<any> {
  if (req.body && typeof req.body === 'object') {
    return req.body;
  }
  return new Promise((resolve, reject) => {
    let data = '';
    req.on('data', (chunk: any) => (data += chunk));
    req.on('end', () => {
      if (!data) return resolve({});
      const contentType = String(req.headers?.['content-type'] || '').toLowerCase();
      if (contentType.includes('application/x-www-form-urlencoded')) {
        try {
          return resolve(Object.fromEntries(new URLSearchParams(data)));
        } catch {}
      }
      try {
        resolve(JSON.parse(data));
      } catch (err) {
        if (data.includes('=')) {
          try {
            return resolve(Object.fromEntries(new URLSearchParams(data)));
          } catch {}
        }
        reject(err);
      }
    });
    req.on('error', reject);
  });
}

export function sendJson(res: any, status: number, body: any) {
  if (typeof res.status === 'function') {
    res.status(status).json(body);
  } else {
    res.statusCode = status;
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify(body));
  }
}

export function sendHtml(res: any, status: number, html: string) {
  if (typeof res.status === 'function' && typeof res.send === 'function') {
    res.status(status).set('Content-Type', 'text/html; charset=utf-8').send(html);
  } else {
    res.statusCode = status;
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    res.end(html);
  }
}

let teahouseRosterTimer: NodeJS.Timeout | null = null;

export function broadcastTeahousePlayersRoster(broadcast?: Function, _db?: any): void {
  if (typeof broadcast !== 'function') return;
  if (teahouseRosterTimer) return;
  teahouseRosterTimer = setTimeout(() => {
    teahouseRosterTimer = null;
    try {
      const database = loadDatabase();
      const freshPlayers = presenceTracker.getAllPlayers(database.profiles || {});
      broadcast({
        type: 'teahouse_players_update',
        count: freshPlayers.length,
        onlineCount: freshPlayers.filter((p: any) => p.isOnline).length,
        players: freshPlayers,
      });
    } catch {}
  }, 300);
}

export function sanitizeProfileForClient(profile: any) {
  if (!profile || typeof profile !== 'object') return profile;
  const { password: _pwd, sessionToken: _tok, ...safe } = profile;
  return safe;
}

export function safeTokenEqual(a: string, b: string): boolean {
  if (!a || !b) return false;
  const bufA = Buffer.from(a, 'utf8');
  const bufB = Buffer.from(b, 'utf8');
  if (bufA.length !== bufB.length) return false;
  return crypto.timingSafeEqual(bufA, bufB);
}

export function extractSessionToken(req: any): string {
  const auth = String(req?.headers?.authorization || '');
  if (auth.toLowerCase().startsWith('bearer ')) {
    return auth.slice(7).trim();
  }
  const hdr = req?.headers?.['x-session-token'];
  return typeof hdr === 'string' ? hdr.trim() : '';
}

export function authenticateRequest(req: any, db: ReturnType<typeof loadDatabase>): UserProfileData | null {
  const token = extractSessionToken(req);
  if (!token) return null;
  for (const p of Object.values(db.profiles || {})) {
    if (p && p.sessionToken && !p.isBanned && safeTokenEqual(p.sessionToken, token)) {
      return p;
    }
  }
  return null;
}

export function authenticateAdmin(req: any, db: ReturnType<typeof loadDatabase>): boolean {
  const envSecret = String(process.env.ADMIN_API_SECRET || '').trim();
  const headerSecret = String(req?.headers?.['x-admin-secret'] || '').trim();
  const authHeader = String(req?.headers?.authorization || '');
  const bearerSecret = authHeader.toLowerCase().startsWith('bearer ') ? authHeader.slice(7).trim() : '';
  if (envSecret && ((headerSecret && safeTokenEqual(headerSecret, envSecret)) || (bearerSecret && safeTokenEqual(bearerSecret, envSecret)))) {
    return true;
  }
  const authUser = authenticateRequest(req, db);
  if (authUser && authUser.isAdmin) {
    return true;
  }
  return false;
}

export function isTrustedOrigin(origin: string, reqHost?: string): boolean {
  if (!origin) return false;
  const trimmed = origin.trim();
  const lower = trimmed.toLowerCase();
  if (
    lower.startsWith('capacitor://') ||
    lower.startsWith('ionic://') ||
    lower === 'file://' ||
    lower === 'null'
  ) {
    return true;
  }
  try {
    const parsed = new URL(trimmed);
    const host = parsed.hostname.toLowerCase();
    if (reqHost) {
      const bareHost = String(reqHost).split(':')[0].toLowerCase();
      if (bareHost && host === bareHost) return true;
    }
    if (
      host === 'localhost' ||
      host === '127.0.0.1' ||
      host === '::1' ||
      host === 'm.my66.cc.cd' ||
      host.endsWith('.trycloudflare.com') ||
      host.endsWith('.ngrok-free.app') ||
      host.endsWith('.ngrok.io') ||
      host.endsWith('.loca.lt') ||
      /^192\.168\.\d+\.\d+$/.test(host) ||
      /^10\.\d+\.\d+\.\d+$/.test(host) ||
      /^172\.(1[6-9]|2\d|3[01])\.\d+\.\d+$/.test(host)
    ) {
      return true;
    }
    const envUrls = [
      process.env.URL,
      process.env.APP_URL,
      process.env.WEBHOOK_URL,
      process.env.SERVER_URL,
      process.env.ALLOWED_ORIGIN,
    ].filter(Boolean);
    for (const eu of envUrls) {
      try {
        const euParsed = new URL(String(eu).trim());
        if (euParsed.hostname.toLowerCase() === host) return true;
      } catch {}
    }
  } catch {}
  return false;
}

export function applyCorsHeaders(req: any, res: any): void {
  const origin = String(req?.headers?.origin || '').trim();
  const reqHost = req?.headers?.host;
  if (origin && isTrustedOrigin(origin, reqHost)) {
    res.setHeader('Access-Control-Allow-Origin', origin);
    res.setHeader('Access-Control-Allow-Credentials', 'true');
    res.setHeader('Vary', 'Origin');
  } else if (!origin) {
    res.setHeader('Access-Control-Allow-Origin', '*');
  } else {
    res.setHeader('Access-Control-Allow-Origin', '*');
    res.setHeader('Vary', 'Origin');
  }
  res.setHeader('Access-Control-Allow-Methods', 'GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH');
  res.setHeader(
    'Access-Control-Allow-Headers',
    'Content-Type, Authorization, X-Session-Token, X-Admin-Secret, X-Requested-With, Range, Accept, Origin, x-telegram-bot-api-secret-token'
  );
  res.setHeader('Access-Control-Max-Age', '86400');
}

