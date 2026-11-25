/**
 * YanSan (Four Players Poker Classic) Rules Engine
 * 52 cards (no Jokers), 13 cards per player
 * Diamond 4 (♦4) starts first and must be played on the first trick.
 * Ranking: 3 > 2 > A > K > Q > J > 10 > 9 > 8 > 7 > 6 > 5 > 4
 * Suits: ♠ > ♥ > ♣ > ♦
 * Allowed Combos: Single, Pair, Triple, Straight (no 10JQKA), Fulu, Four-With-One, Straight Flush.
 * No pure 4-card bomb, no pure flush, free pass (no must-beat), no top-defense required.
 * Qiang Gong returns played cards to hand (no redeal) and resets lead to Diamond 4.
 */

export interface YansanCard {
  id: string;
  rank: number; // 3..15 (15=2, 14=A, 13=K... 3=3)
  suitVal: number; // 4=♠, 3=♥, 2=♣, 1=♦
  label: string;
  suit: '♠' | '♥' | '♣' | '♦';
  isRed: boolean;
}

export type YansanComboType = 'single' | 'pair' | 'triplet' | '5_card';

export interface YansanCombo {
  type: YansanComboType;
  comboTier?: number; // 1=Straight, 2=Flush, 3=Fulu, 4=Four-With-One, 5=Straight Flush
  rank: number;
  suitVal?: number;
  length?: number;
  cards: YansanCard[];
  seat?: number;
  label?: string;
}

export function getYansanRankLabel(rank: number): string {
  if (rank === 15) return '2';
  if (rank === 14) return 'A';
  if (rank === 13) return 'K';
  if (rank === 12) return 'Q';
  if (rank === 11) return 'J';
  return String(rank);
}

export function getYansanComboLabel(combo: YansanCombo): string {
  const rLabel = getYansanRankLabel(combo.rank);
  if (combo.type === 'single') return `单张 ${combo.cards[0]?.suit || ''}${rLabel}`;
  if (combo.type === 'pair') return `对 ${rLabel}`;
  if (combo.type === 'triplet') return `三条 ${rLabel}`;
  if (combo.type === '5_card') {
    if (combo.comboTier === 5) return `同花顺 (${rLabel})`;
    if (combo.comboTier === 4) return `四带一 (${rLabel})`;
    if (combo.comboTier === 3) return `俘虏/葫芦 (${rLabel})`;
    if (combo.comboTier === 2) return `同花 (${combo.cards[0]?.suit || ''}${rLabel})`;
    return `顺子 (至${rLabel})`;
  }
  return combo.label || '组合牌';
}

export function getYansanRankWeight(rank: number): number {
  if (rank === 3) return 13;
  if (rank === 15) return 12; // 2
  if (rank === 14) return 11; // A
  if (rank === 13) return 10; // K
  if (rank === 12) return 9;  // Q
  if (rank === 11) return 8;  // J
  if (rank === 10) return 7;  // 10
  if (rank === 9) return 6;   // 9
  if (rank === 8) return 5;   // 8
  if (rank === 7) return 4;   // 7
  if (rank === 6) return 3;   // 6
  if (rank === 5) return 2;   // 5
  if (rank === 4) return 1;   // 4
  return 0;
}

export function getYansanCardPower(c: YansanCard): number {
  return getYansanRankWeight(c.rank) * 10 + c.suitVal;
}

export function parseYansanCombo(cards: YansanCard[]): YansanCombo | null {
  if (!cards || cards.length === 0) return null;
  const count = cards.length;
  const sorted = [...cards].sort((a, b) => getYansanCardPower(b) - getYansanCardPower(a));

  const rankCounts = new Map<number, number>();
  for (const c of sorted) {
    rankCounts.set(c.rank, (rankCounts.get(c.rank) || 0) + 1);
  }
  const entries = Array.from(rankCounts.entries());

  let result: YansanCombo | null = null;

  if (count === 1) {
    result = { type: 'single', rank: sorted[0].rank, suitVal: sorted[0].suitVal, cards: sorted };
  } else if (count === 2 && entries.length === 1) {
    const highestSuit = Math.max(...sorted.map((c) => c.suitVal));
    result = { type: 'pair', rank: entries[0][0], suitVal: highestSuit, cards: sorted };
  } else if (count === 3 && entries.length === 1) {
    result = { type: 'triplet', rank: entries[0][0], cards: sorted };
  } else if (count === 5) {
    const isFlush = sorted.every((c) => c.suit === sorted[0].suit);
    const ranks = sorted.map((c) => c.rank).sort((a, b) => a - b);
    
    let isNormalSeq = true;
    for (let i = 1; i < 5; i++) {
      if (ranks[i] !== ranks[i - 1] + 1) {
        isNormalSeq = false;
        break;
      }
    }
    if (ranks.includes(15)) isNormalSeq = false;
    const isA2345 = ranks[0] === 3 && ranks[1] === 4 && ranks[2] === 5 && ranks[3] === 14 && ranks[4] === 15;
    const is10JQKA = ranks[0] === 10 && ranks[1] === 11 && ranks[2] === 12 && ranks[3] === 13 && ranks[4] === 14;
    const isLegalStraight = (isNormalSeq || isA2345) && !is10JQKA;

    // ① 同花顺 (Tier 5)：同花色且构成合法顺子 (允许A2345/34567，不可出10JQKA)
    if (isFlush && isLegalStraight) {
      const maxCard = sorted[0];
      result = { type: '5_card', comboTier: 5, rank: maxCard.rank, suitVal: sorted[0].suitVal, cards: sorted };
    } else if (entries.some(([, cnt]) => cnt === 4)) {
      // ② 四带一 (Tier 4)：4张相同点数 + 任意1张单牌
      const fourRank = entries.find(([, cnt]) => cnt === 4)![0];
      result = { type: '5_card', comboTier: 4, rank: fourRank, cards: sorted };
    } else if (entries.some(([, cnt]) => cnt === 3) && entries.length === 2) {
      // ③ 俘虏 (Tier 3)：3张同点 + 1对子 (三带一对 / 葫芦)
      const threeRank = entries.find(([, cnt]) => cnt === 3)![0];
      result = { type: '5_card', comboTier: 3, rank: threeRank, cards: sorted };
    } else if (isFlush) {
      // ④ 同花 (Tier 2)：5张同花色牌
      const maxCard = sorted[0];
      result = { type: '5_card', comboTier: 2, rank: maxCard.rank, suitVal: sorted[0].suitVal, cards: sorted };
    } else if (isLegalStraight) {
      // ⑤ 顺子 (Tier 1)：5张相连
      const maxCard = sorted[0];
      result = { type: '5_card', comboTier: 1, rank: maxCard.rank, suitVal: maxCard.suitVal, cards: sorted };
    }
  }

  if (result) {
    result.label = getYansanComboLabel(result);
  }
  return result;
}

export function canBeatYansanCombo(candidate: YansanCombo, target: YansanCombo | null): boolean {
  if (!target) return true;
  if (candidate.type !== target.type) return false;

  if (candidate.type === 'single' || candidate.type === 'pair') {
    const candRW = getYansanRankWeight(candidate.rank);
    const targetRW = getYansanRankWeight(target.rank);
    if (candRW !== targetRW) return candRW > targetRW;
    return (candidate.suitVal || 0) > (target.suitVal || 0);
  }

  if (candidate.type === 'triplet') {
    return getYansanRankWeight(candidate.rank) > getYansanRankWeight(target.rank);
  }

  if (candidate.type === '5_card') {
    const candTier = candidate.comboTier || 1;
    const targTier = target.comboTier || 1;
    if (candTier !== targTier) return candTier > targTier;

    // 同花顺 (Tier 5) 或 同花 (Tier 2)：先比花色(♠>♥>♣>♦)，相同花色比单张最大牌
    if (candTier === 5 || candTier === 2) {
      const candSuit = candidate.suitVal || 0;
      const targSuit = target.suitVal || 0;
      if (candSuit !== targSuit) return candSuit > targSuit;
      const candRW = getYansanRankWeight(candidate.rank);
      const targRW = getYansanRankWeight(target.rank);
      return candRW > targRW;
    }

    // 四带一 (Tier 4) 或 俘虏 (Tier 3)：比主牌点数权重
    if (candTier === 4 || candTier === 3) {
      return getYansanRankWeight(candidate.rank) > getYansanRankWeight(target.rank);
    }

    // 顺子 (Tier 1)：比单张最大的牌（先比点数权重，相同比花色）
    if (candTier === 1) {
      const candRW = getYansanRankWeight(candidate.rank);
      const targRW = getYansanRankWeight(target.rank);
      if (candRW !== targRW) return candRW > targRW;
      return (candidate.suitVal || 0) > (target.suitVal || 0);
    }
  }

  return false;
}

export interface YansanSeatBelief {
  seat: number;
  relation: 'confirmed_ally' | 'suspected_ally' | 'unknown' | 'suspected_enemy' | 'confirmed_enemy' | 'solo_enemy';
  score: number; // -1.0 (certain enemy) to +1.0 (certain ally)
  confidence: number; // 0.0 to 1.0
  isConfirmed: boolean;
  reason: string;
  hasSpade3: boolean;
  hasSpadeA: boolean;
  playedCardsCount: number;
  remainingCardsCount: number;
}

export interface YansanPlayRecord {
  seat: number;
  cards: YansanCard[];
  combo: YansanCombo | null;
  targetCombo: YansanCombo | null;
  isPass: boolean;
  actionSeq?: number;
  timestamp?: number;
}

export interface YansanAllianceInference {
  observerSeat: number;
  spade3Seat: number | null;
  spadeASeat: number | null;
  isSolo: boolean;
  soloSeat: number | null;
  beliefs: [YansanSeatBelief, YansanSeatBelief, YansanSeatBelief, YansanSeatBelief];
}

export interface YansanAIPlayResult {
  cards: YansanCard[] | null;
  thought: string;
  tacticalIntent: 'win_clear' | 'feed_ally' | 'cover_ally' | 'yield_ally' | 'block_enemy' | 'counter_enemy' | 'dump_combo' | 'lead_singleton' | 'strategic_pass' | 'first_turn_d4';
  deducedAlliances: YansanSeatBelief[];
}

export interface YansanAIContext {
  isFirstTurn?: boolean;
  seat?: number;
  teammateSeat?: number;
  handsCount?: number[];
  isTeammateTarget?: boolean;
  seatPlayedCards?: YansanCard[][];
  playHistory?: YansanPlayRecord[];
  isQiangGong?: boolean;
  qiangGongSeat?: number | null;
  downstreamSeat?: number;
  lastTargetSeat?: number;
}

/**
 * 基于出牌历史与行为深度推算各席位的敌友身份 (Alliance Inference Engine)
 * 核心逻辑：
 * 1. 铁证推导：黑桃3/黑桃A公开打出，或自身手牌持有时确立阵营
 * 2. 行为推断：
 *    - 进攻压制 (Attack)：对特定玩家的高频拦截，指示敌对关系
 *    - 战术让行 (Yield/Pass)：在特定玩家控盘时主动过牌，指示同盟掩护
 *    - 残局助攻 (Feeding)：在特定玩家报单(剩1~2张)时送出小单牌，极大概率是盟友
 *    - 残局封杀 (Blocking)：在特定玩家报单时顶大牌或变型组合封杀，必定是敌对
 */
export function inferYansanAlliances(
  observerSeat: number,
  observerHand: YansanCard[],
  seatPlayedCards: YansanCard[][] = [[], [], [], []],
  playHistory: YansanPlayRecord[] = [],
  handsCount: number[] = [13, 13, 13, 13],
  isQiangGong: boolean = false,
  qiangGongSeat: number | null = null
): YansanAllianceInference {
  const seatNames = ['本家', '西席', '北席', '东席'];

  // 1. 检查各家公开打出的牌中是否已出现 ♠3 或 ♠A
  let publicSpade3Seat: number | null = null;
  let publicSpadeASeat: number | null = null;

  for (let s = 0; s < 4; s++) {
    const played = seatPlayedCards[s] || [];
    if (played.some((c) => c.rank === 3 && c.suit === '♠')) publicSpade3Seat = s;
    if (played.some((c) => c.rank === 14 && c.suit === '♠')) publicSpadeASeat = s;
  }

  // 2. 检查出牌者自身的暗牌与打出的牌 (Observer Private Knowledge)
  const myCardsAll = [...(observerHand || []), ...(seatPlayedCards[observerSeat] || [])];
  const observerHasSpade3 = myCardsAll.some((c) => c.rank === 3 && c.suit === '♠');
  const observerHasSpadeA = myCardsAll.some((c) => c.rank === 14 && c.suit === '♠');

  const knownSpade3Seat = observerHasSpade3 ? observerSeat : publicSpade3Seat;
  const knownSpadeASeat = observerHasSpadeA ? observerSeat : publicSpadeASeat;

  // 3. 强攻特判：如果有人宣告强攻，强攻者与全场为敌，其余三人结盟
  if (isQiangGong && qiangGongSeat !== null) {
    const isObserverSolo = observerSeat === qiangGongSeat;
    const beliefs: YansanSeatBelief[] = [];

    for (let s = 0; s < 4; s++) {
      if (s === observerSeat) {
        beliefs.push({
          seat: s,
          relation: 'confirmed_ally',
          score: 1.0,
          confidence: 1.0,
          isConfirmed: true,
          reason: isObserverSolo ? '自身开启强攻，独战全场' : '自身为平民反抗防守阵营',
          hasSpade3: s === qiangGongSeat,
          hasSpadeA: s === qiangGongSeat,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      } else if (isObserverSolo) {
        beliefs.push({
          seat: s,
          relation: 'solo_enemy',
          score: -1.0,
          confidence: 1.0,
          isConfirmed: true,
          reason: `${seatNames[s]} 属于联合防守敌方`,
          hasSpade3: false,
          hasSpadeA: false,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      } else {
        const isEnemy = s === qiangGongSeat;
        beliefs.push({
          seat: s,
          relation: isEnemy ? 'solo_enemy' : 'confirmed_ally',
          score: isEnemy ? -1.0 : 1.0,
          confidence: 1.0,
          isConfirmed: true,
          reason: isEnemy ? `【${seatNames[s]}】开启强攻，全场公敌` : `【${seatNames[s]}】同为防守反击同盟`,
          hasSpade3: isEnemy,
          hasSpadeA: isEnemy,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      }
    }

    return {
      observerSeat,
      spade3Seat: qiangGongSeat,
      spadeASeat: qiangGongSeat,
      isSolo: true,
      soloSeat: qiangGongSeat,
      beliefs: beliefs as [YansanSeatBelief, YansanSeatBelief, YansanSeatBelief, YansanSeatBelief],
    };
  }

  // 4. 自身同时持 ♠3 和 ♠A (未开强攻独战)
  if (observerHasSpade3 && observerHasSpadeA) {
    const beliefs: YansanSeatBelief[] = [];
    for (let s = 0; s < 4; s++) {
      if (s === observerSeat) {
        beliefs.push({
          seat: s,
          relation: 'confirmed_ally',
          score: 1.0,
          confidence: 1.0,
          isConfirmed: true,
          reason: '自身独占♠3与♠A，以一敌三',
          hasSpade3: true,
          hasSpadeA: true,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      } else {
        beliefs.push({
          seat: s,
          relation: 'confirmed_enemy',
          score: -1.0,
          confidence: 1.0,
          isConfirmed: true,
          reason: `${seatNames[s]} 属于平民三人同盟，敌对目标`,
          hasSpade3: false,
          hasSpadeA: false,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      }
    }
    return {
      observerSeat,
      spade3Seat: observerSeat,
      spadeASeat: observerSeat,
      isSolo: true,
      soloSeat: observerSeat,
      beliefs: beliefs as [YansanSeatBelief, YansanSeatBelief, YansanSeatBelief, YansanSeatBelief],
    };
  }

  // 5. 统计历史攻击与助攻行为
  // hostility[A][B]: A 主动顶压 B 的次数/强度
  // support[A][B]: A 主动给 B 让行或送牌的次数/强度
  const hostilityMatrix = [
    [0, 0, 0, 0],
    [0, 0, 0, 0],
    [0, 0, 0, 0],
    [0, 0, 0, 0],
  ];
  const supportMatrix = [
    [0, 0, 0, 0],
    [0, 0, 0, 0],
    [0, 0, 0, 0],
    [0, 0, 0, 0],
  ];

  for (const record of playHistory) {
    if (record.isPass) {
      if (record.targetCombo && record.targetCombo.seat !== undefined && record.targetCombo.seat !== record.seat) {
        const targetSeat = record.targetCombo.seat;
        const targetRW = getYansanRankWeight(record.targetCombo.rank);
        const targetTier = record.targetCombo.comboTier || 1;
        // 只有针对中小牌（单张<=Q、对子<=J、普通顺子）主动过牌，才说明有让行配合倾向；
        // 对 2/3/A/大组合 过牌多半是真要不起，不应计入友好信号
        const isSmallTarget =
          (record.targetCombo.type === 'single' && targetRW <= 9) ||
          (record.targetCombo.type === 'pair' && targetRW <= 8) ||
          (record.targetCombo.type === '5_card' && targetTier === 1 && targetRW <= 9);
        const isUnavoidableGiant =
          (record.targetCombo.type === 'single' && targetRW >= 12) ||
          (record.targetCombo.type === 'pair' && targetRW >= 11) ||
          (record.targetCombo.type === '5_card' && targetTier >= 3);
        if (isSmallTarget) {
          supportMatrix[record.seat][targetSeat] += 1.6;
        } else if (!isUnavoidableGiant) {
          supportMatrix[record.seat][targetSeat] += 0.35;
        }
      }
    } else if (record.combo && record.targetCombo && record.targetCombo.seat !== undefined) {
      const targetSeat = record.targetCombo.seat;
      if (targetSeat !== record.seat) {
        const rw = getYansanRankWeight(record.combo.rank);
        const isBigAttack =
          rw >= 11 ||
          (record.combo.comboTier || 1) >= 3 ||
          record.combo.type === '5_card';
        const isTargetLowHand = (handsCount[targetSeat] || 10) <= 3;
        const hostilityAdd = isTargetLowHand ? 3.2 : isBigAttack ? 2.1 : 1.0;
        hostilityMatrix[record.seat][targetSeat] += hostilityAdd;
      }
    } else if (record.combo && !record.targetCombo) {
      // 首出 ♦4 是规则强制，不作为阵营信号；其余领出检查报单喂牌/封杀行为
      const isForcedD4 =
        record.actionSeq === 1 && record.combo.cards?.some((c) => c.rank === 4 && c.suit === '♦');
      if (!isForcedD4) {
        for (let s = 0; s < 4; s++) {
          if (s !== record.seat && (handsCount[s] || 10) === 1) {
            if (record.combo.type === 'single' && getYansanRankWeight(record.combo.rank) <= 6) {
              supportMatrix[record.seat][s] += 3.5;
            } else {
              hostilityMatrix[record.seat][s] += 3.0;
            }
          }
        }
      }
    }
  }

  // 6. 综合判定各席位阵营
  const beliefs: YansanSeatBelief[] = [];

  // 判断是否黑桃同盟已完全揭晓，以及通过“已出完手牌却未打出黑桃双牌”排除确认平民
  const isBothSpadesPublic = publicSpade3Seat !== null && publicSpadeASeat !== null;
  const isSoloPublic = isBothSpadesPublic && publicSpade3Seat === publicSpadeASeat;
  const confirmedCivilianSeats = new Set<number>();
  for (let s = 0; s < 4; s++) {
    const played = seatPlayedCards[s] || [];
    const hasSpadePublic = played.some((c) => (c.rank === 3 || c.rank === 14) && c.suit === '♠');
    if (s === observerSeat && !observerHasSpade3 && !observerHasSpadeA) {
      confirmedCivilianSeats.add(s);
    } else if (s !== observerSeat && (handsCount[s] ?? 13) === 0 && !hasSpadePublic) {
      confirmedCivilianSeats.add(s);
    }
  }

  // 如果已有 2 名确认平民（且无1v3迹象），则其余 2 家必为黑桃阵营
  const inferredSpadeByElimination = new Set<number>();
  if (confirmedCivilianSeats.size === 2) {
    for (let s = 0; s < 4; s++) {
      if (!confirmedCivilianSeats.has(s)) inferredSpadeByElimination.add(s);
    }
  }

  for (let s = 0; s < 4; s++) {
    if (s === observerSeat) {
      beliefs.push({
        seat: s,
        relation: 'confirmed_ally',
        score: 1.0,
        confidence: 1.0,
        isConfirmed: true,
        reason: observerHasSpade3 || observerHasSpadeA ? '自身属于黑桃同盟' : '自身属于平民同盟',
        hasSpade3: observerHasSpade3,
        hasSpadeA: observerHasSpadeA,
        playedCardsCount: (seatPlayedCards[s] || []).length,
        remainingCardsCount: handsCount[s] || 0,
      });
      continue;
    }

    const hasPSpade3 = publicSpade3Seat === s;
    const hasPSpadeA = publicSpadeASeat === s;

    // 情况 A：全场黑桃双牌已公开
    if (isBothSpadesPublic) {
      if (isSoloPublic) {
        const isSolo = s === publicSpade3Seat;
        beliefs.push({
          seat: s,
          relation: isSolo ? 'solo_enemy' : 'confirmed_ally',
          score: isSolo ? -1.0 : 1.0,
          confidence: 1.0,
          isConfirmed: true,
          reason: isSolo ? `【${seatNames[s]}】独揽♠3与♠A，以一敌三之公敌` : `【${seatNames[s]}】同为平民防守同盟`,
          hasSpade3: hasPSpade3,
          hasSpadeA: hasPSpadeA,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      } else {
        const isSpade = s === publicSpade3Seat || s === publicSpadeASeat;
        const observerIsSpade = observerHasSpade3 || observerHasSpadeA;
        const isAlly = isSpade === observerIsSpade;
        beliefs.push({
          seat: s,
          relation: isAlly ? 'confirmed_ally' : 'confirmed_enemy',
          score: isAlly ? 1.0 : -1.0,
          confidence: 1.0,
          isConfirmed: true,
          reason: isSpade
            ? `【${seatNames[s]}】打出${hasPSpade3 ? '♠3' : '♠A'}，确定为黑桃阵营`
            : `【${seatNames[s]}】未持黑桃双牌，确定为平民阵营`,
          hasSpade3: hasPSpade3,
          hasSpadeA: hasPSpadeA,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      }
      continue;
    }

    // 情况 B：出牌者属于黑桃阵营 (持♠3或♠A，需找出另一位黑桃盟友)
    if (observerHasSpade3 || observerHasSpadeA) {
      const partnerCardName = observerHasSpade3 ? '♠A' : '♠3';
      const hasPartnerCard = observerHasSpade3 ? hasPSpadeA : hasPSpade3;

      if (hasPartnerCard) {
        beliefs.push({
          seat: s,
          relation: 'confirmed_ally',
          score: 1.0,
          confidence: 1.0,
          isConfirmed: true,
          reason: `【${seatNames[s]}】已打出${partnerCardName}，确认为同舟共济之黑桃盟友！`,
          hasSpade3: hasPSpade3,
          hasSpadeA: hasPSpadeA,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      } else if (confirmedCivilianSeats.has(s)) {
        beliefs.push({
          seat: s,
          relation: 'confirmed_enemy',
          score: -1.0,
          confidence: 1.0,
          isConfirmed: true,
          reason: `【${seatNames[s]}】已出清且未现${partnerCardName}，确认为敌对平民`,
          hasSpade3: false,
          hasSpadeA: false,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      } else {
        const supportFromS = supportMatrix[s][observerSeat];
        const attackFromS = hostilityMatrix[s][observerSeat];
        const netAttitude = supportFromS - attackFromS;

        if (netAttitude >= 1.6) {
          beliefs.push({
            seat: s,
            relation: 'suspected_ally',
            score: 0.65,
            confidence: 0.72,
            isConfirmed: false,
            reason: `【${seatNames[s]}】多次主动让行并配合出牌，高度疑似持有${partnerCardName}的同盟`,
            hasSpade3: false,
            hasSpadeA: false,
            playedCardsCount: (seatPlayedCards[s] || []).length,
            remainingCardsCount: handsCount[s] || 0,
          });
        } else if (netAttitude <= -1.8) {
          beliefs.push({
            seat: s,
            relation: 'suspected_enemy',
            score: -0.75,
            confidence: 0.75,
            isConfirmed: false,
            reason: `【${seatNames[s]}】多次猛烈顶压进攻，判定为敌对平民`,
            hasSpade3: false,
            hasSpadeA: false,
            playedCardsCount: (seatPlayedCards[s] || []).length,
            remainingCardsCount: handsCount[s] || 0,
          });
        } else {
          beliefs.push({
            seat: s,
            relation: 'unknown',
            score: 0.0,
            confidence: 0.3,
            isConfirmed: false,
            reason: `【${seatNames[s]}】出牌克制，暗中试探阵营归属`,
            hasSpade3: false,
            hasSpadeA: false,
            playedCardsCount: (seatPlayedCards[s] || []).length,
            remainingCardsCount: handsCount[s] || 0,
          });
        }
      }
      continue;
    }

    // 情况 C：出牌者属于平民同盟 (手牌既无♠3也无♠A)
    const isKnownSpade = hasPSpade3 || hasPSpadeA || inferredSpadeByElimination.has(s);
    if (isKnownSpade) {
      beliefs.push({
        seat: s,
        relation: 'confirmed_enemy',
        score: -1.0,
        confidence: 1.0,
        isConfirmed: true,
        reason: hasPSpade3 || hasPSpadeA
          ? `【${seatNames[s]}】已亮明${hasPSpade3 ? '♠3' : '♠A'}，确定为黑桃敌对势力！`
          : `【${seatNames[s]}】排除已出清平民后锁定为黑桃敌对势力！`,
        hasSpade3: hasPSpade3,
        hasSpadeA: hasPSpadeA,
        playedCardsCount: (seatPlayedCards[s] || []).length,
        remainingCardsCount: handsCount[s] || 0,
      });
    } else if (confirmedCivilianSeats.has(s)) {
      beliefs.push({
        seat: s,
        relation: 'confirmed_ally',
        score: 1.0,
        confidence: 1.0,
        isConfirmed: true,
        reason: `【${seatNames[s]}】已出清且未持黑桃双牌，确认为同道平民盟友`,
        hasSpade3: false,
        hasSpadeA: false,
        playedCardsCount: (seatPlayedCards[s] || []).length,
        remainingCardsCount: handsCount[s] || 0,
      });
    } else {
      let attacksOnSpade = 0;
      let supportsSpade = 0;

      if (knownSpade3Seat !== null) {
        attacksOnSpade += hostilityMatrix[s][knownSpade3Seat];
        supportsSpade += supportMatrix[s][knownSpade3Seat];
      }
      if (knownSpadeASeat !== null) {
        attacksOnSpade += hostilityMatrix[s][knownSpadeASeat];
        supportsSpade += supportMatrix[s][knownSpadeASeat];
      }

      const myAttitude = supportMatrix[s][observerSeat] - hostilityMatrix[s][observerSeat];

      if (attacksOnSpade >= 1.8 || myAttitude >= 1.6) {
        beliefs.push({
          seat: s,
          relation: 'suspected_ally',
          score: 0.7,
          confidence: 0.72,
          isConfirmed: false,
          reason: `【${seatNames[s]}】坚决顶压黑桃领牌并与本方协同，判定为同道平民盟友`,
          hasSpade3: false,
          hasSpadeA: false,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      } else if (supportsSpade >= 1.8 || myAttitude <= -1.8) {
        beliefs.push({
          seat: s,
          relation: 'suspected_enemy',
          score: -0.7,
          confidence: 0.7,
          isConfirmed: false,
          reason: `【${seatNames[s]}】处处庇护黑桃首出，高度疑似黑桃暗合隐蔽盟友`,
          hasSpade3: false,
          hasSpadeA: false,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      } else {
        beliefs.push({
          seat: s,
          relation: 'unknown',
          score: 0.0,
          confidence: 0.25,
          isConfirmed: false,
          reason: `【${seatNames[s]}】局势未明，正在通过走牌轨迹研判身份`,
          hasSpade3: false,
          hasSpadeA: false,
          playedCardsCount: (seatPlayedCards[s] || []).length,
          remainingCardsCount: handsCount[s] || 0,
        });
      }
    }
  }

  // 7. 2v2 排除法传导补全：2 黑桃 vs 2 平民，已知 1 友 1 敌时可直接推演剩余未知席位
  if (!isBothSpadesPublic) {
    const others = beliefs.filter((b) => b.seat !== observerSeat);
    const strongAllies = others.filter((b) => b.score >= 0.6);
    const strongEnemies = others.filter((b) => b.score <= -0.6);
    const unknowns = others.filter((b) => !b.isConfirmed && Math.abs(b.score) < 0.35);

    if (strongEnemies.length >= 2 && unknowns.length === 1) {
      const target = unknowns[0];
      target.relation = 'suspected_ally';
      target.score = 0.65;
      target.confidence = 0.75;
      target.reason = `【${seatNames[target.seat]}】其余两家均已显露敌对姿态，按2v2排除法锁定为同盟`;
    } else if (strongAllies.length >= 1 && unknowns.length >= 1) {
      for (const u of unknowns) {
        u.relation = 'suspected_enemy';
        u.score = -0.6;
        u.confidence = 0.68;
        u.reason = `【${seatNames[u.seat]}】己方已有高度可信同盟，按2v2排除法研判为敌对势力`;
      }
    }
  }

  return {
    observerSeat,
    spade3Seat: knownSpade3Seat,
    spadeASeat: knownSpadeASeat,
    isSolo: isSoloPublic || (observerHasSpade3 && observerHasSpadeA),
    soloSeat: isSoloPublic ? publicSpade3Seat : observerHasSpade3 && observerHasSpadeA ? observerSeat : null,
    beliefs: beliefs as [YansanSeatBelief, YansanSeatBelief, YansanSeatBelief, YansanSeatBelief],
  };
}

export interface YansanDecomposedHand {
  straightFlushes: YansanCard[][];
  fourOnes: YansanCard[][];
  fulus: YansanCard[][];
  flushes: YansanCard[][];
  straights: YansanCard[][];
  triplets: YansanCard[][];
  pairs: YansanCard[][];
  pureSingletons: YansanCard[];
  all5CardCombos: YansanCard[][];
}

export interface YansanHandPartition {
  combos: YansanCard[][];
  singles: YansanCard[];
  turnCount: number;
  effectiveTempo: number;
  unbeatableCount: number;
  cost: number;
}

function isYansanControlCard(c: YansanCard): boolean {
  return getYansanRankWeight(c.rank) >= 11; // A(11), 2(12), 3(13)
}

function buildYansanAvailablePool(
  ownHand: YansanCard[],
  seatPlayedCards?: YansanCard[][]
): YansanCard[] {
  const seenIds = new Set<string>();
  if (seatPlayedCards) {
    for (const pList of seatPlayedCards) {
      if (pList) for (const c of pList) seenIds.add(c.id);
    }
  }
  for (const c of ownHand) seenIds.add(c.id);

  const suitDefs: { suit: '♠' | '♥' | '♣' | '♦'; suitVal: number }[] = [
    { suit: '♠', suitVal: 4 },
    { suit: '♥', suitVal: 3 },
    { suit: '♣', suitVal: 2 },
    { suit: '♦', suitVal: 1 },
  ];
  const availablePool: YansanCard[] = [];
  for (let r = 3; r <= 15; r++) {
    const rLabel = getYansanRankLabel(r);
    for (const sd of suitDefs) {
      const cardId = `${rLabel}-${sd.suit}`;
      if (!seenIds.has(cardId)) {
        availablePool.push({
          id: cardId,
          rank: r,
          suitVal: sd.suitVal,
          label: rLabel,
          suit: sd.suit,
          isRed: sd.suit === '♥' || sd.suit === '♦',
        });
      }
    }
  }
  return availablePool;
}

function selectBestKickerForFour(
  sortedAsc: YansanCard[],
  fourRank: number,
  rankGroups: Map<number, YansanCard[]>
): YansanCard | null {
  const candidates = sortedAsc.filter((c) => c.rank !== fourRank);
  if (candidates.length === 0) return null;
  // 优先挑非控制单张（既不是对子/三条、又不是A/2/3的最小孤张）
  const pureLow = candidates.find(
    (c) => (rankGroups.get(c.rank)?.length || 0) === 1 && !isYansanControlCard(c)
  );
  if (pureLow) return pureLow;
  const anyLowSingle = candidates.find((c) => (rankGroups.get(c.rank)?.length || 0) === 1);
  if (anyLowSingle) return anyLowSingle;
  const nonControl = candidates.find((c) => !isYansanControlCard(c));
  return nonControl || candidates[0];
}

/**
 * 智能手牌结构分解：保护顺子、同花、俘虏等整牌，提炼出真正无依无靠的散张 (Pure Singletons)
 * 排序规则：低耗 5 张（顺子 < 同花 < 俘虏）优先出，四带一与同花顺留作大杀器，不再随意乱甩！
 */
export function decomposeYansanHand(cards: YansanCard[]): YansanDecomposedHand {
  const sorted = [...cards].sort((a, b) => getYansanCardPower(a) - getYansanCardPower(b));
  const rankGroups = new Map<number, YansanCard[]>();
  const suitGroups = new Map<string, YansanCard[]>();

  for (const c of sorted) {
    if (!rankGroups.has(c.rank)) rankGroups.set(c.rank, []);
    rankGroups.get(c.rank)!.push(c);
    if (!suitGroups.has(c.suit)) suitGroups.set(c.suit, []);
    suitGroups.get(c.suit)!.push(c);
  }

  const sortedRanks = Array.from(rankGroups.keys()).sort(
    (a, b) => getYansanRankWeight(a) - getYansanRankWeight(b)
  );

  const straightFlushes: YansanCard[][] = [];
  const fourOnes: YansanCard[][] = [];
  const fulus: YansanCard[][] = [];
  const flushes: YansanCard[][] = [];
  const straights: YansanCard[][] = [];

  // 选牌辅助：在同一点数下优先选用散张/低花色，保护♠3、♠A等关键控制张不被普通顺子无谓裹走
  const pickBestStraightCard = (r: number) => {
    const grp = rankGroups.get(r) || [];
    return [...grp].sort((a, b) => a.suitVal - b.suitVal)[0];
  };

  // ① 提取顺子 (包含 A2345 与 34567，排除 10JQKA)
  const uniqueRanks = Array.from(new Set(sorted.map((c) => c.rank))).sort((a, b) => a - b);
  for (let i = 0; i <= uniqueRanks.length - 5; i++) {
    const sub = uniqueRanks.slice(i, i + 5);
    let isSeq = true;
    for (let j = 1; j < 5; j++) {
      if (sub[j] !== sub[j - 1] + 1) {
        isSeq = false;
        break;
      }
    }
    const is10JQKA = sub[0] === 10 && sub[4] === 14;
    if (isSeq && !is10JQKA) {
      const combo = sub.map(pickBestStraightCard);
      const parsed = parseYansanCombo(combo);
      if (parsed && parsed.comboTier === 1) straights.push(combo);
    }
  }
  if ([3, 4, 5, 14, 15].every((r) => uniqueRanks.includes(r))) {
    const combo = [3, 4, 5, 14, 15].map(pickBestStraightCard);
    const parsed = parseYansanCombo(combo);
    if (parsed && parsed.comboTier === 1) straights.push(combo);
  }

  // ② 提取同花与同花顺
  for (const group of suitGroups.values()) {
    if (group.length >= 5) {
      const sSorted = [...group].sort((a, b) => getYansanCardPower(a) - getYansanCardPower(b));
      // 先找同花顺
      const suitRanks = Array.from(new Set(sSorted.map((c) => c.rank))).sort((a, b) => a - b);
      for (let i = 0; i <= suitRanks.length - 5; i++) {
        const subR = suitRanks.slice(i, i + 5);
        if (subR.every((r, idx) => idx === 0 || r === subR[idx - 1] + 1) && !(subR[0] === 10 && subR[4] === 14)) {
          const sf = subR.map((r) => sSorted.find((c) => c.rank === r)!);
          const parsed = parseYansanCombo(sf);
          if (parsed && parsed.comboTier === 5) straightFlushes.push(sf);
        }
      }
      if ([3, 4, 5, 14, 15].every((r) => suitRanks.includes(r))) {
        const sf = [3, 4, 5, 14, 15].map((r) => sSorted.find((c) => c.rank === r)!);
        const parsed = parseYansanCombo(sf);
        if (parsed && parsed.comboTier === 5) straightFlushes.push(sf);
      }

      // 提取同花（优先取低张组合，避免把 3/2/♠A 浪费在普通同花里）
      for (let i = 0; i <= sSorted.length - 5; i++) {
        const sub = sSorted.slice(i, i + 5);
        const parsed = parseYansanCombo(sub);
        if (parsed && parsed.comboTier === 2) {
          flushes.push(sub);
        }
      }
    }
  }

  // ③ 提取俘虏 (葫芦)：主三条搭最小对子，避免拆掉大对 2 / 3
  const triplesList = sortedRanks.filter((rk) => rankGroups.get(rk)!.length >= 3);
  const pairsList = sortedRanks.filter((rk) => rankGroups.get(rk)!.length >= 2);
  for (const tRk of triplesList) {
    for (const pRk of pairsList) {
      if (tRk !== pRk) {
        const fh = [...rankGroups.get(tRk)!.slice(0, 3), ...rankGroups.get(pRk)!.slice(0, 2)];
        const parsed = parseYansanCombo(fh);
        if (parsed && parsed.comboTier === 3) fulus.push(fh);
      }
    }
  }

  // ④ 提取四带一 (带最优小散张)
  const fourRanks = sortedRanks.filter((rk) => rankGroups.get(rk)!.length === 4);
  for (const fRk of fourRanks) {
    const kicker = selectBestKickerForFour(sorted, fRk, rankGroups);
    if (kicker) {
      const fo = [...rankGroups.get(fRk)!, kicker];
      const parsed = parseYansanCombo(fo);
      if (parsed && parsed.comboTier === 4) fourOnes.push(fo);
    }
  }

  // 领出友好的弱到强排序：顺子 -> 同花 -> 俘虏 -> 四带一 -> 同花顺
  const all5CardCombos = [...straights, ...flushes, ...fulus, ...fourOnes, ...straightFlushes];

  // ⑤ 提取三条
  const triplets: YansanCard[][] = [];
  for (const rk of sortedRanks) {
    const grp = rankGroups.get(rk)!;
    if (grp.length === 3) triplets.push(grp);
  }

  // ⑥ 提取对子
  const pairs: YansanCard[][] = [];
  for (const rk of sortedRanks) {
    const grp = rankGroups.get(rk)!;
    if (grp.length === 2) pairs.push(grp);
  }

  // ⑦ 提取纯散牌
  const lockedIn5s = new Set<string>();
  for (const c5 of all5CardCombos) {
    for (const c of c5) lockedIn5s.add(c.id);
  }

  const pureSingletons = sorted.filter((c) => {
    const rkCount = rankGroups.get(c.rank)!.length;
    if (rkCount === 1) return !lockedIn5s.has(c.id);
    return false;
  });

  return {
    straightFlushes,
    fourOnes,
    fulus,
    flushes,
    straights,
    triplets,
    pairs,
    pureSingletons: pureSingletons.length > 0 ? pureSingletons : sorted.filter((c) => rankGroups.get(c.rank)!.length === 1),
    all5CardCombos,
  };
}

/**
 * 全局最小手数拆牌求解器 (Disjoint Optimal Hand Partition)
 * 在 <= 13 张手牌上快速求解互不重叠的最佳组合拆法，平衡“总手数最少”、“大牌控制张保留”、“消灭小单张”。
 */
export function partitionYansanHandOptimal(
  hand: YansanCard[],
  availablePool: YansanCard[] = [],
  maxOpponentHandSize: number = 13
): YansanHandPartition {
  const sorted = [...hand].sort((a, b) => getYansanCardPower(a) - getYansanCardPower(b));
  if (sorted.length === 0) {
    return { combos: [], singles: [], turnCount: 0, effectiveTempo: 0, unbeatableCount: 0, cost: 0 };
  }

  // 收集去重候选多张组合
  const decomp = decomposeYansanHand(sorted);
  const candidateMap = new Map<string, { cards: YansanCard[]; parsed: YansanCombo; bonus: number; unbeatable: boolean }>();

  const addCandidate = (cards: YansanCard[]) => {
    const key = [...cards].map((c) => c.id).sort().join('|');
    if (candidateMap.has(key)) return;
    const parsed = parseYansanCombo(cards);
    if (!parsed) return;
    const unbeatable = availablePool.length > 0
      ? isYansanComboDeductivelyUnbeatable(parsed, availablePool, maxOpponentHandSize)
      : false;
    // 组合质量微调分：优先用顺子/同花/俘虏消掉多张散牌；保护非绝大的控制张不被无谓锁进低价值组合
    let bonus = 0;
    if (unbeatable) bonus += 1.45;
    if (parsed.type === '5_card') {
      bonus += 0.35;
      if ((parsed.comboTier || 1) <= 2 && cards.some((c) => getYansanRankWeight(c.rank) >= 12)) {
        // 把 2 或 3 塞进低阶顺子/同花略微扣分，留给其他更好拆法
        bonus -= 0.4;
      }
    } else if (parsed.type === 'triplet') {
      bonus += 0.25;
    } else if (parsed.type === 'pair') {
      bonus += 0.15;
    }
    candidateMap.set(key, { cards, parsed, bonus, unbeatable });
  };

  for (const c5 of decomp.all5CardCombos) addCandidate(c5);
  for (const tr of decomp.triplets) addCandidate(tr);
  // 对同点4张或3张拆出来的对子也入候选（若3张拆成顺子+对子可能更优）
  const rankGroups = new Map<number, YansanCard[]>();
  for (const c of sorted) {
    if (!rankGroups.has(c.rank)) rankGroups.set(c.rank, []);
    rankGroups.get(c.rank)!.push(c);
  }
  for (const grp of rankGroups.values()) {
    if (grp.length >= 2) {
      addCandidate(grp.slice(0, 2));
      if (grp.length >= 3) addCandidate(grp.slice(grp.length - 2));
    }
    if (grp.length === 4) {
      addCandidate(grp.slice(0, 3));
    }
  }

  const candidates = Array.from(candidateMap.values());
  const cardIndex = new Map<string, number>();
  sorted.forEach((c, idx) => cardIndex.set(c.id, idx));

  const candMasks = candidates.map((cand) => {
    let mask = 0;
    for (const c of cand.cards) {
      mask |= 1 << cardIndex.get(c.id)!;
    }
    return mask;
  });

  let bestPart: YansanHandPartition = {
    combos: [],
    singles: [...sorted],
    turnCount: sorted.length,
    effectiveTempo: sorted.length,
    unbeatableCount: 0,
    cost: 9999,
  };

  const evaluateLeaf = (usedMask: number, chosenIdxs: number[]): YansanHandPartition => {
    const singles: YansanCard[] = [];
    let singlePenalty = 0;
    let singleUnbeatable = 0;
    for (let i = 0; i < sorted.length; i++) {
      if ((usedMask & (1 << i)) === 0) {
        const c = sorted[i];
        singles.push(c);
        const rw = getYansanRankWeight(c.rank);
        const parsedSingle = parseYansanCombo([c])!;
        const isUnb = availablePool.length > 0 && isYansanComboDeductivelyUnbeatable(parsedSingle, availablePool, maxOpponentHandSize);
        if (isUnb) {
          singleUnbeatable++;
          singlePenalty -= 0.9; // 绝大单张不亏手数
        } else if (rw >= 12) {
          singlePenalty += 0.15; // 2/3 是强控制单张，几乎不亏损
        } else if (rw >= 10) {
          singlePenalty += 0.55;
        } else {
          singlePenalty += 1.05; // 弱小散牌越少越好
        }
      }
    }

    let comboBonus = 0;
    let comboUnbeatable = 0;
    const combos: YansanCard[][] = [];
    for (const idx of chosenIdxs) {
      combos.push(candidates[idx].cards);
      comboBonus += candidates[idx].bonus;
      if (candidates[idx].unbeatable) comboUnbeatable++;
    }

    const turnCount = combos.length + singles.length;
    const unbeatableCount = comboUnbeatable + singleUnbeatable;
    const effectiveTempo = Math.max(0, turnCount - unbeatableCount);
    const cost = turnCount * 2.0 + singlePenalty - comboBonus;
    return { combos, singles, turnCount, effectiveTempo, unbeatableCount, cost };
  };

  // 初始化全散牌基线
  bestPart = evaluateLeaf(0, []);

  let nodeCount = 0;
  const MAX_NODES = 900;

  const dfs = (startIdx: number, usedMask: number, chosen: number[]) => {
    if (nodeCount++ > MAX_NODES) return;
    if (chosen.length > 0) {
      const leaf = evaluateLeaf(usedMask, chosen);
      if (leaf.cost < bestPart.cost) bestPart = leaf;
    }
    for (let i = startIdx; i < candidates.length; i++) {
      if ((usedMask & candMasks[i]) === 0) {
        chosen.push(i);
        dfs(i + 1, usedMask | candMasks[i], chosen);
        chosen.pop();
      }
    }
  };

  dfs(0, 0, []);

  // 把 combos 按从弱到强排列，方便领出决策直接消费
  bestPart.combos.sort((a, b) => {
    const pa = parseYansanCombo(a);
    const pb = parseYansanCombo(b);
    const tierA = pa?.type === '5_card' ? (pa.comboTier || 1) : pa?.type === 'triplet' ? 0 : -1;
    const tierB = pb?.type === '5_card' ? (pb.comboTier || 1) : pb?.type === 'triplet' ? 0 : -1;
    if (tierA !== tierB) return tierA - tierB;
    return getYansanRankWeight(pa?.rank || 4) - getYansanRankWeight(pb?.rank || 4);
  });

  return bestPart;
}

/**
 * 终局 N 步连发必胜推算器：
 * 推算手牌中是否存在“一手合法清盘”或“若干手全场绝大牌连控 + 最后唯一收官套牌”的 100% 必胜序列
 */
export function findYansanWinningSequence(
  hand: YansanCard[],
  availablePool: YansanCard[],
  maxOpponentHandSize: number,
  requireDiamond4: boolean = false,
  depth: number = 0
): YansanCard[] | null {
  if (!hand || hand.length === 0) return null;
  const sorted = [...hand].sort((a, b) => getYansanCardPower(a) - getYansanCardPower(b));

  const instant = parseYansanCombo(sorted);
  if (instant && (!requireDiamond4 || sorted.some((c) => c.rank === 4 && c.suit === '♦'))) {
    return sorted;
  }

  if (sorted.length > 11 || depth >= 3) return null;

  const decomp = decomposeYansanHand(sorted);
  const candidateCombos: YansanCard[][] = [
    ...decomp.all5CardCombos,
    ...decomp.triplets,
    ...decomp.pairs,
    ...sorted.map((c) => [c]),
  ];

  // 去重
  const seenKeys = new Set<string>();
  const uniqueCands: YansanCard[][] = [];
  for (const comboCards of candidateCombos) {
    if (requireDiamond4 && !comboCards.some((c) => c.rank === 4 && c.suit === '♦')) continue;
    const k = comboCards.map((c) => c.id).sort().join('|');
    if (!seenKeys.has(k)) {
      seenKeys.add(k);
      uniqueCands.push(comboCards);
    }
  }

  for (const comboCards of uniqueCands) {
    const parsed = parseYansanCombo(comboCards);
    if (!parsed) continue;

    if (isYansanComboDeductivelyUnbeatable(parsed, availablePool, maxOpponentHandSize)) {
      const rmIds = new Set(comboCards.map((cc) => cc.id));
      const remainingHand = sorted.filter((c) => !rmIds.has(c.id));
      if (remainingHand.length === 0) return comboCards;

      const remParsed = parseYansanCombo(remainingHand);
      if (remParsed) {
        return comboCards;
      }

      // 递归寻找多步绝大连击（最多 3 步绝大控场 + 收官套牌）
      if (remainingHand.length <= 8) {
        const subWin = findYansanWinningSequence(remainingHand, availablePool, maxOpponentHandSize, false, depth + 1);
        if (subWin) return comboCards;
      }
    }
  }

  return null;
}

/**
 * 智能强攻评估：AI 是否同时握有 ♠3 & ♠A 且牌型足够强横以一敌三
 */
export function shouldYansanAIQiangGong(hand: YansanCard[]): boolean {
  if (!hand || hand.length !== 13) return false;
  const hasS3 = hand.some((c) => c.rank === 3 && c.suit === '♠');
  const hasSA = hand.some((c) => c.rank === 14 && c.suit === '♠');
  if (!hasS3 || !hasSA) return false;

  const part = partitionYansanHandOptimal(hand, [], 13);
  const bigControls = hand.filter((c) => getYansanRankWeight(c.rank) >= 12).length; // 2 和 3 的总数
  const hasStrong5 = part.combos.some((c5) => {
    const p = parseYansanCombo(c5);
    return p?.type === '5_card' && (p.comboTier || 1) >= 3;
  });
  const weakSingles = part.singles.filter((c) => getYansanRankWeight(c.rank) <= 8).length;

  if (part.turnCount <= 4) return true;
  if (part.turnCount <= 5 && bigControls >= 2) return true;
  if (part.turnCount <= 6 && (bigControls >= 3 || hasStrong5) && weakSingles <= 2) return true;
  return false;
}

/**
 * 增强型高智能 AI 决策中枢 (Grandmaster Smart AI Play Engine)
 * 融合：记牌器推算 + 全局最优手数拆牌 + N步必胜连发 + 2v2/1v3阵营排除推断 + 下家防报单卡位封锁
 */
export function getYansanSmartAIPlay(
  hand: YansanCard[],
  target: YansanCombo | null,
  ctx: YansanAIContext = {}
): YansanAIPlayResult {
  const seat = ctx.seat !== undefined ? ctx.seat : 0;
  const handsCount = ctx.handsCount || [13, 13, 13, 13];
  const sorted = [...hand].sort((a, b) => getYansanCardPower(a) - getYansanCardPower(b));
  const isFirstTurn = !!ctx.isFirstTurn;

  // 1. 推断当前各玩家阵营与敌友置信度
  const inference = inferYansanAlliances(
    seat,
    hand,
    ctx.seatPlayedCards || [[], [], [], []],
    ctx.playHistory || [],
    handsCount,
    ctx.isQiangGong || false,
    ctx.qiangGongSeat ?? null
  );
  const beliefs = inference.beliefs;

  // 找真正的逆时针存活下家
  let downstream = ctx.downstreamSeat !== undefined ? ctx.downstreamSeat : (seat - 1 + 4) % 4;
  if ((handsCount[downstream] ?? 0) === 0) {
    for (let step = 1; step <= 4; step++) {
      const cand = (seat - step + 4) % 4;
      if (cand !== seat && (handsCount[cand] || 0) > 0) {
        downstream = cand;
        break;
      }
    }
  }

  let allySeat: number | null = null;
  let maxAllyScore = 0.35;
  let minNonAllyCards = 99;
  for (let s = 0; s < 4; s++) {
    if (s === seat) continue;
    const rem = handsCount[s] || 0;
    if (rem === 0) continue;
    if (beliefs[s].score > maxAllyScore) {
      maxAllyScore = beliefs[s].score;
      allySeat = s;
    }
    // 若未确认为可信盟友，则都必须计入潜在跑牌风险（杜绝放跑未知身份玩家）
    if (beliefs[s].score < 0.5 && rem < minNonAllyCards) {
      minNonAllyCards = rem;
    }
  }

  const isDownstreamAlly = beliefs[downstream].score >= 0.45;
  const isDownstreamEnemyOrUnknown = beliefs[downstream].score < 0.45;
  const downstreamCards = handsCount[downstream] || 0;

  // 2. 构建全场公开记牌池 (availablePool) 与对手最大手牌数
  const availablePool = buildYansanAvailablePool(hand, ctx.seatPlayedCards);
  let maxOpponentHandSize = 0;
  for (let s = 0; s < 4; s++) {
    if (s !== seat && (handsCount[s] || 0) > maxOpponentHandSize) {
      maxOpponentHandSize = handsCount[s] || 0;
    }
  }

  const decomp = decomposeYansanHand(sorted);
  const optPart = partitionYansanHandOptimal(sorted, availablePool, maxOpponentHandSize);

  // -------------------------------------------------------------------------
  // 终局一击：若一手牌直接合法且能管上/领出，直接走完获胜！
  // -------------------------------------------------------------------------
  const instantCombo = parseYansanCombo(sorted);
  if (instantCombo) {
    if (isFirstTurn) {
      if (sorted.some((c) => c.rank === 4 && c.suit === '♦')) {
        return {
          cards: sorted,
          thought: '手牌完美成型，首出直接打出整套牌型一击清空获胜！',
          tacticalIntent: 'win_clear',
          deducedAlliances: beliefs,
        };
      }
    } else if (!target || canBeatYansanCombo(instantCombo, target)) {
      return {
        cards: sorted,
        thought: '手牌一次性出清！直接终局获胜！',
        tacticalIntent: 'win_clear',
        deducedAlliances: beliefs,
      };
    }
  }

  // -------------------------------------------------------------------------
  // 领出阶段 (Lead: 当桌面无目标牌时自由领出)
  // -------------------------------------------------------------------------
  if (!target) {
    // 0. 终局 N 步绝大连击必胜算力
    const winSeqCombo = findYansanWinningSequence(sorted, availablePool, maxOpponentHandSize, isFirstTurn);
    if (winSeqCombo) {
      const p = parseYansanCombo(winSeqCombo);
      return {
        cards: winSeqCombo,
        thought: `【必胜连发】算定【${p?.label || '绝大牌'}】全场无人能接，先行控盘后即可稳步清盘！`,
        tacticalIntent: 'win_clear',
        deducedAlliances: beliefs,
      };
    }

    // 1. 首出必须带方块4 (♦4)：优先按最优拆牌组合一起顺走
    if (isFirstTurn) {
      const diamond4 = sorted.find((c) => c.rank === 4 && c.suit === '♦');
      if (diamond4) {
        // 优先从最优拆法中找到包含 ♦4 的多张组合（如顺子、同花、四带一、对4、三条4）
        const optWithD4 = optPart.combos.find((cb) => cb.some((c) => c.id === diamond4.id));
        if (optWithD4) {
          const p = parseYansanCombo(optWithD4);
          return {
            cards: optWithD4,
            thought: `首出方块4，顺势打出【${p?.label || '组合牌'}】消减手牌并保持最优牌型`,
            tacticalIntent: 'first_turn_d4',
            deducedAlliances: beliefs,
          };
        }
        for (const st of decomp.straights) {
          if (st.some((c) => c.id === diamond4.id)) {
            return {
              cards: st,
              thought: '首出方块4，顺势甩出5张连贯顺子占尽先机',
              tacticalIntent: 'first_turn_d4',
              deducedAlliances: beliefs,
            };
          }
        }
        const fours = sorted.filter((c) => c.rank === 4);
        if (fours.length >= 2) {
          return {
            cards: fours.length >= 3 ? fours.slice(0, 3) : fours,
            thought: '首出方块4，出对4/三条4起步',
            tacticalIntent: 'first_turn_d4',
            deducedAlliances: beliefs,
          };
        }
        return {
          cards: [diamond4],
          thought: '首出打出底牌方块4试探全局',
          tacticalIntent: 'first_turn_d4',
          deducedAlliances: beliefs,
        };
      }
    }

    // 2. 高危报单/双张局势：仅当“非明确盟友”剩 1 张时，严禁喂小单张！
    let enemySingleThreat = false;
    let enemyPairThreat = false;
    for (let s = 0; s < 4; s++) {
      if (s === seat) continue;
      const rem = handsCount[s] || 0;
      if (rem === 0) continue;
      if (beliefs[s].score < 0.5) {
        if (rem === 1) enemySingleThreat = true;
        if (rem === 2) enemyPairThreat = true;
      }
    }

    // 3. 明确盟友报单/剩2张助攻（且无未明敌人报单抢跑风险，或盟友就在下家）
    if (
      allySeat !== null &&
      beliefs[allySeat].score >= 0.5 &&
      handsCount[allySeat] === 1 &&
      (!enemySingleThreat || allySeat === downstream)
    ) {
      const lowestCard = optPart.singles[0] || sorted[0];
      return {
        cards: [lowestCard],
        thought: `【精准助攻】${['本家', '西席', '北席', '东席'][allySeat]}为己方同盟且已报单，送出底牌${lowestCard.label}保送出清！`,
        tacticalIntent: 'feed_ally',
        deducedAlliances: beliefs,
      };
    }

    if (allySeat !== null && beliefs[allySeat].score >= 0.5 && handsCount[allySeat] === 2 && !enemySingleThreat) {
      const lowOptPair = optPart.combos.find((cb) => cb.length === 2 && getYansanRankWeight(cb[0].rank) <= 9);
      const lowPair = lowOptPair || decomp.pairs.find((p) => getYansanRankWeight(p[0].rank) <= 8);
      if (lowPair) {
        return {
          cards: lowPair,
          thought: `【配合同盟】盟友仅剩2张牌，打出对${lowPair[0].label}协助盟友快速走牌`,
          tacticalIntent: 'feed_ally',
          deducedAlliances: beliefs,
        };
      }
    }

    // 4. 防对手/未知席位报单封杀：绝对禁止送小单牌！
    if (enemySingleThreat) {
      const safeCombos = optPart.combos.length > 0 ? optPart.combos : [...decomp.all5CardCombos, ...decomp.triplets, ...decomp.pairs];
      if (safeCombos.length > 0) {
        const c = safeCombos[0];
        const p = parseYansanCombo(c);
        return {
          cards: c,
          thought: `【防报单封锁】场上有对手仅剩1张牌，出【${p?.label || '多张组合'}】彻底堵死单张通道！`,
          tacticalIntent: 'block_enemy',
          deducedAlliances: beliefs,
        };
      }
      const highestCard = sorted[sorted.length - 1];
      return {
        cards: [highestCard],
        thought: `【顶大牌控场】仅有单牌可出，打出最大单张${highestCard.label}死守防线！`,
        tacticalIntent: 'block_enemy',
        deducedAlliances: beliefs,
      };
    }

    // 5. 对手剩2张（防对子走脱）
    if (enemyPairThreat) {
      const fiveOrTriple = optPart.combos.find((cb) => cb.length === 5 || cb.length === 3);
      if (fiveOrTriple) {
        const p = parseYansanCombo(fiveOrTriple);
        return {
          cards: fiveOrTriple,
          thought: `【残局防对】对手仅余2张，优先出【${p?.label || '组合牌'}】封堵对子`,
          tacticalIntent: 'block_enemy',
          deducedAlliances: beliefs,
        };
      }
      if (optPart.singles.length > 0) {
        // 若单张走低会送给下家，尽量出中等单张；否则出最低孤张迫使对手拆对
        const lowSingle = optPart.singles[0];
        return {
          cards: [lowSingle],
          thought: `【拆解敌对】打出孤张${lowSingle.label}逼迫仅余2张牌的对手拆牌`,
          tacticalIntent: 'lead_singleton',
          deducedAlliances: beliefs,
        };
      }
    }

    // 6. 记牌器联动领出：若对手最大手牌数不足以接某个组合（例如对手最多剩4张而我有5张组合），
    //    直接先甩这张“规则级全场绝大”的弱5张组合，白赚一手出牌权！
    for (const cb of optPart.combos) {
      const parsed = parseYansanCombo(cb);
      if (parsed && isYansanComboDeductivelyUnbeatable(parsed, availablePool, maxOpponentHandSize)) {
        // 但如果本身只剩这手+一手，已在上方 winSeqCombo 处理；多手时若因对手张数不足而绝大，先白嫖甩出！
        const needed = parsed.type === '5_card' ? 5 : parsed.type === 'triplet' ? 3 : parsed.type === 'pair' ? 2 : 1;
        if (maxOpponentHandSize < needed) {
          return {
            cards: cb,
            thought: `【记牌绝大白嫖】对手手牌均不足${needed}张，直接甩出${parsed.label}白赚牌权！`,
            tacticalIntent: 'dump_combo',
            deducedAlliances: beliefs,
          };
        }
      }
    }

    // 7. 常规智能领出（依据最优拆牌，绝不随意先交四带一/同花顺/对2/对3！）
    const nonBombLow5 = optPart.combos.find((cb) => {
      const p = parseYansanCombo(cb);
      return p?.type === '5_card' && (p.comboTier || 1) <= 3 && getYansanRankWeight(p.rank) <= 10;
    });
    if (nonBombLow5) {
      const p = parseYansanCombo(nonBombLow5)!;
      return {
        cards: nonBombLow5,
        thought: `中局先手甩出低耗【${p.label}】，快速压缩手牌并保留大牌控场`,
        tacticalIntent: 'dump_combo',
        deducedAlliances: beliefs,
      };
    }

    // 若下家是已知盟友且牌很少，优先送小散牌或小对子接应盟友
    if (isDownstreamAlly && downstreamCards <= 5 && optPart.singles.length > 0 && !isYansanControlCard(optPart.singles[0])) {
      return {
        cards: [optPart.singles[0]],
        thought: `下家为同盟，轻送小散张${optPart.singles[0].label}协助过牌顺溜`,
        tacticalIntent: 'feed_ally',
        deducedAlliances: beliefs,
      };
    }

    // 挑选最优非控制小组合：低三条(<=Q) / 低对子(<=J) / 弱散牌(<=10) 比较谁更小更安全
    const lowTriplet = optPart.combos.find((cb) => cb.length === 3 && getYansanRankWeight(cb[0].rank) <= 9);
    const lowPair = optPart.combos.find((cb) => cb.length === 2 && getYansanRankWeight(cb[0].rank) <= 8);
    const lowSingle = optPart.singles.find((c) => getYansanRankWeight(c.rank) <= 7);

    // 若手里弱散牌较多(>=3张)且有小单张(<=8)，且下家不是极低手牌敌人，先清理小孤张，不轻易用中大对子消耗
    if (lowSingle && (optPart.singles.length >= 3 || !lowPair) && !(isDownstreamEnemyOrUnknown && downstreamCards <= 3)) {
      return {
        cards: [lowSingle],
        thought: `清理低阶孤张${lowSingle.label}，保护整牌结构与高位控制张`,
        tacticalIntent: 'lead_singleton',
        deducedAlliances: beliefs,
      };
    }
    if (lowTriplet) {
      return {
        cards: lowTriplet,
        thought: `领出中低三条${lowTriplet[0].label}压制节奏`,
        tacticalIntent: 'dump_combo',
        deducedAlliances: beliefs,
      };
    }
    if (lowPair) {
      return {
        cards: lowPair,
        thought: `打出低对${lowPair[0].label}消耗对手对子`,
        tacticalIntent: 'dump_combo',
        deducedAlliances: beliefs,
      };
    }

    // 次选：中等5张/中等三条/中等对子（仍尽量晚交四带一/同花顺）
    const nonSupremeCombo = optPart.combos.find((cb) => {
      const p = parseYansanCombo(cb);
      if (!p) return false;
      if (p.type === '5_card' && (p.comboTier || 1) >= 4) return false;
      if (getYansanRankWeight(p.rank) >= 12) return false;
      return true;
    });
    if (nonSupremeCombo) {
      const p = parseYansanCombo(nonSupremeCombo)!;
      return {
        cards: nonSupremeCombo,
        thought: `领出【${p.label}】掌控主动权`,
        tacticalIntent: 'dump_combo',
        deducedAlliances: beliefs,
      };
    }

    // 若非控制单张存在，先走非控制单张
    const nonControlSingle = optPart.singles.find((c) => !isYansanControlCard(c));
    if (nonControlSingle) {
      return {
        cards: [nonControlSingle],
        thought: `打出散张${nonControlSingle.label}，保留顶级控制张`,
        tacticalIntent: 'lead_singleton',
        deducedAlliances: beliefs,
      };
    }

    // 兜底：若手里只剩大组合/大控制牌，出最弱那一手
    if (optPart.combos.length > 0) {
      const p = parseYansanCombo(optPart.combos[0]);
      return {
        cards: optPart.combos[0],
        thought: `残局控场，领出【${p?.label || '大牌组合'}】`,
        tacticalIntent: 'dump_combo',
        deducedAlliances: beliefs,
      };
    }
    return {
      cards: [sorted[0]],
      thought: `从容领出${sorted[0].label}`,
      tacticalIntent: 'lead_singleton',
      deducedAlliances: beliefs,
    };
  }

  // -------------------------------------------------------------------------
  // 跟牌阶段 (Follow: 目标牌型在桌)
  // -------------------------------------------------------------------------
  const targetSeat = target.seat !== undefined ? target.seat : (seat + 1) % 4;
  const isTargetFromAlly = beliefs[targetSeat].score >= 0.45;
  const isTargetFromEnemy = beliefs[targetSeat].score <= -0.2;

  // 0. 抢赢判胜检查：
  // - 若能直接一击出清 (remHand.length === 0)，立刻出牌获胜；
  // - 若打出“当前全场绝大”的压制牌夺得绝对牌权，且剩余手牌构成 100% N 步必胜序列，才果断出手锁定胜局！
  const allDirectBeats = collectAllLegalBeatingPlays(sorted, target, decomp);
  for (const beatCards of allDirectBeats) {
    const rmIds = new Set(beatCards.map((c) => c.id));
    const remHand = sorted.filter((c) => !rmIds.has(c.id));
    if (remHand.length === 0) {
      return {
        cards: beatCards,
        thought: '一击压制顺势清盘获胜！',
        tacticalIntent: 'win_clear',
        deducedAlliances: beliefs,
      };
    }
    const beatParsed = parseYansanCombo(beatCards);
    if (
      beatParsed &&
      isYansanComboDeductivelyUnbeatable(beatParsed, availablePool, maxOpponentHandSize) &&
      findYansanWinningSequence(remHand, availablePool, maxOpponentHandSize, false)
    ) {
      return {
        cards: beatCards,
        thought: `【锁定胜局】打出全场绝大【${beatParsed.label || '绝杀牌'}】夺得绝对牌权，剩余手牌已形成100%必胜连发！`,
        tacticalIntent: 'win_clear',
        deducedAlliances: beliefs,
      };
    }
  }

  // 1. 盟友出牌保送与让行原则（彻底取消随机乱顶，且绝不卡自己盟友下家）
  if (isTargetFromAlly) {
    const targetRW = getYansanRankWeight(target.rank);
    const isTargetAlreadyUnbeatable = isYansanComboDeductivelyUnbeatable(target, availablePool, maxOpponentHandSize);
    const isTargetStrong =
      isTargetAlreadyUnbeatable ||
      (target.type === 'single' && targetRW >= 10) ||
      (target.type === 'pair' && targetRW >= 9) ||
      target.type === 'triplet' ||
      (target.type === '5_card' && (target.comboTier || 1) >= 2);

    // 若桌面本就较大、盟友手牌少(<=4)、或下家还是己方同盟、或桌面已是绝大，直接无条件让行！
    if (isTargetStrong || handsCount[targetSeat] <= 4 || isDownstreamAlly) {
      return {
        cards: null,
        thought: `【盟友让行】桌面牌由盟友【${['本家', '西席', '北席', '东席'][targetSeat]}】掌控，主动过牌保送盟友连击！`,
        tacticalIntent: 'yield_ally',
        deducedAlliances: beliefs,
      };
    }

    // 仅当下家是危险敌方/未知玩家(<=5张)且盟友出了极小牌时，才考虑用“不拆牌的中档单/对”(<=Q)顺便替盟友接风顶死下家
    if (isDownstreamEnemyOrUnknown && downstreamCards <= 5) {
      const mediumCover = findMinimumEffectiveBeat(sorted, target, decomp, availablePool, maxOpponentHandSize, false, optPart);
      if (mediumCover) {
        const mc = parseYansanCombo(mediumCover);
        const mcRW = getYansanRankWeight(mc?.rank || 15);
        if (mc && mc.type !== '5_card' && mcRW >= 8 && mcRW <= 10) {
          return {
            cards: mediumCover,
            thought: `【接风挡敌】下家【${['本家', '西席', '北席', '东席'][downstream]}】手牌仅剩${downstreamCards}张，替盟友用${mc.label}卡位阻断！`,
            tacticalIntent: 'cover_ally',
            deducedAlliances: beliefs,
          };
        }
      }
    }

    return {
      cards: null,
      thought: '【让牌配合】盟友控牌，保留己方实力待机出手',
      tacticalIntent: 'yield_ally',
      deducedAlliances: beliefs,
    };
  }

  // 2. 敌方或未知威胁拦截 (综合考虑出牌者与下家谁更危险！)
  const targetEnemyCards = handsCount[targetSeat] || 10;
  const downstreamDanger =
    target.type === 'single' && isDownstreamEnemyOrUnknown && downstreamCards <= 2;
  const anyNonAllySingleDanger = target.type === 'single' && minNonAllyCards <= 2;
  const isDesperateSituation =
    targetEnemyCards <= 3 || downstreamDanger || anyNonAllySingleDanger || minNonAllyCards <= 2;

  // 如果处于危急关头且手上有多个管牌方案，优先选用最小的“全场绝大”牌瞬间锁死敌方
  if (isDesperateSituation && allDirectBeats.length > 1) {
    for (const beatCards of allDirectBeats) {
      const p = parseYansanCombo(beatCards);
      if (p && isYansanComboDeductivelyUnbeatable(p, availablePool, maxOpponentHandSize)) {
        return {
          cards: beatCards,
          thought: `【记牌绝杀拦截】局势告急，果断动用当前全场绝大【${p.label}】夺权封锁！`,
          tacticalIntent: 'block_enemy',
          deducedAlliances: beliefs,
        };
      }
    }
  }

  const winningPlay = findMinimumEffectiveBeat(
    sorted,
    target,
    decomp,
    availablePool,
    maxOpponentHandSize,
    isDesperateSituation,
    optPart
  );

  if (winningPlay) {
    const playCombo = parseYansanCombo(winningPlay)!;
    const isPlayUnbeatable = isYansanComboDeductivelyUnbeatable(playCombo, availablePool, maxOpponentHandSize);
    const isTrueSupreme = playCombo.rank === 3 && playCombo.suitVal === 4;
    const isEarlyGame = sorted.length >= 8;

    // 仅在早中期、非绝境、且未确定是敌人且并非能直接保住节奏时才留黑桃3
    if (isTrueSupreme && isEarlyGame && !isDesperateSituation && !isTargetFromEnemy && !isPlayUnbeatable) {
      return {
        cards: null,
        thought: '【战术隐忍】保留黑桃3终极大杀器，伺机一击必杀',
        tacticalIntent: 'strategic_pass',
        deducedAlliances: beliefs,
      };
    }

    return {
      cards: winningPlay,
      thought: isDesperateSituation
        ? `【绝境封锁】对手手牌告急，果断打出【${playCombo.label}】强力拦截阻断敌方！`
        : isPlayUnbeatable
        ? `【记牌精准压制】打出当前全场绝大【${playCombo.label}】直接夺回领出权！`
        : `【精准拦截】打出${playCombo.label}管上对手，夺回牌桌主导权`,
      tacticalIntent: isDesperateSituation ? 'block_enemy' : 'counter_enemy',
      deducedAlliances: beliefs,
    };
  }

  return {
    cards: null,
    thought: '保留关键整牌或牌力不济，暂时过牌以逸待劳',
    tacticalIntent: 'strategic_pass',
    deducedAlliances: beliefs,
  };
}

/**
 * 枚举手牌中所有能够合法压制 target 的组合（按代价从轻到重排序）
 */
function collectAllLegalBeatingPlays(
  sorted: YansanCard[],
  target: YansanCombo,
  decomp: YansanDecomposedHand
): YansanCard[][] {
  const results: YansanCard[][] = [];
  const seen = new Set<string>();
  const pushIfValid = (cards: YansanCard[]) => {
    const parsed = parseYansanCombo(cards);
    if (!parsed || !canBeatYansanCombo(parsed, target)) return;
    const k = cards.map((c) => c.id).sort().join('|');
    if (!seen.has(k)) {
      seen.add(k);
      results.push(cards);
    }
  };

  if (target.type === 'single') {
    for (const c of sorted) pushIfValid([c]);
  } else if (target.type === 'pair') {
    const rankGroups = new Map<number, YansanCard[]>();
    for (const c of sorted) {
      if (!rankGroups.has(c.rank)) rankGroups.set(c.rank, []);
      rankGroups.get(c.rank)!.push(c);
    }
    for (const grp of rankGroups.values()) {
      if (grp.length >= 2) {
        pushIfValid([grp[0], grp[1]]);
        if (grp.length >= 3) pushIfValid([grp[grp.length - 2], grp[grp.length - 1]]);
      }
    }
  } else if (target.type === 'triplet') {
    const rankGroups = new Map<number, YansanCard[]>();
    for (const c of sorted) {
      if (!rankGroups.has(c.rank)) rankGroups.set(c.rank, []);
      rankGroups.get(c.rank)!.push(c);
    }
    for (const grp of rankGroups.values()) {
      if (grp.length >= 3) pushIfValid(grp.slice(0, 3));
    }
  } else if (target.type === '5_card') {
    for (const c5 of decomp.all5CardCombos) pushIfValid(c5);
  }

  results.sort((a, b) => {
    const pa = parseYansanCombo(a)!;
    const pb = parseYansanCombo(b)!;
    const tierDiff = (pa.comboTier || 1) - (pb.comboTier || 1);
    if (tierDiff !== 0) return tierDiff;
    const rwDiff = getYansanRankWeight(pa.rank) - getYansanRankWeight(pb.rank);
    if (rwDiff !== 0) return rwDiff;
    return (pa.suitVal || 0) - (pb.suitVal || 0);
  });
  return results;
}

/**
 * 寻找最小且代价最优的合法压制组合：
 * 1. 修复旧版把 ♥3/♣3/♦3 与 对3 当成低牌乱丢的严重 bug（统一使用 rankWeight >= 11 判断大牌控制张）
 * 2. 保护最优拆牌中的整牌，优先出散牌/对子中不拆套牌的牌
 * 3. 若打出后恰好成为全场绝大 (availablePool 推算)，允许在高压局面直接接盘
 */
function findMinimumEffectiveBeat(
  hand: YansanCard[],
  target: YansanCombo,
  decomp: YansanDecomposedHand,
  availablePool: YansanCard[] = [],
  maxOpponentHandSize: number = 13,
  isEmergency: boolean = false,
  optPart?: YansanHandPartition
): YansanCard[] | null {
  const sorted = [...hand].sort((a, b) => getYansanCardPower(a) - getYansanCardPower(b));
  const rankGroups = new Map<number, YansanCard[]>();
  for (const c of sorted) {
    if (!rankGroups.has(c.rank)) rankGroups.set(c.rank, []);
    rankGroups.get(c.rank)!.push(c);
  }

  // 1. 单张管牌
  if (target.type === 'single') {
    const targetRW = getYansanRankWeight(target.rank);
    const optSingles = optPart ? optPart.singles : decomp.pureSingletons;

    // ① 优先从最优拆牌的真正散牌中找能管上的最小单张（绝不拆顺子/同花/对子/三条）
    for (const c of optSingles) {
      const cand = parseYansanCombo([c]);
      if (!cand || !canBeatYansanCombo(cand, target)) continue;
      const cRW = getYansanRankWeight(c.rank);
      const isHeavyControl = cRW >= 12; // 2 或 3 (含 ♥3/♣3/♦3/♠3)
      const isMediumControl = cRW === 11; // A
      // 严禁拿 2 或 任何花色的 3 去压 10 及以下的小单牌（除非紧急拦截或手牌极少）
      if (isHeavyControl && targetRW <= 8 && !isEmergency && sorted.length > 3) continue;
      if (isMediumControl && targetRW <= 5 && !isEmergency && sorted.length > 5) continue;
      return [c];
    }

    // ② 若目标是高牌 (>=J) 或处于绝境，允许从 3 张相同点数的组里拆出 1 张（但不拆 4 张炸弹除非紧急）
    if (isEmergency || targetRW >= 8) {
      for (const c of sorted) {
        const cnt = rankGroups.get(c.rank)!.length;
        if (cnt === 3 || (cnt === 4 && isEmergency)) {
          const cand = parseYansanCombo([c]);
          if (cand && canBeatYansanCombo(cand, target)) {
            const cRW = getYansanRankWeight(c.rank);
            if (cRW >= 12 && targetRW <= 8 && !isEmergency && sorted.length > 4) continue;
            return [c];
          }
        }
      }
    }

    // ③ 拆对子（最后手段）：仅在对手大牌(>=K)、敌方告急或手牌极少时才允许拆对；绝不平时拆对2/对3/对A打小牌
    const pairCount = Array.from(rankGroups.values()).filter((g) => g.length === 2).length;
    const isTargetHigh = targetRW >= 10; // >= K
    if (isEmergency || sorted.length <= 3 || (isTargetHigh && pairCount >= 2)) {
      for (const c of sorted) {
        const cRW = getYansanRankWeight(c.rank);
        if (cRW >= 11 && !isEmergency && sorted.length > 3) continue;
        const cand = parseYansanCombo([c]);
        if (cand && canBeatYansanCombo(cand, target)) {
          return [c];
        }
      }
    }
    return null;
  }

  // 2. 对子管牌
  if (target.type === 'pair') {
    const targetRW = getYansanRankWeight(target.rank);
    const optPairs = (optPart?.combos || []).filter((cb) => cb.length === 2);
    const pairPool = [...optPairs, ...decomp.pairs];

    for (const p of pairPool) {
      const cand = parseYansanCombo(p);
      if (!cand || !canBeatYansanCombo(cand, target)) continue;
      const pRW = getYansanRankWeight(p[0].rank);
      // 保护 对A / 对2 / 对3：不拿大对子去压 对4~对10（除非紧急或手牌很少）
      if (pRW >= 12 && targetRW <= 8 && !isEmergency && sorted.length > 4) continue;
      if (pRW === 11 && targetRW <= 5 && !isEmergency && sorted.length > 5) continue;
      return p;
    }

    // 拆三条的对子（需目标较大或紧急）
    if (isEmergency || targetRW >= 8 || sorted.length <= 5) {
      for (const t of decomp.triplets) {
        const subPair = t.slice(0, 2);
        const cand = parseYansanCombo(subPair);
        if (cand && canBeatYansanCombo(cand, target)) {
          return subPair;
        }
      }
    }

    if (isEmergency || sorted.length <= 4) {
      for (const grp of rankGroups.values()) {
        if (grp.length >= 2) {
          const subPair = grp.slice(0, 2);
          const cand = parseYansanCombo(subPair);
          if (cand && canBeatYansanCombo(cand, target)) {
            return subPair;
          }
        }
      }
    }
    return null;
  }

  // 3. 三条管牌
  if (target.type === 'triplet') {
    const targetRW = getYansanRankWeight(target.rank);
    for (const t of decomp.triplets) {
      const cand = parseYansanCombo(t);
      if (cand && canBeatYansanCombo(cand, target)) {
        if (getYansanRankWeight(t[0].rank) >= 12 && targetRW <= 6 && !isEmergency && sorted.length > 5) continue;
        return t;
      }
    }
    for (const grp of rankGroups.values()) {
      if (grp.length >= 3) {
        const sub = grp.slice(0, 3);
        const cand = parseYansanCombo(sub);
        if (cand && canBeatYansanCombo(cand, target)) {
          return sub;
        }
      }
    }
    return null;
  }

  // 4. 5张牌型管牌（按代价升序，避免轻易拿同花顺/四带一接小顺子）
  if (target.type === '5_card') {
    const winning5s: YansanCard[][] = [];
    for (const c5 of decomp.all5CardCombos) {
      const cand = parseYansanCombo(c5);
      if (cand && canBeatYansanCombo(cand, target)) {
        winning5s.push(c5);
      }
    }
    if (winning5s.length > 0) {
      winning5s.sort((a, b) => {
        const ca = parseYansanCombo(a)!;
        const cb = parseYansanCombo(b)!;
        return (
          (ca.comboTier || 1) - (cb.comboTier || 1) ||
          getYansanRankWeight(ca.rank) - getYansanRankWeight(cb.rank) ||
          (ca.suitVal || 0) - (cb.suitVal || 0)
        );
      });
      const best5 = winning5s[0];
      const bestParsed = parseYansanCombo(best5)!;
      const targTier = target.comboTier || 1;
      // 如果对手只是普通顺子，自己仅剩手牌很多且需要拆大炸弹(四带一/同花顺)才能管时，除非对方牌很少或可绝大，否则忍让
      if (
        targTier === 1 &&
        (bestParsed.comboTier || 1) >= 4 &&
        !isEmergency &&
        sorted.length >= 9 &&
        !isYansanComboDeductivelyUnbeatable(bestParsed, availablePool, maxOpponentHandSize)
      ) {
        return null;
      }
      return best5;
    }
  }

  return null;
}

export function getYansanAIPlay(
  hand: YansanCard[],
  target: YansanCombo | null,
  isFirstTurnOrContext: boolean | YansanAIContext = false
): YansanCard[] | null {
  if (!hand || hand.length === 0) return null;
  const ctx: YansanAIContext =
    typeof isFirstTurnOrContext === 'boolean'
      ? { isFirstTurn: isFirstTurnOrContext }
      : isFirstTurnOrContext;

  const result = getYansanSmartAIPlay(hand, target, ctx);
  return result.cards;
}

/**
 * 判定某一特定玩家手牌中是否存在任何合法牌型能够压制目标 target 牌型
 */
export function canHandBeatYansanCombo(hand: YansanCard[], target: YansanCombo): boolean {
  if (!hand || hand.length === 0 || !target) return false;

  // 1. 单张
  if (target.type === 'single') {
    const sortedDesc = [...hand].sort((a, b) => getYansanCardPower(b) - getYansanCardPower(a));
    const maxCard = sortedDesc[0];
    const cand = parseYansanCombo([maxCard]);
    return !!(cand && canBeatYansanCombo(cand, target));
  }

  // 2. 对子
  if (target.type === 'pair') {
    if (hand.length < 2) return false;
    const rankMap = new Map<number, YansanCard[]>();
    for (const c of hand) {
      if (!rankMap.has(c.rank)) rankMap.set(c.rank, []);
      rankMap.get(c.rank)!.push(c);
    }
    for (const grp of rankMap.values()) {
      if (grp.length >= 2) {
        const sortedByPower = [...grp].sort((a, b) => getYansanCardPower(b) - getYansanCardPower(a));
        const cand = parseYansanCombo([sortedByPower[0], sortedByPower[1]]);
        if (cand && canBeatYansanCombo(cand, target)) {
          return true;
        }
      }
    }
    return false;
  }

  // 3. 三条
  if (target.type === 'triplet') {
    if (hand.length < 3) return false;
    const rankMap = new Map<number, YansanCard[]>();
    for (const c of hand) {
      if (!rankMap.has(c.rank)) rankMap.set(c.rank, []);
      rankMap.get(c.rank)!.push(c);
    }
    for (const grp of rankMap.values()) {
      if (grp.length >= 3) {
        const cand = parseYansanCombo(grp.slice(0, 3));
        if (cand && canBeatYansanCombo(cand, target)) {
          return true;
        }
      }
    }
    return false;
  }

  // 4. 五张牌型
  if (target.type === '5_card') {
    if (hand.length < 5) return false;
    // 如果当前是黑桃至尊同花顺且具有最高单牌，全场绝大
    if (target.comboTier === 5 && target.suitVal === 4 && target.rank === 3) {
      return false;
    }
    const aiPlay = getYansanAIPlay(hand, target);
    return !!(aiPlay && aiPlay.length === 5);
  }

  return false;
}

/**
 * 判定其他三位玩家中，是否有任意一位玩家的独立手牌能够压制当前打出的 combo。
 */
export function canOpponentsBeatYansanCombo(
  playedCombo: YansanCombo,
  opponentHands: YansanCard[][]
): boolean {
  if (!playedCombo) return false;
  for (const hand of opponentHands) {
    if (hand && hand.length > 0 && canHandBeatYansanCombo(hand, playedCombo)) {
      return true;
    }
  }
  return false;
}

/**
 * 检验剩余未知牌池中，是否能组合出任何大于 target 的 5 张牌型
 */
export function canPoolBeatYansan5CardCombo(
  pool: YansanCard[],
  target: YansanCombo
): boolean {
  if (!pool || pool.length < 5) return false;

  const targetTier = target.comboTier || 1;
  const targetRW = getYansanRankWeight(target.rank);
  const targetSuit = target.suitVal || 0;

  // 准备花色分组与点数分组
  const suitMap = new Map<string, YansanCard[]>();
  const rankMap = new Map<number, YansanCard[]>();
  for (const c of pool) {
    if (!suitMap.has(c.suit)) suitMap.set(c.suit, []);
    suitMap.get(c.suit)!.push(c);
    if (!rankMap.has(c.rank)) rankMap.set(c.rank, []);
    rankMap.get(c.rank)!.push(c);
  }

  const legalStraightRankSets = [
    [3, 4, 5, 14, 15],
    [3, 4, 5, 6, 7],
    [4, 5, 6, 7, 8],
    [5, 6, 7, 8, 9],
    [6, 7, 8, 9, 10],
    [7, 8, 9, 10, 11],
    [8, 9, 10, 11, 12],
    [9, 10, 11, 12, 13],
  ];

  // (1) 检查同花顺 (Tier 5)
  for (const [, sCards] of suitMap.entries()) {
    if (sCards.length >= 5) {
      const sRankSet = new Set(sCards.map((c) => c.rank));
      for (const rSet of legalStraightRankSets) {
        if (rSet.every((r) => sRankSet.has(r))) {
          const straightCards = rSet.map((r) => sCards.find((c) => c.rank === r)!);
          const parsed = parseYansanCombo(straightCards);
          if (parsed && canBeatYansanCombo(parsed, target)) {
            return true;
          }
        }
      }
    }
  }

  if (targetTier === 5) return false;

  // (2) 检查四带一 (Tier 4)
  const fourRanks = Array.from(rankMap.entries())
    .filter(([, list]) => list.length === 4)
    .map(([r]) => r);
  if (fourRanks.length > 0 && pool.length >= 5) {
    if (targetTier < 4) return true;
    for (const fr of fourRanks) {
      if (getYansanRankWeight(fr) > targetRW) return true;
    }
  }

  if (targetTier === 4) return false;

  // (3) 检查俘虏 (Tier 3)
  const tripleRanks = Array.from(rankMap.entries())
    .filter(([, list]) => list.length >= 3)
    .map(([r]) => r);
  const pairRanks = Array.from(rankMap.entries())
    .filter(([, list]) => list.length >= 2)
    .map(([r]) => r);

  for (const tr of tripleRanks) {
    for (const pr of pairRanks) {
      if (tr !== pr) {
        if (targetTier < 3) return true;
        if (targetTier === 3 && getYansanRankWeight(tr) > targetRW) return true;
      }
    }
  }

  if (targetTier === 3) return false;

  // (4) 检查同花 (Tier 2)
  for (const [, sCards] of suitMap.entries()) {
    if (sCards.length >= 5) {
      const suitVal = sCards[0].suitVal;
      if (targetTier < 2) return true;
      if (targetTier === 2) {
        if (suitVal > targetSuit) return true;
        if (suitVal === targetSuit) {
          const sortedDesc = [...sCards].sort((a, b) => getYansanCardPower(b) - getYansanCardPower(a));
          const maxPower = getYansanCardPower(sortedDesc[0]);
          const targetPower = getYansanRankWeight(target.rank) * 10 + targetSuit;
          if (maxPower > targetPower) return true;
        }
      }
    }
  }

  if (targetTier === 2) return false;

  // (5) 检查顺子 (Tier 1)
  for (const rSet of legalStraightRankSets) {
    if (rSet.every((r) => rankMap.has(r) && rankMap.get(r)!.length > 0)) {
      const cards = rSet.map((r) => {
        const list = rankMap.get(r)!;
        return [...list].sort((a, b) => getYansanCardPower(b) - getYansanCardPower(a))[0];
      });
      const parsed = parseYansanCombo(cards);
      if (parsed && canBeatYansanCombo(parsed, target)) {
        return true;
      }
    }
  }

  return false;
}

/**
 * 烟三基于公开已出牌库（公开记牌推算）的绝对压制判定：
 * 
 * 核心设计原则（杜绝信息泄露，确保绝对公开推算）：
 * 1. 绝不可通过读取对手暗牌来判定！
 * 2. 严禁结合出牌者自身的剩余手牌来推算！
 *    因为全场最大并直接连出是公开的全局广播行为。如果结合出牌者自己手里还保留的手牌（例如出了一对三红桃方块，自己手里还捏着梅花三）
 *    而判定全场最大连出，就会直接向全场暴露该玩家手里还藏有另一张三！
 * 3. 必须严格仅依据公开打出过的废牌堆 (playedCardsHistory) 与对手剩余张数，推算出“全场公开未出牌池 (availablePool)”中是否存在任何理论上能压制当前牌型的组合：
 *    - 张数推算：若全场对手中最多手牌者张数不足（如打出5张牌，但对手最多只剩4张），则必定绝大！
 *    - 单张推算：若公开未出牌池中不存在任何点数更大、或同点数花色更大的单张（如♠3全场至尊，或♠3已出后的♥3），则必定绝大！
 *    - 对子推算：若公开未出牌池中任何大于当前对子的点数，剩余可用张数都小于2张（无法凑出一对）；且同点数的无法产生更大花色对子，则必定绝大！
 *    - 三条推算：若公开未出牌池中任何大于当前三条的点数，剩余可用张数都小于3张（如3张3已出，打出三条2），则必定绝大！
 *    - 五张牌型推算：若公开未出牌池完全无法拼出任何能够压制当前牌型的五张组合（同花顺、四带一、俘虏、同花、顺子），则必定绝大！
 * 
 * 若返回 true，说明从全场公开信息推算，任何对手在数学物理意义上都不可能要得起，无需等待对手过牌，首出者直接保留出牌权连打！
 */
export function isYansanComboDeductivelyUnbeatable(
  combo: YansanCombo,
  availablePool: YansanCard[],
  maxOpponentHandSize: number
): boolean {
  if (!combo) return false;

  // 1. 张数推算：若对手最多手牌数少于当前牌型所需张数，全场无人能接
  const neededCards = combo.type === '5_card' ? 5 : combo.type === 'triplet' ? 3 : combo.type === 'pair' ? 2 : 1;
  if (maxOpponentHandSize < neededCards || availablePool.length < neededCards) {
    return true;
  }

  // 2. 单张推算
  if (combo.type === 'single') {
    const targetPower = getYansanRankWeight(combo.rank) * 10 + (combo.suitVal || 0);
    for (const c of availablePool) {
      const p = getYansanRankWeight(c.rank) * 10 + c.suitVal;
      if (p > targetPower) {
        return false;
      }
    }
    return true;
  }

  // 3. 对子推算
  if (combo.type === 'pair') {
    const targetRW = getYansanRankWeight(combo.rank);
    const targetSuit = combo.suitVal || 0;

    const rankMap = new Map<number, YansanCard[]>();
    for (const c of availablePool) {
      if (!rankMap.has(c.rank)) rankMap.set(c.rank, []);
      rankMap.get(c.rank)!.push(c);
    }

    for (const [rank, grp] of rankMap.entries()) {
      if (grp.length >= 2) {
        const rw = getYansanRankWeight(rank);
        if (rw > targetRW) return false;
        if (rw === targetRW) {
          const maxSuitInPool = Math.max(...grp.map((c) => c.suitVal));
          if (maxSuitInPool > targetSuit) return false;
        }
      }
    }
    return true;
  }

  // 4. 三条推算
  if (combo.type === 'triplet') {
    const targetRW = getYansanRankWeight(combo.rank);
    const rankCounts = new Map<number, number>();
    for (const c of availablePool) {
      rankCounts.set(c.rank, (rankCounts.get(c.rank) || 0) + 1);
    }
    for (const [rank, count] of rankCounts.entries()) {
      if (count >= 3 && getYansanRankWeight(rank) > targetRW) {
        return false;
      }
    }
    return true;
  }

  // 5. 五张牌型推算
  if (combo.type === '5_card') {
    return !canPoolBeatYansan5CardCombo(availablePool, combo);
  }

  return false;
}

export interface YansanSpecialHolders {
  spade3Seat: number;
  spadeASeat: number;
  isSolo: boolean; // True if same seat holds both ♠3 and ♠A
  soloSeat: number | null;
}

export function detectYansanSpecialHolders(
  hands: YansanCard[][],
  seatPlayedCards?: YansanCard[][]
): YansanSpecialHolders {
  let spade3Seat = -1;
  let spadeASeat = -1;

  for (let s = 0; s < hands.length; s++) {
    const hand = hands[s] || [];
    const played = seatPlayedCards ? (seatPlayedCards[s] || []) : [];
    if (hand.some((c) => c.rank === 3 && c.suit === '♠') || played.some((c) => c.rank === 3 && c.suit === '♠')) {
      spade3Seat = s;
    }
    if (hand.some((c) => c.rank === 14 && c.suit === '♠') || played.some((c) => c.rank === 14 && c.suit === '♠')) {
      spadeASeat = s;
    }
  }

  if (spade3Seat === -1) spade3Seat = 0;
  if (spadeASeat === -1) spadeASeat = 0;

  const isSolo = spade3Seat === spadeASeat;
  return {
    spade3Seat,
    spadeASeat,
    isSolo,
    soloSeat: isSolo ? spade3Seat : null,
  };
}

export interface YansanSettlement {
  winnerSeat: number;
  winnerAlliance: string;
  isQiangGong: boolean;
  qiangGongSeat: number | null;
  seatScores: [number, number, number, number];
  summary: string;
  isTie?: boolean;
}

export function calculateYansanSettlement(
  roomType: string,
  winnerSeat: number,
  isQiangGong: boolean,
  qiangGongSeat: number | null,
  spade3Seat: number,
  spadeASeat: number,
  finishedSeats: number[]
): YansanSettlement {
  const lowStake = String(roomType).includes('20-40') ? 20 : String(roomType).includes('10-20') ? 10 : 5;
  const highStake = lowStake * 2;
  const seatNames = ['本家', '西席', '北席', '东席'];

  if (isQiangGong && qiangGongSeat !== null) {
    // 强攻规则：全胜翻倍或翻倍全输，根据所选场景底分判定：
    // 5-10分场：强攻者赢60分（其余3家各输20分）；强攻者输60分（其余3家各得20分）
    // 10-20分场：强攻者赢120分（其余3家各输40分）；强攻者输120分（其余3家各得40分）
    // 20-40分场：强攻者赢240分（其余3家各输80分）；强攻者输240分（其余3家各得80分）
    const eachStake = highStake * 2;
    const totalStake = eachStake * 3;
    const isSoloWin = winnerSeat === qiangGongSeat;

    const scores: [number, number, number, number] = [0, 0, 0, 0];
    for (let i = 0; i < 4; i++) {
      if (i === qiangGongSeat) {
        scores[i] = isSoloWin ? totalStake : -totalStake;
      } else {
        scores[i] = isSoloWin ? -eachStake : eachStake;
      }
    }

    const summary = isSoloWin
      ? `${seatNames[qiangGongSeat]} 开启强攻率先出清，全胜翻倍！独赢 +${totalStake} 分，其余三家各输 -${eachStake} 分`
      : `强攻被破！${seatNames[winnerSeat]} 率先出清，强攻者 ${seatNames[qiangGongSeat]} 翻倍全输 -${totalStake} 分，其余三家各得 +${eachStake} 分`;

    return {
      winnerSeat,
      winnerAlliance: isSoloWin ? `${seatNames[qiangGongSeat]} (强攻独胜)` : `${seatNames[winnerSeat]} (防守同盟)`,
      isQiangGong: true,
      qiangGongSeat,
      seatScores: scores,
      summary,
    };
  }

  const scores: [number, number, number, number] = [0, 0, 0, 0];

  // 经典 2v2 对局：同盟由 ♠3 与 ♠A 持有者构成
  if (spade3Seat !== spadeASeat) {
    const isSpade = (s: number) => s === spade3Seat || s === spadeASeat;

    const p1 = finishedSeats[0] ?? winnerSeat;
    const p2 = finishedSeats[1] ?? -1;
    const p3 = finishedSeats[2] ?? -1;

    let spadeWinPoints = 0;
    let winningAllianceName = '';
    let summary = '';
    let isTie = false;

    // 规则判定：
    // ① 双胜（双跑）：同一阵营两人包揽前两名（如1和3或2和4先出清手牌）
    // 5-10分场：胜方各得10分，负方各输10分
    if (p2 !== -1 && isSpade(p1) === isSpade(p2)) {
      if (isSpade(p1) && isSpade(p2)) {
        // 1号与3号玩家包揽前二 (1,3 或 3,1)
        spadeWinPoints = highStake;
        winningAllianceName = '黑桃同盟 (♠3 & ♠A)';
        summary = `黑桃同盟 (♠3 & ♠A) 双跑全胜！包揽前两名率先出清，各得 +${highStake} 分，平民各输 -${highStake} 分`;
      } else {
        // 2号与4号玩家包揽前二 (2,4 或 4,2)
        spadeWinPoints = -highStake;
        winningAllianceName = '平民同盟';
        summary = `平民同盟 双跑全胜！包揽前两名率先出清，各得 +${highStake} 分，黑桃同盟各输 -${highStake} 分`;
      }
    } else if (p3 !== -1) {
      // ② 出清三人时分出胜负或打和：
      const p1IsSpade = isSpade(p1);
      const p3IsSpade = isSpade(p3);

      if (p1IsSpade && p3IsSpade) {
        // 顺序 123 / 321 / 143 / 341：黑桃同盟拿下第1名与第3名出清
        // 5-10分场：1和3各得5分，2和4各输5分
        spadeWinPoints = lowStake;
        winningAllianceName = '黑桃同盟 (♠3 & ♠A)';
        summary = `黑桃同盟 (♠3 & ♠A) 单胜！夺得第一名及第三名出清，各得 +${lowStake} 分，平民各输 -${lowStake} 分`;
      } else if (!p1IsSpade && !p3IsSpade) {
        // 顺序 214 / 234 / 412 / 432：平民同盟拿下第1名与第3名出清
        // 5-10分场：2和4各得5分，1和3各输5分
        spadeWinPoints = -lowStake;
        winningAllianceName = '平民同盟';
        summary = `平民同盟 单胜！夺得第一名及第三名出清，各得 +${lowStake} 分，黑桃同盟各输 -${lowStake} 分`;
      } else {
        // ③ 打和：顺序 124 / 142 / 324 / 342 / 213 / 231 / 413 / 431
        // 一方拿第1名，但另一方包揽第2名和第3名出清，双方打和，大家都没有输赢 (各得0分)
        spadeWinPoints = 0;
        winningAllianceName = '双方平局 (打和)';
        isTie = true;
        summary = `势均力敌 · 本局打和！一方拿下头彩但另一方包揽二三名，双方势均力敌，大家都没有输赢 (各 0 分)`;
      }
    } else {
      // 容错备用
      if (isSpade(p1)) {
        spadeWinPoints = lowStake;
        winningAllianceName = '黑桃同盟 (♠3 & ♠A)';
        summary = `黑桃同盟率先出清，各得 +${lowStake} 分，平民同盟各输 -${lowStake} 分`;
      } else {
        spadeWinPoints = -lowStake;
        winningAllianceName = '平民同盟';
        summary = `平民同盟率先出清，各得 +${lowStake} 分，黑桃同盟各输 -${lowStake} 分`;
      }
    }

    for (let i = 0; i < 4; i++) {
      const val = isSpade(i) ? spadeWinPoints : spadeWinPoints === 0 ? 0 : -spadeWinPoints;
      scores[i] = val === 0 ? 0 : val;
    }

    return {
      winnerSeat: isTie ? -1 : winnerSeat,
      winnerAlliance: winningAllianceName,
      isQiangGong: false,
      qiangGongSeat: null,
      seatScores: scores,
      summary,
      isTie,
    };
  } else {
    // 独占 ♠3 & ♠A 但未开启强攻（1对3 双煞对局）
    const solo = spade3Seat;
    // 找到双煞出清的名次 (0: 头游/第1名; 1: 二游/第2名; 2: 三游/第3名; -1 或 3: 未出清/第4名末游垫底)
    const soloFinishedRank = finishedSeats.indexOf(solo);
    let soloScore = 0;
    let eachFarmerScore = 0;
    let winningAllianceName = '';
    let summary = '';
    let isTie = false;

    if (soloFinishedRank === 0) {
      // ① 双煞第 1 个打完手牌（头游出清）：全赢！
      // 5-10 分场: +30 / -10; 10-20 分场: +60 / -20; 20-40 分场: +120 / -40
      soloScore = highStake * 3;
      eachFarmerScore = -highStake;
      winningAllianceName = `${seatNames[solo]} (双煞全胜)`;
      summary = `${seatNames[solo]} (持双煞♠3♠A) 第一个打完手牌夺得头游，1打3全胜大捷！独赢 +${soloScore} 分，其余三家各输 -${highStake} 分`;
    } else if (soloFinishedRank === 1) {
      // ② 双煞第 2 个打完手牌（二游出清）：半赢！
      // 5-10 分场: +15 / -5; 10-20 分场: +30 / -10; 20-40 分场: +60 / -20
      soloScore = lowStake * 3;
      eachFarmerScore = -lowStake;
      winningAllianceName = `${seatNames[solo]} (双煞半胜)`;
      summary = `${seatNames[solo]} (双煞) 第二个打完手牌出清，1打3半赢！独赢 +${soloScore} 分，其余三家各输 -${lowStake} 分`;
    } else if (soloFinishedRank === 2) {
      // ③ 双煞第 3 个打完手牌（三游出清）：半输！
      // 5-10 分场: -15 / +5; 10-20 分场: -30 / +10; 20-40 分场: -60 / +20
      soloScore = -(lowStake * 3);
      eachFarmerScore = lowStake;
      winningAllianceName = '平民同盟 (半胜)';
      summary = `前两名平民率先出清，${seatNames[solo]} (双煞) 第三个打完手牌出清半输，扣 -${Math.abs(soloScore)} 分，其余三家各得 +${lowStake} 分`;
    } else {
      // ④ 其他三个玩家全部打完手牌只剩下双煞（末游第四名）：全输！
      // 5-10 分场: -30 / +10; 10-20 分场: -60 / +20; 20-40 分场: -120 / +40
      soloScore = -(highStake * 3);
      eachFarmerScore = highStake;
      winningAllianceName = '平民同盟 (全胜)';
      summary = `其他三名平民全部打完手牌包揽前三，只剩下双煞 ${seatNames[solo]}，双煞全输！扣 -${Math.abs(soloScore)} 分，其余三家各得 +${highStake} 分`;
    }

    for (let i = 0; i < 4; i++) {
      scores[i] = i === solo ? soloScore : eachFarmerScore;
    }

    return {
      winnerSeat: soloFinishedRank === 0 ? solo : (finishedSeats[0] ?? winnerSeat),
      winnerAlliance: winningAllianceName,
      isQiangGong: false,
      qiangGongSeat: null,
      seatScores: scores,
      summary,
      isTie,
    };
  }
}

export function calculateYansanSurrenderSettlement(
  roomType: string,
  surrenderSeat: number
): YansanSettlement {
  // 5-10分场：投降输给其他玩家各5分，共输15分 (eachStake = 5, totalStake = 15)
  // 10-20分场：投降输给其他玩家各10分，共输30分 (eachStake = 10, totalStake = 30)
  // 20-40分场：投降输给其他玩家各20分，共输60分 (eachStake = 20, totalStake = 60)
  const eachStake = String(roomType).includes('20-40') ? 20 : String(roomType).includes('10-20') ? 10 : 5;
  const totalStake = eachStake * 3;
  const seatNames = ['本家', '西席', '北席', '东席'];

  const scores: [number, number, number, number] = [0, 0, 0, 0];
  for (let i = 0; i < 4; i++) {
    if (i === surrenderSeat) {
      scores[i] = -totalStake;
    } else {
      scores[i] = eachStake;
    }
  }

  const summary = `${seatNames[surrenderSeat]} 在起手手牌未动时选择投降认输！输给其他玩家各 ${eachStake} 分，共输 -${totalStake} 分，其余三家各得 +${eachStake} 分`;

  return {
    winnerSeat: (surrenderSeat + 1) % 4,
    winnerAlliance: '胜方同盟 (对手投降)',
    isQiangGong: false,
    qiangGongSeat: null,
    seatScores: scores,
    summary,
  };
}
