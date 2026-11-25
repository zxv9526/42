import type { WebSocket } from 'ws';
import { generateRoomDeal } from '../../gameDeckService.ts';
import { checkMahjongWin, computeMahjongSettlement, sortMahjongTiles } from '../../engine/mahjongRules.ts';
import {
  isBotLikePlayer,
  SETTLEMENT_TIMEOUT_MS,
  type RealtimeHubContext,
} from '../realtimeTypes.ts';
import { maskHiddenMahjongTile, sanitizeBoardStateForSeat } from '../sanitizers.ts';

export function executeMahjongWinSettlement(
  ctx: RealtimeHubContext,
  room: any,
  winnerSeat: number,
  isFourHz: boolean,
  pPayload?: any
) {
  if (!room || !room.boardState) return;
  if (room.boardState.phase === 'ended') return;
  const settlementExpiresAt = Date.now() + SETTLEMENT_TIMEOUT_MS;
  room.boardState.winnerSeat = winnerSeat;
  room.boardState.phase = 'ended';
  if (room.boardState.extraState) {
    room.boardState.extraState.claimWindow = null;
    room.boardState.extraState.pendingPengSeat = null;
  }
  room.status = 'finished';
  room.settlementExpiresAt = settlementExpiresAt;

  const dealObj = room.deal || {};
  const authSeatCards = Array.isArray(dealObj.seatCards?.[winnerSeat])
    ? dealObj.seatCards[winnerSeat].filter((t: any) => t && !t.hidden)
    : [];
  const payloadWinnerCards = Array.isArray(pPayload?.winningHand)
    ? pPayload.winningHand.filter((t: any) => t && !t.hidden)
    : [];
  const rawWinnerHand =
    authSeatCards.length > 0
      ? authSeatCards
      : payloadWinnerCards.length > 0
        ? payloadWinnerCards
        : pPayload?.tile && !pPayload.tile.hidden
          ? [pPayload.tile]
          : [];
  const winnerHand = sortMahjongTiles(rawWinnerHand);

  const remWall = dealObj.remainingWall || [];
  const sMode = room.sceneMode || 'hongzhong-6';

  const serverMeldsRaw = room.boardState.extraState?.mahjongMelds?.[winnerSeat] || [];
  const fallbackWinningMelds = Array.isArray(serverMeldsRaw)
    ? serverMeldsRaw
        .map((m: any) =>
          Array.isArray(m)
            ? m
            : Array.isArray(m?.tiles)
              ? m.tiles
              : m?.tile
                ? Array(m.type === 'gang' ? 4 : 3).fill(m.tile)
                : []
        )
        .filter((arr: any[]) => arr.length > 0)
    : [];
  const resolvedWinningMelds =
    fallbackWinningMelds.length > 0
      ? fallbackWinningMelds
      : Array.isArray(pPayload?.winningMelds)
        ? pPayload.winningMelds
        : [];

  const resolvedKongScores: [number, number, number, number] =
    Array.isArray(room.boardState.seatKongScores) && room.boardState.seatKongScores.length === 4
      ? [
          Number(room.boardState.seatKongScores[0]) || 0,
          Number(room.boardState.seatKongScores[1]) || 0,
          Number(room.boardState.seatKongScores[2]) || 0,
          Number(room.boardState.seatKongScores[3]) || 0,
        ]
      : [0, 0, 0, 0];
  const resolvedKongHistory = Array.isArray(room.boardState.extraState?.kongHistory)
    ? [...room.boardState.extraState.kongHistory]
    : [];

  const stl = computeMahjongSettlement(
    winnerSeat,
    winnerHand,
    resolvedWinningMelds,
    remWall,
    sMode,
    resolvedKongScores,
    isFourHz,
    pPayload?.maTiles
  );

  room.boardState.winDesc = stl.isFourHongzhong ? '摸到四张红中直接胡牌' : '自摸和牌';
  room.boardState.settlement = {
    ...stl,
    winnerSeat,
    isFourHz: stl.isFourHongzhong,
    kongHistory: resolvedKongHistory,
    settlementExpiresAt,
    winDesc: room.boardState.winDesc,
  };

  ctx.settleRoomCoins(room, stl.netCoinsPerSeat, 'mahjong', {
    ratingDeltaBySeat: stl.netCoinsPerSeat.map((c) => (c > 0 ? 15 : c < 0 ? -5 : 0)),
  });

  const nowTs = Date.now();
  const huEvtPayload = {
    action: 'hu',
    seat: winnerSeat,
    absSeat: winnerSeat,
    winner: winnerSeat,
    isFourHz: stl.isFourHongzhong,
    winningHand: stl.winningHand,
    winningMelds: stl.winningMelds,
    maCount: stl.maCount,
    maTiles: stl.maTiles,
    hitMaCount: stl.hitMaCount,
    payoutPerOpponent: stl.payoutPerOpponent,
    netCoinsPerSeat: stl.netCoinsPerSeat,
    seatKongScores: stl.seatKongScores,
    kongHistory: resolvedKongHistory,
    rewardCoins: stl.netCoinsPerSeat[winnerSeat],
    reason: stl.isFourHongzhong ? 'four_hongzhong' : 'zimo_hu',
    settlementExpiresAt,
    serverTs: nowTs,
  };

  ctx.sendSanitizedToRoom(room, () => ({
    type: 'game_event',
    roomId: room.id,
    seat: winnerSeat,
    event: huEvtPayload,
    payload: huEvtPayload,
    boardState: room.boardState,
  }));

  ctx.resetSceneRoomReadyAndCleanDisconnected(room.id);
}

export function executeMahjongDrawStalemate(
  ctx: RealtimeHubContext,
  room: any,
  triggerSeat: number,
  reasonMsg = '牌墙抓毕，本局荒庄（流局）！'
) {
  if (!room || !room.boardState) return;
  if (room.boardState.phase === 'ended') return;

  const nowTs = Date.now();
  const settlementExpiresAt = nowTs + SETTLEMENT_TIMEOUT_MS;
  const bs = room.boardState;
  bs.phase = 'ended';
  bs.isDraw = true;
  if (!bs.extraState) bs.extraState = {};
  bs.extraState.wallRemaining = 0;
  bs.extraState.pendingPengSeat = null;
  bs.extraState.claimWindow = null;
  room.status = 'finished';
  room.settlementExpiresAt = settlementExpiresAt;
  room.moveCount = (room.moveCount || 0) + 1;
  bs.actionSeq = (bs.actionSeq || 0) + 1;
  bs.moveCount = room.moveCount;
  bs.updatedAt = nowTs;

  const kongScores: [number, number, number, number] =
    Array.isArray(bs.seatKongScores) && bs.seatKongScores.length === 4
      ? [
          Number(bs.seatKongScores[0]) || 0,
          Number(bs.seatKongScores[1]) || 0,
          Number(bs.seatKongScores[2]) || 0,
          Number(bs.seatKongScores[3]) || 0,
        ]
      : [0, 0, 0, 0];
  const hasKongDeltas = kongScores.some((k) => k !== 0);
  if (hasKongDeltas) {
    ctx.settleRoomCoins(room, kongScores, 'mahjong');
  }

  const stalemateKongHistory = Array.isArray(bs.extraState?.kongHistory)
    ? [...bs.extraState.kongHistory]
    : [];

  bs.settlement = {
    isDraw: true,
    message: reasonMsg,
    seatKongScores: kongScores,
    netCoinsPerSeat: kongScores,
    kongHistory: stalemateKongHistory,
    settlementExpiresAt,
  };

  ctx.resetSceneRoomReadyAndCleanDisconnected(room.id);

  const stalematePayload = {
    action: 'draw_stalemate',
    isDraw: true,
    reason: 'wall_exhausted',
    message: reasonMsg,
    msg: reasonMsg,
    seatKongScores: kongScores,
    netCoinsPerSeat: kongScores,
    kongHistory: stalemateKongHistory,
    settlementExpiresAt,
    serverTs: nowTs,
  };

  ctx.sendSanitizedToRoom(room, () => ({
    type: 'game_event',
    roomId: room.id,
    seat: triggerSeat,
    event: stalematePayload,
    payload: stalematePayload,
    moveCount: room.moveCount,
    boardState: bs,
  }));
}

export function computeMahjongClaimPendingSeats(room: any, bs: any, discardSeat: number, discTile: any): number[] {
  if (!discTile) return [];
  const isHongzhongMode = room.sceneMode?.includes('hongzhong') ?? true;
  const isRedDragonTile = (t: any) =>
    Boolean(t && t.type === 'zi' && (Number(t.value) === 5 || t.text === '红中' || t.isRedDragon));
  if (isHongzhongMode && isRedDragonTile(discTile)) return [];

  const seatCardsAll = (room as any).deal?.seatCards;
  if (!Array.isArray(seatCardsAll)) return [];

  const pending: number[] = [];
  for (let s = 0; s < 4; s++) {
    if (s === discardSeat) continue;
    const pl = room.players?.find((x: any) => x.seat === s);
    const isAuto = Boolean(
      pl?.isAutoManaged ||
        pl?.isDisconnected ||
        isBotLikePlayer(pl) ||
        bs.autoManagedSeats?.[s]
    );
    if (isAuto) continue;
    const hand = seatCardsAll[s];
    if (!Array.isArray(hand)) continue;
    const matchCount = hand.filter((c: any) => c.type === discTile.type && Number(c.value) === Number(discTile.value)).length;
    if (matchCount >= 2 && hand.length % 3 === 1) {
      pending.push(s);
    }
  }
  return pending;
}

export function handleMahjongGameMove(
  ctx: RealtimeHubContext,
  ws: WebSocket,
  room: any,
  seat: number,
  action: string,
  combo: any,
  p: any,
  evt: any
): void {
  const bs = room.boardState;
  if (!bs.extraState) bs.extraState = {};
  if (!bs.extraState.mahjongDiscards) bs.extraState.mahjongDiscards = [];
  if (!bs.extraState.mahjongMelds) bs.extraState.mahjongMelds = [[], [], [], []];
  if (bs.extraState.wallRemaining === undefined) {
    bs.extraState.wallRemaining = (room as any).deal?.remainingWall?.length ?? 83;
  }

  const isHongzhongMode = room.sceneMode?.includes('hongzhong') ?? true;
  const isTileMatch = (a: any, b: any) => Boolean(a && b && a.type === b.type && Number(a.value) === Number(b.value));
  const isRedDragonTile = (t: any) =>
    Boolean(t && t.type === 'zi' && (Number(t.value) === 5 || t.text === '红中' || t.isRedDragon));

  const sendMahjongReject = (rejectAction: string, reason: string, extra: Record<string, any> = {}) => {
    const seatHand = Array.isArray((room as any).deal?.seatCards?.[seat]) ? [...(room as any).deal.seatCards[seat]] : undefined;
    const rejectEvt = {
      action: rejectAction,
      seat,
      absSeat: seat,
      turnSeat: bs.turnSeat,
      pendingPengSeat: bs.extraState?.pendingPengSeat ?? null,
      wallRemaining: bs.extraState?.wallRemaining ?? ((room as any).deal?.remainingWall?.length ?? 0),
      seatHand,
      reason,
      ...extra,
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

  const tile = p.tile || evt.tile;

  if (action === 'discard') {
    const sCards: any[] | undefined = (room as any).deal?.seatCards?.[seat];
    if (
      (bs.extraState.pendingPengSeat !== null &&
        bs.extraState.pendingPengSeat !== undefined &&
        bs.extraState.pendingPengSeat !== seat) ||
      (typeof bs.turnSeat === 'number' && bs.turnSeat !== seat)
    ) {
      sendMahjongReject('discard_rejected', 'not_your_turn', { tile });
      return;
    }
    if (Array.isArray(sCards) && sCards.length % 3 !== 2) {
      sendMahjongReject('discard_rejected', 'invalid_hand_count', { tile });
      return;
    }
    let matchedTile = tile;
    if (Array.isArray(sCards)) {
      if (!tile) {
        sendMahjongReject('discard_rejected', 'missing_tile');
        return;
      }
      let fIdx = tile.id ? sCards.findIndex((c: any) => c.id === tile.id) : -1;
      if (fIdx < 0) {
        fIdx = sCards.findIndex((c: any) => isTileMatch(c, tile));
      }
      if (fIdx < 0) {
        sendMahjongReject('discard_rejected', 'tile_not_in_hand', { tile });
        return;
      }
      matchedTile = sCards.splice(fIdx, 1)[0];
    }

    const discTile = matchedTile ? { ...matchedTile, discardBy: seat } : tile;
    if (discTile) {
      bs.extraState.mahjongDiscards.push(discTile);
      bs.extraState.lastDiscarded = discTile;
      p.tile = discTile;
      if (!bs.discardsHistory) bs.discardsHistory = [];
      bs.discardsHistory.push({ type: 'mahjong_discard', rank: discTile.value || 0, cards: [discTile], seat });
    }

    if (Array.isArray(sCards)) {
      bs.handsCount[seat] = sCards.length;
    } else if (bs.handsCount && bs.handsCount[seat] > 0) {
      bs.handsCount[seat] = Math.max(0, bs.handsCount[seat] - 1);
    }

    const nextTurnSeat = p.nextTurn !== undefined ? Number(p.nextTurn) : (seat + 1) % 4;
    bs.turnSeat = nextTurnSeat;
    room.turnSeat = nextTurnSeat;
    bs.extraState.pendingPengSeat = null;
    bs.extraState.lastDrawnSeat = null;
    bs.extraState.lastDrawnTile = null;

    const wallArr = (room as any).deal?.remainingWall;
    if (Array.isArray(wallArr)) {
      bs.extraState.wallRemaining = wallArr.length;
    }
    p.wallRemaining = bs.extraState.wallRemaining;

    const pendingClaimSeats = discTile ? computeMahjongClaimPendingSeats(room, bs, seat, discTile) : [];
    if (pendingClaimSeats.length > 0) {
      bs.extraState.claimWindow = {
        discardId: discTile.id,
        discarderSeat: seat,
        pendingSeats: pendingClaimSeats,
        passedSeats: [],
        createdAt: Date.now(),
      };
    } else {
      bs.extraState.claimWindow = null;
    }
    p.pendingClaimSeats = pendingClaimSeats;
    p.hasClaimWindow = pendingClaimSeats.length > 0;

    room.moveCount = (room.moveCount || 0) + 1;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.updatedAt = Date.now();
    room.lastTurnTs = Date.now();

    const discEvtPayload = { ...p, action: 'discard', seat, nextTurn: nextTurnSeat };
    ctx.sendSanitizedToRoom(room, (rSeat) => ({
      type: 'game_event',
      roomId: room.id,
      seat,
      event: discEvtPayload,
      payload: discEvtPayload,
      moveCount: room.moveCount,
      boardState: sanitizeBoardStateForSeat(bs, 'mahjong', rSeat),
    }));

    if (pendingClaimSeats.length === 0) {
      if (Array.isArray(wallArr) && wallArr.length === 0) {
        executeMahjongDrawStalemate(ctx, room, nextTurnSeat, '牌墙抓毕，本局荒庄（流局）！');
      } else {
        bs.actionSeq = (bs.actionSeq || 0) + 1;
        const crPayload = {
          action: 'claim_resolved',
          discardId: discTile?.id || null,
          nextTurn: nextTurnSeat,
          wallRemaining: bs.extraState.wallRemaining,
        };
        ctx.sendSanitizedToRoom(room, (rSeat) => ({
          type: 'game_event',
          roomId: room.id,
          seat: nextTurnSeat,
          event: crPayload,
          payload: crPayload,
          moveCount: room.moveCount,
          boardState: sanitizeBoardStateForSeat(bs, 'mahjong', rSeat),
        }));
      }
    }
    return;
  } else if (action === 'draw') {
    const cw = bs.extraState.claimWindow;
    if (cw && cw.pendingSeats.some((ps: number) => !cw.passedSeats.includes(ps))) {
      sendMahjongReject('draw_rejected', 'claim_window_open');
      return;
    }
    if (
      bs.extraState.pendingPengSeat !== null &&
      bs.extraState.pendingPengSeat !== undefined &&
      bs.extraState.pendingPengSeat !== seat
    ) {
      sendMahjongReject('draw_rejected', 'pending_peng');
      return;
    }
    if (bs.turnSeat !== seat) {
      sendMahjongReject('draw_rejected', 'not_your_turn');
      return;
    }
    const sCards: any[] | undefined = (room as any).deal?.seatCards?.[seat];
    if (Array.isArray(sCards) && sCards.length % 3 !== 1) {
      sendMahjongReject('draw_rejected', 'already_drew');
      return;
    }
    const wallArr = (room as any).deal?.remainingWall;
    if (!Array.isArray(wallArr) || wallArr.length === 0) {
      executeMahjongDrawStalemate(ctx, room, seat, '牌墙抓毕，本局荒庄（流局）！');
      return;
    }
    const drawn = wallArr.shift();
    if (drawn && (room as any).deal?.seatCards?.[seat]) {
      (room as any).deal.seatCards[seat].push(drawn);
    }
    bs.extraState.wallRemaining = wallArr.length;
    p.tile = drawn;
    p.wallRemaining = wallArr.length;
    bs.turnSeat = seat;
    room.turnSeat = bs.turnSeat;
    room.moveCount = (room.moveCount || 0) + 1;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.updatedAt = Date.now();
    room.lastTurnTs = Date.now();
    if (bs.handsCount) {
      bs.handsCount[seat] = (room as any).deal?.seatCards?.[seat]?.length ?? ((bs.handsCount[seat] || 13) + 1);
    }
    bs.extraState.lastDrawnSeat = seat;
    bs.extraState.lastDrawnTile = drawn;
  } else if (action === 'peng') {
    const lastDisc = bs.extraState.lastDiscarded;
    const sCards: any[] | undefined = (room as any).deal?.seatCards?.[seat];
    if (
      !lastDisc ||
      (bs.extraState.pendingPengSeat !== null && bs.extraState.pendingPengSeat !== undefined) ||
      lastDisc.discardBy === seat ||
      !tile ||
      !isTileMatch(tile, lastDisc) ||
      (p.targetDiscardId && lastDisc.id !== p.targetDiscardId) ||
      (isHongzhongMode && isRedDragonTile(lastDisc))
    ) {
      sendMahjongReject('claim_rejected', 'discard_not_available', { claimType: 'peng', tile });
      return;
    }
    if (Array.isArray(sCards)) {
      const matchCount = sCards.filter((c: any) => isTileMatch(c, lastDisc)).length;
      if (sCards.length % 3 !== 1 || matchCount < 2) {
        sendMahjongReject('claim_rejected', 'insufficient_matching_tiles', { claimType: 'peng', tile });
        return;
      }
    }

    bs.extraState.claimWindow = null;

    let canceledDrawSeat: number | null = null;
    let canceledDrawTile: any = null;
    if (bs.extraState.lastDrawnSeat !== null && bs.extraState.lastDrawnSeat !== undefined && bs.extraState.lastDrawnSeat !== seat) {
      canceledDrawSeat = bs.extraState.lastDrawnSeat;
      const dSeat = canceledDrawSeat;
      const drawnRef = bs.extraState.lastDrawnTile;
      const seatHand = (room as any).deal?.seatCards?.[dSeat];
      if (Array.isArray(seatHand) && seatHand.length % 3 === 2) {
        let returned: any = null;
        if (drawnRef?.id) {
          const idx = seatHand.findIndex((c: any) => c.id === drawnRef.id);
          if (idx >= 0) returned = seatHand.splice(idx, 1)[0];
        }
        if (!returned) returned = seatHand.pop();
        if (returned && Array.isArray((room as any).deal.remainingWall)) {
          (room as any).deal.remainingWall.unshift(returned);
          canceledDrawTile = returned;
        }
      }
      if (Array.isArray((room as any).deal?.remainingWall)) {
        bs.extraState.wallRemaining = (room as any).deal.remainingWall.length;
      } else if (typeof bs.extraState.wallRemaining === 'number') {
        bs.extraState.wallRemaining++;
      }
      if (bs.handsCount && bs.handsCount[dSeat] > 0) {
        bs.handsCount[dSeat] = (room as any).deal?.seatCards?.[dSeat]?.length ?? Math.max(0, bs.handsCount[dSeat] - 1);
      }
      bs.extraState.lastDrawnSeat = null;
      bs.extraState.lastDrawnTile = null;
    }

    let claimedTile = lastDisc;
    if (bs.extraState.mahjongDiscards.length > 0) {
      const lastIdx = bs.extraState.mahjongDiscards.length - 1;
      if (bs.extraState.mahjongDiscards[lastIdx].id === lastDisc.id) {
        claimedTile = bs.extraState.mahjongDiscards.pop();
      } else {
        const fIdx = bs.extraState.mahjongDiscards.findIndex((d: any) => d.id === lastDisc.id);
        if (fIdx >= 0) claimedTile = bs.extraState.mahjongDiscards.splice(fIdx, 1)[0];
      }
    }
    const removedHandTiles: any[] = [];
    if (Array.isArray(sCards)) {
      for (let i = sCards.length - 1; i >= 0 && removedHandTiles.length < 2; i--) {
        if (isTileMatch(sCards[i], lastDisc)) {
          removedHandTiles.push(sCards.splice(i, 1)[0]);
        }
      }
    }
    const pengMeldTiles = [...removedHandTiles, claimedTile];
    if (!bs.extraState.mahjongMelds[seat]) bs.extraState.mahjongMelds[seat] = [];
    bs.extraState.mahjongMelds[seat].push({
      type: 'peng',
      tile: claimedTile,
      tiles: pengMeldTiles,
      seat,
    });
    bs.extraState.lastDiscarded = null;
    bs.extraState.pendingPengSeat = seat;
    bs.turnSeat = seat;
    room.turnSeat = bs.turnSeat;
    room.moveCount = (room.moveCount || 0) + 1;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.updatedAt = Date.now();
    room.lastTurnTs = Date.now();
    if (Array.isArray(sCards)) {
      bs.handsCount[seat] = sCards.length;
    } else if (bs.handsCount && bs.handsCount[seat] >= 2) {
      bs.handsCount[seat] -= 2;
    }
    p.tile = claimedTile;
    p.meldTiles = pengMeldTiles;
    p.wallRemaining = bs.extraState.wallRemaining;
    if (canceledDrawSeat !== null) {
      p.canceledDrawSeat = canceledDrawSeat;
      if (canceledDrawTile) p.canceledDrawTile = canceledDrawTile;
    }
  } else if (action === 'gang') {
    const seatMelds = bs.extraState.mahjongMelds[seat] || [];
    const sCards: any[] | undefined = (room as any).deal?.seatCards?.[seat];
    const lastDisc = bs.extraState.lastDiscarded;
    let resolvedGangType: 'ming' | 'an' | 'bu' = 'ming';
    if (p.gangType === 'bu') {
      resolvedGangType = 'bu';
    } else if (p.gangType === 'an') {
      resolvedGangType = 'an';
    } else if (p.gangType === 'an_or_bu') {
      const hasMatchingPeng = Boolean(
        tile && seatMelds.some((m: any) => m.type === 'peng' && m.tile && isTileMatch(m.tile, tile))
      );
      resolvedGangType = hasMatchingPeng ? 'bu' : 'an';
    } else if (!p.isMingGang && p.fromSeat === undefined && !(lastDisc && lastDisc.discardBy !== seat)) {
      const hasMatchingPeng = Boolean(
        tile && seatMelds.some((m: any) => m.type === 'peng' && m.tile && isTileMatch(m.tile, tile))
      );
      resolvedGangType = hasMatchingPeng ? 'bu' : 'an';
    }
    p.gangType = resolvedGangType;

    const wallForGang = (room as any).deal?.remainingWall;
    if (!Array.isArray(wallForGang) || wallForGang.length === 0) {
      sendMahjongReject('claim_rejected', 'wall_empty_no_gang', { claimType: 'gang', gangType: resolvedGangType, tile });
      return;
    }

    if (resolvedGangType === 'ming') {
      if (
        !lastDisc ||
        (bs.extraState.pendingPengSeat !== null && bs.extraState.pendingPengSeat !== undefined) ||
        typeof lastDisc.discardBy !== 'number' ||
        lastDisc.discardBy < 0 ||
        lastDisc.discardBy >= 4 ||
        lastDisc.discardBy === seat ||
        !tile ||
        !isTileMatch(tile, lastDisc) ||
        (p.targetDiscardId && lastDisc.id !== p.targetDiscardId) ||
        (isHongzhongMode && isRedDragonTile(lastDisc))
      ) {
        sendMahjongReject('claim_rejected', 'discard_not_available', { claimType: 'gang', gangType: 'ming', tile });
        return;
      }
      if (Array.isArray(sCards)) {
        const matchCount = sCards.filter((c: any) => isTileMatch(c, lastDisc)).length;
        if (sCards.length % 3 !== 1 || matchCount < 3) {
          sendMahjongReject('claim_rejected', 'insufficient_matching_tiles', { claimType: 'gang', gangType: 'ming', tile });
          return;
        }
      }
    } else if (resolvedGangType === 'an') {
      if (
        bs.turnSeat !== seat ||
        (bs.extraState.pendingPengSeat !== null &&
          bs.extraState.pendingPengSeat !== undefined &&
          bs.extraState.pendingPengSeat !== seat) ||
        !tile ||
        (isHongzhongMode && isRedDragonTile(tile))
      ) {
        sendMahjongReject('claim_rejected', 'invalid_an_gang', { claimType: 'gang', gangType: 'an', tile });
        return;
      }
      if (Array.isArray(sCards)) {
        const matchCount = sCards.filter((c: any) => isTileMatch(c, tile)).length;
        if (sCards.length % 3 !== 2 || matchCount !== 4) {
          sendMahjongReject('claim_rejected', 'insufficient_matching_tiles', { claimType: 'gang', gangType: 'an', tile });
          return;
        }
      }
    } else if (resolvedGangType === 'bu') {
      const hasMatchingPeng = Boolean(
        tile && seatMelds.some((m: any) => m.type === 'peng' && m.tile && isTileMatch(m.tile, tile))
      );
      if (
        bs.turnSeat !== seat ||
        (bs.extraState.pendingPengSeat !== null &&
          bs.extraState.pendingPengSeat !== undefined &&
          bs.extraState.pendingPengSeat !== seat) ||
        !tile ||
        !hasMatchingPeng
      ) {
        sendMahjongReject('claim_rejected', 'invalid_bu_gang', { claimType: 'gang', gangType: 'bu', tile });
        return;
      }
      if (Array.isArray(sCards)) {
        const matchCount = sCards.filter((c: any) => isTileMatch(c, tile)).length;
        if (sCards.length % 3 !== 2 || matchCount < 1) {
          sendMahjongReject('claim_rejected', 'insufficient_matching_tiles', { claimType: 'gang', gangType: 'bu', tile });
          return;
        }
      }
    }

    bs.extraState.claimWindow = null;

    let canceledDrawSeat: number | null = null;
    let canceledDrawTile: any = null;
    if (resolvedGangType === 'ming') {
      if (bs.extraState.lastDrawnSeat !== null && bs.extraState.lastDrawnSeat !== undefined && bs.extraState.lastDrawnSeat !== seat) {
        canceledDrawSeat = bs.extraState.lastDrawnSeat;
        const dSeat = canceledDrawSeat;
        const drawnRef = bs.extraState.lastDrawnTile;
        const seatHand = (room as any).deal?.seatCards?.[dSeat];
        if (Array.isArray(seatHand) && seatHand.length % 3 === 2) {
          let returned: any = null;
          if (drawnRef?.id) {
            const idx = seatHand.findIndex((c: any) => c.id === drawnRef.id);
            if (idx >= 0) returned = seatHand.splice(idx, 1)[0];
          }
          if (!returned) returned = seatHand.pop();
          if (returned && Array.isArray((room as any).deal.remainingWall)) {
            (room as any).deal.remainingWall.unshift(returned);
            canceledDrawTile = returned;
          }
        }
        if (Array.isArray((room as any).deal?.remainingWall)) {
          bs.extraState.wallRemaining = (room as any).deal.remainingWall.length;
        } else if (typeof bs.extraState.wallRemaining === 'number') {
          bs.extraState.wallRemaining++;
        }
        if (bs.handsCount && bs.handsCount[dSeat] > 0) {
          bs.handsCount[dSeat] = (room as any).deal?.seatCards?.[dSeat]?.length ?? Math.max(0, bs.handsCount[dSeat] - 1);
        }
        bs.extraState.lastDrawnSeat = null;
        bs.extraState.lastDrawnTile = null;
      }
    }

    const targetMatchTile = resolvedGangType === 'ming' ? lastDisc : tile;
    const removeNeeded = resolvedGangType === 'bu' ? 1 : resolvedGangType === 'an' ? 4 : 3;
    const removedHandTiles: any[] = [];
    if (Array.isArray(sCards)) {
      for (let i = sCards.length - 1; i >= 0 && removedHandTiles.length < removeNeeded; i--) {
        if (isTileMatch(sCards[i], targetMatchTile)) {
          removedHandTiles.push(sCards.splice(i, 1)[0]);
        }
      }
    }
    if (Array.isArray(sCards)) {
      bs.handsCount[seat] = sCards.length;
    } else if (bs.handsCount && typeof bs.handsCount[seat] === 'number') {
      bs.handsCount[seat] = Math.max(0, bs.handsCount[seat] - removeNeeded);
    }

    if (!bs.extraState.mahjongMelds[seat]) bs.extraState.mahjongMelds[seat] = [];
    if (resolvedGangType === 'bu') {
      const pengMeldIdx = bs.extraState.mahjongMelds[seat].findIndex(
        (m: any) => m.type === 'peng' && m.tile && isTileMatch(m.tile, targetMatchTile)
      );
      const addedTile = removedHandTiles[0] || targetMatchTile;
      if (pengMeldIdx >= 0) {
        const prevMeld = bs.extraState.mahjongMelds[seat][pengMeldIdx];
        const prevTiles = Array.isArray(prevMeld.tiles) ? prevMeld.tiles : [prevMeld.tile, prevMeld.tile, prevMeld.tile];
        const fullGangTiles = [...prevTiles, addedTile];
        bs.extraState.mahjongMelds[seat][pengMeldIdx] = {
          type: 'gang',
          tile: addedTile,
          tiles: fullGangTiles,
          seat,
          gangType: 'bu',
        };
        p.meldTiles = fullGangTiles;
      } else {
        const fallbackTiles = Array.from({ length: 4 }, (_, idx) => ({ ...addedTile, id: `${addedTile.id}-bu-${idx}` }));
        bs.extraState.mahjongMelds[seat].push({ type: 'gang', tile: addedTile, tiles: fallbackTiles, seat, gangType: 'bu' });
        p.meldTiles = fallbackTiles;
      }
      bs.extraState.lastDiscarded = null;
    } else {
      let claimedTile = targetMatchTile;
      if (resolvedGangType === 'ming') {
        if (bs.extraState.mahjongDiscards.length > 0 && lastDisc) {
          const lastIdx = bs.extraState.mahjongDiscards.length - 1;
          if (bs.extraState.mahjongDiscards[lastIdx].id === lastDisc.id) {
            claimedTile = bs.extraState.mahjongDiscards.pop();
          } else {
            const fIdx = bs.extraState.mahjongDiscards.findIndex((d: any) => d.id === lastDisc.id);
            if (fIdx >= 0) claimedTile = bs.extraState.mahjongDiscards.splice(fIdx, 1)[0];
          }
        }
        bs.extraState.lastDiscarded = null;
      } else {
        bs.extraState.lastDiscarded = null;
      }
      const meldTiles = resolvedGangType === 'ming' ? [...removedHandTiles, claimedTile] : [...removedHandTiles];
      bs.extraState.mahjongMelds[seat].push({ type: 'gang', tile: claimedTile, tiles: meldTiles, seat, gangType: resolvedGangType });
      p.tile = claimedTile;
      p.meldTiles = meldTiles;
    }

    if ((room as any).deal?.remainingWall?.length > 0) {
      const supp = (room as any).deal.remainingWall.pop();
      if (supp && (room as any).deal.seatCards?.[seat]) {
        (room as any).deal.seatCards[seat].push(supp);
      }
      if (supp && bs.handsCount && typeof bs.handsCount[seat] === 'number') {
        bs.handsCount[seat] = Array.isArray(sCards) ? sCards.length : bs.handsCount[seat] + 1;
      }
      p.suppTile = supp || null;
      bs.extraState.lastDrawnSeat = seat;
      bs.extraState.lastDrawnTile = supp || null;
    }
    if (Array.isArray((room as any).deal?.remainingWall)) {
      bs.extraState.wallRemaining = (room as any).deal.remainingWall.length;
    } else if (bs.extraState.wallRemaining > 0) {
      bs.extraState.wallRemaining--;
    }
    p.wallRemaining = bs.extraState.wallRemaining;
    if (!Array.isArray(bs.seatKongScores) || bs.seatKongScores.length !== 4) {
      bs.seatKongScores = [0, 0, 0, 0];
    }
    if (!Array.isArray(bs.extraState.kongHistory)) {
      bs.extraState.kongHistory = [];
    }
    const targetOpp =
      resolvedGangType === 'ming'
        ? lastDisc.discardBy
        : undefined;
    const kongDeltas: [number, number, number, number] = [0, 0, 0, 0];
    if (resolvedGangType === 'ming' && typeof targetOpp === 'number' && targetOpp >= 0 && targetOpp < 4 && targetOpp !== seat) {
      p.fromSeat = targetOpp;
      kongDeltas[seat] = 10;
      kongDeltas[targetOpp] = -10;
      bs.seatKongScores[seat] += 10;
      bs.seatKongScores[targetOpp] -= 10;
    } else {
      for (let k = 0; k < 4; k++) {
        if (k !== seat) {
          kongDeltas[k] = -10;
          bs.seatKongScores[k] -= 10;
        }
      }
      kongDeltas[seat] = 30;
      bs.seatKongScores[seat] += 30;
    }
    bs.extraState.kongHistory.push({
      gangType: resolvedGangType,
      seat,
      fromSeat: resolvedGangType === 'ming' ? targetOpp : null,
      tile: p.tile || targetMatchTile,
      deltas: kongDeltas,
    });
    p.seatKongScores = [...bs.seatKongScores];
    p.kongHistory = [...bs.extraState.kongHistory];
    bs.turnSeat = seat;
    room.turnSeat = bs.turnSeat;
    room.moveCount = (room.moveCount || 0) + 1;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.updatedAt = Date.now();
    room.lastTurnTs = Date.now();
    bs.extraState.pendingPengSeat = seat;
    if (canceledDrawSeat !== null) {
      p.canceledDrawSeat = canceledDrawSeat;
      if (canceledDrawTile) p.canceledDrawTile = canceledDrawTile;
    }
  } else if (action === 'pass') {
    const cw = bs.extraState.claimWindow;
    if (cw && (!p.discardId || p.discardId === cw.discardId)) {
      if (!cw.passedSeats.includes(seat)) {
        cw.passedSeats.push(seat);
      }
      const stillPending = cw.pendingSeats.filter((ps: number) => !cw.passedSeats.includes(ps));
      if (stillPending.length === 0) {
        const nextTurnSeat = (cw.discarderSeat + 1) % 4;
        const resolvedDiscardId = cw.discardId;
        bs.extraState.claimWindow = null;
        bs.turnSeat = nextTurnSeat;
        room.turnSeat = nextTurnSeat;
        room.moveCount = (room.moveCount || 0) + 1;
        bs.actionSeq = (bs.actionSeq || 0) + 1;
        bs.moveCount = room.moveCount;
        bs.updatedAt = Date.now();
        room.lastTurnTs = Date.now();

        const wallArr = (room as any).deal?.remainingWall;
        if (Array.isArray(wallArr) && wallArr.length === 0) {
          executeMahjongDrawStalemate(ctx, room, nextTurnSeat, '牌墙抓毕，本局荒庄（流局）！');
          return;
        }
        const crPayload = {
          action: 'claim_resolved',
          discardId: resolvedDiscardId,
          nextTurn: nextTurnSeat,
          wallRemaining: wallArr?.length ?? bs.extraState.wallRemaining,
        };
        ctx.sendSanitizedToRoom(room, (rSeat) => ({
          type: 'game_event',
          roomId: room.id,
          seat: nextTurnSeat,
          event: crPayload,
          payload: crPayload,
          moveCount: room.moveCount,
          boardState: sanitizeBoardStateForSeat(bs, 'mahjong', rSeat),
        }));
      }
      return;
    }
    if (p.nextTurn !== undefined && !cw) {
      bs.turnSeat = Number(p.nextTurn);
      room.turnSeat = bs.turnSeat;
      room.moveCount = (room.moveCount || 0) + 1;
      bs.actionSeq = (bs.actionSeq || 0) + 1;
      bs.moveCount = room.moveCount;
      bs.updatedAt = Date.now();
    } else {
      return;
    }
  } else if (action === 'hu') {
    const sCards: any[] = (room as any).deal?.seatCards?.[seat] || [];
    const seatMelds = bs.extraState.mahjongMelds?.[seat] || [];
    const expectedTotal = sCards.length + seatMelds.length * 3;
    const hzCount = isHongzhongMode ? sCards.filter(isRedDragonTile).length : 0;
    const isFourHzAuth = isHongzhongMode && hzCount >= 4;
    const winRes = sCards.length % 3 === 2 ? checkMahjongWin(sCards as any, isHongzhongMode) : { win: false, desc: '', fan: 0 };

    if (expectedTotal !== 14 || sCards.length % 3 !== 2 || (!isFourHzAuth && !winRes.win)) {
      console.warn(
        `🛡️ [服务端防炸胡拦截] 席位 ${seat} 请求胡牌但手牌校验未通过 (手牌=${sCards.length}, 副露=${seatMelds.length}, 总计=${expectedTotal}, fourHz=${isFourHzAuth}, win=${winRes.win})`
      );
      sendMahjongReject('hu_rejected', 'invalid_hu_hand');
      return;
    }
    if (bs.turnSeat !== seat && bs.extraState.pendingPengSeat !== seat) {
      sendMahjongReject('hu_rejected', 'not_your_turn');
      return;
    }

    room.moveCount = (room.moveCount || 0) + 1;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.updatedAt = Date.now();
    executeMahjongWinSettlement(ctx, room, seat, isFourHzAuth, p);
    return;
  } else if (action === 'draw_stalemate') {
    const wallArr = (room as any).deal?.remainingWall;
    if (Array.isArray(wallArr) && wallArr.length > 0) {
      sendMahjongReject('draw_rejected', 'wall_not_empty');
      return;
    }
    executeMahjongDrawStalemate(ctx, room, seat, '牌墙抓毕，本局荒庄（流局）！');
    return;
  } else {
    room.moveCount = (room.moveCount || 0) + 1;
    bs.actionSeq = (bs.actionSeq || 0) + 1;
    bs.moveCount = room.moveCount;
    bs.updatedAt = Date.now();
    room.lastTurnTs = Date.now();
    if (p.multiplier) bs.multiplier = Number(p.multiplier);
    if (p.nextTurn !== undefined) {
      bs.turnSeat = Number(p.nextTurn);
      room.turnSeat = bs.turnSeat;
    }
    if (combo) {
      bs.lastCombo = combo;
      if (!bs.discardsHistory) bs.discardsHistory = [];
      bs.discardsHistory.push(combo);
      if (bs.discardsHistory.length > 100) bs.discardsHistory = bs.discardsHistory.slice(-100);
    }
  }

  const resolvedNextTurn = p.nextTurn !== undefined ? Number(p.nextTurn) : bs.turnSeat;
  ctx.sendSanitizedToRoom(room, (rSeat) => {
    const maskedEvt: any = {
      action,
      seat,
      combo,
      nextTurn: resolvedNextTurn,
      winner: bs.winnerSeat ?? p.winner,
      finishedSeats: bs.finishedSeats || p.finishedSeats,
      allianceScoreResult: bs.allianceScoreResult || p.allianceScoreResult,
      ...p,
    };
    if (action === 'draw' && rSeat !== seat && maskedEvt.tile) {
      maskedEvt.tile = maskHiddenMahjongTile(maskedEvt.tile);
    }
    if (action === 'gang' && rSeat !== seat && maskedEvt.suppTile) {
      maskedEvt.suppTile = maskHiddenMahjongTile(maskedEvt.suppTile);
    }
    if (maskedEvt.canceledDrawTile && rSeat !== maskedEvt.canceledDrawSeat) {
      maskedEvt.canceledDrawTile = maskHiddenMahjongTile(maskedEvt.canceledDrawTile);
    }
    return {
      type: 'game_event',
      roomId: room.id,
      seat,
      event: maskedEvt,
      payload: maskedEvt,
      moveCount: room.moveCount,
      boardState: sanitizeBoardStateForSeat(bs, 'mahjong', rSeat),
    };
  });
}

export function tickMahjongClaimWindow(ctx: RealtimeHubContext, room: any, now: number): boolean {
  if (room.gameType !== 'mahjong' || !room.boardState?.extraState?.claimWindow) {
    return false;
  }
  const cw = room.boardState.extraState.claimWindow;
  if (now - cw.createdAt > 10500) {
    const nextTurnSeat = (cw.discarderSeat + 1) % 4;
    const resolvedDiscardId = cw.discardId;
    room.boardState.extraState.claimWindow = null;
    room.boardState.turnSeat = nextTurnSeat;
    room.turnSeat = nextTurnSeat;
    room.lastTurnTs = now;
    room.moveCount = (room.moveCount || 0) + 1;
    room.boardState.actionSeq = (room.boardState.actionSeq || 0) + 1;
    room.boardState.moveCount = room.moveCount;
    room.boardState.updatedAt = now;

    const wallArr = (room as any).deal?.remainingWall;
    if (Array.isArray(wallArr) && wallArr.length === 0) {
      executeMahjongDrawStalemate(ctx, room, nextTurnSeat, '牌墙抓毕，本局荒庄（流局）！');
    } else {
      const crPayload = {
        action: 'claim_resolved',
        discardId: resolvedDiscardId,
        nextTurn: nextTurnSeat,
        wallRemaining: wallArr?.length ?? room.boardState.extraState.wallRemaining,
        timeout: true,
      };
      ctx.sendSanitizedToRoom(room, (rSeat) => ({
        type: 'game_event',
        roomId: room.id,
        seat: nextTurnSeat,
        event: crPayload,
        payload: crPayload,
        moveCount: room.moveCount,
        boardState: sanitizeBoardStateForSeat(room.boardState, 'mahjong', rSeat),
      }));
    }
  }
  return true;
}

export function tickMahjongAutoTurn(ctx: RealtimeHubContext, room: any, currentSeat: number, now: number): void {
  let deal = (room as any).deal;
  const isHongzhongMode = room.sceneMode?.includes('hongzhong') ?? true;

  if (!deal || !deal.seatCards) {
    deal = generateRoomDeal(room.gameType, room.sceneMode, 4);
    (room as any).deal = deal;
  }
  if (!deal.seatCards[currentSeat]) {
    deal.seatCards[currentSeat] = [];
  }

  const hand: any[] = deal.seatCards[currentSeat];
  const wall: any[] = deal.remainingWall || [];
  const melds = room.boardState.extraState?.mahjongMelds?.[currentSeat] || [];

  const totalTiles = hand.length + melds.length * 3;
  let justDrawnTile: any = null;
  if (totalTiles % 3 === 1) {
    if (wall.length === 0) {
      executeMahjongDrawStalemate(ctx, room, currentSeat, '牌墙已摸空，荒牌流局！');
      return;
    }

    const drawnTile = wall.shift()!;
    justDrawnTile = drawnTile;
    hand.push(drawnTile);
    if (!room.boardState.extraState) room.boardState.extraState = {};
    room.boardState.extraState.wallRemaining = wall.length;
    room.boardState.extraState.lastDrawnSeat = currentSeat;
    room.boardState.extraState.lastDrawnTile = drawnTile;
    room.boardState.actionSeq = (room.boardState.actionSeq || 0) + 1;
    if (room.boardState.handsCount) room.boardState.handsCount[currentSeat] = hand.length;

    ctx.sendSanitizedToRoom(room, (rSeat) => {
      const tileForSeat = rSeat === currentSeat ? drawnTile : maskHiddenMahjongTile(drawnTile);
      const drawEvt = {
        action: 'draw',
        seat: currentSeat,
        absSeat: currentSeat,
        tile: tileForSeat,
        wallRemaining: wall.length,
        autoDraw: true,
        moveCount: room.moveCount,
      };
      return {
        type: 'game_event',
        roomId: room.id,
        seat: currentSeat,
        moveCount: room.moveCount,
        event: drawEvt,
        payload: drawEvt,
        boardState: sanitizeBoardStateForSeat(room.boardState, 'mahjong', rSeat),
      };
    });
  }

  const redDragonCount = isHongzhongMode
    ? hand.filter((t: any) => t.type === 'zi' && (t.value === 5 || t.text === '红中' || t.isRedDragon)).length
    : 0;
  const isFourHz = isHongzhongMode && redDragonCount >= 4;
  const winRes =
    hand.length % 3 === 2 && hand.length + melds.length * 3 === 14
      ? checkMahjongWin(hand as any, isHongzhongMode)
      : { win: false, desc: '', fan: 0 };

  if (hand.length % 3 === 2 && hand.length + melds.length * 3 === 14 && (isFourHz || winRes.win)) {
    executeMahjongWinSettlement(ctx, room, currentSeat, isFourHz, justDrawnTile ? { tile: justDrawnTile } : {});
    return;
  }

  let discardIdx = 0;
  const nonHzTiles = isHongzhongMode
    ? hand
        .map((t, idx) => ({ t, idx }))
        .filter((item) => !(item.t.type === 'zi' && (item.t.value === 5 || item.t.text === '红中' || item.t.isRedDragon)))
    : hand.map((t, idx) => ({ t, idx }));

  if (nonHzTiles.length > 0) {
    const ziSingles = nonHzTiles.filter((item) => {
      if (item.t.type !== 'zi') return false;
      return hand.filter((x) => x.type === 'zi' && x.value === item.t.value).length === 1;
    });
    if (ziSingles.length > 0) {
      discardIdx = ziSingles[0].idx;
    } else {
      const isolatedNum = nonHzTiles.filter((item) => {
        if (item.t.type === 'zi') return false;
        const hasSame = hand.filter((x) => x.type === item.t.type && x.value === item.t.value).length > 1;
        const hasNeighbour = hand.some(
          (x) => x.type === item.t.type && Math.abs(x.value - item.t.value) <= 2 && x.id !== item.t.id
        );
        return !hasSame && !hasNeighbour;
      });
      discardIdx = isolatedNum.length > 0 ? isolatedNum[0].idx : nonHzTiles[nonHzTiles.length - 1].idx;
    }
  }

  const [discardedTile] = hand.splice(discardIdx >= 0 && discardIdx < hand.length ? discardIdx : Math.max(0, hand.length - 1), 1);
  const nextSeat = (currentSeat + 1) % 4;
  room.turnSeat = nextSeat;
  room.boardState.turnSeat = nextSeat;
  room.boardState.updatedAt = now;
  room.moveCount = (room.moveCount || 0) + 1;
  room.boardState.actionSeq = (room.boardState.actionSeq || 0) + 1;
  room.boardState.moveCount = room.moveCount;
  room.lastTurnTs = now;

  if (!room.boardState.extraState) room.boardState.extraState = {};
  if (!room.boardState.extraState.mahjongDiscards) room.boardState.extraState.mahjongDiscards = [];

  const discTile = { ...discardedTile, discardBy: currentSeat };
  room.boardState.extraState.mahjongDiscards.push(discTile);
  room.boardState.extraState.lastDiscarded = discTile;
  room.boardState.extraState.pendingPengSeat = null;
  room.boardState.extraState.lastDrawnSeat = null;
  room.boardState.extraState.lastDrawnTile = null;
  room.boardState.extraState.wallRemaining = wall.length;

  const pendingClaimSeats = computeMahjongClaimPendingSeats(room, room.boardState, currentSeat, discTile);
  if (pendingClaimSeats.length > 0) {
    room.boardState.extraState.claimWindow = {
      discardId: discTile.id,
      discarderSeat: currentSeat,
      pendingSeats: pendingClaimSeats,
      passedSeats: [],
      createdAt: now,
    };
  } else {
    room.boardState.extraState.claimWindow = null;
  }

  if (room.boardState.handsCount) {
    room.boardState.handsCount[currentSeat] = hand.length;
  }

  const autoDiscEvt = {
    action: 'discard',
    seat: currentSeat,
    absSeat: currentSeat,
    tile: discTile,
    autoDiscard: true,
    nextTurn: nextSeat,
    wallRemaining: wall.length,
    pendingClaimSeats,
    hasClaimWindow: pendingClaimSeats.length > 0,
    reason: 'trustee_auto_discard',
  };
  ctx.sendSanitizedToRoom(room, (rSeat) => ({
    type: 'game_event',
    roomId: room.id,
    seat: currentSeat,
    event: autoDiscEvt,
    payload: autoDiscEvt,
    moveCount: room.moveCount,
    boardState: sanitizeBoardStateForSeat(room.boardState, 'mahjong', rSeat),
  }));

  if (pendingClaimSeats.length === 0) {
    if (wall.length === 0) {
      executeMahjongDrawStalemate(ctx, room, nextSeat, '牌墙已摸空，荒牌流局！');
    } else {
      room.boardState.actionSeq = (room.boardState.actionSeq || 0) + 1;
      const crPayload = {
        action: 'claim_resolved',
        discardId: discTile?.id || null,
        nextTurn: nextSeat,
        wallRemaining: wall.length,
      };
      ctx.sendSanitizedToRoom(room, (rSeat) => ({
        type: 'game_event',
        roomId: room.id,
        seat: nextSeat,
        event: crPayload,
        payload: crPayload,
        moveCount: room.moveCount,
        boardState: sanitizeBoardStateForSeat(room.boardState, 'mahjong', rSeat),
      }));
    }
  }
}
