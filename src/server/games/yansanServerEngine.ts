import type { WebSocket } from 'ws';
import {
  parseYansanCombo,
  canBeatYansanCombo,
  getYansanSmartAIPlay,
  getYansanRankWeight,
  isYansanComboDeductivelyUnbeatable,
  calculateYansanSettlement,
  calculateYansanSurrenderSettlement,
  detectYansanSpecialHolders,
  type YansanCard,
} from '../../engine/yansanRules.ts';
import type { RealtimeHubContext } from '../realtimeTypes.ts';
import { createYansanStandardDeck } from '../sanitizers.ts';

export function resolveYansanFinish(
  ctx: RealtimeHubContext,
  room: any,
  triggerWinnerSeat: number,
  holders?: ReturnType<typeof detectYansanSpecialHolders>
) {
  if (!room || !room.boardState) return;
  const bs = room.boardState;
  if (bs.phase === 'ended') return;

  const dealObj = (room as any).deal || {};
  const h =
    holders ||
    detectYansanSpecialHolders(
      dealObj.initialSeatCards || dealObj.seatCards || [[], [], [], []],
      bs.extraState?.seatPlayedCards
    );
  const fin = Array.isArray(bs.finishedSeats) ? [...bs.finishedSeats] : [];
  const isQG = Boolean(bs.extraState?.isQiangGongActive);
  const qgSeat = bs.extraState?.qiangGongSeat ?? null;
  const firstFin = fin[0] ?? triggerWinnerSeat;

  const stl = calculateYansanSettlement(
    room.sceneMode || '5-10',
    firstFin,
    isQG,
    qgSeat,
    h.spade3Seat,
    h.spadeASeat,
    fin
  );

  bs.winnerSeat = stl.winnerSeat;
  bs.phase = 'ended';
  room.status = 'finished';
  bs.finishedSeats = fin;
  const enrichedStl = {
    ...stl,
    spade3Seat: h.spade3Seat,
    spadeASeat: h.spadeASeat,
    soloSeat: h.soloSeat,
  };
  bs.allianceScoreResult = enrichedStl;
  bs.settlement = enrichedStl;
  room.moveCount = (room.moveCount || 0) + 1;
  bs.moveCount = room.moveCount;
  bs.actionSeq = (bs.actionSeq || 0) + 1;
  bs.updatedAt = Date.now();

  ctx.settleRoomCoins(room, stl.seatScores, 'yansan', {
    ratingDeltaBySeat: stl.seatScores.map((pts) => Math.round(pts * 0.8)),
  });

  ctx.resetSceneRoomReadyAndCleanDisconnected(room.id);

  const finPayload = {
    action: 'finish',
    seat: firstFin,
    winner: stl.winnerSeat,
    finishedSeats: fin,
    spade3Seat: h.spade3Seat,
    spadeASeat: h.spadeASeat,
    soloSeat: h.soloSeat,
    allianceScoreResult: enrichedStl,
    fromServerSettlement: true,
  };

  ctx.sendSanitizedToRoom(room, () => ({
    type: 'game_event',
    roomId: room.id,
    seat: firstFin,
    event: finPayload,
    payload: finPayload,
    moveCount: room.moveCount,
    boardState: bs,
  }));
}

export function handleYansanGameMove(
  ctx: RealtimeHubContext,
  room: any,
  seat: number,
  action: string,
  combo: any,
  p: any,
  ws?: WebSocket | null
): void {
  const bs = room.boardState;
  if (!bs.extraState) bs.extraState = {};
  if (!bs.extraState.playedCardsHistory) bs.extraState.playedCardsHistory = [];
  if (!bs.extraState.seatPlayedCards) bs.extraState.seatPlayedCards = [[], [], [], []];
  if (!bs.extraState.playHistory) bs.extraState.playHistory = [];
  if (!bs.extraState.hasPassedSeats) bs.extraState.hasPassedSeats = [false, false, false, false];
  if (!bs.finishedSeats) bs.finishedSeats = [];

  const dealObj = (room as any).deal || {};
  const holders = detectYansanSpecialHolders(
    dealObj.initialSeatCards || dealObj.seatCards || [[], [], [], []],
    bs.extraState.seatPlayedCards
  );

  const sendYansanReject = (reason: string) => {
    if (!ws) return;
    const seatHand = Array.isArray(dealObj.seatCards?.[seat]) ? [...dealObj.seatCards[seat]] : undefined;
    const rejectEvt = {
      action: 'yansan_move_rejected',
      seat,
      absSeat: seat,
      turnSeat: bs.turnSeat,
      lastCombo: bs.lastCombo ?? null,
      seatHand,
      handsCount: Array.isArray(bs.handsCount) ? [...bs.handsCount] : undefined,
      playedCardsHistory: [...(bs.extraState.playedCardsHistory || [])],
      seatPlayedCards: (bs.extraState.seatPlayedCards || [[], [], [], []]).map((arr: any[]) => [...arr]),
      finishedSeats: [...(bs.finishedSeats || [])],
      isQiangGongActive: Boolean(bs.extraState.isQiangGongActive),
      qiangGongSeat: bs.extraState.qiangGongSeat ?? null,
      hasPassedSeats: [...(bs.extraState.hasPassedSeats || [false, false, false, false])],
      consecutivePasses: bs.extraState.consecutivePasses || 0,
      reason,
    };
    try {
      ws.send(
        JSON.stringify({
          type: 'game_event',
          roomId: room.id,
          seat,
          event: rejectEvt,
          payload: rejectEvt,
          boardState: bs,
        })
      );
    } catch {}
  };

  if (action === 'qianggong') {
    const seatInitial: any[] = dealObj.initialSeatCards?.[seat] || dealObj.seatCards?.[seat] || [];
    const holdsSpade3 = seatInitial.some((c: any) => c.rank === 3 && (c.suit === '♠' || c.suitVal === 4));
    const holdsSpadeA = seatInitial.some((c: any) => c.rank === 14 && (c.suit === '♠' || c.suitVal === 4));
    if (
      bs.extraState.isQiangGongActive ||
      bs.extraState.hasPassedSeats[seat] ||
      !holdsSpade3 ||
      !holdsSpadeA
    ) {
      sendYansanReject('invalid_qianggong');
      return;
    }

    bs.extraState.isQiangGongActive = true;
    bs.extraState.qiangGongSeat = seat;
    if (Array.isArray(dealObj.initialSeatCards)) {
      dealObj.seatCards = dealObj.initialSeatCards.map((h: any[]) => [...h]);
    }
    let d4Seat = dealObj.starterSeat ?? 0;
    if (Array.isArray(dealObj.seatCards)) {
      for (let s = 0; s < dealObj.seatCards.length; s++) {
        if (dealObj.seatCards[s]?.some((c: any) => c.rank === 4 && (c.suit === '♦' || c.suitVal === 1))) {
          d4Seat = s;
          break;
        }
      }
    }
    bs.handsCount = [13, 13, 13, 13];
    bs.finishedSeats = [];
    bs.lastCombo = null;
    bs.discardsHistory = [];
    bs.extraState.playedCardsHistory = [];
    bs.extraState.seatPlayedCards = [[], [], [], []];
    bs.extraState.playHistory = [];
    bs.extraState.consecutivePasses = 0;
    bs.extraState.hasPassedSeats = [false, false, false, false];
    bs.turnSeat = d4Seat;
    room.turnSeat = d4Seat;
    room.moveCount = (room.moveCount || 0) + 1;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.updatedAt = Date.now();
    room.lastTurnTs = Date.now();

    const qgEvt = {
      action: 'qianggong',
      seat,
      qiangGongSeat: seat,
      starterSeat: d4Seat,
      nextTurn: d4Seat,
    };
    ctx.sendSanitizedToRoom(room, () => ({
      type: 'game_event',
      roomId: room.id,
      seat,
      event: qgEvt,
      payload: qgEvt,
      moveCount: room.moveCount,
      boardState: bs,
    }));
    return;
  }

  if (action === 'surrender') {
    const seatHand: any[] = dealObj.seatCards?.[seat] || [];
    const holdsSpade3 = seatHand.some((c: any) => c.rank === 3 && (c.suit === '♠' || c.suitVal === 4));
    const holdsSpadeA = seatHand.some((c: any) => c.rank === 14 && (c.suit === '♠' || c.suitVal === 4));
    if (
      bs.extraState.isQiangGongActive ||
      bs.extraState.hasPassedSeats[seat] ||
      (Array.isArray(seatHand) && seatHand.length !== 13) ||
      !holdsSpade3 ||
      !holdsSpadeA
    ) {
      sendYansanReject('invalid_surrender');
      return;
    }

    const stl = calculateYansanSurrenderSettlement(room.sceneMode || '5-10', seat);
    bs.winnerSeat = stl.winnerSeat;
    bs.phase = 'ended';
    room.status = 'finished';
    bs.allianceScoreResult = stl;
    bs.settlement = stl;
    room.moveCount = (room.moveCount || 0) + 1;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.updatedAt = Date.now();

    ctx.settleRoomCoins(room, stl.seatScores, 'yansan', {
      ratingDeltaBySeat: stl.seatScores.map((pts) => Math.round(pts * 0.8)),
    });
    ctx.resetSceneRoomReadyAndCleanDisconnected(room.id);

    const surEvt = {
      action: 'surrender',
      seat,
      winner: stl.winnerSeat,
      finishedSeats: [stl.winnerSeat],
      allianceScoreResult: stl,
      fromServerSettlement: true,
    };
    ctx.sendSanitizedToRoom(room, () => ({
      type: 'game_event',
      roomId: room.id,
      seat,
      event: surEvt,
      payload: surEvt,
      moveCount: room.moveCount,
      boardState: bs,
    }));
    return;
  }

  if (action === 'finish' || (p.winner !== undefined && p.winner !== null)) {
    if (Array.isArray(p.finishedSeats)) {
      bs.finishedSeats = [...p.finishedSeats];
    }
    resolveYansanFinish(ctx, room, Number(p.winner ?? seat), holders);
    return;
  }

  if (action === 'play') {
    if (typeof bs.turnSeat === 'number' && bs.turnSeat !== seat) {
      sendYansanReject('not_your_turn');
      return;
    }
    const cardsToPlay: YansanCard[] = combo?.cards || [];
    const parsed = parseYansanCombo(cardsToPlay);
    const serverHand: any[] | undefined = dealObj.seatCards?.[seat];
    if (!parsed || (Array.isArray(serverHand) && !cardsToPlay.every((c) => serverHand.some((h) => h.id === c.id)))) {
      sendYansanReject('invalid_combo_or_hand');
      return;
    }
    const activeTarget = bs.lastCombo && bs.lastCombo.seat !== seat ? bs.lastCombo : null;
    if (activeTarget && !canBeatYansanCombo(parsed, activeTarget)) {
      sendYansanReject('cannot_beat_target');
      return;
    }
    const hasAnyCardsPlayed = (bs.extraState.playedCardsHistory || []).length > 0;
    if (!hasAnyCardsPlayed) {
      const hasD4 = cardsToPlay.some((c) => c.rank === 4 && (c.suit === '♦' || c.suitVal === 1));
      if (
        Array.isArray(serverHand) &&
        serverHand.some((c: any) => c.rank === 4 && (c.suit === '♦' || c.suitVal === 1)) &&
        !hasD4
      ) {
        sendYansanReject('must_contain_diamond4');
        return;
      }
    }

    const primaryCard = parsed.cards[0] || cardsToPlay[0];
    const isTier3Or4 = parsed.type === '5_card' && (parsed.comboTier === 3 || parsed.comboTier === 4);
    const resolvedSuitVal = parsed.suitVal || primaryCard?.suitVal || 1;
    const authoritativeCombo = {
      ...parsed,
      seat,
      primaryRank: parsed.rank,
      primarySuitVal: isTier3Or4 ? 0 : getYansanRankWeight(parsed.rank) * 10 + resolvedSuitVal,
      flushSuit: (parsed.comboTier === 2 || parsed.comboTier === 5) ? resolvedSuitVal : undefined,
    };
    room.moveCount = (room.moveCount || 0) + 1;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.updatedAt = Date.now();
    room.lastTurnTs = Date.now();

    bs.lastCombo = authoritativeCombo;
    if (!bs.discardsHistory) bs.discardsHistory = [];
    bs.discardsHistory.push(authoritativeCombo);
    if (bs.discardsHistory.length > 100) bs.discardsHistory = bs.discardsHistory.slice(-100);
    bs.extraState.playedCardsHistory.push(...cardsToPlay);
    if (!bs.extraState.seatPlayedCards[seat]) bs.extraState.seatPlayedCards[seat] = [];
    bs.extraState.seatPlayedCards[seat].push(...cardsToPlay);
    bs.extraState.playHistory.push({
      seat,
      cards: cardsToPlay,
      combo: authoritativeCombo,
      targetCombo: activeTarget,
      isPass: false,
      actionSeq: bs.actionSeq,
      timestamp: Date.now(),
    });
    bs.extraState.consecutivePasses = 0;

    if (Array.isArray(serverHand)) {
      const playedIds = new Set(cardsToPlay.map((c: any) => c.id));
      dealObj.seatCards[seat] = serverHand.filter((c: any) => !playedIds.has(c.id));
      bs.handsCount[seat] = dealObj.seatCards[seat].length;
    } else if (seat < bs.handsCount.length) {
      bs.handsCount[seat] = Math.max(0, bs.handsCount[seat] - cardsToPlay.length);
    }

    if (bs.handsCount[seat] === 0 && !bs.finishedSeats.includes(seat)) {
      bs.finishedSeats.push(seat);
    }

    const totalSeats = bs.handsCount?.length || 4;
    const finished = bs.finishedSeats || [];
    let nextSeat = (seat - 1 + totalSeats) % totalSeats;
    let guard = 0;
    while (finished.includes(nextSeat) && guard < totalSeats) {
      nextSeat = (nextSeat - 1 + totalSeats) % totalSeats;
      guard++;
    }

    const isQG = Boolean(bs.extraState.isQiangGongActive);
    const is2v2 = holders.spade3Seat !== holders.spadeASeat;
    let shouldEnd = false;
    if (isQG) {
      shouldEnd = finished.length >= 1;
    } else if (is2v2) {
      const isSpade = (s: number) => s === holders.spade3Seat || s === holders.spadeASeat;
      if (finished.length >= 2 && isSpade(finished[0]) === isSpade(finished[1])) {
        shouldEnd = true;
      } else if (finished.length >= 3) {
        shouldEnd = true;
      }
    } else {
      if ((holders.soloSeat !== null && finished.includes(holders.soloSeat)) || finished.length >= 3) {
        shouldEnd = true;
      }
    }

    let resolvedNextTurn = nextSeat;
    let unbeatableAutoClear = false;
    if (!shouldEnd) {
      const playedIdsAll = new Set((bs.extraState.playedCardsHistory || []).map((c: any) => c.id));
      const availablePool = createYansanStandardDeck().filter((c) => !playedIdsAll.has(c.id));
      let maxOppCards = 0;
      for (let sIdx = 0; sIdx < totalSeats; sIdx++) {
        if (sIdx !== seat && !finished.includes(sIdx)) {
          maxOppCards = Math.max(maxOppCards, Number(bs.handsCount?.[sIdx] || 0));
        }
      }
      const isDeductivelyUnbeatable = isYansanComboDeductivelyUnbeatable(parsed, availablePool, maxOppCards);
      const hasSpade3 = cardsToPlay.some((c) => c.rank === 3 && (c.suit === '♠' || c.suitVal === 4));
      if (bs.handsCount[seat] === 0 && (isDeductivelyUnbeatable || hasSpade3)) {
        bs.lastCombo = null;
        bs.extraState.consecutivePasses = 0;
        resolvedNextTurn = nextSeat;
        unbeatableAutoClear = true;
      } else if (bs.handsCount[seat] > 0 && isDeductivelyUnbeatable) {
        bs.lastCombo = null;
        bs.extraState.consecutivePasses = 0;
        resolvedNextTurn = seat;
        unbeatableAutoClear = true;
      }
    }
    bs.turnSeat = resolvedNextTurn;
    room.turnSeat = resolvedNextTurn;

    const playEvt = {
      ...p,
      action: 'play',
      seat,
      combo: authoritativeCombo,
      nextTurn: resolvedNextTurn,
      finishedSeats: bs.finishedSeats,
      unbeatableAutoClear: unbeatableAutoClear || undefined,
    };
    ctx.sendSanitizedToRoom(room, () => ({
      type: 'game_event',
      roomId: room.id,
      seat,
      event: playEvt,
      payload: playEvt,
      moveCount: room.moveCount,
      boardState: bs,
    }));

    if (shouldEnd) {
      resolveYansanFinish(ctx, room, finished[0] ?? seat, holders);
    }
    return;
  }

  if (action === 'pass') {
    if (typeof bs.turnSeat === 'number' && bs.turnSeat !== seat) {
      sendYansanReject('not_your_turn');
      return;
    }
    if (!bs.lastCombo || bs.lastCombo.seat === seat) {
      sendYansanReject('cannot_pass_on_lead');
      return;
    }
    room.moveCount = (room.moveCount || 0) + 1;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.updatedAt = Date.now();
    room.lastTurnTs = Date.now();

    bs.extraState.hasPassedSeats[seat] = true;
    bs.extraState.consecutivePasses = (bs.extraState.consecutivePasses || 0) + 1;
    bs.extraState.playHistory.push({
      seat,
      cards: [],
      combo: null,
      targetCombo: bs.lastCombo,
      isPass: true,
      actionSeq: bs.actionSeq,
      timestamp: Date.now(),
    });

    const totalSeats = bs.handsCount?.length || 4;
    const finished = bs.finishedSeats || [];
    const activeCount = Math.max(1, totalSeats - finished.length);
    const lastLeader = bs.lastCombo?.seat;
    const leaderFinished = lastLeader !== undefined && finished.includes(lastLeader);
    const passThreshold = leaderFinished ? activeCount : Math.max(1, activeCount - 1);
    if (bs.extraState.consecutivePasses >= passThreshold) {
      bs.lastCombo = null;
      bs.extraState.consecutivePasses = 0;
    }

    let nextSeat = (seat - 1 + totalSeats) % totalSeats;
    let guard = 0;
    while (finished.includes(nextSeat) && guard < totalSeats) {
      nextSeat = (nextSeat - 1 + totalSeats) % totalSeats;
      guard++;
    }
    const resolvedNextTurn = nextSeat;
    bs.turnSeat = resolvedNextTurn;
    room.turnSeat = resolvedNextTurn;

    const passEvt = {
      ...p,
      action: 'pass',
      seat,
      nextTurn: resolvedNextTurn,
      finishedSeats: bs.finishedSeats,
    };
    ctx.sendSanitizedToRoom(room, () => ({
      type: 'game_event',
      roomId: room.id,
      seat,
      event: passEvt,
      payload: passEvt,
      moveCount: room.moveCount,
      boardState: bs,
    }));
    return;
  }
}

export function tickYansanAutoTurn(ctx: RealtimeHubContext, room: any, currentSeat: number, now: number): void {
  const totalSeats = room.boardState.handsCount?.length || 4;
  const finished: number[] = Array.isArray(room.boardState.finishedSeats) ? room.boardState.finishedSeats : [];
  const bs = room.boardState;
  const seatHand: YansanCard[] = Array.isArray((room as any).deal?.seatCards?.[currentSeat])
    ? (room as any).deal.seatCards[currentSeat]
    : [];
  if (seatHand.length === 0) return;

  if (!bs.extraState) bs.extraState = {};
  if (!bs.extraState.seatPlayedCards) bs.extraState.seatPlayedCards = [[], [], [], []];
  if (!bs.extraState.playHistory) bs.extraState.playHistory = [];
  if (!bs.extraState.hasPassedSeats) bs.extraState.hasPassedSeats = [false, false, false, false];

  const isAbsoluteFirst = (bs.extraState.playedCardsHistory || []).length === 0;
  const targetCombo = bs.lastCombo && bs.lastCombo.seat !== currentSeat ? bs.lastCombo : null;
  const aiRes = getYansanSmartAIPlay(seatHand, targetCombo, {
    isFirstTurn: isAbsoluteFirst,
    seat: currentSeat,
    handsCount: bs.handsCount,
    seatPlayedCards: bs.extraState.seatPlayedCards,
    playHistory: bs.extraState.playHistory,
    isQiangGong: Boolean(bs.extraState.isQiangGongActive),
    qiangGongSeat: bs.extraState.qiangGongSeat ?? null,
  });

  let nextSeat = (currentSeat - 1 + totalSeats) % totalSeats;
  let stepGuard = 0;
  while (finished.includes(nextSeat) && stepGuard < totalSeats) {
    nextSeat = (nextSeat - 1 + totalSeats) % totalSeats;
    stepGuard++;
  }

  const parsedAiCombo = aiRes.cards && aiRes.cards.length > 0 ? parseYansanCombo(aiRes.cards) : null;
  const canPlayAi = Boolean(parsedAiCombo && (!targetCombo || canBeatYansanCombo(parsedAiCombo, targetCombo)));

  if (canPlayAi && parsedAiCombo) {
    room.moveCount = (room.moveCount || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.updatedAt = now;

    const playedIds = new Set(parsedAiCombo.cards.map((c) => c.id));
    (room as any).deal.seatCards[currentSeat] = seatHand.filter((c) => !playedIds.has(c.id));

    const primaryCard = parsedAiCombo.cards[0];
    const isTier3Or4 = parsedAiCombo.type === '5_card' && (parsedAiCombo.comboTier === 3 || parsedAiCombo.comboTier === 4);
    const resolvedSuitVal = parsedAiCombo.suitVal || primaryCard?.suitVal || 1;
    const authoritativeCombo = {
      ...parsedAiCombo,
      seat: currentSeat,
      primaryRank: parsedAiCombo.rank,
      primarySuitVal: isTier3Or4 ? 0 : getYansanRankWeight(parsedAiCombo.rank) * 10 + resolvedSuitVal,
      flushSuit: (parsedAiCombo.comboTier === 2 || parsedAiCombo.comboTier === 5) ? resolvedSuitVal : undefined,
    };
    bs.lastCombo = authoritativeCombo;
    if (!bs.extraState.playedCardsHistory) bs.extraState.playedCardsHistory = [];
    bs.extraState.playedCardsHistory.push(...parsedAiCombo.cards);
    bs.extraState.seatPlayedCards[currentSeat].push(...parsedAiCombo.cards);
    bs.extraState.playHistory.push({
      seat: currentSeat,
      cards: parsedAiCombo.cards,
      combo: authoritativeCombo,
      targetCombo,
      isPass: false,
      actionSeq: bs.actionSeq,
      timestamp: now,
    });
    bs.extraState.consecutivePasses = 0;
    if (!bs.discardsHistory) bs.discardsHistory = [];
    bs.discardsHistory.push(authoritativeCombo);

    if (bs.handsCount) {
      bs.handsCount[currentSeat] = (room as any).deal.seatCards[currentSeat].length;
    }
    if ((room as any).deal.seatCards[currentSeat].length === 0 && !finished.includes(currentSeat)) {
      if (!bs.finishedSeats) bs.finishedSeats = [];
      bs.finishedSeats.push(currentSeat);
    }

    const dealObj = (room as any).deal || {};
    const holders = detectYansanSpecialHolders(dealObj.initialSeatCards || dealObj.seatCards || [], bs.extraState.seatPlayedCards);
    const fin = bs.finishedSeats || [];
    const isQG = Boolean(bs.extraState.isQiangGongActive);
    const is2v2 = holders.spade3Seat !== holders.spadeASeat;
    let shouldEnd = false;
    if (isQG) {
      shouldEnd = fin.length >= 1;
    } else if (is2v2) {
      const isSpade = (s: number) => s === holders.spade3Seat || s === holders.spadeASeat;
      if (fin.length >= 2 && isSpade(fin[0]) === isSpade(fin[1])) shouldEnd = true;
      else if (fin.length >= 3) shouldEnd = true;
    } else {
      if ((holders.soloSeat !== null && fin.includes(holders.soloSeat)) || fin.length >= 3) shouldEnd = true;
    }

    let afterSeat = (currentSeat - 1 + totalSeats) % totalSeats;
    let guard = 0;
    while (fin.includes(afterSeat) && guard < totalSeats) {
      afterSeat = (afterSeat - 1 + totalSeats) % totalSeats;
      guard++;
    }
    let unbeatableAutoClear = false;
    if (!shouldEnd) {
      const playedIdsAll = new Set((bs.extraState.playedCardsHistory || []).map((c: any) => c.id));
      const availablePool = createYansanStandardDeck().filter((c) => !playedIdsAll.has(c.id));
      let maxOppCards = 0;
      for (let sIdx = 0; sIdx < totalSeats; sIdx++) {
        if (sIdx !== currentSeat && !fin.includes(sIdx)) {
          maxOppCards = Math.max(maxOppCards, Number(bs.handsCount?.[sIdx] || 0));
        }
      }
      const isDeductivelyUnbeatable = isYansanComboDeductivelyUnbeatable(parsedAiCombo, availablePool, maxOppCards);
      const hasSpade3 = parsedAiCombo.cards.some((c) => c.rank === 3 && (c.suit === '♠' || c.suitVal === 4));
      if ((bs.handsCount?.[currentSeat] || 0) === 0 && (isDeductivelyUnbeatable || hasSpade3)) {
        bs.lastCombo = null;
        bs.extraState.consecutivePasses = 0;
        unbeatableAutoClear = true;
      } else if ((bs.handsCount?.[currentSeat] || 0) > 0 && isDeductivelyUnbeatable) {
        bs.lastCombo = null;
        bs.extraState.consecutivePasses = 0;
        afterSeat = currentSeat;
        unbeatableAutoClear = true;
      }
    }
    room.turnSeat = afterSeat;
    bs.turnSeat = afterSeat;

    const evtPayload = {
      action: 'play',
      seat: currentSeat,
      autoPlay: true,
      combo: authoritativeCombo,
      cardsCount: parsedAiCombo.cards.length,
      nextTurn: afterSeat,
      finishedSeats: bs.finishedSeats,
      unbeatableAutoClear: unbeatableAutoClear || undefined,
      reason: isAbsoluteFirst ? 'timeout_first_move_diamond4' : seatHand.length === 1 ? 'baodan_auto_beat' : 'timeout_smart_play',
    };
    ctx.sendSanitizedToRoom(room, () => ({
      type: 'game_event',
      roomId: room.id,
      seat: currentSeat,
      event: evtPayload,
      payload: evtPayload,
      moveCount: room.moveCount,
      boardState: bs,
    }));

    if (shouldEnd) {
      resolveYansanFinish(ctx, room, fin[0] ?? currentSeat, holders);
    }
  } else {
    bs.extraState.hasPassedSeats[currentSeat] = true;
    bs.extraState.consecutivePasses = (bs.extraState.consecutivePasses || 0) + 1;
    room.moveCount = (room.moveCount || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.updatedAt = now;
    bs.extraState.playHistory.push({
      seat: currentSeat,
      cards: [],
      combo: null,
      targetCombo: bs.lastCombo,
      isPass: true,
      actionSeq: bs.actionSeq,
      timestamp: now,
    });

    const activeCount = Math.max(1, totalSeats - finished.length);
    const lastLeader = bs.lastCombo?.seat;
    const leaderFinished = lastLeader !== undefined && finished.includes(lastLeader);
    const passThreshold = leaderFinished ? activeCount : Math.max(1, activeCount - 1);
    if (bs.extraState.consecutivePasses >= passThreshold) {
      bs.lastCombo = null;
      bs.extraState.consecutivePasses = 0;
    }
    room.turnSeat = nextSeat;
    bs.turnSeat = nextSeat;

    ctx.sendSanitizedToRoom(room, () => ({
      type: 'game_event',
      roomId: room.id,
      seat: currentSeat,
      event: {
        action: 'pass',
        seat: currentSeat,
        autoPass: true,
        nextTurn: nextSeat,
        reason: 'timeout_pass',
      },
      payload: {
        action: 'pass',
        seat: currentSeat,
        autoPass: true,
        nextTurn: nextSeat,
        reason: 'timeout_pass',
      },
      moveCount: room.moveCount,
      boardState: bs,
    }));
  }
}
