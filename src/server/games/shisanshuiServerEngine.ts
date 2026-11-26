import type { WebSocket } from 'ws';
import {
  autoArrangeThirteen,
  compareTwoPlayers,
  detectSpecialHand,
  evaluate3CardTier,
  evaluate5CardTier,
  isFoulHand,
  type SSSCard,
} from '../../engine/shisanshuiRules.ts';
import { isBotLikePlayer, type RealtimeHubContext } from '../realtimeTypes.ts';
import { sanitizeBoardStateForSeat } from '../sanitizers.ts';

export function settleShisanshuiRound(ctx: RealtimeHubContext, room: any, isTimeout = false) {
  if (!room || !room.boardState) return;
  const bs = room.boardState;
  if (bs.phase === 'ended') return;

  if (!bs.shisanshuiSubmissions) bs.shisanshuiSubmissions = {};
  const totalSeats = room.players?.length || (room.sceneMode?.includes('eight') ? 8 : 4);
  const dealCards: SSSCard[][] = (room as any).deal?.seatCards || [];

  for (let s = 0; s < totalSeats; s++) {
    const sub = bs.shisanshuiSubmissions[s];
    if (!sub || !sub.tiers || !sub.rawTiers) {
      const rawHand = Array.isArray(dealCards[s]) ? dealCards[s] : [];
      const opt = autoArrangeThirteen(rawHand as SSSCard[], 'optimal');
      bs.shisanshuiSubmissions[s] = {
        seat: s,
        tiers: { head: opt.head, mid: opt.mid, tail: opt.tail },
        rawTiers: { front: opt.head, mid: opt.mid, back: opt.tail },
        isSpecial: Boolean(opt.specialHand),
        autoSubmitted: true,
        submittedAt: Date.now(),
      };
    }
  }

  const netWaters: number[] = Array(totalSeats).fill(0);
  const grandSlamSeats: boolean[] = Array(totalSeats).fill(false);
  const perSeatOppResults: Record<number, any[]> = {};

  for (let i = 0; i < totalSeats; i++) {
    const myTiers = bs.shisanshuiSubmissions[i]?.tiers;
    let totalNet = 0;
    let shotCount = 0;
    const oppCount = totalSeats - 1;
    const oppResList: any[] = [];

    for (let j = 0; j < totalSeats; j++) {
      if (i === j) continue;
      const oppTiers = bs.shisanshuiSubmissions[j]?.tiers;
      const oppPlayer = room.players?.find((p: any, idx: number) => (typeof p?.seat === 'number' ? p.seat : idx) === j);
      const oppName = oppPlayer?.name || `牌友${j + 1}`;
      const vsResult = myTiers && oppTiers
        ? compareTwoPlayers(myTiers, oppTiers, j, oppName)
        : {
            oppSeat: j,
            oppName,
            tiers: [],
            isShot: false,
            isFoulOpp: false,
            isFoulMe: false,
            rawWater: 0,
            multiplier: 1,
            finalWater: 0,
            summary: '打和 0 水',
            specialMe: undefined,
            specialOpp: undefined,
          };

      if (vsResult.isShot && vsResult.finalWater > 0) shotCount++;
      totalNet += vsResult.finalWater;
      oppResList.push({
        seat: j,
        name: oppName,
        netWater: vsResult.finalWater,
        rawWater: vsResult.rawWater,
        isShot: vsResult.isShot,
        isFoulOpp: vsResult.isFoulOpp,
        tiers: vsResult.tiers,
        summary: vsResult.summary,
        specialMe: vsResult.specialMe,
        specialOpp: vsResult.specialOpp,
      });
    }

    if (shotCount === oppCount && oppCount > 0) {
      grandSlamSeats[i] = true;
      totalNet *= 2;
      for (const res of oppResList) {
        res.netWater *= 2;
        res.summary += ' · 【触发全垒打再翻倍！】';
        if (Array.isArray(res.tiers)) {
          res.tiers.forEach((t: any) => {
            t.netWater *= 2;
            t.baseWater *= 2;
            t.bonusWater *= 2;
          });
        }
      }
    }

    netWaters[i] = totalNet;
    perSeatOppResults[i] = oppResList;
  }

  const settlementPayload = {
    netWaters,
    grandSlamSeats,
    perSeatOppResults,
  };

  bs.shisanshuiSettlement = settlementPayload;
  bs.phase = 'ended';
  room.status = 'finished';
  bs.actionSeq = (bs.actionSeq || 0) + 1;
  bs.updatedAt = Date.now();

  ctx.settleRoomCoins(
    room,
    netWaters.map((w) => Math.trunc(w)),
    'shisanshui',
    { ratingDeltaBySeat: netWaters.map((w) => Math.round(w * 2.5)) }
  );

  ctx.resetSceneRoomReadyAndCleanDisconnected(room.id);

  const allSubEvt = {
    action: 'shisanshui_all_submitted',
    submissions: bs.shisanshuiSubmissions,
    settlement: settlementPayload,
    timeout: isTimeout || undefined,
  };

  ctx.sendSanitizedToRoom(room, () => ({
    type: 'game_event',
    roomId: room.id,
    event: allSubEvt,
    payload: allSubEvt,
    boardState: bs,
  }));
}

export function handleShisanshuiGameMove(
  ctx: RealtimeHubContext,
  ws: WebSocket,
  room: any,
  seat: number,
  p: any
): void {
  const bs = room.boardState;
  if (bs.shisanshuiSubmissions?.[seat]) {
    return;
  }
  const headArr = p.tiers?.head || p.rawTiers?.front;
  const midArr = p.tiers?.mid || p.rawTiers?.mid;
  const tailArr = p.tiers?.tail || p.rawTiers?.back;
  if (
    !Array.isArray(headArr) ||
    headArr.length !== 3 ||
    !Array.isArray(midArr) ||
    midArr.length !== 5 ||
    !Array.isArray(tailArr) ||
    tailArr.length !== 5
  ) {
    return;
  }
  const submittedIds = [...headArr, ...midArr, ...tailArr].map((c: any) => c?.id).filter(Boolean);
  const uniqueSubmitted = new Set(submittedIds);
  if (uniqueSubmitted.size !== 13) {
    return;
  }
  const authSeatCards: any[] | undefined = (room as any).deal?.seatCards?.[seat];
  const cardMap = new Map<string, any>();
  if (Array.isArray(authSeatCards) && authSeatCards.length === 13) {
    for (const ac of authSeatCards) cardMap.set(ac.id, ac);
    if (!submittedIds.every((id) => cardMap.has(id))) {
      return;
    }
  }
  const reconstruct = (arr: any[]) => arr.map((c: any) => cardMap.get(c.id) || c);
  const suitValMap: Record<string, number> = { '♠': 4, '♥': 3, '♣': 2, '♦': 1 };
  const toSss = (c: any): SSSCard => ({
    id: c.id,
    rank: Number(c.rank),
    suitVal: typeof c.suitVal === 'number' ? c.suitVal : (suitValMap[c.suit] || 1),
    label: c.label,
    suit: c.suit,
    isRed: Boolean(c.isRed),
  });
  const authHeadRaw = reconstruct(headArr);
  const authMidRaw = reconstruct(midArr);
  const authBackRaw = reconstruct(tailArr);
  const sssHead = authHeadRaw.map(toSss);
  const sssMid = authMidRaw.map(toSss);
  const sssBack = authBackRaw.map(toSss);
  const serverSpecial = detectSpecialHand([...sssHead, ...sssMid, ...sssBack]);
  if (!serverSpecial && isFoulHand(sssHead, sssMid, sssBack)) {
    ws.send(
      JSON.stringify({
        type: 'game_event',
        roomId: room.id,
        seat,
        event: {
          action: 'shisanshui_submit_rejected',
          reason: 'foul_hand',
          message: '当前摆牌倒水相公（尾道需 >= 中道 >= 头道），请调整后提交',
        },
        boardState: bs,
      })
    );
    return;
  }

  if (!bs.shisanshuiSubmissions) {
    bs.shisanshuiSubmissions = {};
  }
  bs.shisanshuiSubmissions[seat] = {
    seat,
    tiers: { head: sssHead, mid: sssMid, tail: sssBack },
    rawTiers: {
      front: authHeadRaw,
      mid: authMidRaw,
      back: authBackRaw,
      frontLabel: evaluate3CardTier(sssHead).name,
      midLabel: evaluate5CardTier(sssMid).name,
      backLabel: evaluate5CardTier(sssBack).name,
      specialHand: serverSpecial,
    },
    isSpecial: Boolean(serverSpecial),
    submittedAt: Date.now(),
  };
  const submittedCount = Object.keys(bs.shisanshuiSubmissions).length;
  const targetCount = room.players?.length || (room.sceneMode?.includes('eight') ? 8 : 4);
  bs.actionSeq = (bs.actionSeq || 0) + 1;
  bs.updatedAt = Date.now();

  ctx.sendSanitizedToRoom(room, (rSeat) => ({
    type: 'game_event',
    roomId: room.id,
    seat,
    event: {
      action: 'shisanshui_submit_hand',
      seat,
      submittedCount,
      targetCount,
      isSpecial: Boolean(serverSpecial),
    },
    payload: {
      action: 'shisanshui_submit_hand',
      seat,
      submittedCount,
      targetCount,
      isSpecial: Boolean(serverSpecial),
    },
    boardState: sanitizeBoardStateForSeat(bs, 'shisanshui', rSeat),
  }));

  if (submittedCount >= targetCount) {
    settleShisanshuiRound(ctx, room, false);
  }
}

export function tickShisanshuiRoom(ctx: RealtimeHubContext, room: any, now: number): void {
  const subs = room.boardState.shisanshuiSubmissions || {};
  const totalP = room.players?.length || (room.sceneMode?.includes('eight') ? 8 : 4);
  const submittedCount = Object.keys(subs).length;
  let onlyAutoRemaining = submittedCount > 0 && submittedCount < totalP;
  if (onlyAutoRemaining) {
    for (let s = 0; s < totalP; s++) {
      if (subs[s]) continue;
      const pl = room.players?.find((x: any, idx: number) => (typeof x?.seat === 'number' ? x.seat : idx) === s);
      if (pl && !isBotLikePlayer(pl) && !pl.isDisconnected && !pl.isAutoManaged) {
        onlyAutoRemaining = false;
        break;
      }
    }
  }
  if (onlyAutoRemaining || (room.lastTurnTs && now - room.lastTurnTs > 60000)) {
    room.lastTurnTs = now;
    settleShisanshuiRound(ctx, room, !onlyAutoRemaining);
  }
}
