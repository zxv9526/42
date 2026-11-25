import assert from 'node:assert/strict';
import {
  YansanCard,
  parseYansanCombo,
  canBeatYansanCombo,
  getYansanSmartAIPlay,
  isYansanComboDeductivelyUnbeatable,
  findYansanWinningSequence,
  shouldYansanAIQiangGong,
  calculateYansanSettlement,
  calculateYansanSurrenderSettlement,
} from '../src/engine/yansanRules.ts';
import { getSceneRequiredCoins } from '../src/games/index.ts';
import {
  handleYansanGameMove,
  tickYansanAutoTurn,
} from '../src/server/games/yansanServerEngine.ts';
import {
  tickMahjongAutoTurn,
} from '../src/server/games/mahjongServerEngine.ts';
import {
  SSSCard,
  detectSpecialHand,
  isFoulHand,
  compareTwoPlayers,
} from '../src/engine/shisanshuiRules.ts';
import {
  MahjongTile,
  checkMahjongWin,
  computeMahjongSettlement,
} from '../src/engine/mahjongRules.ts';
import { calculateMahjongSettlement } from '../src/games/mahjong/settlement.ts';
import {
  calculateNext0600Countdown,
  getToday0600CycleInfo,
} from '../src/engine/shisanshuiBookingTrain.ts';
import {
  sanitizeDealForSeat,
  sanitizeBoardStateForSeat,
} from '../src/server/sanitizers.ts';
import {
  sortMahjongHand,
  isYiWuJiuMaTile,
} from '../src/games/mahjong/aiWinEngine.ts';
import {
  filterHumanScenePlayers,
  pickBestWaitingRoom,
  isBotLikePlayer,
  SceneWaitingRoom,
} from '../src/server/realtimeTypes.ts';
import { presenceTracker } from '../src/presenceTracker.ts';

function yCard(rank: number, suit: '♠' | '♥' | '♣' | '♦'): YansanCard {
  const suitVal = suit === '♠' ? 4 : suit === '♥' ? 3 : suit === '♣' ? 2 : 1;
  const label = rank === 15 ? '2' : rank === 14 ? 'A' : rank === 13 ? 'K' : rank === 12 ? 'Q' : rank === 11 ? 'J' : String(rank);
  return {
    id: `${label}-${suit}`,
    rank,
    suitVal,
    label,
    suit,
    isRed: suit === '♥' || suit === '♦',
  };
}

function sCard(rank: number, suit: '♠' | '♥' | '♣' | '♦', tag = ''): SSSCard {
  const suitVal = suit === '♠' ? 4 : suit === '♥' ? 3 : suit === '♣' ? 2 : 1;
  const label = rank === 14 ? 'A' : rank === 13 ? 'K' : rank === 12 ? 'Q' : rank === 11 ? 'J' : String(rank);
  return {
    id: `${label}-${suit}${tag}`,
    rank,
    suitVal,
    suit,
    label,
    isRed: suit === '♥' || suit === '♦',
  };
}

function mjTile(type: 'wan' | 'tiao' | 'tong' | 'zi', value: number, idSuffix: string): MahjongTile {
  const cn = ['', '一', '二', '三', '四', '五', '六', '七', '八', '九'];
  const unit = type === 'wan' ? '万' : type === 'tiao' ? '条' : type === 'tong' ? '筒' : '';
  const text = type === 'zi' ? (value === 5 ? '红中' : '东风') : `${cn[value]}${unit}`;
  return {
    id: `${type}-${value}-${idSuffix}`,
    type,
    value,
    text,
    isRedDragon: type === 'zi' && value === 5,
  };
}

function runYansanTests() {
  // 1. Canonical ID & rank weight ordering (3 > 2 > A > ... > 4)
  const single3S = parseYansanCombo([yCard(3, '♠')]);
  const single2S = parseYansanCombo([yCard(15, '♠')]);
  assert(single3S && single2S);
  assert.equal(canBeatYansanCombo(single3S, single2S), true, '♠3 must beat ♠2');
  assert.equal(canBeatYansanCombo(single2S, single3S), false, '♠2 must not beat ♠3');

  // 2. First turn must play ♦4 via SmartAI lead solver (exercises canonical availablePool IDs)
  const firstTurnHand: YansanCard[] = [
    yCard(4, '♦'),
    yCard(5, '♣'),
    yCard(6, '♥'),
    yCard(7, '♠'),
    yCard(8, '♦'),
    yCard(3, '♠'),
  ];
  const aiPlay = getYansanSmartAIPlay(firstTurnHand, null, {
    seat: 0,
    isFirstTurn: true,
    handsCount: [6, 13, 13, 13],
  });
  assert(aiPlay.cards.some((c) => c.id === '4-♦'), 'First turn lead must include ♦4');

  // 3. Deductive unbeatable check (3-arg signature: combo, availablePool, maxOpponentHandSize)
  const combo3S = parseYansanCombo([yCard(3, '♠')])!;
  assert.equal(
    isYansanComboDeductivelyUnbeatable(combo3S, [yCard(4, '♣'), yCard(15, '♠')], 5),
    true,
    '♠3 single must be deductively unbeatable even against ♠2 in pool'
  );

  const combo3H = parseYansanCombo([yCard(3, '♥')])!;
  assert.equal(
    isYansanComboDeductivelyUnbeatable(combo3H, [yCard(3, '♠'), yCard(15, '♠')], 5),
    false,
    '♥3 single is not unbeatable while ♠3 remains in availablePool'
  );
  assert.equal(
    isYansanComboDeductivelyUnbeatable(combo3H, [yCard(15, '♠'), yCard(14, '♠')], 5),
    true,
    '♥3 single must be unbeatable once ♠3 is excluded from availablePool (played or held by practice player)'
  );

  const pair3SpadeDiamond = parseYansanCombo([yCard(3, '♠'), yCard(3, '♦')])!;
  assert.equal(
    isYansanComboDeductivelyUnbeatable(pair3SpadeDiamond, [yCard(3, '♥'), yCard(3, '♣'), yCard(15, '♠')], 5),
    true,
    '对3 (♠3+♦3) must be deductively unbeatable against remaining ♥3+♣3'
  );

  const pair3HeartClub = parseYansanCombo([yCard(3, '♥'), yCard(3, '♣')])!;
  assert.equal(
    isYansanComboDeductivelyUnbeatable(pair3HeartClub, [yCard(3, '♠'), yCard(3, '♦')], 5),
    false,
    '对3 (♥3+♣3) cannot be marked unbeatable while ♠3+♦3 remain in pool'
  );
  assert.equal(
    isYansanComboDeductivelyUnbeatable(pair3HeartClub, [yCard(3, '♦'), yCard(15, '♠'), yCard(15, '♥')], 5),
    true,
    '对3 (♥3+♣3) must be unbeatable once ♠3 is excluded (only ♦3 remains of 3s)'
  );

  const triplet3 = parseYansanCombo([yCard(3, '♠'), yCard(3, '♥'), yCard(3, '♣')])!;
  assert.equal(
    isYansanComboDeductivelyUnbeatable(triplet3, [yCard(3, '♦'), yCard(15, '♠'), yCard(15, '♥'), yCard(15, '♣')], 5),
    true,
    '三条3 must be deductively unbeatable'
  );

  const triplet2 = parseYansanCombo([yCard(15, '♠'), yCard(15, '♥'), yCard(15, '♣')])!;
  assert.equal(
    isYansanComboDeductivelyUnbeatable(triplet2, [yCard(3, '♠'), yCard(3, '♥'), yCard(14, '♠'), yCard(14, '♥'), yCard(14, '♣')], 5),
    true,
    '三条2 must be unbeatable when fewer than 3 threes remain in pool'
  );

  const fourTwo = parseYansanCombo([yCard(15, '♠'), yCard(15, '♥'), yCard(15, '♣'), yCard(15, '♦'), yCard(4, '♣')])!;
  assert.equal(
    isYansanComboDeductivelyUnbeatable(fourTwo, [yCard(3, '♠'), yCard(4, '♠'), yCard(5, '♠'), yCard(6, '♠'), yCard(7, '♠')], 5),
    false,
    '四带一2 is beatable if a straight flush can exist in pool with maxOpponentHandSize >= 5'
  );
  assert.equal(
    isYansanComboDeductivelyUnbeatable(fourTwo, [yCard(3, '♠'), yCard(4, '♠'), yCard(5, '♠'), yCard(6, '♠'), yCard(7, '♠')], 4),
    true,
    '四带一2 must be unbeatable when maxOpponentHandSize < 5'
  );
  assert.equal(
    isYansanComboDeductivelyUnbeatable(fourTwo, [yCard(3, '♠'), yCard(3, '♥'), yCard(3, '♣'), yCard(14, '♠'), yCard(13, '♥')], 5),
    true,
    '四带一2 must be unbeatable when no higher four-of-a-kind or straight flush exists in pool'
  );

  const royalSpadeSF = parseYansanCombo([yCard(3, '♠'), yCard(4, '♠'), yCard(5, '♠'), yCard(6, '♠'), yCard(7, '♠')])!;
  assert.equal(
    isYansanComboDeductivelyUnbeatable(royalSpadeSF, [yCard(3, '♥'), yCard(4, '♥'), yCard(5, '♥'), yCard(6, '♥'), yCard(7, '♥')], 13),
    true,
    '♠3-4-5-6-7 同花顺 must be deductively unbeatable'
  );

  // 4. SmartAI must NEVER answer a single/pair/triplet with a 5-card combo during emergency follow
  const handWith5CardOnly = [
    yCard(4, '♠'), yCard(5, '♠'), yCard(6, '♠'), yCard(7, '♠'), yCard(8, '♠'),
  ];
  const emergencyFollowSingle = getYansanSmartAIPlay(handWith5CardOnly, single3S, {
    seat: 1,
    handsCount: [1, 5, 8, 8],
  });
  assert.equal(emergencyFollowSingle.cards, null, 'AI must pass rather than answer a single with a 5-card combo');

  const highPairA = parseYansanCombo([yCard(14, '♠'), yCard(14, '♥')])!;
  const emergencyFollowPair = getYansanSmartAIPlay(handWith5CardOnly, highPairA, {
    seat: 1,
    handsCount: [1, 5, 8, 8],
  });
  assert.equal(emergencyFollowPair.cards, null, 'AI must pass rather than answer a pair with a 5-card combo');

  // 5. Smart AI control preservation: never waste ♥3/♣3/♦3 or 对3 on tiny 5/对4 midgame
  const handWithHeart3And9 = [
    yCard(3, '♥'), yCard(9, '♠'), yCard(4, '♣'), yCard(4, '♦'), yCard(7, '♠'), yCard(7, '♥'),
  ];
  const single5S = parseYansanCombo([yCard(5, '♠')])!;
  single5S.seat = 0;
  const follow5 = getYansanSmartAIPlay(handWithHeart3And9, single5S, {
    seat: 1,
    handsCount: [6, 6, 8, 8],
  });
  assert(follow5.cards && follow5.cards[0].rank === 9, 'AI must play 9♠ instead of wasting ♥3 on a 5♠');

  const handWithHeart3AndStraight = [
    yCard(3, '♥'), yCard(4, '♣'), yCard(5, '♠'), yCard(6, '♥'), yCard(7, '♦'), yCard(8, '♠'),
  ];
  const holdHeart3 = getYansanSmartAIPlay(handWithHeart3AndStraight, single5S, {
    seat: 1,
    handsCount: [6, 6, 8, 8],
  });
  assert.equal(holdHeart3.cards, null, 'AI must pass rather than waste ♥3 or break 4-8 straight on a tiny 5');

  const pair4Target = parseYansanCombo([yCard(4, '♠'), yCard(4, '♥')])!;
  pair4Target.seat = 0;
  const handWithPair3AndPair8 = [
    yCard(3, '♥'), yCard(3, '♦'), yCard(8, '♠'), yCard(8, '♥'), yCard(10, '♣'), yCard(11, '♦'),
  ];
  const followPair4 = getYansanSmartAIPlay(handWithPair3AndPair8, pair4Target, {
    seat: 1,
    handsCount: [6, 6, 8, 8],
  });
  assert(followPair4.cards && followPair4.cards[0].rank === 8, 'AI must play 对8 instead of wasting 对3 on 对4');

  // 6. Smart AI midgame free lead: never dump 同花顺 / 四带一 ahead of weaker 顺子
  const handStraightAndSF = [
    yCard(4, '♠'), yCard(5, '♠'), yCard(6, '♠'), yCard(7, '♠'), yCard(8, '♠'),
    yCard(4, '♥'), yCard(5, '♣'), yCard(6, '♦'), yCard(7, '♥'), yCard(8, '♣'),
    yCard(9, '♦'), yCard(10, '♣'),
  ];
  const freeLeadRes = getYansanSmartAIPlay(handStraightAndSF, null, {
    seat: 1,
    handsCount: [10, 12, 10, 10],
  });
  const freeLeadCombo = parseYansanCombo(freeLeadRes.cards || []);
  assert(
    freeLeadCombo && freeLeadCombo.type === '5_card' && freeLeadCombo.comboTier === 1,
    'Midgame free lead must shed ordinary 顺子 before 同花顺'
  );

  // 7. Smart AI must block 1-card unknown seat (never feed small single)
  const handBlockingBaodan = [yCard(4, '♣'), yCard(9, '♠'), yCard(9, '♥'), yCard(11, '♦')];
  const blockUnknown1Card = getYansanSmartAIPlay(handBlockingBaodan, null, {
    seat: 2,
    handsCount: [8, 1, 4, 8], // seat 1 (unknown) only has 1 card left!
  });
  assert(
    blockUnknown1Card.cards && blockUnknown1Card.cards.length === 2 && blockUnknown1Card.cards[0].rank === 9,
    'AI must lead pair/combo rather than feeding a 1-card unknown player'
  );

  // 8. Smart AI in 1v3 QiangGong must never block ally when downstream is also ally
  const qgAllySingle6 = parseYansanCombo([yCard(6, '♣')])!;
  qgAllySingle6.seat = 0;
  const aiDefenseHand = [yCard(10, '♠'), yCard(11, '♥'), yCard(4, '♦'), yCard(5, '♦'), yCard(7, '♣'), yCard(9, '♣')];
  const yieldInQG = getYansanSmartAIPlay(aiDefenseHand, qgAllySingle6, {
    seat: 2, // seat 2 -> downstream is seat 1 (ally!) while seat 3 is QiangGong solo enemy
    handsCount: [6, 6, 6, 6],
    isQiangGong: true,
    qiangGongSeat: 3,
  });
  assert.equal(yieldInQG.cards, null, 'AI must yield to ally when downstream is also an ally in 1v3');

  // 9. N-step forced win sequence & follow snatching forced win
  const threeStepHand = [
    yCard(3, '♠'),
    yCard(15, '♠'), yCard(15, '♥'),
    yCard(4, '♣'), yCard(5, '♦'), yCard(6, '♥'), yCard(7, '♠'), yCard(8, '♣'),
  ];
  const threeStepWin = findYansanWinningSequence(
    threeStepHand,
    [yCard(14, '♠'), yCard(13, '♣'), yCard(9, '♦'), yCard(10, '♥'), yCard(11, '♠')], // no 3s left in pool
    5
  );
  assert(threeStepWin && threeStepWin.length > 0, 'N-step solver must recognize ♠3 -> 对2 -> 顺子 forced win');

  const singleKS = parseYansanCombo([yCard(13, '♠')])!;
  singleKS.seat = 1;
  // Case A: All other 3s are played, so ♥2 itself is deductively unbeatable and keeps ♠3+♠2+4-8 forced win
  const followForcedWin2 = getYansanSmartAIPlay(threeStepHand, singleKS, {
    seat: 2,
    handsCount: [5, 5, 8, 5],
    seatPlayedCards: [[yCard(3, '♥'), yCard(3, '♣'), yCard(3, '♦')], [], [], []],
  });
  assert(
    followForcedWin2.cards && followForcedWin2.cards.length === 1 && followForcedWin2.cards[0].rank === 15 && followForcedWin2.tacticalIntent === 'win_clear',
    'AI should snatch forced-win lead with unbeatable ♥2 when all 3s are accounted for'
  );

  // Case B: ♦3 is still in opponent hands, so 2s are beatable; AI must snatch lead with ♠3 so 对2+顺子 wins guaranteed
  const followForcedWin3 = getYansanSmartAIPlay(threeStepHand, singleKS, {
    seat: 2,
    handsCount: [5, 5, 8, 5],
    seatPlayedCards: [[yCard(3, '♥'), yCard(3, '♣')], [], [], []],
  });
  assert(
    followForcedWin3.cards && followForcedWin3.cards.length === 1 && followForcedWin3.cards[0].rank === 3 && followForcedWin3.tacticalIntent === 'win_clear',
    'AI must snatch lead with ♠3 when ♦3 is still out and remaining hand (对2+顺子) forms a 100% forced win'
  );

  // 10. Smart QiangGong evaluation rejects weak scattered hands and accepts monster hands
  const weakQGHand = [
    yCard(3, '♠'), yCard(14, '♠'), yCard(13, '♦'), yCard(13, '♣'),
    yCard(4, '♣'), yCard(4, '♥'), yCard(6, '♦'), yCard(7, '♠'), yCard(9, '♥'),
    yCard(9, '♣'), yCard(10, '♦'), yCard(11, '♣'), yCard(12, '♥'),
  ];
  assert.equal(shouldYansanAIQiangGong(weakQGHand), false, 'Weak scattered hand with ♠3+♠A must not QiangGong');

  const monsterQGHand = [
    yCard(3, '♠'), yCard(14, '♠'), yCard(15, '♠'), yCard(15, '♥'),
    yCard(12, '♠'), yCard(12, '♥'), yCard(12, '♣'), yCard(12, '♦'),
    yCard(4, '♣'), yCard(5, '♦'), yCard(6, '♥'), yCard(7, '♠'), yCard(8, '♣'),
  ];
  assert.equal(shouldYansanAIQiangGong(monsterQGHand), true, 'Monster low-turn hand with ♠3+♠A+对2+四带一+顺子 should QiangGong');

  // 11. 2v2 vs 1v3 settlements zero-sum check
  const s2v2 = calculateYansanSettlement('5-10', 0, false, null, 0, 2, [0, 2]);
  assert.equal(s2v2.seatScores.reduce((a, b) => a + b, 0), 0, '2v2 settlement must sum to 0');
  assert.equal(s2v2.seatScores[0], 10);
  assert.equal(s2v2.seatScores[1], -10);

  const s1v3 = calculateYansanSettlement('5-10', 1, false, null, 1, 1, [0, 1]);
  assert.equal(s1v3.seatScores.reduce((a, b) => a + b, 0), 0, '1v3 settlement must sum to 0');

  // 12. 2v2 Tie (allies finish 1st and 4th) & straight with rank 3
  const straightWith3 = parseYansanCombo([
    yCard(3, '♠'), yCard(4, '♥'), yCard(5, '♣'), yCard(6, '♦'), yCard(7, '♠'),
  ]);
  assert(straightWith3 && straightWith3.type === '5_card', '3-4-5-6-7 must parse as a valid 5-card straight');
  const tie2v2 = calculateYansanSettlement('yansan-10-20', 0, false, null, 0, 2, [0, 1, 3, 2]);
  assert.deepEqual(tie2v2.seatScores, [0, 0, 0, 0], '2v2 1st+4th vs 2nd+3rd must be 0-score tie');
  assert.equal(tie2v2.winnerSeat, -1, '2v2 tie must report winnerSeat = -1');
  const singleWin2v2 = calculateYansanSettlement('yansan-10-20', 0, false, null, 0, 2, [0, 1, 2, 3]);
  assert.deepEqual(singleWin2v2.seatScores, [10, -10, 10, -10], '2v2 1st+3rd must win lowStake (10)');
}

function runShisanshuiTests() {
  // 1. Special hands detection
  const supremeDragon = Array.from({ length: 13 }, (_, i) => sCard(i + 2, '♠'));
  assert.equal(detectSpecialHand(supremeDragon)?.name, '至尊青龙');

  const mixedDragon = Array.from({ length: 13 }, (_, i) => sCard(i + 2, i % 2 === 0 ? '♠' : '♥'));
  assert.equal(detectSpecialHand(mixedDragon)?.name, '一条龙');

  const sixAndHalfPairs: SSSCard[] = [
    sCard(2, '♠'), sCard(2, '♥'),
    sCard(4, '♠'), sCard(4, '♥'),
    sCard(6, '♠'), sCard(6, '♥'),
    sCard(8, '♠'), sCard(8, '♥'),
    sCard(10, '♠'), sCard(10, '♥'),
    sCard(12, '♠'), sCard(12, '♥'),
    sCard(14, '♣'),
  ];
  assert.equal(detectSpecialHand(sixAndHalfPairs)?.name, '六对半');

  // 2. Foul hand detection (head stronger than mid)
  const head = [sCard(14, '♠'), sCard(14, '♥'), sCard(14, '♣')];
  const mid = [sCard(3, '♠'), sCard(5, '♥'), sCard(7, '♣'), sCard(9, '♦'), sCard(11, '♠')];
  const tail = [sCard(13, '♠'), sCard(13, '♥'), sCard(13, '♣'), sCard(13, '♦'), sCard(2, '♠')];
  assert.equal(isFoulHand(head, mid, tail), true, 'Triplet A head beating high-card mid must be foul');

  // 3. Valid hand vs weaker hand (shotgun check)
  const strong = {
    head: [sCard(14, '♠'), sCard(14, '♥'), sCard(9, '♣')],
    mid: [sCard(8, '♠'), sCard(8, '♥'), sCard(8, '♣'), sCard(4, '♦'), sCard(2, '♠')],
    tail: [sCard(13, '♠'), sCard(13, '♥'), sCard(13, '♣'), sCard(13, '♦'), sCard(5, '♠')],
  };
  const weak = {
    head: [sCard(3, '♠'), sCard(5, '♥'), sCard(7, '♣')],
    mid: [sCard(4, '♠'), sCard(6, '♥'), sCard(9, '♣'), sCard(10, '♦'), sCard(12, '♠')],
    tail: [sCard(7, '♠'), sCard(7, '♥'), sCard(11, '♣'), sCard(12, '♦'), sCard(14, '♦')],
  };
  assert.equal(isFoulHand(strong.head, strong.mid, strong.tail), false);
  assert.equal(isFoulHand(weak.head, weak.mid, weak.tail), false);
  const cmp = compareTwoPlayers(strong, weak, 1, '测试牌友');
  assert.equal(cmp.isShot, true, 'Winning all 3 tiers must trigger shotgun');
  assert(cmp.finalWater > 0, 'Stronger valid hand must yield positive water');

  // 4. Booking countdown & cycle helper with calibrated dates
  const before0600 = new Date(2026, 4, 10, 5, 30, 0);
  const cdBefore = calculateNext0600Countdown(before0600);
  assert.equal(cdBefore.hours, 0);
  assert.equal(cdBefore.minutes, 30);
  const cycleBefore = getToday0600CycleInfo(before0600);
  assert.equal(cycleBefore.latestCycleKey, '2026-05-09_0600');

  const after0600 = new Date(2026, 4, 10, 8, 0, 0);
  const cycleAfter = getToday0600CycleInfo(after0600);
  assert.equal(cycleAfter.latestCycleKey, '2026-05-10_0600');

  // 5. Twelve Royals (12 cards >= J + 1 low card) & Three Straight Flushes
  const twelveRoyals = [
    sCard(11, '♠'), sCard(12, '♠'), sCard(13, '♠'), sCard(14, '♠'),
    sCard(11, '♥'), sCard(12, '♥'), sCard(13, '♥'), sCard(14, '♥'),
    sCard(11, '♣'), sCard(12, '♣'), sCard(13, '♣'), sCard(14, '♣'),
    sCard(2, '♦'),
  ];
  assert.equal(detectSpecialHand(twelveRoyals)?.type, 'twelve_royals', '12 royals + 1 non-royal must be detected');

  const threeSF = [
    sCard(2, '♠'), sCard(3, '♠'), sCard(4, '♠'),
    sCard(2, '♥'), sCard(3, '♥'), sCard(4, '♥'), sCard(5, '♥'), sCard(6, '♥'),
    sCard(10, '♣'), sCard(11, '♣'), sCard(12, '♣'), sCard(13, '♣'), sCard(14, '♣'),
  ];
  assert.equal(detectSpecialHand(threeSF)?.type, 'three_straight_flushes', '3 straight flushes must be detected ahead of 3 flushes');

  // 6. Special vs special difference (36 vs 26 = +10) & Shot bonus-not-doubled check
  const spVsSp = compareTwoPlayers(
    { head: supremeDragon.slice(0, 3), mid: supremeDragon.slice(3, 8), tail: supremeDragon.slice(8, 13) },
    { head: mixedDragon.slice(0, 3), mid: mixedDragon.slice(3, 8), tail: mixedDragon.slice(8, 13) },
    1,
    '对手'
  );
  assert.equal(spVsSp.finalWater, 10, 'Supreme Dragon (36) vs Dragon (26) must yield +10');

  const shotWithHeadTrip = {
    head: [sCard(10, '♠'), sCard(10, '♥'), sCard(10, '♣')], // 冲三 +2 bonus (+3 total on head)
    mid: [sCard(11, '♠'), sCard(11, '♥'), sCard(11, '♣'), sCard(4, '♦'), sCard(5, '♠')],
    tail: [sCard(14, '♠'), sCard(14, '♥'), sCard(14, '♣'), sCard(13, '♠'), sCard(13, '♥')],
  };
  const shotRes = compareTwoPlayers(shotWithHeadTrip, weak, 1, '弱手');
  assert.equal(shotRes.isShot, true);
  // Base 3 tiers won * 2 (shot) = 6 + un-doubled head triplet bonus 2 = 8? Wait: compareTwoPlayers doubles base (3*2=6) + bonus (2) = 8!
  assert.equal(shotRes.finalWater, 8, 'Shot must double base tier waters (3*2=6) without doubling tier bonus (+2 => 8)');
}

function runMahjongTests() {
  // 1. Standard 4 melds + pair (14 tiles)
  const stdWin: MahjongTile[] = [
    mjTile('wan', 1, 'a'), mjTile('wan', 2, 'a'), mjTile('wan', 3, 'a'),
    mjTile('tiao', 4, 'a'), mjTile('tiao', 5, 'a'), mjTile('tiao', 6, 'a'),
    mjTile('tong', 7, 'a'), mjTile('tong', 8, 'a'), mjTile('tong', 9, 'a'),
    mjTile('wan', 5, 'a'), mjTile('wan', 5, 'b'), mjTile('wan', 5, 'c'),
    mjTile('tong', 2, 'a'), mjTile('tong', 2, 'b'),
  ];
  assert.equal(checkMahjongWin(stdWin, false).win, true, 'Standard 14-tile hand must win');

  // 2. Seven pairs with Hongzhong wildcard
  const sevenPairsWithHz: MahjongTile[] = [
    mjTile('wan', 1, 'a'), mjTile('wan', 1, 'b'),
    mjTile('wan', 3, 'a'), mjTile('wan', 3, 'b'),
    mjTile('tiao', 2, 'a'), mjTile('tiao', 2, 'b'),
    mjTile('tiao', 6, 'a'), mjTile('tiao', 6, 'b'),
    mjTile('tong', 4, 'a'), mjTile('tong', 4, 'b'),
    mjTile('tong', 8, 'a'), mjTile('tong', 8, 'b'),
    mjTile('wan', 9, 'a'), mjTile('zi', 5, 'hz'),
  ];
  assert.equal(checkMahjongWin(sevenPairsWithHz, true).win, true, 'Seven pairs with 1 Hongzhong must win');

  // 3. No-Hongzhong bonus +2 Ma in Hongzhong-6 mode
  const wallMa = Array.from({ length: 8 }, (_, i) => mjTile('wan', 1, `m${i}`)); // all 1-wan => all hit Ma!
  const noHzSettlement = computeMahjongSettlement(0, stdWin, [], wallMa, 'hongzhong-6', [0, 0, 0, 0]);
  assert.equal(noHzSettlement.maTiles.length, 8, 'No-Hongzhong in 6-ma mode must draw 8 Ma');
  assert.equal(noHzSettlement.hitMaCount, 8);
  assert.equal(noHzSettlement.netCoinsPerSeat.reduce((a, b) => a + b, 0), 0, 'Mahjong settlement must be zero-sum');

  // 4. 4 Hongzhong instant win (6-ma => +70 each, +210 total)
  const fourHzHand = [
    mjTile('zi', 5, '1'), mjTile('zi', 5, '2'), mjTile('zi', 5, '3'), mjTile('zi', 5, '4'),
    ...stdWin.slice(0, 10),
  ];
  const fourHzSettle = computeMahjongSettlement(0, fourHzHand, [], wallMa, 'hongzhong-6', [0, 0, 0, 0]);
  assert.equal(fourHzSettle.isFourHongzhong, true);
  assert.equal(fourHzSettle.payoutPerOpponent, 70);
  assert.equal(fourHzSettle.netCoinsPerSeat[0], 210);
  assert.equal(fourHzSettle.netCoinsPerSeat.reduce((a, b) => a + b, 0), 0);

  // 5. Unsorted winningHand + object melds must be normalized & sorted (wan -> tiao -> tong -> zi)
  const unsortedWin: MahjongTile[] = [
    mjTile('zi', 5, 's1'),
    mjTile('tong', 9, 's2'),
    mjTile('wan', 1, 's3'),
    mjTile('tiao', 4, 's4'),
    mjTile('wan', 3, 's5'),
    mjTile('tong', 2, 's6'),
    mjTile('wan', 2, 's7'),
    mjTile('tiao', 5, 's8'),
  ];
  const rawObjectMelds: any[] = [
    { type: 'peng', tile: mjTile('tong', 7, 'm1') },
    [mjTile('tiao', 3, 'm2a'), mjTile('tiao', 3, 'm2b'), mjTile('tiao', 3, 'm2c')],
  ];
  const sortedSettle = computeMahjongSettlement(1, unsortedWin, rawObjectMelds, wallMa, 'hongzhong-6', [0, 0, 0, 0]);
  assert.deepEqual(
    sortedSettle.winningHand.map((t) => `${t.type}-${t.value}`),
    ['wan-1', 'wan-2', 'wan-3', 'tiao-4', 'tiao-5', 'tong-2', 'tong-9', 'zi-5'],
    'computeMahjongSettlement must sort winningHand deterministically'
  );
  assert.equal(sortedSettle.winningMelds.length, 2);
  assert.equal(sortedSettle.winningMelds[0].length, 3);

  // 6. Self-draw with 0 hit Ma across every seat 0..3: winner MUST win +30, opponents MUST lose -10 each
  const zeroHitWallMa = Array.from({ length: 8 }, (_, i) => mjTile('wan', 2, `miss${i}`)); // 2-wan never hits Ma
  for (let wSeat = 0; wSeat < 4; wSeat++) {
    const zeroMaStl = computeMahjongSettlement(wSeat, stdWin, [], zeroHitWallMa, 'hongzhong-6', [0, 0, 0, 0]);
    assert.equal(zeroMaStl.hitMaCount, 0, '2-wan Ma tiles must hit 0 Ma');
    assert.equal(zeroMaStl.payoutPerOpponent, 10, '0-Ma payoutPerOpponent must be base 10');
    assert.equal(zeroMaStl.netCoinsPerSeat[wSeat], 30, `Winner seat ${wSeat} with 0 Ma must win +30`);
    for (let opp = 0; opp < 4; opp++) {
      if (opp !== wSeat) {
        assert.equal(zeroMaStl.netCoinsPerSeat[opp], -10, `Opponent seat ${opp} must lose -10 when seat ${wSeat} wins 0 Ma`);
      }
    }
    assert.equal(zeroMaStl.netCoinsPerSeat.reduce((a, b) => a + b, 0), 0, '0-Ma settlement must be zero-sum');
  }

  // 7. Kong scoring (明杠 +10/-10, 暗杠 +30/-10*3, 补杠 +30/-10*3) combined with 0-Ma Zimo must stay zero-sum
  const kongScores: [number, number, number, number] = [0, 0, 0, 0];
  // Seat 1 明杠 Seat 0 -> seat1 +10, seat0 -10
  kongScores[1] += 10;
  kongScores[0] -= 10;
  // Seat 0 暗杠 -> seat0 +30, seats 1/2/3 -10 each => net [20, 0, -10, -10]
  kongScores[0] += 30;
  kongScores[1] -= 10;
  kongScores[2] -= 10;
  kongScores[3] -= 10;
  // Seat 2 补杠 -> seat2 +30, seats 0/1/3 -10 each => net [10, -10, 20, -20]
  kongScores[2] += 30;
  kongScores[0] -= 10;
  kongScores[1] -= 10;
  kongScores[3] -= 10;
  assert.deepEqual(kongScores, [10, -10, 20, -20]);
  assert.equal(kongScores.reduce((a, b) => a + b, 0), 0, 'Kong scores alone must be zero-sum');

  const kongPlusZeroMa = computeMahjongSettlement(3, stdWin, [], zeroHitWallMa, 'hongzhong-6', kongScores);
  // Seat 3 wins Zimo 0 Ma (+30) + kong (-20) = +10; Seat 0 (-10 + 10 = 0); Seat 1 (-10 - 10 = -20); Seat 2 (-10 + 20 = +10)
  assert.deepEqual(kongPlusZeroMa.netCoinsPerSeat, [0, -20, 10, 10]);
  assert.equal(kongPlusZeroMa.netCoinsPerSeat.reduce((a, b) => a + b, 0), 0, 'Kong + Zimo settlement must be zero-sum');

  // 8. Client settlement helper fallback consistency (0-Ma Zimo + tie stalemate)
  const fallbackSelfWin = calculateMahjongSettlement({ winner: 0, payoutPerOpponent: 10 });
  assert.equal(fallbackSelfWin.scorePoints, 30);
  assert.equal(fallbackSelfWin.coinsDelta, 30);
  const fallbackOppWin = calculateMahjongSettlement({ winner: 1, payoutPerOpponent: 10 });
  assert.equal(fallbackOppWin.scorePoints, -10);
  assert.equal(fallbackOppWin.coinsDelta, -10);
  const stalemateWithKong = calculateMahjongSettlement({ winner: -1, rewardCoins: 20 });
  assert.equal(stalemateWithKong.isTie, true);
  assert.equal(stalemateWithKong.scorePoints, 20);
  assert.equal(stalemateWithKong.coinsDelta, 20);
  // 9. Extracted Mahjong aiWinEngine helpers
  const unsortedAiTiles = [
    { id: 'z1', type: 'zi' as const, value: 5, text: '红中', color: '#DC2626' },
    { id: 'to2', type: 'tong' as const, value: 9, text: '九筒', color: '#2563EB' },
    { id: 'w1', type: 'wan' as const, value: 1, text: '一万', color: '#DC2626' },
    { id: 'ti5', type: 'tiao' as const, value: 5, text: '五条', color: '#16A34A' },
  ];
  assert.deepEqual(
    sortMahjongHand(unsortedAiTiles).map((t) => t.id),
    ['w1', 'ti5', 'to2', 'z1'],
    'sortMahjongHand must sort wan -> tiao -> tong -> zi'
  );
  assert.equal(isYiWuJiuMaTile(unsortedAiTiles[0]), true, '红中 must hit Ma');
  assert.equal(isYiWuJiuMaTile(unsortedAiTiles[2]), true, '一万 must hit Ma');
  assert.equal(
    isYiWuJiuMaTile({ id: 'w2', type: 'wan', value: 2, text: '二万', color: '#DC2626' }),
    false,
    '二万 must not hit Ma'
  );

  // 10. Hongzhong-10 mode Ma count & Scene required coins matrix
  const wallMa12 = Array.from({ length: 12 }, (_, i) => mjTile('wan', 1, `m10-${i}`));
  const hz10Stl = computeMahjongSettlement(0, stdWin, [], wallMa12, 'hongzhong-10', [0, 0, 0, 0]);
  assert.equal(hz10Stl.maTiles.length, 12, 'No-Hongzhong in 10-ma mode must draw 12 Ma');
  assert.equal(hz10Stl.hitMaCount, 12);
  assert.equal(hz10Stl.netCoinsPerSeat.reduce((a, b) => a + b, 0), 0);

  assert.equal(getSceneRequiredCoins('yansan', 'yansan-5-10'), 100);
  assert.equal(getSceneRequiredCoins('yansan', 'yansan-10-20'), 200);
  assert.equal(getSceneRequiredCoins('yansan', 'yansan-20-40'), 500);
  assert.equal(getSceneRequiredCoins('shisanshui', 'four-realtime'), 100);
  assert.equal(getSceneRequiredCoins('shisanshui', 'eight-realtime'), 500);
  assert.equal(getSceneRequiredCoins('shisanshui', 'four-booking'), 100);
  assert.equal(getSceneRequiredCoins('shisanshui', 'eight-booking'), 500);
  assert.equal(getSceneRequiredCoins('shisanshui', 'shisanshui-10-20'), 200);
  assert.equal(getSceneRequiredCoins('shisanshui', 'shisanshui-20-40'), 500);
  assert.equal(getSceneRequiredCoins('mahjong', 'hongzhong-6'), 100);
  assert.equal(getSceneRequiredCoins('mahjong', 'hongzhong-10'), 200);
  assert.equal(getSceneRequiredCoins('mahjong', 'standard-10'), 100);
  assert.equal(getSceneRequiredCoins('mahjong', 'standard-6'), 100);
  assert.equal(getSceneRequiredCoins('mahjong', 'mahjong-10-20'), 200);
  assert.equal(getSceneRequiredCoins('mahjong', 'mahjong-20-40'), 500);
}

function runSanitizerTests() {
  const deal = {
    deck: ['secret-deck'],
    initialSeatCards: [[1], [2]],
    seatCards: [
      [yCard(3, '♠'), yCard(4, '♦')],
      [yCard(15, '♠'), yCard(14, '♥')],
    ],
  };
  const sanitized = sanitizeDealForSeat('yansan', deal, 0);
  assert.equal(sanitized.deck, undefined, 'sanitizeDealForSeat must strip deck');
  assert.equal(sanitized.initialSeatCards, undefined, 'sanitizeDealForSeat must strip initialSeatCards');
  assert.equal(sanitized.seatCards[0][0].rank, 3, 'Recipient seat keeps visible cards');
  assert.equal(sanitized.seatCards[1][0].hidden, true, 'Opponent seat cards must be masked');

  const maskedBoard = sanitizeBoardStateForSeat(
    { phase: 'playing', extraState: { lastDrawnSeat: 1, lastDrawnTile: { id: 'wan-9-0', type: 'wan', value: 9 } } },
    'mahjong',
    0
  );
  assert.equal(maskedBoard.extraState.lastDrawnTile.hidden, true, 'Opponent drawn tile must be masked during play');
}

function runRealtimeHubTests() {
  // 1. isBotLikePlayer and filterHumanScenePlayers
  const mixedPlayers = [
    { id: 'user-1', name: '真实玩家1', avatar: '👑', rating: 1800, coins: 2000, seat: 0 },
    { id: 'sim-123-1', name: '人机1', avatar: '🤖', rating: 1800, coins: 2000, seat: 1, isSimulated: true },
    { id: 'BOT-456', name: '人机2', avatar: '🤖', rating: 1800, coins: 2000, seat: 2, isAi: true },
    { id: 'user-2', name: '真实玩家2', avatar: '👑', rating: 1850, coins: 3000, seat: 3 },
  ];
  assert.equal(isBotLikePlayer(mixedPlayers[0]), false);
  assert.equal(isBotLikePlayer(mixedPlayers[1]), true);
  assert.equal(isBotLikePlayer(mixedPlayers[2]), true);
  assert.equal(isBotLikePlayer(mixedPlayers[3]), false);

  const humanOnly = filterHumanScenePlayers(mixedPlayers);
  assert.equal(humanOnly.length, 2, 'Bots must be cleanly filtered out');
  assert.equal(humanOnly[0].id, 'user-1');
  assert.equal(humanOnly[1].id, 'user-2');

  // 2. pickBestWaitingRoom prioritization
  const waitingRooms: SceneWaitingRoom[] = [
    {
      id: 'room-finished',
      gameType: 'mahjong',
      sceneMode: 'hongzhong-6',
      sceneName: '红中6码场',
      maxPlayers: 4,
      players: [{ id: 'u1', name: 'U1', avatar: '👑', rating: 1800, coins: 2000, seat: 0 }],
      status: 'finished',
      createdAt: Date.now() - 10000,
    },
    {
      id: 'room-empty',
      gameType: 'mahjong',
      sceneMode: 'hongzhong-6',
      sceneName: '红中6码场',
      maxPlayers: 4,
      players: [],
      status: 'waiting',
      createdAt: Date.now() - 5000,
    },
    {
      id: 'room-two-players',
      gameType: 'mahjong',
      sceneMode: 'hongzhong-6',
      sceneName: '红中6码场',
      maxPlayers: 4,
      players: [
        { id: 'u2', name: 'U2', avatar: '👑', rating: 1800, coins: 2000, seat: 0 },
        { id: 'u3', name: 'U3', avatar: '👑', rating: 1800, coins: 2000, seat: 1 },
      ],
      status: 'waiting',
      createdAt: Date.now() - 8000,
    },
    {
      id: 'room-full',
      gameType: 'mahjong',
      sceneMode: 'hongzhong-6',
      sceneName: '红中6码场',
      maxPlayers: 4,
      players: [
        { id: 'p1', name: 'P1', avatar: '👑', rating: 1800, coins: 2000, seat: 0 },
        { id: 'p2', name: 'P2', avatar: '👑', rating: 1800, coins: 2000, seat: 1 },
        { id: 'p3', name: 'P3', avatar: '👑', rating: 1800, coins: 2000, seat: 2 },
        { id: 'p4', name: 'P4', avatar: '👑', rating: 1800, coins: 2000, seat: 3 },
      ],
      status: 'waiting',
      createdAt: Date.now() - 12000,
    },
  ];

  const best = pickBestWaitingRoom(waitingRooms, 'mahjong', 'hongzhong-6');
  assert(best, 'Must find a waiting room');
  assert.equal(best.id, 'room-two-players', 'Must pick fullest waiting room with space left');

  // 3. presenceTracker.getSceneWaitingStats accurately reflects scene status without bots
  const testSceneMap = new Map<string, SceneWaitingRoom>();
  testSceneMap.set('room-1', {
    id: 'room-1',
    gameType: 'mahjong',
    sceneMode: 'hongzhong-6',
    sceneName: '红中6码场',
    maxPlayers: 4,
    players: [
      { id: 'real-1', name: '张三', avatar: '👑', rating: 1800, coins: 2000, seat: 0 },
      { id: 'sim-bot-1', name: '人机1', avatar: '🤖', rating: 1800, coins: 2000, seat: 1, isSimulated: true },
    ],
    status: 'waiting',
    createdAt: Date.now(),
  });

  presenceTracker.setReferences({ sceneWaitingRooms: testSceneMap });
  const stats = presenceTracker.getSceneWaitingStats();
  assert(stats['hongzhong-6'], 'hongzhong-6 stats must exist');
  assert.equal(stats['hongzhong-6'].waitingCount, 1, 'Waiting count must be 1 (bot excluded)');
  assert.equal(stats['hongzhong-6'].totalInScene, 1, 'Total in scene must be 1 (bot excluded)');
  assert.equal(stats['hongzhong-6'].waitingPlayers.length, 1, 'Waiting players list must contain only human player');
  assert.equal(stats['hongzhong-6'].waitingPlayers[0].name, '张三');

  // Test getAllPlayers mapping with authoritative waiting room
  presenceTracker.recordClient('sock-test-1', {
    playerId: 'real-1',
    playerName: '张三',
    sceneMode: 'hall', // Client presence claims hall
  });
  const allPlayers = presenceTracker.getAllPlayers({});
  const found = allPlayers.find((p) => p.id === 'real-1');
  assert(found, 'Player must be found');
  assert.equal(found.sceneMode, 'hongzhong-6', 'Authoritative waiting room sceneMode must override client hall');
}

function runServerEngineRegressionTests() {
  // 1. Prefixed Yansan stakes ('yansan-10-20', 'yansan-20-40')
  const y1020Stl = calculateYansanSettlement('yansan-10-20', 0, false, null, 0, 2, [0, 2, 1]);
  assert.deepEqual(y1020Stl.seatScores, [20, -20, 20, -20], 'yansan-10-20 double win must pay +/-20');
  const y2040Stl = calculateYansanSettlement('yansan-20-40', 0, true, 0, 0, 0, [0]);
  assert.deepEqual(y2040Stl.seatScores, [240, -80, -80, -80], 'yansan-20-40 QiangGong solo win must pay +240/-80');
  const y1020Sur = calculateYansanSurrenderSettlement('yansan-10-20', 1);
  assert.deepEqual(y1020Sur.seatScores, [10, -30, 10, 10], 'yansan-10-20 surrender must deduct 30 from solo');
  const y2040Sur = calculateYansanSurrenderSettlement('yansan-20-40', 2);
  assert.deepEqual(y2040Sur.seatScores, [20, 20, -60, 20], 'yansan-20-40 surrender must deduct 60 from solo');

  // Mock RealtimeHubContext for server engine unit tests
  const broadcasts: any[] = [];
  const settledRecords: any[] = [];
  const mockCtx: any = {
    broadcast: (msg: any) => broadcasts.push(msg),
    sendSanitizedToRoom: (room: any, buildPayloadForSeat: (seat: number, player: any) => unknown) => {
      broadcasts.push(buildPayloadForSeat(0, room.players?.[0] || null));
    },
    settleRoomCoins: (room: any, deltas: number[], gameType: string) => {
      settledRecords.push({ roomId: room.id, deltas, gameType });
    },
    resetSceneRoomReadyAndCleanDisconnected: () => {},
  };

  // 2. Yansan Server Engine: 1v3 solo finishing 2nd ends the round with half-win
  const soloCard3 = yCard(3, '♠');
  const soloCardA = yCard(14, '♠');
  const d4 = yCard(4, '♦');
  const yansanRoom: any = {
    id: 'test-ys-1v3',
    gameType: 'yansan',
    sceneMode: 'yansan-10-20',
    status: 'playing',
    moveCount: 2,
    turnSeat: 0,
    players: [
      { id: 'u0', seat: 0 },
      { id: 'u1', seat: 1 },
      { id: 'u2', seat: 2 },
      { id: 'u3', seat: 3 },
    ],
    deal: {
      initialSeatCards: [
        [soloCard3, soloCardA, yCard(7, '♥')],
        [d4, yCard(8, '♣')],
        [yCard(9, '♦')],
        [yCard(10, '♣')],
      ],
      seatCards: [
        [soloCard3], // Solo seat 0 has 1 card left after playing soloCardA earlier
        [yCard(8, '♣')],
        [], // Farmer seat 2 already finished 1st
        [yCard(10, '♣')],
      ],
    },
    boardState: {
      phase: 'playing',
      turnSeat: 0,
      handsCount: [1, 1, 0, 1],
      finishedSeats: [2],
      lastCombo: null,
      extraState: {
        playedCardsHistory: [yCard(9, '♦'), soloCardA],
        seatPlayedCards: [[soloCardA], [], [yCard(9, '♦')], []],
        playHistory: [],
        hasPassedSeats: [false, false, false, false],
        consecutivePasses: 0,
      },
    },
  };
  handleYansanGameMove(
    mockCtx,
    yansanRoom,
    0,
    'play',
    { cards: [soloCard3] },
    { action: 'play', seat: 0, combo: { cards: [soloCard3] } }
  );
  assert.equal(yansanRoom.status, 'finished', '1v3 solo finishing 2nd must immediately finish round');
  assert.equal(yansanRoom.boardState.phase, 'ended');
  assert.deepEqual(yansanRoom.boardState.settlement.seatScores, [30, -10, -10, -10], '1v3 2nd place in yansan-10-20 must score +30/-10');

  // 3. Yansan Server Engine: invalid play triggers targeted yansan_move_rejected
  const sentWs: any[] = [];
  const mockWs: any = { send: (raw: string) => sentWs.push(JSON.parse(raw)) };
  const rejectRoom: any = {
    id: 'test-ys-reject',
    gameType: 'yansan',
    sceneMode: 'yansan-5-10',
    status: 'playing',
    moveCount: 0,
    turnSeat: 1,
    deal: {
      initialSeatCards: [[d4, yCard(5, '♠')], [yCard(6, '♥')], [yCard(7, '♣')], [yCard(8, '♦')]],
      seatCards: [[d4, yCard(5, '♠')], [yCard(6, '♥')], [yCard(7, '♣')], [yCard(8, '♦')]],
    },
    boardState: {
      phase: 'playing',
      turnSeat: 0,
      handsCount: [2, 1, 1, 1],
      finishedSeats: [],
      lastCombo: null,
      extraState: {
        playedCardsHistory: [],
        seatPlayedCards: [[], [], [], []],
        playHistory: [],
        hasPassedSeats: [false, false, false, false],
      },
    },
  };
  handleYansanGameMove(
    mockCtx,
    rejectRoom,
    0,
    'play',
    { cards: [yCard(5, '♠')] }, // First play missing ♦4!
    { action: 'play', seat: 0 },
    mockWs
  );
  assert.equal(sentWs.length, 1, 'Invalid first play without ♦4 must send rejection');
  assert.equal(sentWs[0].event.action, 'yansan_move_rejected');
  assert.equal(sentWs[0].event.reason, 'must_contain_diamond4');

  // 4. Yansan Server Engine: post-QiangGong AI auto-turn plays ♦4 even though room.moveCount > 0
  const qgRoom: any = {
    id: 'test-ys-qg',
    gameType: 'yansan',
    sceneMode: 'yansan-5-10',
    status: 'playing',
    moveCount: 5,
    turnSeat: 1,
    deal: {
      initialSeatCards: [
        [soloCard3, soloCardA],
        [d4, yCard(9, '♣')],
        [yCard(7, '♥')],
        [yCard(8, '♠')],
      ],
      seatCards: [
        [soloCard3, soloCardA],
        [d4, yCard(9, '♣')],
        [yCard(7, '♥')],
        [yCard(8, '♠')],
      ],
    },
    boardState: {
      phase: 'playing',
      turnSeat: 1,
      handsCount: [2, 2, 1, 1],
      finishedSeats: [],
      lastCombo: null,
      extraState: {
        isQiangGongActive: true,
        qiangGongSeat: 0,
        playedCardsHistory: [],
        seatPlayedCards: [[], [], [], []],
        playHistory: [],
        hasPassedSeats: [false, false, false, false],
      },
    },
  };
  tickYansanAutoTurn(mockCtx, qgRoom, 1, Date.now());
  const qgPlayedIds = (qgRoom.boardState.extraState.playedCardsHistory || []).map((c: any) => c.id);
  assert(qgPlayedIds.includes(d4.id), 'Post-QiangGong AI first lead must include ♦4 despite room.moveCount > 0');

  // 5. Mahjong Server Engine: tickMahjongAutoTurn detects win on pre-existing 3k+2 (14 tiles) hand
  const mjWinHand14 = [
    mjTile('wan', 1, 'w1a'), mjTile('wan', 1, 'w1b'), mjTile('wan', 1, 'w1c'),
    mjTile('wan', 2, 'w2a'), mjTile('wan', 3, 'w3a'), mjTile('wan', 4, 'w4a'),
    mjTile('tiao', 5, 't5a'), mjTile('tiao', 5, 't5b'), mjTile('tiao', 5, 't5c'),
    mjTile('tong', 7, 'o7a'), mjTile('tong', 8, 'o8a'), mjTile('tong', 9, 'o9a'),
    mjTile('wan', 9, 'w9a'), mjTile('wan', 9, 'w9b'),
  ];
  const mjRoom: any = {
    id: 'test-mj-auto-hu',
    gameType: 'mahjong',
    sceneMode: 'hongzhong-6',
    status: 'playing',
    moveCount: 0,
    turnSeat: 0,
    players: [{ id: 'm0', seat: 0 }, { id: 'm1', seat: 1 }, { id: 'm2', seat: 2 }, { id: 'm3', seat: 3 }],
    deal: {
      seatCards: [mjWinHand14, [], [], []],
      remainingWall: Array.from({ length: 12 }, (_, i) => mjTile('wan', 2, `wall-${i}`)),
    },
    boardState: {
      phase: 'playing',
      turnSeat: 0,
      handsCount: [14, 13, 13, 13],
      seatKongScores: [0, 0, 0, 0],
      extraState: {
        wallRemaining: 12,
        mahjongDiscards: [],
        mahjongMelds: [[], [], [], []],
      },
    },
  };
  tickMahjongAutoTurn(mockCtx, mjRoom, 0, Date.now());
  assert.equal(mjRoom.status, 'finished', 'tickMahjongAutoTurn must detect Hu when active seat already holds 3k+2 winning hand');
  assert.equal(mjRoom.boardState.phase, 'ended');
  assert.equal(mjRoom.boardState.winnerSeat, 0);
}

runYansanTests();
runShisanshuiTests();
runMahjongTests();
runSanitizerTests();
runRealtimeHubTests();
runServerEngineRegressionTests();
console.log('✅ All rule engine regression tests passed (Yansan, Shisanshui, Booking, Mahjong, Sanitizers, RealtimeHub, ServerEngines).');
