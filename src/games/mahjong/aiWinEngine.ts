export interface MahjongTile {
  id: string;
  type: 'wan' | 'tiao' | 'tong' | 'zi';
  value: number; // 1..9 (zi: 1..7 for 东南西北中发白)
  text: string;
  color: string;
  discardBy?: number;
  hidden?: boolean;
  isRedDragon?: boolean;
}

export interface WinningBreakdown {
  fans: string[];
  totalFan: number;
  rewardCoins: number;
  maCount: number;
  rewardMaTiles: MahjongTile[];
  hitMaCount: number;
  payoutPerOpponent: number;
  isFourHongzhong?: boolean;
  kongBonusTotal?: number;
  myKongNet?: number;
  opponentPayouts?: number[];
  winnerSeat?: number;
  netCoinsPerSeat?: number[];
  winningHand?: MahjongTile[];
  winningMelds?: MahjongTile[][];
  winnerNameSnapshot?: string;
}

export const TILE_CHARS: Record<string, string[]> = {
  wan: ['一万', '二万', '三万', '四万', '五万', '六万', '七万', '八万', '九万'],
  tiao: ['一条', '二条', '三条', '四条', '五条', '六条', '七条', '八条', '九条'],
  tong: ['一筒', '二筒', '三筒', '四筒', '五筒', '六筒', '七筒', '八筒', '九筒'],
  zi: ['东风', '南风', '西风', '北风', '红中', '发财', '白板'],
};

export const ALL_TILE_KINDS = [
  ...Array.from({ length: 9 }, (_, i) => ({ type: 'wan' as const, value: i + 1, text: TILE_CHARS.wan[i] })),
  ...Array.from({ length: 9 }, (_, i) => ({ type: 'tiao' as const, value: i + 1, text: TILE_CHARS.tiao[i] })),
  ...Array.from({ length: 9 }, (_, i) => ({ type: 'tong' as const, value: i + 1, text: TILE_CHARS.tong[i] })),
  ...Array.from({ length: 7 }, (_, i) => ({ type: 'zi' as const, value: i + 1, text: TILE_CHARS.zi[i] })),
];

export function isHongzhong(tile: MahjongTile | null | undefined): boolean {
  if (!tile) return false;
  return (tile.type === 'zi' && tile.value === 5) || tile.text === '红中';
}

export function isYiWuJiuMaTile(t: MahjongTile, isFourHongzhong = false): boolean {
  if (isFourHongzhong) return true;
  if (t.type !== 'zi') {
    return t.value === 1 || t.value === 5 || t.value === 9;
  }
  return t.value === 1 || t.value === 5 || t.text === '东风' || t.text === '红中';
}

export function sortMahjongHand(hand: MahjongTile[]): MahjongTile[] {
  const typeOrder = { wan: 0, tiao: 1, tong: 2, zi: 3 };
  return [...hand].sort((a, b) => typeOrder[a.type] - typeOrder[b.type] || a.value - b.value);
}

export function createMahjongTilePool(): MahjongTile[] {
  const pool: MahjongTile[] = [];
  (['wan', 'tiao', 'tong'] as const).forEach((t) => {
    for (let v = 1; v <= 9; v++) {
      for (let copy = 0; copy < 4; copy++) {
        pool.push({
          id: `${t}-${v}-${copy}`,
          type: t,
          value: v,
          text: TILE_CHARS[t][v - 1],
          color: t === 'wan' ? '#DC2626' : (t === 'tiao' ? '#16A34A' : '#2563EB'),
        });
      }
    }
  });
  for (let v = 1; v <= 7; v++) {
    for (let copy = 0; copy < 4; copy++) {
      pool.push({
        id: `zi-${v}-${copy}`,
        type: 'zi',
        value: v,
        text: TILE_CHARS.zi[v - 1],
        color: v === 5 ? '#DC2626' : (v === 6 ? '#16A34A' : '#475569'),
      });
    }
  }
  return pool.sort(() => Math.random() - 0.5);
}

export function normalizeServerMelds(rawMelds: any[]): MahjongTile[][] {
  if (!Array.isArray(rawMelds)) return [];
  return rawMelds
    .map((m: any) => {
      if (Array.isArray(m)) return m;
      if (Array.isArray(m?.tiles) && m.tiles.length > 0) return m.tiles;
      if (m && m.tile) {
        const cnt = m.type === 'gang' ? 4 : 3;
        return Array.from({ length: cnt }, (_, i) => ({ ...m.tile, id: `${m.tile.id}-m-${i}` }));
      }
      return [];
    })
    .filter((arr: MahjongTile[]) => arr.length > 0);
}

export function canDecomposeIntoMelds(vals: number[], isZi = false): boolean {
  if (vals.length === 0) return true;
  if (vals.length % 3 !== 0) return false;

  const arr = [...vals].sort((a, b) => a - b);
  if (arr[0] === arr[1] && arr[1] === arr[2]) {
    const nextArr = arr.slice(3);
    if (canDecomposeIntoMelds(nextArr, isZi)) return true;
  }
  if (!isZi) {
    const v = arr[0];
    const idx2 = arr.indexOf(v + 1);
    const idx3 = arr.indexOf(v + 2);
    if (idx2 !== -1 && idx3 !== -1) {
      const nextArr = [...arr];
      nextArr.splice(idx3, 1);
      nextArr.splice(idx2, 1);
      nextArr.splice(0, 1);
      if (canDecomposeIntoMelds(nextArr, isZi)) return true;
    }
  }
  return false;
}

export function checkMahjongWinLocal(hand: Pick<MahjongTile, 'type' | 'value'>[]): boolean {
  if (hand.length % 3 !== 2) return false;

  if (hand.length === 14) {
    const counts = new Map<string, number>();
    hand.forEach((c) => counts.set(`${c.type}-${c.value}`, (counts.get(`${c.type}-${c.value}`) || 0) + 1));
    let pairCount = 0;
    for (const cnt of counts.values()) {
      if (cnt === 2) pairCount++;
      if (cnt === 4) pairCount += 2;
    }
    if (pairCount === 7) return true;
  }

  const groups: Record<string, number[]> = { wan: [], tiao: [], tong: [], zi: [] };
  hand.forEach((c) => groups[c.type].push(c.value));
  Object.values(groups).forEach((arr) => arr.sort((a, b) => a - b));

  for (const [t, vals] of Object.entries(groups)) {
    for (let i = 0; i < vals.length - 1; i++) {
      if (vals[i] === vals[i + 1]) {
        const testGroups: Record<string, number[]> = {
          wan: [...groups.wan],
          tiao: [...groups.tiao],
          tong: [...groups.tong],
          zi: [...groups.zi],
        };
        testGroups[t].splice(i, 2);

        let allValid = true;
        for (const [subT, subVals] of Object.entries(testGroups)) {
          if (!canDecomposeIntoMelds(subVals, subT === 'zi')) {
            allValid = false;
            break;
          }
        }
        if (allValid) return true;
      }
    }
  }

  return false;
}

const suitWildcardCache = new Map<string, number>();

export function minWildcardsForSuit(arr: number[]): number {
  if (arr.length === 0) return 0;
  if (suitWildcardCache.size > 5000) suitWildcardCache.clear();
  const key = arr.join(',');
  if (suitWildcardCache.has(key)) return suitWildcardCache.get(key)!;

  const v = arr[0];
  let minCost = 999;

  const countV = arr.filter((x) => x === v).length;
  if (countV >= 3) {
    const nextArr = [...arr];
    nextArr.splice(nextArr.indexOf(v), 1);
    nextArr.splice(nextArr.indexOf(v), 1);
    nextArr.splice(nextArr.indexOf(v), 1);
    minCost = Math.min(minCost, minWildcardsForSuit(nextArr));
  }
  if (countV >= 2) {
    const nextArr = [...arr];
    nextArr.splice(nextArr.indexOf(v), 1);
    nextArr.splice(nextArr.indexOf(v), 1);
    minCost = Math.min(minCost, 1 + minWildcardsForSuit(nextArr));
  }
  {
    const nextArr = [...arr];
    nextArr.splice(nextArr.indexOf(v), 1);
    minCost = Math.min(minCost, 2 + minWildcardsForSuit(nextArr));
  }

  const idx1 = arr.indexOf(v + 1);
  const idx2 = arr.indexOf(v + 2);

  if (idx1 !== -1 && idx2 !== -1) {
    const nextArr = [...arr];
    nextArr.splice(idx2, 1);
    nextArr.splice(idx1, 1);
    nextArr.splice(0, 1);
    minCost = Math.min(minCost, minWildcardsForSuit(nextArr));
  }
  if (idx1 !== -1) {
    const nextArr = [...arr];
    nextArr.splice(idx1, 1);
    nextArr.splice(0, 1);
    minCost = Math.min(minCost, 1 + minWildcardsForSuit(nextArr));
  }
  if (idx2 !== -1) {
    const nextArr = [...arr];
    nextArr.splice(idx2, 1);
    nextArr.splice(0, 1);
    minCost = Math.min(minCost, 1 + minWildcardsForSuit(nextArr));
  }

  suitWildcardCache.set(key, minCost);
  return minCost;
}

export function minWildcardsForZi(vals: number[]): number {
  if (vals.length === 0) return 0;
  const counts = new Map<number, number>();
  vals.forEach((v) => counts.set(v, (counts.get(v) || 0) + 1));
  let needed = 0;
  for (const c of counts.values()) {
    const rem = c % 3;
    if (rem === 1) needed += 2;
    else if (rem === 2) needed += 1;
  }
  return needed;
}

export function totalWildcardsNeeded(w: number[], ti: number[], to: number[], z: number[]): number {
  return minWildcardsForSuit(w) + minWildcardsForSuit(ti) + minWildcardsForSuit(to) + minWildcardsForZi(z);
}

export function checkHongzhongWinLocal(hand: Pick<MahjongTile, 'type' | 'value' | 'text'>[]): boolean {
  if (hand.length % 3 !== 2) return false;

  const hzCount = hand.filter((t) => (t.type === 'zi' && t.value === 5) || t.text === '红中').length;
  if (hzCount === 4) return true;

  const normalTiles = hand.filter((t) => !((t.type === 'zi' && t.value === 5) || t.text === '红中'));

  if (hand.length === 14) {
    const counts = new Map<string, number>();
    normalTiles.forEach((c) => counts.set(`${c.type}-${c.value}`, (counts.get(`${c.type}-${c.value}`) || 0) + 1));
    let neededForSingles = 0;
    for (const cnt of counts.values()) {
      neededForSingles += cnt % 2;
    }
    if (hzCount >= neededForSingles) {
      return true;
    }
  }

  const wan = normalTiles.filter((t) => t.type === 'wan').map((t) => t.value).sort((a, b) => a - b);
  const tiao = normalTiles.filter((t) => t.type === 'tiao').map((t) => t.value).sort((a, b) => a - b);
  const tong = normalTiles.filter((t) => t.type === 'tong').map((t) => t.value).sort((a, b) => a - b);
  const zi = normalTiles.filter((t) => t.type === 'zi').map((t) => t.value).sort((a, b) => a - b);

  if (hzCount >= 2) {
    if (totalWildcardsNeeded(wan, tiao, tong, zi) <= hzCount - 2) {
      return true;
    }
  }

  if (hzCount >= 1) {
    const tried = new Set<string>();
    for (const t of normalTiles) {
      const key = `${t.type}-${t.value}`;
      if (tried.has(key)) continue;
      tried.add(key);

      const testWan = [...wan];
      const testTiao = [...tiao];
      const testTong = [...tong];
      const testZi = [...zi];

      if (t.type === 'wan') testWan.splice(testWan.indexOf(t.value), 1);
      else if (t.type === 'tiao') testTiao.splice(testTiao.indexOf(t.value), 1);
      else if (t.type === 'tong') testTong.splice(testTong.indexOf(t.value), 1);
      else if (t.type === 'zi') testZi.splice(testZi.indexOf(t.value), 1);

      if (totalWildcardsNeeded(testWan, testTiao, testTong, testZi) <= hzCount - 1) {
        return true;
      }
    }
  }

  const countsByKind = new Map<string, { type: 'wan' | 'tiao' | 'tong' | 'zi'; value: number; count: number }>();
  normalTiles.forEach((t) => {
    const key = `${t.type}-${t.value}`;
    if (!countsByKind.has(key)) {
      countsByKind.set(key, { type: t.type, value: t.value, count: 0 });
    }
    countsByKind.get(key)!.count++;
  });

  for (const item of countsByKind.values()) {
    if (item.count >= 2) {
      const testWan = [...wan];
      const testTiao = [...tiao];
      const testTong = [...tong];
      const testZi = [...zi];

      if (item.type === 'wan') {
        testWan.splice(testWan.indexOf(item.value), 1);
        testWan.splice(testWan.indexOf(item.value), 1);
      } else if (item.type === 'tiao') {
        testTiao.splice(testTiao.indexOf(item.value), 1);
        testTiao.splice(testTiao.indexOf(item.value), 1);
      } else if (item.type === 'tong') {
        testTong.splice(testTong.indexOf(item.value), 1);
        testTong.splice(testTong.indexOf(item.value), 1);
      } else if (item.type === 'zi') {
        testZi.splice(testZi.indexOf(item.value), 1);
        testZi.splice(testZi.indexOf(item.value), 1);
      }

      if (totalWildcardsNeeded(testWan, testTiao, testTong, testZi) <= hzCount) {
        return true;
      }
    }
  }

  return false;
}

export function shouldSmartPengHand(hand: MahjongTile[], tile: MahjongTile, hongzhongMode: boolean): boolean {
  if (!tile) return false;
  if (hongzhongMode && isHongzhong(tile)) return false;

  const matches = hand.filter((c) => c.type === tile.type && c.value === tile.value);
  if (matches.length < 2) return false;

  const counts = new Map<string, number>();
  hand.forEach((c) => {
    if (hongzhongMode && isHongzhong(c)) return;
    counts.set(`${c.type}-${c.value}`, (counts.get(`${c.type}-${c.value}`) || 0) + 1);
  });
  let pairCount = 0;
  for (const cnt of counts.values()) {
    if (cnt >= 2) pairCount++;
  }
  if (pairCount >= 4) {
    return false;
  }

  if (tile.type === 'zi') {
    return true;
  }

  const remainingHand = [...hand];
  let removed = 0;
  for (let i = remainingHand.length - 1; i >= 0 && removed < 2; i--) {
    if (remainingHand[i].type === tile.type && remainingHand[i].value === tile.value) {
      remainingHand.splice(i, 1);
      removed++;
    }
  }

  const suitTiles = hand.filter((c) => c.type === tile.type).map((c) => c.value);
  const v = tile.value;
  const isEmbeddedInSequence =
    (suitTiles.includes(v - 1) && suitTiles.includes(v + 1)) ||
    (suitTiles.includes(v - 2) && suitTiles.includes(v - 1)) ||
    (suitTiles.includes(v + 1) && suitTiles.includes(v + 2));

  let canTingAfterPeng = false;
  for (let i = 0; i < remainingHand.length; i++) {
    const test13 = remainingHand.filter((_, idx) => idx !== i);
    for (const kind of ALL_TILE_KINDS) {
      const test14 = [...test13, { id: 'test-ting', type: kind.type, value: kind.value, text: kind.text }];
      if (hongzhongMode ? checkHongzhongWinLocal(test14) : checkMahjongWinLocal(test14)) {
        canTingAfterPeng = true;
        break;
      }
    }
    if (canTingAfterPeng) break;
  }

  if (canTingAfterPeng) return true;

  if (isEmbeddedInSequence && matches.length === 2) {
    return false;
  }

  return true;
}

export interface MahjongAiContext {
  hongzhongMode: boolean;
  discards: MahjongTile[];
  meldedTiles: MahjongTile[][];
  oppMelds: MahjongTile[][][];
  wallCount: number;
}

export function scoreMahjongHand(
  tiles: MahjongTile[],
  ctx: Pick<MahjongAiContext, 'hongzhongMode' | 'discards' | 'meldedTiles'>,
  evaluateTing = false
): number {
  if (!tiles || tiles.length === 0) return 0;

  const isWin = ctx.hongzhongMode ? checkHongzhongWinLocal(tiles) : checkMahjongWinLocal(tiles);
  if (isWin) return 100000;

  const groups: Record<string, number[]> = { wan: [], tiao: [], tong: [], zi: [] };
  tiles.forEach((t) => groups[t.type].push(t.value));

  let totalScore = 0;

  if (ctx.hongzhongMode) {
    const hzCount = tiles.filter(isHongzhong).length;
    totalScore += hzCount * 2500;
  }

  for (const type in groups) {
    const vals = groups[type];
    if (vals.length === 0) continue;

    const counts: number[] = new Array(10).fill(0);
    vals.forEach((v) => {
      if (v >= 1 && v <= 9) counts[v]++;
    });

    if (type === 'zi') {
      for (let v = 1; v <= 7; v++) {
        const c = counts[v];
        if (ctx.hongzhongMode && v === 5) continue;
        if (c >= 3) totalScore += 600;
        else if (c === 2) totalScore += 250;
        else if (c === 1) totalScore -= 300;
      }
    } else {
      const cMap = [...counts];
      for (let v = 1; v <= 9; v++) {
        if (cMap[v] >= 3) {
          totalScore += 700;
          cMap[v] -= 3;
        }
      }
      for (let v = 1; v <= 7; v++) {
        while (cMap[v] > 0 && cMap[v + 1] > 0 && cMap[v + 2] > 0) {
          totalScore += 650;
          cMap[v]--;
          cMap[v + 1]--;
          cMap[v + 2]--;
        }
      }
      for (let v = 1; v <= 9; v++) {
        if (cMap[v] >= 2) {
          totalScore += 300;
          cMap[v] -= 2;
        }
      }
      for (let v = 1; v <= 8; v++) {
        if (cMap[v] > 0 && cMap[v + 1] > 0) {
          totalScore += 220;
          cMap[v]--;
          cMap[v + 1]--;
        }
      }
      for (let v = 1; v <= 7; v++) {
        if (cMap[v] > 0 && cMap[v + 2] > 0) {
          totalScore += 140;
          cMap[v]--;
          cMap[v + 2]--;
        }
      }
      for (let v = 1; v <= 9; v++) {
        if (cMap[v] > 0) {
          if (v === 1 || v === 9) totalScore -= 180;
          else totalScore -= 80;
        }
      }
    }
  }

  if (evaluateTing) {
    let tingKindsCount = 0;
    let totalTingTilesAvailable = 0;

    for (const kind of ALL_TILE_KINDS) {
      const test14 = [...tiles, { id: 'test-ting', type: kind.type, value: kind.value, text: kind.text }];
      const canWin = ctx.hongzhongMode ? checkHongzhongWinLocal(test14) : checkMahjongWinLocal(test14);
      if (canWin) {
        tingKindsCount++;
        const kindKey = `${kind.type}-${kind.value}`;
        const used =
          ctx.discards.filter((d) => `${d.type}-${d.value}` === kindKey).length +
          ctx.meldedTiles.flatMap((m) => m).filter((t) => `${t.type}-${t.value}` === kindKey).length +
          tiles.filter((t) => `${t.type}-${t.value}` === kindKey).length;
        totalTingTilesAvailable += Math.max(0, 4 - used);
      }
    }

    if (tingKindsCount > 0) {
      totalScore += 20000 + tingKindsCount * 1500 + totalTingTilesAvailable * 200;
    }
  }

  return totalScore;
}

export function findSmartBestDiscardIdx(hand: MahjongTile[], ctx: MahjongAiContext): number {
  if (hand.length <= 1) return 0;

  const nonHzIndices = hand
    .map((t, idx) => ({ t, idx }))
    .filter((item) => !(ctx.hongzhongMode && isHongzhong(item.t)));
  const candidateIndices = nonHzIndices.length > 0 ? nonHzIndices.map((item) => item.idx) : hand.map((_, idx) => idx);

  const uniqueCandidateIndices: number[] = [];
  const seenKinds = new Set<string>();
  for (const idx of candidateIndices) {
    const t = hand[idx];
    const key = `${t.type}-${t.value}`;
    if (!seenKinds.has(key)) {
      seenKinds.add(key);
      uniqueCandidateIndices.push(idx);
    }
  }
  const evalIndices = uniqueCandidateIndices.length > 0 ? uniqueCandidateIndices : candidateIndices;

  let bestIndex = evalIndices[0];
  let maxTotalScore = -999999999;

  const visibleCounts: Record<string, number> = {};
  ctx.discards.forEach((t) => {
    const key = `${t.type}-${t.value}`;
    visibleCounts[key] = (visibleCounts[key] || 0) + 1;
  });
  ctx.meldedTiles.forEach((meld) => {
    meld.forEach((t) => {
      const key = `${t.type}-${t.value}`;
      visibleCounts[key] = (visibleCounts[key] || 0) + 1;
    });
  });
  ctx.oppMelds.forEach((playerMelds) => {
    playerMelds.forEach((meld) => {
      meld.forEach((t) => {
        const key = `${t.type}-${t.value}`;
        visibleCounts[key] = (visibleCounts[key] || 0) + 1;
      });
    });
  });

  const counts = new Map<string, number>();
  hand.forEach((c) => {
    if (ctx.hongzhongMode && isHongzhong(c)) return;
    counts.set(`${c.type}-${c.value}`, (counts.get(`${c.type}-${c.value}`) || 0) + 1);
  });
  let totalPairsInHand = 0;
  for (const cnt of counts.values()) {
    if (cnt >= 2) totalPairsInHand++;
  }
  const isPursuingSevenPairs = totalPairsInHand >= 4;

  for (const i of evalIndices) {
    const tile = hand[i];
    const hand13 = hand.filter((_, idx) => idx !== i);
    const base13Score = scoreMahjongHand(hand13, ctx, false);

    let effectiveScoreSum = 0;
    let tingDoors = 0;
    let tingWinningTilesCount = 0;

    for (const kind of ALL_TILE_KINDS) {
      const kindKey = `${kind.type}-${kind.value}`;
      const usedCount = visibleCounts[kindKey] || 0;
      const inHand13Count = hand13.filter((t) => t.type === kind.type && t.value === kind.value).length;
      const availableInWall = Math.max(0, 4 - usedCount - inHand13Count);

      if (availableInWall === 0) continue;

      const testHand14 = [...hand13, { id: 'test', type: kind.type, value: kind.value, text: kind.text }];
      const canWin = ctx.hongzhongMode ? checkHongzhongWinLocal(testHand14) : checkMahjongWinLocal(testHand14);

      if (canWin) {
        tingDoors++;
        tingWinningTilesCount += availableInWall;
      } else {
        const connects = hand13.some(
          (t) => t.type === kind.type && (t.value === kind.value || (t.type !== 'zi' && Math.abs(t.value - kind.value) <= 2))
        );
        if (connects) {
          effectiveScoreSum += 120 * availableInWall;
        }
      }
    }

    let tingBonus = 0;
    if (tingDoors > 0) {
      tingBonus = 35000 + tingDoors * 5000 + tingWinningTilesCount * 800;
    }

    let discardUrgencyBonus = 0;
    const sameTileCountInHand = hand.filter((t) => t.type === tile.type && t.value === tile.value).length;
    const sameTileVisible = visibleCounts[`${tile.type}-${tile.value}`] || 0;

    if (isPursuingSevenPairs) {
      if (sameTileCountInHand >= 2) {
        discardUrgencyBonus -= 30000;
      } else {
        discardUrgencyBonus += 4500;
      }
    }

    if (tile.type === 'zi') {
      if (sameTileCountInHand === 1) {
        if (sameTileVisible >= 2) {
          discardUrgencyBonus += 2400;
        } else {
          discardUrgencyBonus += 1300;
        }
      } else if (sameTileCountInHand === 2) {
        discardUrgencyBonus -= 800;
      }
    } else {
      if (sameTileCountInHand === 1) {
        if (tile.value === 1 || tile.value === 9) {
          const hasAdjacent = hand.some((t) => t.type === tile.type && Math.abs(t.value - tile.value) === 1);
          if (!hasAdjacent) {
            discardUrgencyBonus += 700;
          }
        } else if (tile.value === 2 || tile.value === 8) {
          const hasNeighbor = hand.some((t) => t.type === tile.type && Math.abs(t.value - tile.value) <= 1 && t !== tile);
          if (!hasNeighbor) {
            discardUrgencyBonus += 380;
          }
        } else {
          const hasCloseNeighbor = hand.some((t) => t.type === tile.type && Math.abs(t.value - tile.value) <= 2 && t !== tile);
          if (!hasCloseNeighbor) {
            discardUrgencyBonus += 220;
          }
        }
      }
    }

    let defenseBonus = 0;
    if (ctx.wallCount < 30) {
      const isAlreadyInDiscards = ctx.discards.some((d) => d.type === tile.type && d.value === tile.value);
      if (isAlreadyInDiscards) {
        defenseBonus += 700;
      } else if (tile.type !== 'zi' && tile.value >= 4 && tile.value <= 6) {
        defenseBonus -= 450;
      }
    }

    const totalEvaluation = base13Score + effectiveScoreSum + tingBonus + discardUrgencyBonus + defenseBonus;

    if (totalEvaluation > maxTotalScore) {
      maxTotalScore = totalEvaluation;
      bestIndex = i;
    }
  }

  return bestIndex;
}
