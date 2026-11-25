import { TableTheme } from '../../../utils/theme';
import { renderMahjongTile, renderMahjongTileBack } from '../../../utils/mahjongAssets';
import type { MahjongTile } from '../../../games/mahjong/aiWinEngine';

export interface MahjongRenderState {
  tableTheme?: TableTheme;
  dealingPhase: 'rolling_dice' | 'dealing_tiles' | 'sorting_tiles' | 'playing' | 'settled';
  turnPlayer: number;
  dealerSeat: number;
  wallCount: number;
  isClaiming: boolean;
  claimCountdown: number;
  turnCountdown: number;
  myHand: MahjongTile[];
  myHandDealtCount: number;
  oppHands: MahjongTile[][];
  oppHandsDealtCount: number[];
  oppMelds: MahjongTile[][][];
  meldedTiles: MahjongTile[][];
  discards: MahjongTile[];
  selectedIndex: number | null;
  handFlipProgress: number;
  roomPlayers?: any[];
  relToAbsSeat: (relSeat: number) => number;
  isSeatAutoManaged: (absSeat: number) => boolean;
  isSeatDisconnected: (absSeat: number) => boolean;
  ensureOpponentHandSize: (botIdx: number) => void;
}

export function renderMahjongTableFrame(canvas: HTMLCanvasElement, state: MahjongRenderState) {
  const ctx = canvas.getContext('2d');
  if (!ctx) return;

  const dpr = window.devicePixelRatio || 1;
  const width = 680;
  const height = 430;
  const targetW = Math.round(width * dpr);
  const targetH = Math.round(height * dpr);
  if (canvas.width !== targetW || canvas.height !== targetH) {
    canvas.width = targetW;
    canvas.height = targetH;
  }
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, width, height);

  const highlight = state.tableTheme?.highlightColor || '#FFFFFF';
  const felt = state.tableTheme?.feltColor || '#ECEFF1';
  const edge = state.tableTheme?.edgeColor || '#CFD8DC';

  const grad = ctx.createRadialGradient(width / 2, height / 2 - 20, 20, width / 2, height / 2, 430);
  grad.addColorStop(0, highlight);
  grad.addColorStop(0.35, felt);
  grad.addColorStop(0.85, edge);
  grad.addColorStop(1, edge);
  ctx.fillStyle = grad;
  ctx.fillRect(0, 0, width, height);

  ctx.save();
  ctx.strokeStyle = 'rgba(255, 255, 255, 0.16)';
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  ctx.roundRect(0, 0, width, height, 0);
  ctx.stroke();

  const spotGrad = ctx.createRadialGradient(width / 2, height / 2 - 20, 0, width / 2, height / 2 - 20, 260);
  spotGrad.addColorStop(0, 'rgba(255, 255, 255, 0.16)');
  spotGrad.addColorStop(0.5, 'rgba(255, 255, 255, 0.06)');
  spotGrad.addColorStop(1, 'rgba(255, 255, 255, 0)');
  ctx.fillStyle = spotGrad;
  ctx.fillRect(0, 0, width, height);
  ctx.restore();

  const drawOvalOpponentAvatar = (
    cx: number,
    cy: number,
    rx: number,
    ry: number,
    avatarEmoji: string,
    seatName: string,
    count: number,
    color: string,
    isActive: boolean,
    orientation: 'horizontal' | 'vertical',
    isDealer = false,
    _countdownSec?: number,
    isAuto = false,
    isDisc = false
  ) => {
    ctx.save();

    if (isActive) {
      ctx.shadowColor = '#EF4444';
      ctx.shadowBlur = 14;
    } else {
      ctx.shadowColor = 'rgba(0, 0, 0, 0.45)';
      ctx.shadowBlur = 6;
      ctx.shadowOffsetY = 2;
    }

    const avatarGrad = ctx.createRadialGradient(cx, cy - ry * 0.25, 4, cx, cy, Math.max(rx, ry));
    if (color === '#3B82F6') {
      avatarGrad.addColorStop(0, isActive ? '#3B82F6' : '#2563EB');
      avatarGrad.addColorStop(0.55, '#1D4ED8');
      avatarGrad.addColorStop(1, '#172554');
    } else if (color === '#EF4444') {
      avatarGrad.addColorStop(0, isActive ? '#EF4444' : '#DC2626');
      avatarGrad.addColorStop(0.55, '#B91C1C');
      avatarGrad.addColorStop(1, '#450A0A');
    } else if (color === '#10B981') {
      avatarGrad.addColorStop(0, isActive ? '#10B981' : '#059669');
      avatarGrad.addColorStop(0.55, '#047857');
      avatarGrad.addColorStop(1, '#022C22');
    } else if (color === '#F59E0B') {
      avatarGrad.addColorStop(0, isActive ? '#F59E0B' : '#D97706');
      avatarGrad.addColorStop(0.55, '#B45309');
      avatarGrad.addColorStop(1, '#451A03');
    } else {
      avatarGrad.addColorStop(0, color);
      avatarGrad.addColorStop(1, '#110C09');
    }

    ctx.fillStyle = avatarGrad;
    ctx.strokeStyle = isActive ? '#EF4444' : color;
    ctx.lineWidth = isActive ? 2.5 : 2;

    ctx.beginPath();
    ctx.ellipse(cx, cy, rx, ry, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.stroke();

    ctx.shadowColor = 'transparent';
    ctx.shadowBlur = 0;
    ctx.shadowOffsetY = 0;

    ctx.beginPath();
    ctx.ellipse(cx, cy, Math.max(1, rx - 3), Math.max(1, ry - 3), 0, 0, Math.PI * 2);
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.28)';
    ctx.lineWidth = 1;
    ctx.stroke();

    const isManagedOrDisc = isAuto || isDisc;
    const finalAvatar = isDisc ? '🤖' : avatarEmoji;

    if (orientation === 'horizontal') {
      const avatarR = 14;
      const avatarCx = cx - rx + avatarR + 6;
      const avatarCy = cy;

      ctx.fillStyle = isDisc ? '#374151' : 'rgba(0, 0, 0, 0.35)';
      ctx.strokeStyle = isActive ? '#EF4444' : (isDisc ? '#F59E0B' : 'rgba(255, 255, 255, 0.4)');
      ctx.lineWidth = isActive ? 1.5 : 1;
      ctx.beginPath();
      ctx.arc(avatarCx, avatarCy, avatarR, 0, Math.PI * 2);
      ctx.fill();
      ctx.stroke();

      ctx.font = '14px sans-serif';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(finalAvatar, avatarCx, avatarCy + 1);

      const textX = cx + 10;
      ctx.fillStyle = '#FFFFFF';
      ctx.font = 'bold 11px sans-serif';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(seatName, textX, cy - 6);

      ctx.fillStyle = '#FEF08A';
      ctx.font = 'bold 10px monospace';
      ctx.fillText(`${count}张`, textX, cy + 7);

      if (isManagedOrDisc) {
        const badgeLabel = isDisc ? '离线托管' : '⚡托管';
        ctx.save();
        ctx.fillStyle = isDisc ? '#DC2626' : '#D97706';
        ctx.strokeStyle = '#FFFFFF';
        ctx.lineWidth = 1;
        const bW = 52;
        const bH = 14;
        const bX = cx - bW / 2;
        const bY = cy + ry + 3;
        ctx.beginPath();
        ctx.roundRect(bX, bY, bW, bH, 7);
        ctx.fill();
        ctx.stroke();
        ctx.fillStyle = '#FFFFFF';
        ctx.font = 'bold 9px sans-serif';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillText(badgeLabel, cx, bY + bH / 2 + 0.5);
        ctx.restore();
      }
    } else {
      const avatarR = 14;
      const avatarCx = cx;
      const avatarCy = cy - ry + avatarR + 6;

      ctx.fillStyle = isDisc ? '#374151' : 'rgba(0, 0, 0, 0.35)';
      ctx.strokeStyle = isActive ? '#EF4444' : (isDisc ? '#F59E0B' : 'rgba(255, 255, 255, 0.4)');
      ctx.lineWidth = isActive ? 1.5 : 1;
      ctx.beginPath();
      ctx.arc(avatarCx, avatarCy, avatarR, 0, Math.PI * 2);
      ctx.fill();
      ctx.stroke();

      ctx.font = '13px sans-serif';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(finalAvatar, avatarCx, avatarCy + 1);

      ctx.fillStyle = '#FFFFFF';
      ctx.font = 'bold 10px sans-serif';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(seatName, cx, cy + 5);

      ctx.fillStyle = '#FEF08A';
      ctx.font = 'bold 10px monospace';
      ctx.fillText(`${count}张`, cx, cy + 18);

      if (isManagedOrDisc) {
        const badgeLabel = isDisc ? '离线托管' : '⚡托管';
        ctx.save();
        ctx.fillStyle = isDisc ? '#DC2626' : '#D97706';
        ctx.strokeStyle = '#FFFFFF';
        ctx.lineWidth = 1;
        const bW = 46;
        const bH = 14;
        const bX = cx - bW / 2;
        const bY = cy + ry + 4;
        ctx.beginPath();
        ctx.roundRect(bX, bY, bW, bH, 7);
        ctx.fill();
        ctx.stroke();
        ctx.fillStyle = '#FFFFFF';
        ctx.font = 'bold 8.5px sans-serif';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillText(badgeLabel, cx, bY + bH / 2 + 0.5);
        ctx.restore();
      }
    }

    if (isDealer) {
      const badgeX = cx + rx - 6;
      const badgeY = cy - ry + 4;
      ctx.fillStyle = '#F59E0B';
      ctx.strokeStyle = '#FFFFFF';
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.arc(badgeX, badgeY, 6.5, 0, Math.PI * 2);
      ctx.fill();
      ctx.stroke();

      ctx.fillStyle = '#FFFFFF';
      ctx.font = 'bold 8px sans-serif';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText('庄', badgeX, badgeY + 0.5);
    }

    ctx.restore();
  };

  function getPlayerInfoByRelSeat(relSeat: number): { name: string; avatar: string } {
    const defaultNames = ['玩家', '牌友 A', '牌友 B', '牌友 C'];
    const defaultEmojis = ['👑', '🧑', '🧓', '🧔'];
    if (state.roomPlayers && state.roomPlayers.length > 0) {
      const absSeat = state.relToAbsSeat(relSeat);
      const p = state.roomPlayers.find((x: any) => x?.seat === absSeat) || state.roomPlayers[absSeat];
      if (p && p.name) {
        const pName = String(p.name);
        const shortName = pName.length > 4 ? `${pName.slice(0, 3)}..` : pName;
        return {
          name: shortName,
          avatar: p.avatar || defaultEmojis[relSeat],
        };
      }
    }
    return {
      name: defaultNames[relSeat],
      avatar: defaultEmojis[relSeat],
    };
  }

  function getOpponentTotalTileCount(relSeat: number): number {
    if (state.dealingPhase === 'dealing_tiles') {
      return state.oppHandsDealtCount[relSeat - 1] || 0;
    }
    if (state.dealingPhase === 'rolling_dice') return 0;
    const botIdx = relSeat - 1;
    state.ensureOpponentHandSize(botIdx);
    return state.oppHands[botIdx]?.length || 0;
  }

  const northInfo = getPlayerInfoByRelSeat(2);
  const northCount = getOpponentTotalTileCount(2);
  const northAbs = state.relToAbsSeat(2);
  drawOvalOpponentAvatar(
    width / 2,
    26,
    52,
    22,
    northInfo.avatar,
    northInfo.name,
    northCount,
    '#3B82F6',
    state.turnPlayer === 2 && state.dealingPhase === 'playing',
    'horizontal',
    state.dealerSeat === 2,
    state.turnCountdown,
    state.isSeatAutoManaged(northAbs),
    state.isSeatDisconnected(northAbs)
  );

  const wallBoxW = 90;
  const wallBoxH = 26;
  const wallBoxX = width / 2 - 52 - wallBoxW - 10;
  const wallBoxY = 13;

  ctx.save();
  ctx.fillStyle = 'rgba(0, 0, 0, 0.45)';
  ctx.strokeStyle = 'rgba(255, 255, 255, 0.22)';
  ctx.lineWidth = 1.2;
  ctx.beginPath();
  ctx.roundRect(wallBoxX, wallBoxY, wallBoxW, wallBoxH, 13);
  ctx.fill();
  ctx.stroke();

  ctx.fillStyle = '#FFFFFF';
  ctx.font = 'bold 11px sans-serif';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.fillText(`🀄 牌墙 ${state.wallCount}张`, wallBoxX + wallBoxW / 2, wallBoxY + wallBoxH / 2);
  ctx.restore();

  if (state.dealingPhase === 'playing') {
    const cdBoxW = 34;
    const cdBoxH = 26;
    const cdBoxX = width / 2 + 52 + 10;
    const cdBoxY = 13;

    ctx.save();
    ctx.fillStyle = 'rgba(0, 0, 0, 0.65)';
    ctx.strokeStyle = '#EF4444';
    ctx.lineWidth = 1.5;
    ctx.shadowColor = 'rgba(239, 68, 68, 0.6)';
    ctx.shadowBlur = 8;
    ctx.beginPath();
    ctx.roundRect(cdBoxX, cdBoxY, cdBoxW, cdBoxH, 13);
    ctx.fill();
    ctx.stroke();

    ctx.fillStyle = '#EF4444';
    ctx.font = 'bold 15px monospace';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    const activeSec = state.isClaiming ? state.claimCountdown : state.turnCountdown;
    ctx.fillText(String(Math.max(0, activeSec)), cdBoxX + cdBoxW / 2, cdBoxY + cdBoxH / 2 + 0.5);
    ctx.restore();
  }

  const westInfo = getPlayerInfoByRelSeat(3);
  const westCount = getOpponentTotalTileCount(3);
  const westAbs = state.relToAbsSeat(3);
  drawOvalOpponentAvatar(
    30,
    90,
    22,
    42,
    westInfo.avatar,
    westInfo.name,
    westCount,
    '#EF4444',
    state.turnPlayer === 3 && state.dealingPhase === 'playing',
    'vertical',
    state.dealerSeat === 3,
    state.turnCountdown,
    state.isSeatAutoManaged(westAbs),
    state.isSeatDisconnected(westAbs)
  );

  const eastInfo = getPlayerInfoByRelSeat(1);
  const eastCount = getOpponentTotalTileCount(1);
  const eastAbs = state.relToAbsSeat(1);
  drawOvalOpponentAvatar(
    width - 30,
    90,
    22,
    42,
    eastInfo.avatar,
    eastInfo.name,
    eastCount,
    '#10B981',
    state.turnPlayer === 1 && state.dealingPhase === 'playing',
    'vertical',
    state.dealerSeat === 1,
    state.turnCountdown,
    state.isSeatAutoManaged(eastAbs),
    state.isSeatDisconnected(eastAbs)
  );

  const neckLeft = 60;
  const neckRight = width - 60;
  const stepY = 134;
  const bodyLeft = 18;
  const bodyRight = width - 18;

  const tileW = 23;
  const tileH = 31;
  const gapX = 3;
  const gapY = 1;

  interface RiverRowConfig {
    ry: number;
    capacity: number;
    startX: number;
  }
  const rowConfigs: RiverRowConfig[] = [];
  let curY = 50;
  for (let r = 0; r < 4; r++) {
    const ry = curY;
    const isNeck = ry < stepY;
    const minX = isNeck ? neckLeft : bodyLeft;
    const maxX = isNeck ? neckRight : bodyRight;
    const availW = maxX - minX - 16;
    const capacity = Math.floor((availW + gapX) / (tileW + gapX));
    const actualW = capacity * tileW + (capacity - 1) * gapX;
    const startX = minX + Math.floor((maxX - minX - actualW) / 2);
    rowConfigs.push({ ry, capacity, startX });
    curY += tileH + gapY;
  }

  const visibleDiscards = state.discards.slice(-87);

  if (visibleDiscards.length === 0) {
    ctx.fillStyle = 'rgba(255, 255, 255, 0.28)';
    ctx.font = '500 11px sans-serif';
    ctx.textAlign = 'center';
    ctx.fillText('暂无弃牌', width / 2, 114);
  } else {
    let tileIndex = 0;
    for (let r = 0; r < rowConfigs.length && tileIndex < visibleDiscards.length; r++) {
      const config = rowConfigs[r];
      for (let c = 0; c < config.capacity && tileIndex < visibleDiscards.length; c++) {
        const tile = visibleDiscards[tileIndex];
        const rx = config.startX + c * (tileW + gapX);
        const ry = config.ry;

        renderMahjongTile(ctx, tile, rx, ry, tileW, tileH, { isSmall: true });

        if (tileIndex === visibleDiscards.length - 1) {
          ctx.save();
          ctx.strokeStyle = '#FDE047';
          ctx.lineWidth = 1.8;
          ctx.beginPath();
          ctx.roundRect(rx - 0.5, ry - 0.5, tileW + 1, tileH + 1, 3);
          ctx.stroke();
          ctx.restore();
        }

        tileIndex++;
      }
    }
  }

  const activeOpponents: { label: string; color: string; melds: MahjongTile[][] }[] = [];
  if (state.oppMelds[2].length > 0) {
    activeOpponents.push({ label: '西席碰牌', color: '#EF4444', melds: state.oppMelds[2] });
  }
  if (state.oppMelds[1].length > 0) {
    activeOpponents.push({ label: '北席碰牌', color: '#3B82F6', melds: state.oppMelds[1] });
  }
  if (state.oppMelds[0].length > 0) {
    activeOpponents.push({ label: '东席碰牌', color: '#10B981', melds: state.oppMelds[0] });
  }

  const oppTileW = 23;
  const oppTileH = 31;
  const meldBaseY = 186;
  const rowSpacing = 36;

  activeOpponents.forEach((opp, rowIdx) => {
    const rowY = meldBaseY + rowIdx * rowSpacing;
    const labelX = bodyLeft + 8;

    ctx.save();
    ctx.shadowColor = opp.color;
    ctx.shadowBlur = 6;
    ctx.fillStyle = opp.color;
    ctx.font = 'bold 12px sans-serif';
    ctx.textAlign = 'left';
    ctx.textBaseline = 'middle';
    ctx.fillText(opp.label, labelX, rowY + oppTileH / 2);
    ctx.restore();

    const tilesStartX = labelX + 62;
    let curX = tilesStartX;
    opp.melds.forEach((set) => {
      set.forEach((t) => {
        renderMahjongTile(ctx, t, curX, rowY, oppTileW, oppTileH, {
          isSmall: true,
          borderColor: opp.color,
          borderWidth: 1.5,
        });
        curX += oppTileW + 2;
      });
      curX += 8;
    });
  });

  const handLen =
    state.dealingPhase === 'playing' || state.dealingPhase === 'sorting_tiles'
      ? state.myHand.length
      : state.dealerSeat === 0
        ? 14
        : 13;
  const handTileW = 38;
  const handTileH = 56;
  const gap = 3;
  const hasDrawn14 = state.myHand.length % 3 === 2 || (state.dealerSeat === 0 && state.dealingPhase !== 'playing');
  const drawnOffset = hasDrawn14 ? 14 : 0;
  const handTotalW = handLen > 0 ? handLen * handTileW + (handLen - 1) * gap + drawnOffset : 0;

  const meldTileW = 28;
  const meldTileH = 40;
  const meldGap = 2;
  const meldSetGap = 12;
  const meldsToHandGap = 18;
  const meldStartX = 24;

  let totalMeldsW = 0;
  if (state.meldedTiles.length > 0) {
    state.meldedTiles.forEach((set, sIdx) => {
      totalMeldsW += set.length * meldTileW + (set.length - 1) * meldGap;
      if (sIdx < state.meldedTiles.length - 1) totalMeldsW += meldSetGap;
    });
  }

  let handStartX = 24;
  if (totalMeldsW > 0) {
    handStartX = meldStartX + totalMeldsW + meldsToHandGap;
  } else {
    handStartX = Math.max(20, Math.floor((width - handTotalW) / 2));
  }
  const baseY = height - 142;

  if (state.meldedTiles.length > 0) {
    let meldOffsetX = meldStartX;
    const meldY = baseY + 40 + (handTileH - meldTileH);

    ctx.save();
    ctx.fillStyle = '#CBD5E1';
    ctx.font = '600 10px sans-serif';
    ctx.textAlign = 'left';
    ctx.fillText('亮副 (碰/杠):', meldStartX, meldY - 5);
    ctx.restore();

    state.meldedTiles.forEach((set) => {
      set.forEach((t) => {
        renderMahjongTile(ctx, t, meldOffsetX, meldY, meldTileW, meldTileH, { isSmall: true });
        meldOffsetX += meldTileW + meldGap;
      });
      meldOffsetX += meldSetGap - meldGap;
    });

    ctx.save();
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.28)';
    ctx.lineWidth = 1;
    ctx.setLineDash([2, 3]);
    ctx.beginPath();
    ctx.moveTo(handStartX - 10, baseY + 36);
    ctx.lineTo(handStartX - 10, baseY + 40 + handTileH);
    ctx.stroke();
    ctx.restore();
  }

  if (state.dealingPhase === 'rolling_dice') {
    // Dice rolling stage: clean table
  } else if (state.dealingPhase === 'dealing_tiles') {
    const dealtLen = state.myHandDealtCount;
    const isDealerWith14 = state.dealerSeat === 0;
    for (let idx = 0; idx < dealtLen; idx++) {
      const isDrawn14 = idx === 13 && isDealerWith14;
      const extraOffset = isDrawn14 ? 14 : 0;
      const tx = handStartX + idx * (handTileW + gap) + extraOffset;
      const ty = baseY + 40;
      renderMahjongTileBack(ctx, tx, ty, handTileW, handTileH);
    }
  } else if (state.dealingPhase === 'sorting_tiles') {
    const p = state.handFlipProgress;
    state.myHand.forEach((t, idx) => {
      const isDrawn14 = idx === handLen - 1 && hasDrawn14;
      const extraOffset = isDrawn14 ? 14 : 0;
      const tx = handStartX + idx * (handTileW + gap) + extraOffset;
      const ty = baseY + 40;

      if (p < 0.5) {
        const scaleY = 1 - p * 2;
        renderMahjongTileBack(ctx, tx, ty, handTileW, handTileH, { scaleY });
      } else {
        const scaleY = (p - 0.5) * 2;
        renderMahjongTile(ctx, t, tx, ty, handTileW, handTileH, { scaleY });
      }
    });
  } else {
    state.myHand.forEach((t, idx) => {
      const isSel = state.selectedIndex === idx;
      const isDrawn14 = idx === handLen - 1 && hasDrawn14;
      const extraOffset = isDrawn14 ? 14 : 0;

      const tx = handStartX + idx * (handTileW + gap) + extraOffset;
      const ty = isSel ? baseY + 40 - 16 : baseY + 40;

      renderMahjongTile(ctx, t, tx, ty, handTileW, handTileH, { isSelected: isSel });
    });
  }
}
