import type { YansanCard } from '../engine/yansanRules.ts';

export function sanitizeDealForSeat(gameType: string, deal: any, recipientSeat: number): any {
  if (!deal || typeof deal !== 'object') return deal;
  const copy: any = { ...deal };
  delete copy.deck;
  delete copy.initialSeatCards;

  const maskOpponentHand = (hand: any[], s: number) => {
    if (!Array.isArray(hand)) return [];
    if (s === recipientSeat) return [...hand];
    if (gameType === 'shisanshui') {
      return hand.map((_, idx) => ({
        id: `hidden-${s}-${idx}`,
        rank: 0,
        suit: '?',
        suitVal: 0,
        label: '?',
        isRed: false,
        hidden: true,
      }));
    }
    if (gameType === 'yansan') {
      return hand.map((_, idx) => ({
        id: `hidden-${s}-${idx}`,
        rank: 0,
        suitVal: 0,
        label: '?',
        suit: '?',
        isRed: false,
        hidden: true,
      }));
    }
    // mahjong
    return hand.map((_, idx) => ({
      id: `hidden-${s}-${idx}`,
      type: 'wan',
      value: 1,
      text: '一万',
      color: '#DC2626',
      hidden: true,
    }));
  };

  if (Array.isArray(deal.seatCards)) {
    copy.seatCards = deal.seatCards.map((hand: any[], s: number) => maskOpponentHand(hand, s));
  } else if (deal.seatCards && typeof deal.seatCards === 'object') {
    const maskedObj: Record<number, any[]> = {};
    for (const [seatKey, hand] of Object.entries(deal.seatCards)) {
      const s = Number(seatKey);
      maskedObj[s] = maskOpponentHand(Array.isArray(hand) ? hand : [], s);
    }
    copy.seatCards = maskedObj;
  }

  if (gameType === 'mahjong' && Array.isArray(deal.remainingWall)) {
    copy.remainingWall = deal.remainingWall.map((_: any, idx: number) => ({
      id: `hidden-wall-${idx}`,
      type: 'wan',
      value: 1,
      text: '一万',
      color: '#DC2626',
      hidden: true,
    }));
  }

  return copy;
}

export function maskHiddenMahjongTile(tile: any): any {
  if (!tile) return null;
  return {
    id: tile.id || 'hidden-tile',
    type: 'wan',
    value: 1,
    text: '一万',
    color: '#DC2626',
    hidden: true,
  };
}

export function sanitizeBoardStateForSeat(bs: any, gameType: string, recipientSeat: number): any {
  if (!bs || bs.phase === 'ended') return bs;
  if (gameType === 'shisanshui' && bs.shisanshuiSubmissions && typeof bs.shisanshuiSubmissions === 'object') {
    const maskedSubs: Record<string, any> = {};
    for (const [seatKey, sub] of Object.entries(bs.shisanshuiSubmissions)) {
      const sNum = Number(seatKey);
      if (sNum === recipientSeat) {
        maskedSubs[seatKey] = sub;
      } else {
        maskedSubs[seatKey] = {
          seat: sNum,
          isSpecial: Boolean((sub as any)?.isSpecial),
          autoSubmitted: Boolean((sub as any)?.autoSubmitted),
          submittedAt: (sub as any)?.submittedAt,
          hidden: true,
        };
      }
    }
    return {
      ...bs,
      shisanshuiSubmissions: maskedSubs,
    };
  }
  if (gameType !== 'mahjong' || !bs.extraState) return bs;
  const drawnSeat = bs.extraState.lastDrawnSeat;
  const drawnTile = bs.extraState.lastDrawnTile;
  if (!drawnTile || drawnSeat === recipientSeat) return bs;
  return {
    ...bs,
    extraState: {
      ...bs.extraState,
      lastDrawnTile: maskHiddenMahjongTile(drawnTile),
    },
  };
}

export function createYansanStandardDeck(): YansanCard[] {
  const suits: { suit: '♠' | '♥' | '♣' | '♦'; suitVal: number; isRed: boolean }[] = [
    { suit: '♠', suitVal: 4, isRed: false },
    { suit: '♥', suitVal: 3, isRed: true },
    { suit: '♣', suitVal: 2, isRed: false },
    { suit: '♦', suitVal: 1, isRed: true },
  ];
  const ranks = [
    { rank: 3, label: '3' }, { rank: 4, label: '4' }, { rank: 5, label: '5' },
    { rank: 6, label: '6' }, { rank: 7, label: '7' }, { rank: 8, label: '8' },
    { rank: 9, label: '9' }, { rank: 10, label: '10' }, { rank: 11, label: 'J' },
    { rank: 12, label: 'Q' }, { rank: 13, label: 'K' }, { rank: 14, label: 'A' },
    { rank: 15, label: '2' },
  ];
  const deck: YansanCard[] = [];
  for (const r of ranks) {
    for (const s of suits) {
      deck.push({
        id: `${r.label}-${s.suit}`,
        rank: r.rank,
        suitVal: s.suitVal,
        label: r.label,
        suit: s.suit,
        isRed: s.isRed,
      });
    }
  }
  return deck;
}
