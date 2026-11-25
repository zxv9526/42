import fs from 'fs';
import path from 'path';
import crypto from 'crypto';
import {
  autoArrangeThirteen,
  compareTwoPlayers,
  detectSpecialHand,
  evaluate3CardTier,
  evaluate5CardTier,
  isFoulHand,
  SSSCard,
  ThirteenVersusResult,
} from '../../engine/shisanshuiRules.ts';
import {
  BookingCarriageState,
  BookingSeat,
  BookingSeatOccupant,
  BookingSettlementRecord,
  BookingTableState,
  Card,
  getToday0600CycleInfo,
  SEATS_4,
  SEATS_8,
  SubmittedHandTiers,
  TableSettlementDetail,
  TableSettlementOpponentSummary,
} from '../../engine/shisanshuiBookingTrain.ts';
import { loadDatabase, saveDatabase, recordServerPointsTx } from '../../serverStorage.ts';
import { generateServerShuffledDeck } from './bookingTrain.ts';

export type BookingSceneMode = 'four-booking' | 'eight-booking';

export interface ServerTableSeatSubmission {
  playerId: string;
  playerName: string;
  avatar?: string;
  playerSeat: string;
  playerHand: SubmittedHandTiers;
  submittedAt: number;
  graceFor0600Cycle?: string;
  settledCycleKey?: string;
  settlement?: TableSettlementDetail;
}

export interface ServerMultiBookingTable {
  tableIndex: number;
  tableName: string;
  tableDeckSeed: string;
  seatCards: Record<string, Card[]>;
  seatSubmissions: Record<string, ServerTableSeatSubmission>;
}

export interface ServerPlayerCarriageProgress {
  playerId: string;
  playerName: string;
  avatar?: string;
  selectedSeat: string;
  unplayedTableIndices: number[];
  currentActiveTableIndex: number | null;
  isCompleted: boolean;
  completedAt?: number;
  settledCycleKey?: string;
  simulationCache?: Record<number, TableSettlementDetail>;
}

export const PREDEALT_TARGET_STOCK = 50;
export const PREDEALT_REPLENISH_THRESHOLD = 10;

export interface PredealtTableHands {
  tableIndex: number;
  tableName: string;
  tableDeckSeed: string;
  seatCards: Record<string, Card[]>;
}

export interface PredealtCarriageUnit {
  predealtId: string;
  seq: number;
  sceneMode: BookingSceneMode;
  createdAt: number;
  tables: PredealtTableHands[];
}

export interface ServerMultiBookingCarriage {
  carriageId: string;
  predealtId?: string;
  carriageNumber: number;
  groupIndex: number;
  sceneMode: BookingSceneMode;
  createdAt: number;
  seatOccupants: Record<string, BookingSeatOccupant>;
  playerProgress: Record<string, ServerPlayerCarriageProgress>;
  tables: ServerMultiBookingTable[];
}

export interface ServerBookingStore {
  version: 3;
  updatedAt: number;
  carriages: Record<string, ServerMultiBookingCarriage>;
  userCurrentCarriage: Record<string, number>;
  userCurrentCarriageId: Record<string, string>;
  userSettlementHistory: Record<string, BookingSettlementRecord[]>;
  userUnreadSettlements: Record<string, BookingSettlementRecord[]>;
  predealtPool: Record<BookingSceneMode, PredealtCarriageUnit[]>;
  predealtSeq: Record<BookingSceneMode, number>;
}

const BOOKING_STORE_FILE = path.resolve(process.cwd(), 'data', 'booking_carriages_v2.json');
const BOOKING_STORE_BACKUP_FILE = `${BOOKING_STORE_FILE}.bak`;

let bookingLockChain: Promise<void> = Promise.resolve();

export function withServerBookingLock<T>(fn: () => Promise<T> | T): Promise<T> {
  const run = bookingLockChain.then(() => fn());
  bookingLockChain = run.then(
    () => undefined,
    () => undefined
  );
  return run;
}

function buildCarriageId(
  sceneMode: BookingSceneMode,
  carriageNumber: number,
  groupIndex: number
): string {
  return `${sceneMode}_car_${carriageNumber}_g${groupIndex}`;
}

function parseGroupIndexFromKey(key: string): number {
  const m = /_g(\d+)$/.exec(key);
  return m ? Math.max(1, parseInt(m[1], 10)) : 1;
}

function normalizeStore(parsed: any): ServerBookingStore {
  const carriages: Record<string, ServerMultiBookingCarriage> = {};
  const oldToNewCarriageId = new Map<string, string>();

  const rawCarriages = parsed?.carriages && typeof parsed.carriages === 'object' ? parsed.carriages : {};
  for (const [rawKey, rawCar] of Object.entries<any>(rawCarriages)) {
    if (!rawCar || !Array.isArray(rawCar.tables)) continue;
    const sceneMode: BookingSceneMode = rawCar.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
    const carriageNumber = Math.max(1, Number(rawCar.carriageNumber) || 1);
    const groupIndex = Math.max(1, Number(rawCar.groupIndex) || parseGroupIndexFromKey(rawKey));
    const canonicalId = rawCar.carriageId || buildCarriageId(sceneMode, carriageNumber, groupIndex);
    oldToNewCarriageId.set(rawKey, canonicalId);
    carriages[canonicalId] = {
      carriageId: canonicalId,
      predealtId: rawCar.predealtId,
      carriageNumber,
      groupIndex,
      sceneMode,
      createdAt: rawCar.createdAt || Date.now(),
      seatOccupants: rawCar.seatOccupants || {},
      playerProgress: rawCar.playerProgress || {},
      tables: rawCar.tables,
    };
  }

  const userCurrentCarriage: Record<string, number> =
    parsed?.userCurrentCarriage && typeof parsed.userCurrentCarriage === 'object'
      ? { ...parsed.userCurrentCarriage }
      : {};
  const userCurrentCarriageId: Record<string, string> =
    parsed?.userCurrentCarriageId && typeof parsed.userCurrentCarriageId === 'object'
      ? { ...parsed.userCurrentCarriageId }
      : {};

  // Remap legacy carriageId pointers and backfill missing userCurrentCarriageId
  for (const [uKey, cid] of Object.entries(userCurrentCarriageId)) {
    if (oldToNewCarriageId.has(cid)) {
      userCurrentCarriageId[uKey] = oldToNewCarriageId.get(cid)!;
    }
  }
  for (const [uKey, cNum] of Object.entries(userCurrentCarriage)) {
    if (!userCurrentCarriageId[uKey]) {
      const [userId, modeRaw] = uKey.split('::');
      const sceneMode: BookingSceneMode = modeRaw === 'eight-booking' ? 'eight-booking' : 'four-booking';
      const matching = Object.values(carriages).find(
        (c) => c.sceneMode === sceneMode && c.carriageNumber === Number(cNum) && c.playerProgress?.[userId]
      );
      if (matching) {
        userCurrentCarriageId[uKey] = matching.carriageId;
      } else {
        const legacyCid = buildCarriageId(sceneMode, Number(cNum) || 1, 1);
        if (carriages[legacyCid]) {
          userCurrentCarriageId[uKey] = legacyCid;
        }
      }
    }
  }

  const rawPool = parsed?.predealtPool && typeof parsed.predealtPool === 'object' ? parsed.predealtPool : {};
  const predealtPool: Record<BookingSceneMode, PredealtCarriageUnit[]> = {
    'four-booking': Array.isArray(rawPool['four-booking']) ? rawPool['four-booking'] : [],
    'eight-booking': Array.isArray(rawPool['eight-booking']) ? rawPool['eight-booking'] : [],
  };
  const rawSeq = parsed?.predealtSeq && typeof parsed.predealtSeq === 'object' ? parsed.predealtSeq : {};
  const predealtSeq: Record<BookingSceneMode, number> = {
    'four-booking': Number(rawSeq['four-booking']) || 0,
    'eight-booking': Number(rawSeq['eight-booking']) || 0,
  };

  return {
    version: 3,
    updatedAt: parsed?.updatedAt || Date.now(),
    carriages,
    userCurrentCarriage,
    userCurrentCarriageId,
    userSettlementHistory: parsed?.userSettlementHistory || {},
    userUnreadSettlements: parsed?.userUnreadSettlements || {},
    predealtPool,
    predealtSeq,
  };
}

let bookingStoreCache: ServerBookingStore | null = null;
let bookingSaveTimer: NodeJS.Timeout | null = null;
let bookingPendingSave = false;
let bookingFlushInProgress = false;
let bookingExitHookRegistered = false;

export function flushBookingStoreSync(): void {
  if (!bookingStoreCache || (!bookingPendingSave && !bookingSaveTimer)) return;
  if (bookingSaveTimer) {
    clearTimeout(bookingSaveTimer);
    bookingSaveTimer = null;
  }
  try {
    const dir = path.dirname(BOOKING_STORE_FILE);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    bookingStoreCache.updatedAt = Date.now();
    if (fs.existsSync(BOOKING_STORE_FILE)) {
      try {
        fs.copyFileSync(BOOKING_STORE_FILE, BOOKING_STORE_BACKUP_FILE);
      } catch {}
    }
    const tmpFile = `${BOOKING_STORE_FILE}.tmp.${Date.now()}_${crypto.randomBytes(3).toString('hex')}`;
    fs.writeFileSync(tmpFile, JSON.stringify(bookingStoreCache), 'utf-8');
    fs.renameSync(tmpFile, BOOKING_STORE_FILE);
    bookingPendingSave = false;
  } catch (err) {
    console.error('[ServerBooking] Failed flushing booking store:', err);
  }
}

async function flushBookingStoreAsync(): Promise<void> {
  if (!bookingStoreCache || !bookingPendingSave || bookingFlushInProgress) return;
  bookingFlushInProgress = true;
  bookingPendingSave = false;
  const snapshotJson = JSON.stringify(bookingStoreCache);
  const tmpFile = `${BOOKING_STORE_FILE}.tmp.${Date.now()}_${crypto.randomBytes(3).toString('hex')}`;
  try {
    const dir = path.dirname(BOOKING_STORE_FILE);
    await fs.promises.mkdir(dir, { recursive: true });
    if (fs.existsSync(BOOKING_STORE_FILE)) {
      try {
        await fs.promises.copyFile(BOOKING_STORE_FILE, BOOKING_STORE_BACKUP_FILE);
      } catch {}
    }
    await fs.promises.writeFile(tmpFile, snapshotJson, 'utf-8');
    await fs.promises.rename(tmpFile, BOOKING_STORE_FILE);
  } catch (err) {
    try {
      if (fs.existsSync(tmpFile)) await fs.promises.unlink(tmpFile);
    } catch {}
    bookingPendingSave = true;
    console.error('[ServerBooking] Failed async saving booking store:', err);
  } finally {
    bookingFlushInProgress = false;
    if (bookingPendingSave && !bookingSaveTimer) {
      bookingSaveTimer = setTimeout(() => {
        bookingSaveTimer = null;
        void flushBookingStoreAsync();
      }, 400);
    }
  }
}

function registerBookingExitHooks(): void {
  if (bookingExitHookRegistered) return;
  bookingExitHookRegistered = true;
  const onExit = () => flushBookingStoreSync();
  process.once('beforeExit', onExit);
  process.once('SIGINT', () => {
    onExit();
    process.exit(0);
  });
  process.once('SIGTERM', () => {
    onExit();
    process.exit(0);
  });
}

function loadStore(): ServerBookingStore {
  if (bookingStoreCache) {
    return bookingStoreCache;
  }
  registerBookingExitHooks();
  for (const candidate of [BOOKING_STORE_FILE, BOOKING_STORE_BACKUP_FILE]) {
    try {
      if (fs.existsSync(candidate)) {
        const raw = fs.readFileSync(candidate, 'utf-8');
        const parsed = JSON.parse(raw);
        if (parsed && typeof parsed.carriages === 'object') {
          bookingStoreCache = normalizeStore(parsed);
          return bookingStoreCache;
        }
      }
    } catch (err) {
      console.warn(`[ServerBooking] Failed reading ${path.basename(candidate)}:`, err);
    }
  }
  bookingStoreCache = {
    version: 3,
    updatedAt: Date.now(),
    carriages: {},
    userCurrentCarriage: {},
    userCurrentCarriageId: {},
    userSettlementHistory: {},
    userUnreadSettlements: {},
    predealtPool: {
      'four-booking': [],
      'eight-booking': [],
    },
    predealtSeq: {
      'four-booking': 0,
      'eight-booking': 0,
    },
  };
  return bookingStoreCache;
}

function saveStore(store: ServerBookingStore): void {
  bookingStoreCache = store;
  store.updatedAt = Date.now();
  bookingPendingSave = true;
  if (bookingSaveTimer) {
    clearTimeout(bookingSaveTimer);
  }
  bookingSaveTimer = setTimeout(() => {
    bookingSaveTimer = null;
    void flushBookingStoreAsync();
  }, 400);
}

function archiveOldCompletedCarriages(store: ServerBookingStore): boolean {
  const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;
  const now = Date.now();
  const activePointerIds = new Set(Object.values(store.userCurrentCarriageId || {}));
  const toArchive: ServerMultiBookingCarriage[] = [];

  for (const [cid, car] of Object.entries(store.carriages || {})) {
    if (!car || activePointerIds.has(cid)) continue;
    if (now - (car.createdAt || 0) < SEVEN_DAYS_MS) continue;
    const progresses = Object.values(car.playerProgress || {});
    if (progresses.length === 0) continue;
    const allCompletedAndSettled = progresses.every((p) => p.isCompleted && Boolean(p.settledCycleKey));
    if (allCompletedAndSettled) {
      toArchive.push(car);
    }
  }

  if (toArchive.length === 0) return false;

  try {
    const d = new Date(now);
    const monthTag = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}`;
    const archiveFile = path.resolve(process.cwd(), 'data', `booking_archive_${monthTag}.json`);
    let existingArchive: ServerMultiBookingCarriage[] = [];
    if (fs.existsSync(archiveFile)) {
      try {
        const parsed = JSON.parse(fs.readFileSync(archiveFile, 'utf-8'));
        if (Array.isArray(parsed?.carriages)) existingArchive = parsed.carriages;
      } catch {}
    }
    const existingIds = new Set(existingArchive.map((c) => c.carriageId));
    for (const c of toArchive) {
      if (!existingIds.has(c.carriageId)) {
        existingArchive.push(c);
      }
      delete store.carriages[c.carriageId];
    }
    const tmp = `${archiveFile}.tmp.${now}`;
    fs.writeFileSync(tmp, JSON.stringify({ updatedAt: now, carriages: existingArchive }), 'utf-8');
    fs.renameSync(tmp, archiveFile);
    return true;
  } catch (err) {
    console.warn('[ServerBooking] Archive error:', err);
    return false;
  }
}

function userModeKey(userId: string, sceneMode: BookingSceneMode): string {
  return `${userId}::${sceneMode}`;
}

function getSeatsForMode(sceneMode: BookingSceneMode): string[] {
  return sceneMode === 'eight-booking' ? [...SEATS_8] : [...SEATS_4];
}

function toSSSCard(c: Card): SSSCard {
  const suitMap: Record<string, number> = { '♠': 4, '♥': 3, '♣': 2, '♦': 1 };
  return {
    id: c.id,
    rank: c.rank,
    suitVal: suitMap[c.suit] || 1,
    label: c.label,
    suit: c.suit,
    isRed: c.isRed,
  };
}

function generatePredealtCarriageUnit(
  store: ServerBookingStore,
  sceneMode: BookingSceneMode
): PredealtCarriageUnit {
  const isEight = sceneMode === 'eight-booking';
  const seats = getSeatsForMode(sceneMode);
  const deckCount = isEight ? 2 : 1;
  store.predealtSeq[sceneMode] = (store.predealtSeq[sceneMode] || 0) + 1;
  const seq = store.predealtSeq[sceneMode];
  const predealtId = `pre-${sceneMode}-${String(seq).padStart(5, '0')}`;
  const tables: PredealtTableHands[] = [];

  for (let i = 1; i <= 10; i++) {
    const entropy = crypto.randomBytes(5).toString('hex');
    const seedStr = `srv-${predealtId}-t${i}-${Date.now().toString(36)}-${entropy}`;
    const deck = generateServerShuffledDeck(deckCount, seedStr);
    const seatCards: Record<string, Card[]> = {};
    seats.forEach((seat, idx) => {
      seatCards[seat] = deck.slice(idx * 13, (idx + 1) * 13);
    });
    tables.push({
      tableIndex: i,
      tableName: `第 ${i} 桌`,
      tableDeckSeed: seedStr,
      seatCards,
    });
  }

  return {
    predealtId,
    seq,
    sceneMode,
    createdAt: Date.now(),
    tables,
  };
}

/**
 * 预发牌库自动补货逻辑：
 * 任意模式库存不足 10 节车厢手牌时，自动发牌补满至 50 节车厢（每节 10 桌，每桌 4/8 份完整手牌）
 */
function ensurePredealtInventory(
  store: ServerBookingStore,
  targetMode?: BookingSceneMode
): boolean {
  const modes: BookingSceneMode[] = targetMode ? [targetMode] : ['four-booking', 'eight-booking'];
  let replenished = false;

  for (const mode of modes) {
    if (!Array.isArray(store.predealtPool[mode])) {
      store.predealtPool[mode] = [];
    }
    const pool = store.predealtPool[mode];
    if (pool.length < PREDEALT_REPLENISH_THRESHOLD) {
      const before = pool.length;
      while (pool.length < PREDEALT_TARGET_STOCK) {
        pool.push(generatePredealtCarriageUnit(store, mode));
      }
      replenished = true;
      console.log(
        `[ServerBooking] 预发牌库自动补货 [${mode}]: 库存 ${before} (<${PREDEALT_REPLENISH_THRESHOLD}) -> 已补满至 ${pool.length} 节车厢 (${pool.length * 10} 桌)`
      );
    }
  }

  return replenished;
}

function popPredealtCarriageUnit(
  store: ServerBookingStore,
  sceneMode: BookingSceneMode
): PredealtCarriageUnit {
  ensurePredealtInventory(store, sceneMode);
  const pool = store.predealtPool[sceneMode];
  const unit = pool.shift() || generatePredealtCarriageUnit(store, sceneMode);
  // 提取后若剩余库存不足 10 节，立即发牌补满至 50 节
  if (pool.length < PREDEALT_REPLENISH_THRESHOLD) {
    ensurePredealtInventory(store, sceneMode);
  }
  return unit;
}

function createServerCarriage(
  store: ServerBookingStore,
  carriageNumber: number,
  groupIndex: number,
  sceneMode: BookingSceneMode
): ServerMultiBookingCarriage {
  const carriageId = buildCarriageId(sceneMode, carriageNumber, groupIndex);
  const preUnit = popPredealtCarriageUnit(store, sceneMode);
  const tables: ServerMultiBookingTable[] = preUnit.tables.map((t) => ({
    tableIndex: t.tableIndex,
    tableName: t.tableName,
    tableDeckSeed: t.tableDeckSeed,
    seatCards: t.seatCards,
    seatSubmissions: {},
  }));

  return {
    carriageId,
    predealtId: preUnit.predealtId,
    carriageNumber,
    groupIndex,
    sceneMode,
    createdAt: Date.now(),
    seatOccupants: {},
    playerProgress: {},
    tables,
  };
}

function carriageHasAnySettledSubmission(car: ServerMultiBookingCarriage): boolean {
  for (const t of car.tables || []) {
    for (const sub of Object.values(t.seatSubmissions || {})) {
      if (sub && sub.settledCycleKey) return true;
    }
  }
  return false;
}

/**
 * 核心机制：无论先进入的玩家已经游玩了多少节车厢（第2、3、N场次），
 * 每位后续新加入的玩家始终从 carriageNumber = 1（第1场次 / 第1节车厢）开始！
 * 同一场次号（carriageNumber）内按 groupIndex（1号车组、2号车组...）自动分批：
 * - 若当前车组仍有空席且未经历过历史06:00结算，则优先同厢异步对决；
 * - 若当前车组已坐满（4/8人）或已进入过历史结算，则自动在同一个 carriageNumber 下从50节预发牌库提取下一并行车组，绝不把新玩家推到第2/3场次。
 */
function findOrCreateCarriageAtNumber(
  store: ServerBookingStore,
  sceneMode: BookingSceneMode,
  userId: string,
  targetCarriageNumber: number,
  requireAllEmpty = false
): { carriage: ServerMultiBookingCarriage; created: boolean } {
  const num = Math.max(1, targetCarriageNumber);
  const maxSeats = getSeatsForMode(sceneMode).length;

  // 1. 若用户已在此场次号下的某一车组中存在进度，优先恢复该车组
  if (!requireAllEmpty) {
    const existingUserCarriage = Object.values(store.carriages)
      .filter((c) => c && c.sceneMode === sceneMode && c.carriageNumber === num && c.playerProgress?.[userId])
      .sort((a, b) => a.groupIndex - b.groupIndex)[0];
    if (existingUserCarriage) {
      return { carriage: existingUserCarriage, created: false };
    }
  }

  // 2. 在该场次号下的所有车组中按 groupIndex 升序查找可用车组
  let g = 1;
  while (g < 10000) {
    const cId = buildCarriageId(sceneMode, num, g);
    const car = store.carriages[cId];
    if (!car) {
      const fresh = createServerCarriage(store, num, g, sceneMode);
      store.carriages[cId] = fresh;
      return { carriage: fresh, created: true };
    }
    const occupiedCount = Object.keys(car.seatOccupants || {}).length;
    const progressCount = Object.keys(car.playerProgress || {}).length;
    if (requireAllEmpty) {
      if (occupiedCount === 0 && progressCount === 0) {
        return { carriage: car, created: false };
      }
    } else {
      const occupiedByOthers = Object.values(car.seatOccupants || {}).filter(
        (occ) => occ && occ.playerId !== userId
      ).length;
      if (occupiedByOthers < maxSeats && !carriageHasAnySettledSubmission(car)) {
        return { carriage: car, created: false };
      }
    }
    g++;
  }

  const fallback = createServerCarriage(store, num, g, sceneMode);
  store.carriages[fallback.carriageId] = fallback;
  return { carriage: fallback, created: true };
}

function resolveUserCurrentCarriage(
  store: ServerBookingStore,
  sceneMode: BookingSceneMode,
  userId: string,
  options: { requestedCarriageId?: string; requestedCarriageNumber?: number; preserveRequestedCarriageId?: boolean } = {}
): { carriage: ServerMultiBookingCarriage; dirty: boolean } {
  const uKey = userModeKey(userId, sceneMode);
  const maxSeats = getSeatsForMode(sceneMode).length;
  let dirty = false;

  if (options.requestedCarriageId && store.carriages[options.requestedCarriageId]) {
    const candidate = store.carriages[options.requestedCarriageId];
    if (candidate.sceneMode === sceneMode) {
      if (candidate.playerProgress?.[userId] || options.preserveRequestedCarriageId) {
        return { carriage: candidate, dirty };
      }
      const occupiedByOthers = Object.values(candidate.seatOccupants || {}).filter(
        (occ) => occ && occ.playerId !== userId
      ).length;
      if (occupiedByOthers < maxSeats && !carriageHasAnySettledSubmission(candidate)) {
        return { carriage: candidate, dirty };
      }
    }
  }

  if (options.requestedCarriageNumber) {
    const found = findOrCreateCarriageAtNumber(
      store,
      sceneMode,
      userId,
      options.requestedCarriageNumber,
      false
    );
    if (found.created) dirty = true;
    if (store.userCurrentCarriageId[uKey] !== found.carriage.carriageId) {
      store.userCurrentCarriageId[uKey] = found.carriage.carriageId;
      store.userCurrentCarriage[uKey] = found.carriage.carriageNumber;
      dirty = true;
    }
    return { carriage: found.carriage, dirty };
  }

  const savedId = store.userCurrentCarriageId[uKey];
  if (savedId && store.carriages[savedId] && store.carriages[savedId].sceneMode === sceneMode) {
    const savedCar = store.carriages[savedId];
    if (savedCar.playerProgress?.[userId]) {
      return { carriage: savedCar, dirty };
    }
    const occupiedByOthers = Object.values(savedCar.seatOccupants || {}).filter(
      (occ) => occ && occ.playerId !== userId
    ).length;
    if (occupiedByOthers < maxSeats && !carriageHasAnySettledSubmission(savedCar)) {
      return { carriage: savedCar, dirty };
    }
    // 未选座时若所浏览的车厢已被他人占满或已过结算周期，自动切往同场次下一个有空位的车组
    const nextOpen = findOrCreateCarriageAtNumber(
      store,
      sceneMode,
      userId,
      savedCar.carriageNumber,
      false
    );
    store.userCurrentCarriageId[uKey] = nextOpen.carriage.carriageId;
    store.userCurrentCarriage[uKey] = nextOpen.carriage.carriageNumber;
    dirty = true;
    return { carriage: nextOpen.carriage, dirty };
  }

  // 若尚无指针，查找玩家已有进度的最高车厢；完全新玩家从第 1 场次（第 1 节车厢）开始
  const userCarriages = Object.values(store.carriages)
    .filter((c) => c && c.sceneMode === sceneMode && c.playerProgress?.[userId])
    .sort((a, b) => b.carriageNumber - a.carriageNumber || b.groupIndex - a.groupIndex);

  if (userCarriages.length > 0) {
    const latest = userCarriages[0];
    store.userCurrentCarriageId[uKey] = latest.carriageId;
    store.userCurrentCarriage[uKey] = latest.carriageNumber;
    dirty = true;
    return { carriage: latest, dirty };
  }

  const first = findOrCreateCarriageAtNumber(store, sceneMode, userId, 1, false);
  store.userCurrentCarriageId[uKey] = first.carriage.carriageId;
  store.userCurrentCarriage[uKey] = first.carriage.carriageNumber;
  dirty = true;
  return { carriage: first.carriage, dirty };
}

function buildSettlementHistoryForMode(
  store: ServerBookingStore,
  userId: string,
  sceneMode: BookingSceneMode
): BookingSettlementRecord[] {
  return (store.userSettlementHistory[userId] || []).filter(
    (rec) => !rec.sceneMode || rec.sceneMode === sceneMode
  );
}

function projectCarriageForUser(
  carriage: ServerMultiBookingCarriage,
  userId: string
): BookingCarriageState {
  const progress = carriage.playerProgress?.[userId];
  const mySeat = progress?.selectedSeat || null;
  const activeIdx = progress?.currentActiveTableIndex ?? null;

  const tables: BookingTableState[] = carriage.tables.map((t) => {
    const mySub = mySeat ? t.seatSubmissions?.[mySeat] : undefined;
    const realOpponentsSubmittedSeats = Object.keys(t.seatSubmissions || {}).filter(
      (s) => s !== mySeat
    );

    // 仅下发玩家本人坐席的真实手牌（严防在比水前泄露出其他空席或对手底牌）
    const safeSeatCards: Record<string, Card[]> = {};
    if (mySeat && Array.isArray(t.seatCards?.[mySeat])) {
      safeSeatCards[mySeat] = t.seatCards[mySeat];
    }

    const previewSettlement = mySub?.settlement || progress?.simulationCache?.[t.tableIndex];

    return {
      tableIndex: t.tableIndex,
      tableName: t.tableName,
      isSubmitted: Boolean(mySub),
      submittedAt: mySub?.submittedAt,
      playerSeat: mySub?.playerSeat || mySeat || '',
      playerHand: mySub?.playerHand,
      tableDeckSeed: t.tableDeckSeed,
      seatCards: safeSeatCards,
      settlement: previewSettlement,
      realOpponentsSubmittedSeats,
      settledCycleKey: mySub?.settledCycleKey,
      graceFor0600Cycle: mySub?.graceFor0600Cycle,
    };
  });

  const unplayedTableIndices = progress
    ? [...progress.unplayedTableIndices]
    : [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];

  return {
    carriageId: carriage.carriageId,
    predealtId: carriage.predealtId,
    carriageNumber: carriage.carriageNumber,
    groupIndex: carriage.groupIndex || 1,
    sceneMode: carriage.sceneMode,
    selectedSeat: mySeat,
    tables,
    unplayedTableIndices,
    currentActiveTableIndex: activeIdx,
    isCompleted: progress ? Boolean(progress.isCompleted) : false,
    completedAt: progress?.completedAt,
    seatOccupants: { ...(carriage.seatOccupants || {}) },
    settledCycleKey: progress?.settledCycleKey,
  };
}

function buildArchivedCarriagesForUser(
  store: ServerBookingStore,
  sceneMode: BookingSceneMode,
  userId: string
): BookingCarriageState[] {
  const list: BookingCarriageState[] = [];
  for (const car of Object.values(store.carriages)) {
    if (!car || car.sceneMode !== sceneMode) continue;
    if (car.playerProgress?.[userId]) {
      list.push(projectCarriageForUser(car, userId));
    }
  }
  return list.sort((a, b) => b.carriageNumber - a.carriageNumber);
}

function settleTableAgainstCarriageOpponents(
  table: ServerMultiBookingTable,
  mySeat: string,
  mySub: ServerTableSeatSubmission,
  sceneMode: BookingSceneMode,
  isSimulation: boolean
): TableSettlementDetail {
  const allSeats = getSeatsForMode(sceneMode);
  const otherSeats = allSeats.filter((s) => s !== mySeat);

  const myHandSSS = {
    head: mySub.playerHand.front.map(toSSSCard),
    mid: mySub.playerHand.mid.map(toSSSCard),
    tail: mySub.playerHand.back.map(toSSSCard),
  };

  const breakdowns: ThirteenVersusResult[] = [];
  const opponentSummaries: TableSettlementOpponentSummary[] = [];
  let totalWater = 0;
  let gunshotWinsCount = 0;

  otherSeats.forEach((seat, idx) => {
    const realOpp = table.seatSubmissions?.[seat];
    let oppHead: SSSCard[];
    let oppMid: SSSCard[];
    let oppTail: SSSCard[];
    let frontLabel: string;
    let midLabel: string;
    let backLabel: string;
    let specialHandName: string | undefined;
    let oppName: string;
    let isRealPlayer = false;

    if (realOpp && realOpp.playerHand) {
      isRealPlayer = true;
      oppHead = realOpp.playerHand.front.map(toSSSCard);
      oppMid = realOpp.playerHand.mid.map(toSSSCard);
      oppTail = realOpp.playerHand.back.map(toSSSCard);
      frontLabel = realOpp.playerHand.frontLabel || evaluate3CardTier(oppHead).name;
      midLabel = realOpp.playerHand.midLabel || evaluate5CardTier(oppMid).name;
      backLabel = realOpp.playerHand.backLabel || evaluate5CardTier(oppTail).name;
      specialHandName = realOpp.playerHand.specialHand?.name;
      oppName = `【${seat}位】${realOpp.playerName || '牌友'}`;
    } else {
      const oppCards = (table.seatCards?.[seat] || []).map(toSSSCard);
      const opt = autoArrangeThirteen(oppCards, 'optimal');
      oppHead = opt.head;
      oppMid = opt.mid;
      oppTail = opt.tail;
      frontLabel = evaluate3CardTier(oppHead).name;
      midLabel = evaluate5CardTier(oppMid).name;
      backLabel = evaluate5CardTier(oppTail).name;
      specialHandName = opt.specialHand?.name;
      oppName = `【${seat}位】系统预设`;
    }

    const vsResult = compareTwoPlayers(
      myHandSSS,
      { head: oppHead, mid: oppMid, tail: oppTail },
      idx + 1,
      oppName
    );

    breakdowns.push(vsResult);
    opponentSummaries.push({
      oppSeat: idx + 1,
      seatLabel: seat,
      oppName,
      isRealPlayer,
      frontLabel,
      midLabel,
      backLabel,
      specialHandName,
      finalWater: vsResult.finalWater,
      isShot: vsResult.isShot,
      isFoulOpp: vsResult.isFoulOpp,
    });
    totalWater += vsResult.finalWater;
    if (vsResult.isShot && vsResult.finalWater > 0) {
      gunshotWinsCount++;
    }
  });

  const isGrandSlam = gunshotWinsCount === otherSeats.length && otherSeats.length > 0;
  if (isGrandSlam) {
    totalWater *= 2;
  }

  const netScore = totalWater;
  return {
    isSettled: !isSimulation,
    totalWater,
    netScore,
    isGrandSlam,
    opponentsBreakdown: breakdowns,
    opponentSummaries,
    settledAt: Date.now(),
  };
}

export interface SettledUserNotification {
  userId: string;
  record: BookingSettlementRecord;
  coins: number;
  delta: number;
  rating: number;
  level: number;
}

export function run0600SettlementSweepInternal(
  store: ServerBookingStore,
  options: {
    onlyUserId?: string;
    forceIncludeGraceForUserId?: string;
    nowTs?: number;
  } = {}
): { settledNotifications: SettledUserNotification[]; changed: boolean } {
  const nowTs = options.nowTs || Date.now();
  const nowDate = new Date(nowTs);
  const { today0600Ts, latestCutoffTs, latestCycleKey } = getToday0600CycleInfo(nowDate);
  const settledNotifications: SettledUserNotification[] = [];
  let changed = false;

  if (nowTs < latestCutoffTs) {
    return { settledNotifications, changed: false };
  }

  // 在 06:00:00 ~ 06:01:10 期间，若有玩家仍在当前牌桌理牌，等待其延时提交或65秒兜底超时
  const inGraceWindow = nowTs >= today0600Ts && nowTs < today0600Ts + 70_000;

  let db: ReturnType<typeof loadDatabase> | null = null;
  let dbDirty = false;

  const sortedCarriages = Object.values(store.carriages).sort(
    (a, b) => a.carriageNumber - b.carriageNumber || (a.groupIndex || 1) - (b.groupIndex || 1)
  );

  for (const carriage of sortedCarriages) {
    if (!carriage || !carriage.playerProgress) continue;
    for (const [userId, progress] of Object.entries(carriage.playerProgress)) {
      if (!progress || !progress.selectedSeat) continue;
      if (options.onlyUserId && userId !== options.onlyUserId) continue;

      const mySeat = progress.selectedSeat;
      if (
        inGraceWindow &&
        progress.currentActiveTableIndex !== null &&
        options.forceIncludeGraceForUserId !== userId
      ) {
        continue;
      }

      const dueTables: { table: ServerMultiBookingTable; sub: ServerTableSeatSubmission }[] = [];
      for (const table of carriage.tables || []) {
        const sub = table.seatSubmissions?.[mySeat];
        if (!sub || sub.playerId !== userId) continue;
        if (sub.settledCycleKey) continue;
        if ((sub.submittedAt || 0) < latestCutoffTs || sub.graceFor0600Cycle === latestCycleKey) {
          dueTables.push({ table, sub });
        }
      }

      if (dueTables.length === 0) continue;

      let sumWater = 0;
      let sumScore = 0;
      const tablesSummary: BookingSettlementRecord['tablesSummary'] = [];

      for (const { table, sub } of dueTables) {
        const detail = settleTableAgainstCarriageOpponents(
          table,
          mySeat,
          sub,
          carriage.sceneMode,
          false
        );
        sub.settlement = detail;
        sub.settledCycleKey = latestCycleKey;
        if (progress.simulationCache) {
          progress.simulationCache[table.tableIndex] = detail;
        }
        sumWater += detail.totalWater;
        sumScore += detail.netScore;
        tablesSummary.push({
          tableIndex: table.tableIndex,
          playerSeat: mySeat,
          totalWater: detail.totalWater,
          specialHandName: sub.playerHand.specialHand?.name,
        });
      }

      progress.settledCycleKey = latestCycleKey;
      changed = true;

      const record: BookingSettlementRecord = {
        id: `srv-settle-${carriage.carriageNumber}-${userId}-${Date.now().toString(36)}`,
        dateStr: nowDate.toLocaleDateString('zh-CN'),
        sceneMode: carriage.sceneMode,
        carriageNumber: carriage.carriageNumber,
        settledTablesCount: dueTables.length,
        totalWater: sumWater,
        netScore: sumScore,
        settledAt: nowTs,
        isSimulation: false,
        settledCycleKey: latestCycleKey,
        coinsCredited: false,
        tablesSummary,
      };

      if (!db) db = loadDatabase();
      const profile = db.profiles?.[userId];
      let updatedCoins = 0;
      let updatedRating = 1800;
      let updatedLevel = 3;

      if (profile) {
        const delta = Math.trunc(sumScore);
        profile.coins = Math.max(0, (profile.coins || 0) + delta);
        const ratingDelta = Math.round(sumWater * 1.2);
        profile.rating = Math.max(1000, (profile.rating || 1800) + ratingDelta);
        profile.level = Math.max(1, Math.min(10, Math.floor(profile.rating / 500)));
        if (!profile.stats) profile.stats = {};
        if (!profile.stats.shisanshui) {
          profile.stats.shisanshui = { played: 0, won: 0, highestScore: 0 };
        }
        profile.stats.shisanshui.played += dueTables.length;
        if (sumScore > 0) {
          profile.stats.shisanshui.won += dueTables.filter((d) => (d.sub.settlement?.totalWater || 0) > 0).length;
          profile.stats.shisanshui.highestScore = Math.max(
            profile.stats.shisanshui.highestScore || 0,
            sumScore
          );
        }
        profile.updatedAt = nowTs;
        updatedCoins = profile.coins;
        updatedRating = profile.rating;
        updatedLevel = profile.level;
        record.coinsCredited = true;

        if (!Array.isArray(db.records)) db.records = [];
        db.records.unshift({
          id: record.id,
          gameType: 'shisanshui',
          gameName: carriage.sceneMode === 'eight-booking' ? '十三张·八人预约专列' : '十三张·四人预约专列',
          result: sumScore > 0 ? 'win' : sumScore < 0 ? 'lose' : 'draw',
          coinsDelta: delta,
          ratingDelta,
          details: `第${carriage.carriageNumber}节车厢 (${dueTables.length}桌06:00结算) · 净胜 ${sumWater >= 0 ? `+${sumWater}` : sumWater} 水`,
          timestamp: new Date(nowTs).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
          updatedAt: nowTs,
        });
        if (db.records.length > 500) db.records = db.records.slice(0, 500);
        recordServerPointsTx(
          db,
          userId,
          delta >= 0 ? 'game_win' : 'game_loss',
          `🌅 十三张预约专列 06:00 结算 (第${carriage.carriageNumber}车厢·${dueTables.length}桌)`,
          delta,
          profile.coins
        );
        dbDirty = true;

        settledNotifications.push({
          userId,
          record,
          coins: updatedCoins,
          delta,
          rating: updatedRating,
          level: updatedLevel,
        });
      }

      if (!store.userSettlementHistory[userId]) {
        store.userSettlementHistory[userId] = [];
      }
      store.userSettlementHistory[userId].unshift(record);
      if (store.userSettlementHistory[userId].length > 50) {
        store.userSettlementHistory[userId] = store.userSettlementHistory[userId].slice(0, 50);
      }

      if (!store.userUnreadSettlements[userId]) {
        store.userUnreadSettlements[userId] = [];
      }
      store.userUnreadSettlements[userId].unshift(record);
      if (store.userUnreadSettlements[userId].length > 10) {
        store.userUnreadSettlements[userId] = store.userUnreadSettlements[userId].slice(0, 10);
      }
    }
  }

  if (dbDirty && db) {
    saveDatabase(db);
  }

  return { settledNotifications, changed };
}

export async function runServer0600SettlementSweep(options?: {
  onlyUserId?: string;
  forceIncludeGraceForUserId?: string;
}): Promise<SettledUserNotification[]> {
  return withServerBookingLock(() => {
    const store = loadStore();
    const { settledNotifications, changed } = run0600SettlementSweepInternal(store, options);
    if (changed) {
      saveStore(store);
    }
    return settledNotifications;
  });
}

export async function initServerBookingInventory(): Promise<{
  fourBookingStock: number;
  eightBookingStock: number;
  replenished: boolean;
}> {
  return withServerBookingLock(() => {
    const store = loadStore();
    const replenished = ensurePredealtInventory(store);
    if (replenished) {
      saveStore(store);
    }
    return {
      fourBookingStock: store.predealtPool['four-booking'].length,
      eightBookingStock: store.predealtPool['eight-booking'].length,
      replenished,
    };
  });
}

export async function getServerBookingState(params: {
  sceneMode: BookingSceneMode;
  userId: string;
  playerName?: string;
  avatar?: string;
  requestedCarriageId?: string;
  requestedCarriageNumber?: number;
}) {
  return withServerBookingLock(() => {
    const { sceneMode, userId } = params;
    const store = loadStore();

    // 确保预发牌库保持 10~50 节车厢库存（不足10自动补满50）
    const stockReplenished = ensurePredealtInventory(store, sceneMode);

    // 先检查并结算该用户所有已到期的 06:00 历史桌次
    const { settledNotifications, changed: settledChanged } = run0600SettlementSweepInternal(store, {
      onlyUserId: userId,
    });

    const { carriage, dirty: resolvedDirty } = resolveUserCurrentCarriage(store, sceneMode, userId, {
      requestedCarriageId: params.requestedCarriageId,
      requestedCarriageNumber: params.requestedCarriageNumber,
    });

    if (stockReplenished || settledChanged || resolvedDirty) {
      saveStore(store);
    }

    const unreadSettlements = store.userUnreadSettlements[userId] || [];
    const archivedOld = archiveOldCompletedCarriages(store);
    if (archivedOld) {
      saveStore(store);
    }

    return {
      carriage: projectCarriageForUser(carriage, userId),
      archivedCarriages: buildArchivedCarriagesForUser(store, sceneMode, userId),
      settlementHistory: buildSettlementHistoryForMode(store, userId, sceneMode),
      unreadSettlements,
      settledNotifications,
      inventoryRemaining: store.predealtPool[sceneMode].length,
      serverTs: Date.now(),
    };
  });
}

export async function ackServerUnreadSettlements(userId: string): Promise<void> {
  return withServerBookingLock(() => {
    const store = loadStore();
    if (store.userUnreadSettlements[userId]?.length) {
      delete store.userUnreadSettlements[userId];
      saveStore(store);
    }
  });
}

export async function selectServerBookingSeat(params: {
  sceneMode: BookingSceneMode;
  userId: string;
  playerName: string;
  avatar?: string;
  seat: BookingSeat;
  carriageId?: string;
  carriageNumber?: number;
}) {
  return withServerBookingLock(() => {
    const { sceneMode, userId, playerName, avatar, seat } = params;
    const validSeats = getSeatsForMode(sceneMode);
    if (!validSeats.includes(seat)) {
      return { ok: false as const, error: '无效的坐席方位' };
    }

    const db = loadDatabase();
    const profile = db.profiles?.[userId];
    const minCoins = sceneMode === 'eight-booking' ? 500 : 100;
    if (profile && (profile.coins || 0) < minCoins) {
      return {
        ok: false as const,
        error: `您的可用积分不足（当前 ${profile.coins || 0} 分，需至少 ${minCoins} 分方可入座预约专列）`,
      };
    }

    const store = loadStore();
    ensurePredealtInventory(store, sceneMode);
    const uKey = userModeKey(userId, sceneMode);
    const { carriage } = resolveUserCurrentCarriage(store, sceneMode, userId, {
      requestedCarriageId: params.carriageId,
      requestedCarriageNumber: params.carriageNumber,
      preserveRequestedCarriageId: true,
    });

    const existingOwner = carriage.seatOccupants[seat];
    if (existingOwner && existingOwner.playerId !== userId) {
      return {
        ok: false as const,
        error: `【${seat}位】已有玩家 ${existingOwner.playerName} 入座，请选择其他空位或开设新节空车厢！`,
        carriage: projectCarriageForUser(carriage, userId),
      };
    }

    const existingProgress = carriage.playerProgress[userId];
    if (existingProgress && existingProgress.selectedSeat !== seat) {
      const submittedCount = 10 - existingProgress.unplayedTableIndices.length;
      if (submittedCount > 0) {
        return {
          ok: false as const,
          error: `您已以【${existingProgress.selectedSeat}位】提交过 ${submittedCount} 桌，本节车厢不可中途换座！`,
          carriage: projectCarriageForUser(carriage, userId),
        };
      }
      if (carriage.seatOccupants[existingProgress.selectedSeat]?.playerId === userId) {
        delete carriage.seatOccupants[existingProgress.selectedSeat];
      }
    }

    carriage.seatOccupants[seat] = {
      seat,
      playerId: userId,
      playerName: playerName || '茶客',
      avatar: avatar || '👑',
      selectedAt: existingOwner?.selectedAt || Date.now(),
      submittedCount: existingProgress ? 10 - existingProgress.unplayedTableIndices.length : 0,
    };

    let progress = carriage.playerProgress[userId];
    if (!progress) {
      const unplayed = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
      const chosen = unplayed[Math.floor(Math.random() * unplayed.length)];
      progress = {
        playerId: userId,
        playerName: playerName || '茶客',
        avatar: avatar || '👑',
        selectedSeat: seat,
        unplayedTableIndices: unplayed,
        currentActiveTableIndex: chosen,
        isCompleted: false,
      };
      carriage.playerProgress[userId] = progress;
    } else {
      progress.selectedSeat = seat;
      progress.playerName = playerName || progress.playerName;
      progress.avatar = avatar || progress.avatar;
      if (!progress.currentActiveTableIndex && progress.unplayedTableIndices.length > 0) {
        progress.currentActiveTableIndex =
          progress.unplayedTableIndices[
            Math.floor(Math.random() * progress.unplayedTableIndices.length)
          ];
      }
    }

    store.userCurrentCarriageId[uKey] = carriage.carriageId;
    store.userCurrentCarriage[uKey] = carriage.carriageNumber;
    saveStore(store);

    return {
      ok: true as const,
      carriage: projectCarriageForUser(carriage, userId),
      archivedCarriages: buildArchivedCarriagesForUser(store, sceneMode, userId),
      rawSeatOccupants: carriage.seatOccupants,
      carriageId: carriage.carriageId,
      carriageNumber: carriage.carriageNumber,
      serverTs: Date.now(),
    };
  });
}

export async function pickServerBookingTable(params: {
  sceneMode: BookingSceneMode;
  userId: string;
  carriageId?: string;
  carriageNumber?: number;
  specificTableIndex?: number;
}) {
  return withServerBookingLock(() => {
    const { sceneMode, userId, specificTableIndex } = params;
    const store = loadStore();
    const { carriage } = resolveUserCurrentCarriage(store, sceneMode, userId, {
      requestedCarriageId: params.carriageId,
      requestedCarriageNumber: params.carriageNumber,
    });

    if (!carriage || !carriage.playerProgress[userId]) {
      return { ok: false as const, error: '请先选定坐席入座' };
    }

    const progress = carriage.playerProgress[userId];
    if (progress.unplayedTableIndices.length === 0) {
      progress.currentActiveTableIndex = null;
      progress.isCompleted = true;
      saveStore(store);
      return {
        ok: true as const,
        carriage: projectCarriageForUser(carriage, userId),
        archivedCarriages: buildArchivedCarriagesForUser(store, sceneMode, userId),
        serverTs: Date.now(),
      };
    }

    const chosen =
      typeof specificTableIndex === 'number' &&
      progress.unplayedTableIndices.includes(specificTableIndex)
        ? specificTableIndex
        : progress.unplayedTableIndices[
            Math.floor(Math.random() * progress.unplayedTableIndices.length)
          ];

    progress.currentActiveTableIndex = chosen;
    saveStore(store);

    return {
      ok: true as const,
      carriage: projectCarriageForUser(carriage, userId),
      archivedCarriages: buildArchivedCarriagesForUser(store, sceneMode, userId),
      serverTs: Date.now(),
    };
  });
}

export async function submitServerBookingHand(params: {
  sceneMode: BookingSceneMode;
  userId: string;
  playerName: string;
  avatar?: string;
  carriageId?: string;
  carriageNumber?: number;
  tableIndex?: number;
  playerSeat?: string;
  handTiers: SubmittedHandTiers;
  isGraceSubmission?: boolean;
}) {
  return withServerBookingLock(() => {
    const { sceneMode, userId, playerName, avatar, handTiers, isGraceSubmission } = params;
    const store = loadStore();
    const { carriage } = resolveUserCurrentCarriage(store, sceneMode, userId, {
      requestedCarriageId: params.carriageId,
      requestedCarriageNumber: params.carriageNumber,
    });

    if (!carriage) {
      return { ok: false as const, error: '未找到对应车厢数据' };
    }

    const progress = carriage.playerProgress[userId];
    if (!progress || !progress.selectedSeat) {
      return { ok: false as const, error: '请先选择车厢坐席后再提交手牌' };
    }

    const seat = progress.selectedSeat;
    const tIdx = Number(params.tableIndex) || progress.currentActiveTableIndex || 1;
    const table = carriage.tables.find((t) => t.tableIndex === tIdx);
    if (!table) {
      return { ok: false as const, error: '未找到对应牌桌' };
    }

    if (table.seatSubmissions[seat]) {
      return { ok: false as const, error: `第 ${tIdx} 桌【${seat}位】已封牌提交，不可重复提交` };
    }

    const front = Array.isArray(handTiers?.front) ? handTiers.front : [];
    const mid = Array.isArray(handTiers?.mid) ? handTiers.mid : [];
    const back = Array.isArray(handTiers?.back) ? handTiers.back : [];
    if (front.length !== 3 || mid.length !== 5 || back.length !== 5) {
      return { ok: false as const, error: '摆牌张数不正确（需头道3张、中道5张、尾道5张）' };
    }

    const expectedHand = table.seatCards[seat] || [];
    const expectedIdSet = new Set(expectedHand.map((c) => c.id));
    const cardLookup = new Map(expectedHand.map((c) => [c.id, c]));
    const submittedIds = [...front, ...mid, ...back].map((c) => c?.id).filter(Boolean);
    const uniqueSubmitted = new Set(submittedIds);

    if (
      uniqueSubmitted.size !== 13 ||
      expectedIdSet.size !== 13 ||
      submittedIds.some((id) => !expectedIdSet.has(id))
    ) {
      return { ok: false as const, error: '提交手牌与服务端发牌不一致，请重新理牌后提交' };
    }

    const authFront = front.map((c) => cardLookup.get(c.id)!);
    const authMid = mid.map((c) => cardLookup.get(c.id)!);
    const authBack = back.map((c) => cardLookup.get(c.id)!);

    const sssFront = authFront.map(toSSSCard);
    const sssMid = authMid.map(toSSSCard);
    const sssBack = authBack.map(toSSSCard);
    const specialHand = detectSpecialHand([...sssFront, ...sssMid, ...sssBack]);

    if (!specialHand && isFoulHand(sssFront, sssMid, sssBack)) {
      return { ok: false as const, error: '当前手牌摆放倒水相公（尾道需 >= 中道 >= 头道），请调整后再提交' };
    }

    const nowTs = Date.now();
    const { today0600Ts, latestCycleKey } = getToday0600CycleInfo(new Date(nowTs));
    const withinGraceWindow = nowTs >= today0600Ts && nowTs < today0600Ts + 90_000;

    const authHandTiers: SubmittedHandTiers = {
      front: authFront,
      mid: authMid,
      back: authBack,
      frontLabel: evaluate3CardTier(sssFront).name,
      midLabel: evaluate5CardTier(sssMid).name,
      backLabel: evaluate5CardTier(sssBack).name,
      specialHand,
    };

    const sub: ServerTableSeatSubmission = {
      playerId: userId,
      playerName: playerName || progress.playerName || '茶客',
      avatar: avatar || progress.avatar || '👑',
      playerSeat: seat,
      playerHand: authHandTiers,
      submittedAt: nowTs,
      graceFor0600Cycle: isGraceSubmission || withinGraceWindow ? latestCycleKey : undefined,
    };

    table.seatSubmissions[seat] = sub;
    progress.unplayedTableIndices = progress.unplayedTableIndices.filter((idx) => idx !== tIdx);
    progress.currentActiveTableIndex = null;
    if (progress.unplayedTableIndices.length === 0) {
      progress.isCompleted = true;
      progress.completedAt = nowTs;
    }

    if (carriage.seatOccupants[seat]) {
      carriage.seatOccupants[seat].submittedCount = 10 - progress.unplayedTableIndices.length;
    }

    let settledNotifications: SettledUserNotification[] = [];
    if (isGraceSubmission) {
      const sweepRes = run0600SettlementSweepInternal(store, {
        onlyUserId: userId,
        forceIncludeGraceForUserId: userId,
        nowTs,
      });
      settledNotifications = sweepRes.settledNotifications;
    }

    saveStore(store);

    const projected = projectCarriageForUser(carriage, userId);
    const submittedTable = projected.tables.find((t) => t.tableIndex === tIdx)!;

    return {
      ok: true as const,
      carriage: projected,
      submittedTable,
      isCarriageCompleted: progress.isCompleted,
      remainingCount: progress.unplayedTableIndices.length,
      archivedCarriages: buildArchivedCarriagesForUser(store, sceneMode, userId),
      settlementHistory: buildSettlementHistoryForMode(store, userId, sceneMode),
      settledNotifications,
      carriageId: carriage.carriageId,
      carriageNumber: carriage.carriageNumber,
      tableIndex: tIdx,
      seat,
      seatOccupants: carriage.seatOccupants,
      serverTs: Date.now(),
    };
  });
}

export async function advanceServerBookingCarriage(params: {
  sceneMode: BookingSceneMode;
  userId: string;
  playerName?: string;
  avatar?: string;
  createFresh?: boolean;
}) {
  return withServerBookingLock(() => {
    const { sceneMode, userId, createFresh } = params;
    const store = loadStore();
    ensurePredealtInventory(store, sceneMode);
    const uKey = userModeKey(userId, sceneMode);
    const { carriage: curCar } = resolveUserCurrentCarriage(store, sceneMode, userId);
    const curProg = curCar.playerProgress?.[userId];
    const submittedCount = curProg ? 10 - curProg.unplayedTableIndices.length : 0;

    let releasedSeatInfo: {
      carriageId: string;
      carriageNumber: number;
      seatOccupants: Record<string, BookingSeatOccupant>;
    } | null = null;

    let targetNumber: number;
    if (createFresh && submittedCount === 0) {
      // 尚未在当前场次提交过任何桌却想换一节全新空车厢：释放占用的空座，保留在同一个场次号（例如仍为第1场次）新开空车组
      if (curProg?.selectedSeat && curCar.seatOccupants?.[curProg.selectedSeat]?.playerId === userId) {
        delete curCar.seatOccupants[curProg.selectedSeat];
        releasedSeatInfo = {
          carriageId: curCar.carriageId,
          carriageNumber: curCar.carriageNumber,
          seatOccupants: { ...(curCar.seatOccupants || {}) },
        };
      }
      if (curProg) {
        delete curCar.playerProgress[userId];
      }
      targetNumber = curCar.carriageNumber;
    } else {
      targetNumber = curCar.carriageNumber + 1;
    }

    const { carriage: nextCar } = findOrCreateCarriageAtNumber(
      store,
      sceneMode,
      userId,
      targetNumber,
      Boolean(createFresh)
    );
    store.userCurrentCarriageId[uKey] = nextCar.carriageId;
    store.userCurrentCarriage[uKey] = nextCar.carriageNumber;
    saveStore(store);

    return {
      ok: true as const,
      carriage: projectCarriageForUser(nextCar, userId),
      archivedCarriages: buildArchivedCarriagesForUser(store, sceneMode, userId),
      releasedSeatInfo,
      inventoryRemaining: store.predealtPool[sceneMode].length,
      serverTs: Date.now(),
    };
  });
}

export async function simulateServerBookingSettlement(params: {
  sceneMode: BookingSceneMode;
  userId: string;
  carriageId?: string;
  carriageNumber?: number;
}) {
  return withServerBookingLock(() => {
    const { sceneMode, userId } = params;
    const store = loadStore();
    const { carriage } = resolveUserCurrentCarriage(store, sceneMode, userId, {
      requestedCarriageId: params.carriageId,
      requestedCarriageNumber: params.carriageNumber,
    });

    if (!carriage) {
      return { ok: false as const, error: '未找到指定车厢' };
    }

    const progress = carriage.playerProgress?.[userId];
    const mySeat = progress?.selectedSeat;
    if (!progress || !mySeat) {
      return { ok: false as const, error: '您尚未在本节车厢选座开局' };
    }

    if (!progress.simulationCache) {
      progress.simulationCache = {};
    }

    const tablesSummary: BookingSettlementRecord['tablesSummary'] = [];
    let sumWater = 0;
    let sumScore = 0;
    let settledCount = 0;

    for (const table of carriage.tables) {
      const sub = table.seatSubmissions?.[mySeat];
      if (!sub || sub.playerId !== userId) continue;
      const detail = settleTableAgainstCarriageOpponents(
        table,
        mySeat,
        sub,
        sceneMode,
        true
      );
      progress.simulationCache[table.tableIndex] = detail;
      sumWater += detail.totalWater;
      sumScore += detail.netScore;
      settledCount++;
      tablesSummary.push({
        tableIndex: table.tableIndex,
        playerSeat: mySeat,
        totalWater: detail.totalWater,
        specialHandName: sub.playerHand.specialHand?.name,
      });
    }

    saveStore(store);

    const record: BookingSettlementRecord = {
      id: `sim-settle-${Date.now().toString(36)}`,
      dateStr: new Date().toLocaleDateString('zh-CN'),
      sceneMode,
      carriageNumber: carriage.carriageNumber,
      settledTablesCount: settledCount,
      totalWater: sumWater,
      netScore: sumScore,
      settledAt: Date.now(),
      isSimulation: true,
      coinsCredited: false,
      tablesSummary,
    };

    return {
      ok: true as const,
      record,
      carriage: projectCarriageForUser(carriage, userId),
      archivedCarriages: buildArchivedCarriagesForUser(store, sceneMode, userId),
      serverTs: Date.now(),
    };
  });
}
