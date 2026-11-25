import fs from 'fs';
import path from 'path';
import { WebSocketServer, WebSocket } from 'ws';
import { loadDatabase, saveDatabase, recordServerPointsTx } from './serverStorage.ts';
import { safeTokenEqual } from './server/apiHelpers.ts';
import {
  initServerBookingInventory,
  runServer0600SettlementSweep,
  type SettledUserNotification,
} from './games/shisanshui/serverBookingService.ts';
import { presenceTracker } from './presenceTracker.ts';
import { generateRoomDeal } from './gameDeckService.ts';
import { createDefaultBoardState, getSceneRequiredCoins } from './gameLogic/index.ts';
import {
  isBotLikePlayer,
  filterHumanScenePlayers,
  pickBestWaitingRoom,
  SETTLEMENT_TIMEOUT_MS,
  type ActiveChallengeRecord,
  type AuthoritativeBoardState,
  type RealtimeHubContext,
  type ScenePlayer,
  type SceneWaitingRoom,
} from './server/realtimeTypes.ts';
import { sanitizeBoardStateForSeat, sanitizeDealForSeat } from './server/sanitizers.ts';
import {
  emitBookingSettlementNotificationsHelper,
  handleBookingWsMessage,
} from './server/handlers/bookingWsHandlers.ts';
import {
  handleMahjongGameMove,
  tickMahjongAutoTurn,
  tickMahjongClaimWindow,
} from './server/games/mahjongServerEngine.ts';
import { handleYansanGameMove, tickYansanAutoTurn } from './server/games/yansanServerEngine.ts';
import { handleShisanshuiGameMove, tickShisanshuiRoom } from './server/games/shisanshuiServerEngine.ts';

export type { AuthoritativeBoardState, ScenePlayer, SceneWaitingRoom, ActiveChallengeRecord };
export { sanitizeDealForSeat };

export function createRealtimeHub(wss: WebSocketServer): (payload: unknown) => void {
  const rooms = new Map<string, any>([
    [
      'RM-202',
      {
        id: 'RM-202',
        title: '风雷堂 · 经典烟三四人赛',
        gameType: 'yansan',
        status: 'playing',
        players: [
          { id: 'P-4', name: '快枪手张三', rating: 2010, seat: 0 },
          { id: 'P-5', name: '关门客李四', rating: 1940, seat: 1 },
          { id: 'P-6', name: '岭南少侠', rating: 1870, seat: 2 },
        ],
        spectators: 9,
        turnSeat: 0,
        moveCount: 6,
        modeLabel: '方块四首出 · 四人争锋',
        chat: [{ id: 'c2', sender: '快枪手张三', text: '方块四首出直接开顺子！', timestamp: '10:04' }],
        boardState: createDefaultBoardState('RM-202', 'yansan'),
      },
    ],
    [
      'RM-204',
      {
        id: 'RM-204',
        title: '水镜阁 · 经典十三水',
        gameType: 'shisanshui',
        status: 'playing',
        players: [
          { id: 'P-11', name: '罗宋巧手', rating: 2060, seat: 0 },
          { id: 'P-12', name: '倒水掌门', rating: 1850, seat: 1 },
          { id: 'P-13', name: '十三飞龙', rating: 2200, seat: 2 },
          { id: 'P-14', name: '七星居士', rating: 1970, seat: 3 },
        ],
        spectators: 18,
        turnSeat: 0,
        moveCount: 1,
        modeLabel: '头中尾三道 · 智能理牌',
        chat: [{ id: 'c4', sender: '十三飞龙', text: '尾道葫芦中道同花，防倒水稳如磐石。', timestamp: '10:06' }],
        boardState: createDefaultBoardState('RM-204', 'shisanshui'),
      },
    ],
    [
      'RM-205',
      {
        id: 'RM-205',
        title: '雀神楼 · 经典国标麻将',
        gameType: 'mahjong',
        status: 'playing',
        players: [
          { id: 'P-15', name: '天和雀圣', rating: 2240, seat: 0 },
          { id: 'P-16', name: '清一色老叟', rating: 2110, seat: 1 },
          { id: 'P-17', name: '大三元道长', rating: 2180, seat: 2 },
          { id: 'P-18', name: '十三幺隐士', rating: 2290, seat: 3 },
        ],
        spectators: 31,
        turnSeat: 0,
        moveCount: 12,
        modeLabel: '万条筒字 · 碰杠听胡',
        chat: [{ id: 'c5', sender: '天和雀圣', text: '自摸万字清一色，听牌在手！', timestamp: '10:07' }],
        boardState: createDefaultBoardState('RM-205', 'mahjong'),
      },
    ],
  ]);

  const sceneWaitingRooms = new Map<string, SceneWaitingRoom>();
  const activeChallenges: ActiveChallengeRecord[] = [];

  presenceTracker.setReferences({ rooms, sceneWaitingRooms, wss });

  function broadcast(payload: unknown) {
    try {
      const raw = JSON.stringify(payload);
      for (const client of wss.clients) {
        if (client.readyState === WebSocket.OPEN) {
          try {
            client.send(raw);
          } catch {}
        }
      }
    } catch {}
  }

  function findOpenSocketForPlayer(pl: any): WebSocket | null {
    if (!pl) return null;
    if (pl.socketId) {
      for (const client of wss.clients) {
        if ((client as any).socketId === pl.socketId && client.readyState === WebSocket.OPEN) {
          return client;
        }
      }
    }
    for (const client of wss.clients) {
      if (
        client.readyState === WebSocket.OPEN &&
        (client as any).scenePlayerId &&
        (client as any).scenePlayerId === pl.id
      ) {
        return client;
      }
    }
    return null;
  }

  function sendSanitizedToRoom(
    room: { id: string; players?: any[] },
    buildPayloadForSeat: (seat: number, player: any) => unknown
  ) {
    const sentSockets = new Set<WebSocket>();
    const combinedPlayers = [
      ...(Array.isArray(room.players) ? room.players : []),
      ...(Array.isArray(sceneWaitingRooms.get(room.id)?.players) ? sceneWaitingRooms.get(room.id)!.players : []),
    ];
    for (const pl of combinedPlayers) {
      if (!pl || isBotLikePlayer(pl)) continue;
      const targetWs = findOpenSocketForPlayer(pl);
      if (targetWs && !sentSockets.has(targetWs)) {
        sentSockets.add(targetWs);
        try {
          targetWs.send(JSON.stringify(buildPayloadForSeat(typeof pl.seat === 'number' ? pl.seat : 0, pl)));
        } catch {}
      }
    }
    for (const client of wss.clients) {
      if (
        client.readyState === WebSocket.OPEN &&
        !sentSockets.has(client) &&
        (client as any).sceneRoomId === room.id
      ) {
        const pid = (client as any).scenePlayerId;
        const matched = combinedPlayers.find((p: any) => p.id === pid);
        const s = matched && typeof matched.seat === 'number' ? matched.seat : -1;
        try {
          client.send(JSON.stringify(buildPayloadForSeat(s, matched || null)));
        } catch {}
      }
    }
  }

  function verifyWsSession(ws: WebSocket, playerId?: string, sessionToken?: string): { ok: boolean; profile?: any } {
    if (!playerId || !sessionToken) return { ok: false };
    try {
      const db = loadDatabase();
      const prof = db.profiles?.[playerId];
      if (prof && prof.sessionToken && safeTokenEqual(prof.sessionToken, sessionToken) && !prof.isBanned) {
        (ws as any).authenticatedUserId = prof.id;
        (ws as any).userId = prof.id;
        return { ok: true, profile: prof };
      }
    } catch {}
    return { ok: false };
  }

  function sanitizeWsText(raw: unknown, maxLen: number): string {
    return String(raw ?? '')
      .replace(/[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/g, '')
      .trim()
      .slice(0, maxLen);
  }

  function allowSocketChat(ws: WebSocket): boolean {
    const now = Date.now();
    const wsAny = ws as any;
    if (!Array.isArray(wsAny._chatTimestamps)) {
      wsAny._chatTimestamps = [];
    }
    wsAny._chatTimestamps = wsAny._chatTimestamps.filter((ts: number) => now - ts < 10000);
    if (wsAny._chatTimestamps.length >= 15) {
      return false;
    }
    wsAny._chatTimestamps.push(now);
    return true;
  }

  const ACTIVE_SNAPSHOT_FILE = path.resolve(process.cwd(), 'data', 'active_rooms_snapshot.json');
  let snapshotSaveTimer: NodeJS.Timeout | null = null;
  function scheduleActiveRoomsSnapshot() {
    if (process.env.DISABLE_ACTIVE_ROOM_SNAPSHOT === '1') return;
    if (snapshotSaveTimer) clearTimeout(snapshotSaveTimer);
    snapshotSaveTimer = setTimeout(() => {
      snapshotSaveTimer = null;
      try {
        const dir = path.dirname(ACTIVE_SNAPSHOT_FILE);
        if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true });
        const defaultRoomIds = new Set(['RM-202', 'RM-204', 'RM-205']);
        const savedRooms = Array.from(rooms.values())
          .filter((r: any) => r && !defaultRoomIds.has(r.id) && (r.status === 'playing' || r.status === 'starting' || r.status === 'waiting'))
          .slice(0, 50);
        const savedWaiting = Array.from(sceneWaitingRooms.values()).slice(0, 50);
        const tmpFile = `${ACTIVE_SNAPSHOT_FILE}.tmp.${Date.now()}`;
        fs.writeFileSync(tmpFile, JSON.stringify({ savedAt: Date.now(), rooms: savedRooms, sceneWaitingRooms: savedWaiting }), 'utf-8');
        fs.renameSync(tmpFile, ACTIVE_SNAPSHOT_FILE);
      } catch {}
    }, 500);
  }

  if (process.env.DISABLE_ACTIVE_ROOM_SNAPSHOT !== '1') {
    try {
      if (fs.existsSync(ACTIVE_SNAPSHOT_FILE)) {
        const parsed = JSON.parse(fs.readFileSync(ACTIVE_SNAPSHOT_FILE, 'utf-8'));
        const maxAgeMs = 4 * 3600 * 1000;
        if (parsed && parsed.savedAt && Date.now() - parsed.savedAt < maxAgeMs) {
          for (const r of parsed.rooms || []) {
            if (!r?.id || rooms.has(r.id)) continue;
            if (Array.isArray(r.players)) {
              r.players.forEach((pl: any) => {
                if (pl && !isBotLikePlayer(pl)) {
                  pl.isDisconnected = true;
                  pl.isAutoManaged = true;
                  delete pl.socketId;
                }
              });
            }
            rooms.set(r.id, r);
          }
          for (const sr of parsed.sceneWaitingRooms || []) {
            if (!sr?.id || sceneWaitingRooms.has(sr.id)) continue;
            if (Array.isArray(sr.players)) {
              sr.players.forEach((pl: any) => {
                if (pl && !isBotLikePlayer(pl)) {
                  pl.isDisconnected = true;
                  if (sr.status === 'playing' || sr.status === 'starting') pl.isAutoManaged = true;
                  delete pl.socketId;
                }
              });
            }
            sceneWaitingRooms.set(sr.id, sr);
          }
        }
      }
    } catch {}
  }

  function broadcastTeahousePlayers() {
    try {
      const db = loadDatabase();
      const players = presenceTracker.getAllPlayers(db.profiles || {});
      broadcast({
        type: 'teahouse_players_update',
        count: players.length,
        onlineCount: players.filter((p: any) => p.isOnline).length,
        players,
      });
    } catch {}
  }

  function resetSceneRoomReadyAndCleanDisconnected(roomId: string) {
    const authRoom = rooms.get(roomId);
    const targetSceneRoom = sceneWaitingRooms.get(roomId);
    const expiresAt =
      (authRoom as any)?.settlementExpiresAt ||
      (targetSceneRoom as any)?.settlementExpiresAt ||
      Date.now() + SETTLEMENT_TIMEOUT_MS;

    if (authRoom) {
      (authRoom as any).settlementExpiresAt = expiresAt;
      if (authRoom.players) {
        authRoom.players.forEach((pl: any) => {
          pl.isReady = isBotLikePlayer(pl);
        });
      }
    }
    if (targetSceneRoom) {
      targetSceneRoom.status = 'finished';
      (targetSceneRoom as any).settlementExpiresAt = expiresAt;
      targetSceneRoom.players.forEach((pl: any) => {
        pl.isReady = isBotLikePlayer(pl);
      });
      sendSanitizedToRoom(targetSceneRoom, () => ({
        type: 'scene_room_update',
        roomId: targetSceneRoom.id,
        gameType: targetSceneRoom.gameType,
        sceneMode: targetSceneRoom.sceneMode,
        players: targetSceneRoom.players,
        maxPlayers: targetSceneRoom.maxPlayers,
        status: targetSceneRoom.status,
        settlementExpiresAt: expiresAt,
        serverTs: Date.now(),
      }));
    }
    scheduleActiveRoomsSnapshot();
  }

  function settleRoomCoins(
    room: any,
    coinsDeltaBySeat: number[],
    gameType: string,
    meta: { ratingDeltaBySeat?: number[] } = {}
  ) {
    if (!room) return;
    const roundKey = `${gameType}_${room.id}_r${room.roundIndex || 1}_${(room as any).deal?.dealId || ''}`;
    if (room._coinsSettledRoundKey === roundKey) {
      return;
    }
    room._coinsSettledRoundKey = roundKey;

    try {
      const db = loadDatabase();
      let dirty = false;
      for (const pl of room.players || []) {
        if (!pl || typeof pl.seat !== 'number' || isBotLikePlayer(pl)) continue;
        const delta = Math.trunc(coinsDeltaBySeat[pl.seat] || 0);
        const profile = db.profiles?.[pl.id] || (pl.originalId ? db.profiles?.[pl.originalId] : undefined);
        const profileKey = db.profiles?.[pl.id] ? pl.id : (pl.originalId && db.profiles?.[pl.originalId] ? pl.originalId : pl.id);
        if (profile) {
          profile.coins = Math.max(0, (profile.coins || 0) + delta);
          if (meta.ratingDeltaBySeat && typeof meta.ratingDeltaBySeat[pl.seat] === 'number') {
            profile.rating = Math.max(1000, (profile.rating || 1800) + Math.trunc(meta.ratingDeltaBySeat[pl.seat]));
            profile.level = Math.max(1, Math.min(10, Math.floor(profile.rating / 500)));
          }
          profile.updatedAt = Date.now();
          if (delta !== 0) {
            const gLabel = gameType === 'mahjong' ? '经典国标麻将' : gameType === 'shisanshui' ? '水镜十三水' : '经典烟三';
            recordServerPointsTx(
              db,
              profileKey,
              delta >= 0 ? 'game_win' : 'game_loss',
              `${gLabel} · 牌桌结算 (${delta >= 0 ? '+' : ''}${delta})`,
              delta,
              profile.coins
            );
          }
          dirty = true;
          broadcast({
            type: 'user_coins_updated',
            userId: profileKey,
            coins: profile.coins,
            delta,
            rating: profile.rating,
            level: profile.level,
            source: 'match_settlement',
            gameType,
            roomId: room.id,
            roundKey,
          });
        }
      }
      if (dirty) {
        saveDatabase(db);
      }
    } catch (e) {
      console.error(`Error settling ${gameType} coins in database:`, e);
    }
  }

  function emitBookingSettlementNotifications(notifications: SettledUserNotification[]) {
    emitBookingSettlementNotificationsHelper(broadcast, notifications);
  }

  const hubContext: RealtimeHubContext = {
    rooms,
    sceneWaitingRooms,
    broadcast,
    sendSanitizedToRoom,
    settleRoomCoins,
    resetSceneRoomReadyAndCleanDisconnected,
    emitBookingSettlementNotifications,
    scheduleActiveRoomsSnapshot,
  };

  function startRoomGameIfFull(room: SceneWaitingRoom) {
    if (!room) return false;

    const humanPlayers = filterHumanScenePlayers(room.players);
    if (humanPlayers.length !== (room.players?.length || 0)) {
      room.players = humanPlayers;
    }

    if (!Array.isArray(room.players) || room.players.length < room.maxPlayers) {
      if (room.status !== 'waiting') {
        room.status = 'waiting';
        delete (room as any).deal;
      }
      return false;
    }

    if ((room.status === 'starting' || room.status === 'playing') && (room as any).deal && (room as any).deal.seatCards) {
      return true;
    }

    const validPlayers = humanPlayers
      .slice(0, room.maxPlayers)
      .sort((a: any, b: any) => {
        const sa = typeof a.seat === 'number' && a.seat >= 0 ? a.seat : 999;
        const sb = typeof b.seat === 'number' && b.seat >= 0 ? b.seat : 999;
        return sa - sb;
      });
    if (validPlayers.length < room.maxPlayers) {
      room.players = validPlayers;
      room.status = 'waiting';
      delete (room as any).deal;
      return false;
    }

    validPlayers.forEach((pl: any, idx: number) => {
      pl.seat = idx;
      pl.isReady = false;
      delete (pl as any).isDisconnected;
      delete (pl as any).isAutoManaged;
      if (!pl.id) pl.id = `P-usr-${idx}-${Date.now().toString(36)}`;
      if (!pl.name) pl.name = `茶客 #${idx + 1}`;
    });
    room.players = validPlayers;
    room.status = 'starting';
    delete (room as any).settlementExpiresAt;
    room.roundIndex = (room.roundIndex || 0) + 1;

    delete (room as any).deal;
    const freshDeal: any = generateRoomDeal(room.gameType, room.sceneMode, room.maxPlayers);
    if (!freshDeal.dealId) {
      freshDeal.dealId = `${room.gameType}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
    }
    if (room.gameType === 'yansan' && freshDeal.seatCards) {
      freshDeal.initialSeatCards = [0, 1, 2, 3].map((s) =>
        Array.isArray(freshDeal.seatCards[s]) ? [...freshDeal.seatCards[s]] : []
      );
    }
    (room as any).deal = freshDeal;

    const defaultBoard = createDefaultBoardState(room.id, room.gameType);
    if (room.gameType === 'yansan') {
      defaultBoard.turnSeat = typeof freshDeal?.starterSeat === 'number' ? freshDeal.starterSeat : 0;
      defaultBoard.extraState.hasPassedSeats = [false, false, false, false];
      defaultBoard.extraState.seatPlayedCards = [[], [], [], []];
      defaultBoard.extraState.playHistory = [];
      defaultBoard.extraState.isQiangGongActive = false;
      defaultBoard.extraState.qiangGongSeat = null;
    }
    if (room.gameType === 'mahjong') {
      defaultBoard.seatKongScores = [0, 0, 0, 0];
      defaultBoard.extraState.kongHistory = [];
      defaultBoard.extraState.claimWindow = null;
      defaultBoard.extraState.wallRemaining = Array.isArray(freshDeal.remainingWall) ? freshDeal.remainingWall.length : 83;
      const dSeat = typeof freshDeal.dealerSeat === 'number' ? freshDeal.dealerSeat : 0;
      defaultBoard.turnSeat = dSeat;
      defaultBoard.handsCount = [0, 1, 2, 3].map((s) =>
        Array.isArray(freshDeal.seatCards?.[s]) ? freshDeal.seatCards[s].length : (s === dSeat ? 14 : 13)
      );
    }

    if (!rooms.has(room.id)) {
      rooms.set(room.id, {
        id: room.id,
        title: `${room.sceneName} · 纯真人对决`,
        gameType: room.gameType,
        status: 'starting',
        players: room.players.map((pl, idx) => ({ ...pl, seat: idx, rating: pl.rating || 1900 })),
        spectators: 0,
        turnSeat: freshDeal?.starterSeat ?? 0,
        lastTurnTs: Date.now(),
        moveCount: 0,
        roundIndex: room.roundIndex,
        deal: freshDeal,
        modeLabel: `${room.sceneName} · 实时对决`,
        chat: [],
        boardState: defaultBoard,
      });
    } else {
      const existingRoom = rooms.get(room.id);
      if (existingRoom) {
        existingRoom.status = 'starting';
        delete existingRoom.settlementExpiresAt;
        existingRoom.moveCount = 0;
        existingRoom.roundIndex = room.roundIndex;
        existingRoom._coinsSettledRoundKey = undefined;
        existingRoom.deal = freshDeal;
        existingRoom.turnSeat = freshDeal?.starterSeat ?? 0;
        existingRoom.lastTurnTs = Date.now();
        existingRoom.boardState = defaultBoard;
        existingRoom.players = room.players.map((pl, idx) => ({ ...pl, seat: idx, rating: pl.rating || 1900 }));
      }
    }

    if ((room as any)._startTimer) {
      clearTimeout((room as any)._startTimer);
      (room as any)._startTimer = null;
    }

    console.log(
      `🚀 [满员自动发牌] 房间 ${room.id} (${room.gameType}-${room.sceneMode}) ${room.players.length}/${room.maxPlayers} 玩家席位校验完毕，定向脱敏广播 scene_room_full！`
    );

    sendSanitizedToRoom(room, (seat, player) => ({
      type: 'scene_room_full',
      roomId: room.id,
      gameType: room.gameType,
      sceneMode: room.sceneMode,
      assignedSeat: seat >= 0 ? seat : undefined,
      assignedPlayerId: player?.id,
      players: room.players,
      maxPlayers: room.maxPlayers,
      deal: sanitizeDealForSeat(room.gameType, freshDeal, seat),
      turnSeat: freshDeal?.starterSeat ?? 0,
      boardState: defaultBoard,
      status: 'starting',
      lockGameLaunch: true,
      forceReset: true,
      timestamp: Date.now(),
    }));

    (room as any)._startTimer = setTimeout(() => {
      (room as any)._startTimer = null;
      if (room && sceneWaitingRooms.has(room.id) && (room as any).deal === freshDeal) {
        room.status = 'playing';
        room.players.forEach((p: any) => {
          p.isReady = isBotLikePlayer(p);
        });
        const authRoom = rooms.get(room.id);
        if (authRoom) {
          authRoom.status = 'playing';
          authRoom.lastTurnTs = Date.now();
          authRoom.players?.forEach((p: any) => {
            p.isReady = isBotLikePlayer(p);
          });
        }
        sendSanitizedToRoom(room, (seat, player) => ({
          type: 'scene_room_started',
          roomId: room.id,
          gameType: room.gameType,
          sceneMode: room.sceneMode,
          assignedSeat: seat >= 0 ? seat : undefined,
          assignedPlayerId: player?.id,
          players: room.players,
          maxPlayers: room.maxPlayers,
          deal: sanitizeDealForSeat(room.gameType, freshDeal, seat),
          turnSeat: freshDeal?.starterSeat ?? 0,
          boardState: authRoom ? authRoom.boardState : defaultBoard,
          status: 'playing',
          lockGameLaunch: true,
          forceReset: true,
          timestamp: Date.now(),
        }));
        scheduleActiveRoomsSnapshot();
      }
    }, 1400);

    scheduleActiveRoomsSnapshot();
    broadcast({
      type: 'presence_counts',
      counts: presenceTracker.getRealGamePresence(),
    });
    broadcastTeahousePlayers();
    return true;
  }

  wss.on('connection', (ws, req) => {
    try {
      (ws as any)?._socket?.setNoDelay?.(true);
      (req?.socket as any)?.setNoDelay?.(true);
    } catch {}
    const socketId = Math.random().toString(36).slice(2, 10);
    (ws as any).socketId = socketId;
    (ws as any).isAlive = true;
    (ws as any).lastHeartbeat = Date.now();
    presenceTracker.recordClient(socketId, { gameType: 'lobby' });

    ws.on('pong', () => {
      (ws as any).isAlive = true;
      (ws as any).lastHeartbeat = Date.now();
    });

    ws.on('error', () => {
      // Gracefully ignore client connection reset
    });

    try {
      const presenceCounts = presenceTracker.getRealGamePresence();
      const db = loadDatabase();
      const teahousePlayers = presenceTracker.getAllPlayers(db.profiles || {});
      ws.send(
        JSON.stringify({
          type: 'lobby_snapshot',
          rooms: Array.from(rooms.values()).map((r) => {
            const { deal: _d, ...rest } = r;
            return rest;
          }),
          onlineCount: wss.clients.size,
          counts: presenceCounts,
          players: teahousePlayers,
          serverTs: Date.now(),
        })
      );
      ws.send(
        JSON.stringify({
          type: 'teahouse_players_update',
          count: teahousePlayers.length,
          onlineCount: teahousePlayers.filter((p: any) => p.isOnline).length,
          players: teahousePlayers,
        })
      );
    } catch {}

    broadcast({
      type: 'presence_counts',
      counts: presenceTracker.getRealGamePresence(),
    });
    broadcastTeahousePlayers();

    ws.on('close', () => {
      const closingSocketId = (ws as any).socketId;
      presenceTracker.removeClient(closingSocketId);
      const sRoomId = (ws as any).sceneRoomId;
      const pId = (ws as any).scenePlayerId;
      let roomMutated = false;
      let room = sRoomId ? sceneWaitingRooms.get(sRoomId) : undefined;
      if (!room && sRoomId) {
        room = rooms.get(sRoomId) as any;
      }
      if (sRoomId && room) {
        const isShisanshuiReservation = room.gameType === 'shisanshui' && room.sceneMode === 'reservation';

        if (room.status === 'finished' && !isShisanshuiReservation) {
          const pl = room.players.find(
            (p: any) =>
              p.socketId === closingSocketId ||
              (!p.socketId && pId && (p.id === pId || (p as any).originalId === pId))
          );
          if (pl) {
            (pl as any).isDisconnected = true;
            delete (pl as any).socketId;
            roomMutated = true;
          }
          const authRoom = rooms.get(sRoomId);
          if (authRoom) {
            const aPl = (authRoom.players || []).find(
              (p: any) =>
                p.socketId === closingSocketId ||
                (!p.socketId && ((pl && p.id === pl.id) || (pId && (p.id === pId || p.originalId === pId))))
            );
            if (aPl) {
              (aPl as any).isDisconnected = true;
              delete (aPl as any).socketId;
              roomMutated = true;
            }
          }
        } else if ((room.status === 'starting' || room.status === 'playing') && !isShisanshuiReservation) {
          const pl = room.players.find(
            (p: any) => p.socketId === closingSocketId || (!p.socketId && pId && p.id === pId)
          );
          if (pl) {
            const plSeat = pl.seat;
            (pl as any).isDisconnected = true;
            (pl as any).isAutoManaged = true;
            roomMutated = true;

            const authRoom = rooms.get(sRoomId);
            if (authRoom) {
              const aPl = (authRoom.players || []).find(
                (p: any) =>
                  p.socketId === closingSocketId ||
                  (!p.socketId && (p.id === pl.id || (pId && p.id === pId)))
              );
              if (aPl) {
                (aPl as any).isDisconnected = true;
                (aPl as any).isAutoManaged = true;
              }
              if (!authRoom.boardState) {
                authRoom.boardState = createDefaultBoardState(authRoom.id, authRoom.gameType);
              }
              if (!authRoom.boardState.autoManagedSeats) authRoom.boardState.autoManagedSeats = {};
              authRoom.boardState.autoManagedSeats[plSeat] = true;
              if (authRoom.turnSeat === plSeat) {
                authRoom.lastTurnTs = Date.now() - 2000;
              }
            }

            console.log(`🤖 [断线自动托管] 玩家 ${pl.name} (${pl.id}) 席位 ${plSeat} 掉线，已启用智能托管完成该局对局`);
            sendSanitizedToRoom(room, (rSeat) => ({
              type: 'scene_room_update',
              roomId: room.id,
              gameType: room.gameType,
              sceneMode: room.sceneMode,
              players: room.players,
              maxPlayers: room.maxPlayers,
              status: room.status,
              assignedSeat: rSeat >= 0 ? rSeat : undefined,
            }));
            sendSanitizedToRoom(room, (rSeat) => ({
              type: 'game_event',
              roomId: room.id,
              seat: plSeat,
              event: {
                action: 'set_auto_host',
                seat: plSeat,
                absSeat: plSeat,
                isAutoManaged: true,
                isAutoHost: true,
                isDisconnected: true,
                playerName: pl.name,
              },
              payload: {
                action: 'set_auto_host',
                seat: plSeat,
                absSeat: plSeat,
                isAutoManaged: true,
                isAutoHost: true,
                isDisconnected: true,
                playerName: pl.name,
              },
              boardState: sanitizeBoardStateForSeat(
                authRoom ? authRoom.boardState : (room as any).boardState,
                room.gameType,
                rSeat
              ),
            }));
          }
        } else {
          const prevLen = room.players.length;
          room.players = room.players.filter((pl: any) => {
            if (pl.socketId) {
              return pl.socketId !== closingSocketId;
            }
            return pl.id !== pId;
          });
          if (room.players.length !== prevLen) {
            roomMutated = true;
            if (room.players.length === 0) {
              sceneWaitingRooms.delete(sRoomId);
            } else {
              room.players.forEach((pl: any, idx: number) => {
                pl.seat = idx;
              });
              if (room.status !== 'playing' && room.status !== 'starting') {
                room.status = 'waiting';
              }
              sendSanitizedToRoom(room, () => ({
                type: 'scene_room_update',
                roomId: room.id,
                gameType: room.gameType,
                sceneMode: room.sceneMode,
                players: room.players,
                maxPlayers: room.maxPlayers,
                status: room.status,
              }));
            }
          }
        }
      }
      if (roomMutated) {
        scheduleActiveRoomsSnapshot();
      }
      if (pId && !presenceTracker.hasPlayerSocket(pId, closingSocketId)) {
        for (let i = activeChallenges.length - 1; i >= 0; i--) {
          const ch = activeChallenges[i];
          if (ch.senderId === pId && !ch.acceptedBy) {
            const rmId = ch.id;
            activeChallenges.splice(i, 1);
            broadcast({
              type: 'challenge_status_update',
              challengeId: rmId,
              status: 'removed',
              senderId: pId,
              senderName: ch.senderName,
              gameType: ch.gameType,
              sceneMode: ch.sceneMode,
            });
          }
        }
      }
      broadcast({
        type: 'presence_counts',
        counts: presenceTracker.getRealGamePresence(),
      });
      broadcastTeahousePlayers();
    });

    ws.on('message', async (data) => {
      try {
        (ws as any).isAlive = true;
        (ws as any).lastHeartbeat = Date.now();
        const msg = JSON.parse(data.toString());

        if (msg.type === 'ping') {
          ws.send(
            JSON.stringify({
              type: 'pong',
              clientTs: msg.clientTs,
              serverTs: Date.now(),
              onlineCount: wss.clients.size,
            })
          );
          return;
        }

        if (msg.type === 'presence_update') {
          const targetGame =
            msg.gameType === 'yansan' || msg.gameType === 'shisanshui' || msg.gameType === 'mahjong'
              ? msg.gameType
              : 'lobby';
          if ('sessionToken' in msg) {
            const authRes = verifyWsSession(ws, msg.playerId, msg.sessionToken);
            if (!authRes.ok) {
              delete (ws as any).authenticatedUserId;
              (ws as any).userId = msg.playerId || undefined;
            }
          }
          presenceTracker.updateClientGame((ws as any).socketId, targetGame, msg.sceneMode);
          if (msg.playerId) {
            presenceTracker.recordClient((ws as any).socketId, {
              gameType: targetGame,
              playerId: msg.playerId,
              playerName: msg.playerName,
              avatar: msg.avatar,
              coins: typeof msg.coins === 'number' ? msg.coins : undefined,
              rating: typeof msg.rating === 'number' ? msg.rating : undefined,
              phone: msg.phone,
              sceneMode: msg.sceneMode,
            });
          }
          broadcast({
            type: 'presence_counts',
            counts: presenceTracker.getRealGamePresence(),
          });
          broadcastTeahousePlayers();
          return;
        }

        if (msg.type === 'request_teahouse_players') {
          const db = loadDatabase();
          const players = presenceTracker.getAllPlayers(db.profiles || {});
          ws.send(
            JSON.stringify({
              type: 'teahouse_players_update',
              count: players.length,
              onlineCount: players.filter((p: any) => p.isOnline).length,
              players,
            })
          );
          return;
        }

        if (await handleBookingWsMessage(ws, msg, hubContext)) {
          return;
        }

        if (msg.type === 'scene_reconnect') {
          if (msg.sessionToken && msg.playerId) {
            verifyWsSession(ws, msg.playerId, msg.sessionToken);
          }
          const pId = msg.playerId || (ws as any).authenticatedUserId || (ws as any).scenePlayerId;
          const targetRoomId = msg.roomId;
          let targetRoom: SceneWaitingRoom | undefined;
          if (targetRoomId && sceneWaitingRooms.has(targetRoomId)) {
            targetRoom = sceneWaitingRooms.get(targetRoomId);
          }
          if (!targetRoom && pId) {
            for (const r of sceneWaitingRooms.values()) {
              if (r.players.some((pl) => pl.id === pId || (pl as any).originalId === pId || (pl as any).userId === pId)) {
                targetRoom = r;
                break;
              }
            }
            if (!targetRoom && msg.playerName) {
              for (const r of sceneWaitingRooms.values()) {
                if (r.players.some((pl) => pl.name === msg.playerName)) {
                  targetRoom = r;
                  break;
                }
              }
            }
          }
          if (targetRoom) {
            const pl =
              targetRoom.players.find((p) => (pId && (p.id === pId || (p as any).originalId === pId || (p as any).userId === pId))) ||
              (msg.playerName ? targetRoom.players.find((p) => p.name === msg.playerName) : undefined);
            if (pl) {
              if (msg.sessionToken) {
                const db = loadDatabase();
                const prof = db.profiles?.[pl.id];
                if (prof && prof.sessionToken && !safeTokenEqual(prof.sessionToken, msg.sessionToken)) {
                  ws.send(JSON.stringify({ type: 'scene_reconnect_ack', failed: true, reason: 'invalid_session' }));
                  return;
                }
              }
              const authRoom = rooms.get(targetRoom.id);
              const isFinishedPhase = targetRoom.status === 'finished' || authRoom?.boardState?.phase === 'ended';
              if (isFinishedPhase) {
                targetRoom.status = 'finished';
              }
              delete (pl as any).isDisconnected;
              if (!isFinishedPhase) {
                delete (pl as any).isAutoManaged;
              }
              (pl as any).socketId = (ws as any).socketId;
              (ws as any).sceneRoomId = targetRoom.id;
              (ws as any).scenePlayerId = pl.id;
              if (authRoom) {
                const aPl =
                  (authRoom.players || []).find((p: any) => p.id === pl.id || (pId && (p.id === pId || p.originalId === pId || p.userId === pId))) ||
                  (msg.playerName ? (authRoom.players || []).find((p: any) => p.name === msg.playerName) : undefined);
                if (aPl) {
                  delete (aPl as any).isDisconnected;
                  if (!isFinishedPhase) {
                    delete (aPl as any).isAutoManaged;
                  }
                  aPl.socketId = (ws as any).socketId;
                }
                if (!isFinishedPhase && authRoom.boardState?.autoManagedSeats && typeof pl.seat === 'number') {
                  delete authRoom.boardState.autoManagedSeats[pl.seat];
                }
              }
              const rawDeal = (authRoom ? (authRoom as any).deal : null) || (targetRoom as any).deal || null;
              const settlementExpiresAt =
                (authRoom as any)?.settlementExpiresAt ||
                (targetRoom as any)?.settlementExpiresAt ||
                authRoom?.boardState?.settlement?.settlementExpiresAt ||
                null;
              const nowTs = Date.now();
              const { deal: _rawRoomDeal, _startTimer: _st, ...safeRoom } = targetRoom as any;
              ws.send(
                JSON.stringify({
                  type: 'scene_reconnect_ack',
                  room: safeRoom,
                  boardState: sanitizeBoardStateForSeat(authRoom ? authRoom.boardState : null, targetRoom.gameType, pl.seat),
                  players: targetRoom.players,
                  deal: rawDeal ? sanitizeDealForSeat(targetRoom.gameType, rawDeal, pl.seat) : null,
                  settlementExpiresAt,
                  serverTs: nowTs,
                })
              );
              scheduleActiveRoomsSnapshot();
              sendSanitizedToRoom(targetRoom, () => ({
                type: 'scene_room_update',
                roomId: targetRoom!.id,
                gameType: targetRoom!.gameType,
                sceneMode: targetRoom!.sceneMode,
                players: targetRoom!.players,
                maxPlayers: targetRoom!.maxPlayers,
                status: targetRoom!.status,
                settlementExpiresAt,
                serverTs: nowTs,
              }));
              if (!isFinishedPhase) {
                sendSanitizedToRoom(targetRoom, (rSeat) => ({
                  type: 'game_event',
                  roomId: targetRoom!.id,
                  seat: pl.seat,
                  event: {
                    action: 'set_auto_host',
                    seat: pl.seat,
                    absSeat: pl.seat,
                    isAutoManaged: false,
                    isAutoHost: false,
                    isDisconnected: false,
                  },
                  payload: {
                    action: 'set_auto_host',
                    seat: pl.seat,
                    absSeat: pl.seat,
                    isAutoManaged: false,
                    isAutoHost: false,
                    isDisconnected: false,
                  },
                  boardState: sanitizeBoardStateForSeat(authRoom ? authRoom.boardState : null, targetRoom!.gameType, rSeat),
                }));
              }
              return;
            }
          }
          ws.send(
            JSON.stringify({
              type: 'scene_reconnect_ack',
              failed: true,
              reason: 'room_not_found',
            })
          );
          return;
        }

        if (msg.type === 'scene_player_ready' || msg.type === 'room_restart') {
          const targetRoomId = msg.roomId || (ws as any).sceneRoomId;
          const pId = msg.playerId || (ws as any).scenePlayerId;
          let room: SceneWaitingRoom | undefined;
          if (targetRoomId && sceneWaitingRooms.has(targetRoomId)) {
            room = sceneWaitingRooms.get(targetRoomId);
          }
          if (!room) {
            for (const r of sceneWaitingRooms.values()) {
              if (
                r.players.some(
                  (pl) =>
                    (pl as any).socketId === (ws as any).socketId ||
                    (pl.id && pId && (pl.id === pId || pl.originalId === pId || (pl as any).userId === pId))
                )
              ) {
                room = r;
                break;
              }
            }
          }
          if (room) {
            const authRoom = rooms.get(room.id);
            if (authRoom && (authRoom.boardState?.phase === 'ended' || authRoom.status === 'finished')) {
              room.status = 'finished';
            }
            if (room.status === 'starting' || room.status === 'playing') {
              return;
            }
            let pl = room.players.find((p) => (p as any).socketId && (p as any).socketId === (ws as any).socketId);
            if (!pl && pId) {
              pl = room.players.find((p) => p.id === pId || p.originalId === pId || (p as any).userId === pId);
            }
            if (!pl && msg.playerName) {
              pl = room.players.find((p) => p.name === msg.playerName);
            }
            if (pl) {
              (pl as any).socketId = (ws as any).socketId;
              (pl as any).isReady = Boolean(msg.isReady ?? true);
              if (authRoom?.players) {
                const aPl = authRoom.players.find((ap: any) => ap.id === pl!.id || (pId && (ap.id === pId || ap.originalId === pId)));
                if (aPl) aPl.isReady = (pl as any).isReady;
              }
            }
            room.players = filterHumanScenePlayers(room.players);

            const readyCount = room.players.filter((p: any) => p.isReady).length;
            console.log(`🎮 [联机准备] 房间 ${room.id} (${room.gameType}) 玩家准备进度: ${readyCount}/${room.maxPlayers}`);

            const allFullReady = room.players.length >= room.maxPlayers && room.players.every((p: any) => p.isReady);
            if (allFullReady) {
              console.log(`🚀 [全员就绪再开] 房间 ${room.id} (${readyCount}/${room.maxPlayers}) 全员准备完成，生成全新发牌切片开启下一局！`);
              room.status = 'waiting';
              delete (room as any).deal;
              delete (room as any).settlementExpiresAt;
              if (authRoom) delete authRoom.settlementExpiresAt;
              startRoomGameIfFull(room);
              return;
            }

            sendSanitizedToRoom(room, () => ({
              type: 'scene_room_update',
              roomId: room.id,
              gameType: room.gameType,
              sceneMode: room.sceneMode,
              players: room.players,
              maxPlayers: room.maxPlayers,
              status: room.status,
              settlementExpiresAt: (room as any).settlementExpiresAt || (authRoom as any)?.settlementExpiresAt || null,
              serverTs: Date.now(),
            }));
            return;
          }
          if (msg.type === 'scene_player_ready') {
            return;
          }
        }

        if (msg.type === 'scene_start_game') {
          const roomId = msg.roomId;
          if (roomId && sceneWaitingRooms.has(roomId)) {
            const room = sceneWaitingRooms.get(roomId)!;
            startRoomGameIfFull(room);
          }
          return;
        }

        if (msg.type === 'scene_fill_bots' || msg.type === 'scene_quick_match') {
          console.warn(`⛔ [纯真人防假人] 拦截人机补位请求 (${msg.type})，当前房间为真实玩家匹配房间，严禁人机补位！`);
          return;
        }

        if (msg.type === 'scene_join') {
          const gType = msg.gameType || 'yansan';
          const sMode = msg.sceneMode || 'standard';
          const sName = msg.sceneName || '经典场';
          const maxP = Number(msg.maxPlayers) || (gType === 'shisanshui' && sMode.includes('eight') ? 8 : 4);
          const p: ScenePlayer = msg.player || {
            id: `P-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
            name: '牌友',
            avatar: '👑',
            rating: 1900,
            coins: 2000,
            seat: 0,
          };

          const rawId = p.id;
          if (!p.id || p.id === 'P-me' || p.id === 'P-123') {
            p.id = `P-usr-${(ws as any).socketId || Date.now()}`;
            (p as any).originalId = rawId || 'P-me';
          }
          delete (p as any).isSimulated;
          delete (p as any).isAi;
          delete (p as any).isBot;

          if (msg.sessionToken && rawId) {
            verifyWsSession(ws, rawId, msg.sessionToken);
          }

          try {
            const db = loadDatabase();
            const prof = db.profiles?.[rawId] || db.profiles?.[p.id];
            if (prof) {
              if (prof.isBanned) {
                ws.send(
                  JSON.stringify({
                    type: 'scene_join_rejected',
                    reason: 'banned',
                    text: `该账号已被管理员封禁：${prof.bannedReason || '违规操作'}`,
                  })
                );
                return;
              }
              if (prof.sessionToken && msg.sessionToken && (ws as any).authenticatedUserId !== prof.id) {
                ws.send(
                  JSON.stringify({
                    type: 'scene_join_rejected',
                    reason: 'invalid_session',
                    text: '登录会话已失效，请重新登录后再入座！',
                  })
                );
                return;
              }
              if (!prof.sessionToken || (ws as any).authenticatedUserId === prof.id) {
                p.coins = typeof prof.coins === 'number' ? prof.coins : 0;
                p.rating = typeof prof.rating === 'number' ? prof.rating : p.rating;
                p.name = prof.name || p.name;
                p.avatar = prof.avatar || p.avatar;
              }
            }
          } catch {}

          const reqCoins = getSceneRequiredCoins(gType, sMode);
          const currentCoins = typeof p.coins === 'number' && !isNaN(p.coins) ? p.coins : 0;
          if (currentCoins < reqCoins || currentCoins <= 0) {
            presenceTracker.updateClientGame((ws as any).socketId, 'lobby', '');
            ws.send(
              JSON.stringify({
                type: 'scene_join_rejected',
                reason: 'insufficient_coins',
                requiredCoins: reqCoins,
                currentCoins,
                text: `您的积分不足 (${currentCoins}/${reqCoins} 分)，无法入座此分场！`,
              })
            );
            broadcast({
              type: 'presence_counts',
              counts: presenceTracker.getRealGamePresence(),
            });
            broadcastTeahousePlayers();
            return;
          }

          const currentSocketId = (ws as any).socketId;
          for (const [otherId, otherRoom] of sceneWaitingRooms.entries()) {
            const isPlayerInOther = otherRoom.players.some(
              (pl) => (pl.id && p.id && pl.id === p.id) || (pl as any).socketId === currentSocketId
            );
            if (!isPlayerInOther) continue;

            const isSameScene = otherRoom.gameType === gType && otherRoom.sceneMode === sMode;
            const isOtherFinished = otherRoom.status === 'finished' || rooms.get(otherId)?.boardState?.phase === 'ended';

            if (!isSameScene || isOtherFinished) {
              if (!isOtherFinished && (otherRoom.status === 'playing' || otherRoom.status === 'starting')) {
                const matchedOld = otherRoom.players.find(
                  (pl) => (pl.id && p.id && pl.id === p.id) || (pl as any).socketId === currentSocketId
                );
                if (matchedOld) {
                  matchedOld.isDisconnected = true;
                  matchedOld.isAutoManaged = true;
                }
              } else {
                otherRoom.players = filterHumanScenePlayers(
                  otherRoom.players.filter(
                    (pl) => pl.id !== p.id && (pl as any).socketId !== currentSocketId
                  )
                );
                if (otherRoom.players.length === 0) {
                  sceneWaitingRooms.delete(otherId);
                  if (isOtherFinished) rooms.delete(otherId);
                } else {
                  if (!isOtherFinished) {
                    otherRoom.players.forEach((pl, idx) => {
                      pl.seat = idx;
                    });
                    otherRoom.status = 'waiting';
                    sendSanitizedToRoom(otherRoom, () => ({
                      type: 'scene_room_update',
                      roomId: otherRoom.id,
                      gameType: otherRoom.gameType,
                      sceneMode: otherRoom.sceneMode,
                      players: otherRoom.players,
                      maxPlayers: otherRoom.maxPlayers,
                      status: otherRoom.status,
                    }));
                  }
                }
              }
            }
          }

          let room: SceneWaitingRoom | undefined;
          for (const r of sceneWaitingRooms.values()) {
            if (r.gameType === gType && r.sceneMode === sMode && (r.status === 'waiting' || r.status === 'starting' || r.status === 'playing')) {
              if (r.players.some((pl) => (pl as any).socketId === currentSocketId)) {
                room = r;
                break;
              }
            }
          }

          if (!room && p.id) {
            for (const r of sceneWaitingRooms.values()) {
              if (r.gameType === gType && r.sceneMode === sMode && r.status === 'waiting') {
                const matchedPlayer = r.players.find((pl) => pl.id === p.id);
                if (matchedPlayer) {
                  let oldSocketAlive = false;
                  for (const client of wss.clients) {
                    if ((client as any).socketId === (matchedPlayer as any).socketId && client.readyState === WebSocket.OPEN) {
                      oldSocketAlive = true;
                      break;
                    }
                  }
                  if (!oldSocketAlive && r.players.length < r.maxPlayers) {
                    room = r;
                    break;
                  }
                }
              }
            }
          }

          if (!room) {
            room = pickBestWaitingRoom(sceneWaitingRooms.values(), gType, sMode);
          }

          if (!room) {
            const id = `SCENE-${gType}-${sMode}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
            room = {
              id,
              gameType: gType,
              sceneMode: sMode,
              sceneName: sName,
              maxPlayers: maxP,
              players: [],
              status: 'waiting',
              createdAt: Date.now(),
            };
            sceneWaitingRooms.set(id, room);
          }

          room.players = filterHumanScenePlayers(room.players);

          let existingIdx = room.players.findIndex((pl) => (pl as any).socketId === currentSocketId);
          if (existingIdx === -1 && p.id) {
            const sameIdPlayer = room.players.find((pl) => pl.id === p.id);
            if (sameIdPlayer) {
              let oldSocketAlive = false;
              for (const client of wss.clients) {
                if ((client as any).socketId === (sameIdPlayer as any).socketId && client.readyState === WebSocket.OPEN) {
                  oldSocketAlive = true;
                  break;
                }
              }
              if (oldSocketAlive) {
                p.originalId = sameIdPlayer.originalId || sameIdPlayer.id || p.id;
                p.originalName = sameIdPlayer.originalName || sameIdPlayer.name || p.name;
                p.id = `${p.id}_tab_${currentSocketId}`;
                p.name = `${p.name} #${room.players.length + 1}`;
              } else {
                existingIdx = room.players.indexOf(sameIdPlayer);
              }
            }
          }

          if (existingIdx === -1 && (room.players.length >= room.maxPlayers || room.status === 'playing' || room.status === 'starting' || room.status === 'finished')) {
            const newRoomId = `SCENE-${gType}-${sMode}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
            room = {
              id: newRoomId,
              gameType: gType,
              sceneMode: sMode,
              sceneName: sName,
              maxPlayers: maxP,
              players: [],
              status: 'waiting',
              createdAt: Date.now(),
            };
            sceneWaitingRooms.set(newRoomId, room);
            existingIdx = -1;
          }

          if (existingIdx !== -1) {
            room.players[existingIdx] = { ...room.players[existingIdx], ...p, socketId: currentSocketId, isReady: true } as any;
          } else if (room.players.length < room.maxPlayers) {
            const usedSeats = new Set(room.players.map((pl) => pl.seat));
            let freeSeat = 0;
            while (usedSeats.has(freeSeat) && freeSeat < room.maxPlayers) freeSeat++;
            p.seat = freeSeat;
            (p as any).socketId = currentSocketId;
            (p as any).isReady = true;
            room.players.push(p);
          }

          (ws as any).sceneRoomId = room.id;
          (ws as any).scenePlayerId = p.id;

          presenceTracker.updateClientGame((ws as any).socketId, gType, sMode);
          presenceTracker.recordClient((ws as any).socketId, {
            gameType: gType,
            playerId: rawId || p.id,
            playerName: p.originalName || p.name,
            sceneMode: sMode,
          });

          const isFullStarted = startRoomGameIfFull(room);

          try {
            ws.send(
              JSON.stringify({
                type: 'scene_join_ack',
                roomId: room.id,
                gameType: room.gameType,
                sceneMode: room.sceneMode,
                player: p,
                assignedPlayerId: p.id,
                assignedSeat: p.seat,
                players: room.players,
                maxPlayers: room.maxPlayers,
                status: room.status,
                deal: (room as any).deal ? sanitizeDealForSeat(room.gameType, (room as any).deal, p.seat) : undefined,
              })
            );
          } catch {}

          if (!isFullStarted) {
            room.status = 'waiting';
            delete (room as any).deal;
            sendSanitizedToRoom(room, () => ({
              type: 'scene_room_update',
              roomId: room.id,
              gameType: room.gameType,
              sceneMode: room.sceneMode,
              players: room.players,
              maxPlayers: room.maxPlayers,
              status: room.status,
            }));
          }
          scheduleActiveRoomsSnapshot();

          broadcast({
            type: 'presence_counts',
            counts: presenceTracker.getRealGamePresence(),
          });
          broadcastTeahousePlayers();
          return;
        }

        if (msg.type === 'scene_leave') {
          presenceTracker.updateClientGame((ws as any).socketId, 'lobby');
          const currentSocketId = (ws as any).socketId;
          const leavePlayerId = msg.playerId || (ws as any).scenePlayerId;
          const leavePlayerName = msg.playerName;
          const targetRoomId = msg.roomId || (ws as any).sceneRoomId;

          const findLeavingPlayer = (playersList: any[]) => {
            if (!Array.isArray(playersList)) return undefined;
            return (
              playersList.find((pl) => (pl as any).socketId && (pl as any).socketId === currentSocketId) ||
              (leavePlayerId
                ? playersList.find((pl) => pl.id === leavePlayerId || (pl as any).originalId === leavePlayerId)
                : undefined) ||
              (leavePlayerName ? playersList.find((pl) => pl.name === leavePlayerName) : undefined)
            );
          };

          for (const [id, room] of sceneWaitingRooms.entries()) {
            const leavingPl = findLeavingPlayer(room.players);
            const isTargetRoom = targetRoomId ? id === targetRoomId : Boolean(leavingPl);

            if (isTargetRoom && leavingPl) {
              const authRoom = rooms.get(id);
              const isShisanshuiReservation = room.gameType === 'shisanshui' && room.sceneMode === 'reservation';
              const isMatchActive =
                (room.status === 'starting' || room.status === 'playing') &&
                !isShisanshuiReservation &&
                authRoom?.boardState?.phase !== 'ended';

              if (isMatchActive) {
                const plSeat = leavingPl.seat;
                (leavingPl as any).isDisconnected = true;
                (leavingPl as any).isAutoManaged = true;
                delete (leavingPl as any).socketId;

                if (authRoom) {
                  const aPl = (authRoom.players || []).find(
                    (p: any) => p.id === leavingPl.id || (p.socketId && p.socketId === currentSocketId)
                  );
                  if (aPl) {
                    (aPl as any).isDisconnected = true;
                    (aPl as any).isAutoManaged = true;
                    delete (aPl as any).socketId;
                  }
                  if (!authRoom.boardState) {
                    authRoom.boardState = createDefaultBoardState(authRoom.id, authRoom.gameType);
                  }
                  if (!authRoom.boardState.autoManagedSeats) authRoom.boardState.autoManagedSeats = {};
                  authRoom.boardState.autoManagedSeats[plSeat] = true;
                  if (authRoom.turnSeat === plSeat) {
                    authRoom.lastTurnTs = Date.now() - 2000;
                  }
                }

                sendSanitizedToRoom(room, () => ({
                  type: 'scene_room_update',
                  roomId: room.id,
                  gameType: room.gameType,
                  sceneMode: room.sceneMode,
                  players: room.players,
                  maxPlayers: room.maxPlayers,
                  status: room.status,
                }));
                sendSanitizedToRoom(room, (rSeat) => ({
                  type: 'game_event',
                  roomId: room.id,
                  seat: plSeat,
                  event: {
                    action: 'set_auto_host',
                    seat: plSeat,
                    absSeat: plSeat,
                    isAutoManaged: true,
                    isAutoHost: true,
                    isDisconnected: true,
                    playerName: leavingPl.name,
                  },
                  payload: {
                    action: 'set_auto_host',
                    seat: plSeat,
                    absSeat: plSeat,
                    isAutoManaged: true,
                    isAutoHost: true,
                    isDisconnected: true,
                    playerName: leavingPl.name,
                  },
                  boardState: sanitizeBoardStateForSeat(
                    authRoom ? authRoom.boardState : (room as any).boardState,
                    room.gameType,
                    rSeat
                  ),
                }));
              } else {
                const wasFinishedSettlement = room.status === 'finished' || authRoom?.boardState?.phase === 'ended';
                room.players = room.players.filter((pl) => pl !== leavingPl);
                if ((ws as any).sceneRoomId === id) {
                  delete (ws as any).sceneRoomId;
                  delete (ws as any).scenePlayerId;
                }

                if (room.players.length === 0) {
                  sceneWaitingRooms.delete(id);
                  rooms.delete(id);
                } else if (wasFinishedSettlement) {
                  if (authRoom) {
                    authRoom.players = (authRoom.players || []).filter(
                      (ap: any) => ap.id !== leavingPl.id && (!ap.socketId || ap.socketId !== currentSocketId)
                    );
                  }
                  sendSanitizedToRoom(room, () => ({
                    type: 'scene_room_update',
                    roomId: room.id,
                    gameType: room.gameType,
                    sceneMode: room.sceneMode,
                    players: room.players,
                    maxPlayers: room.maxPlayers,
                    status: room.status,
                    settlementExpiresAt: (room as any).settlementExpiresAt || authRoom?.settlementExpiresAt || null,
                    serverTs: Date.now(),
                  }));
                } else {
                  room.players.forEach((pl, idx) => {
                    pl.seat = idx;
                  });
                  if (room.players.length < room.maxPlayers) {
                    room.status = 'waiting';
                    delete (room as any).deal;
                  }
                  if (authRoom) {
                    authRoom.players = room.players.map((pl, idx) => ({ ...pl, seat: idx }));
                    authRoom.status = room.status;
                    if (room.status === 'waiting') {
                      delete (authRoom as any).deal;
                    }
                  }
                  sendSanitizedToRoom(room, () => ({
                    type: 'scene_room_update',
                    roomId: room.id,
                    gameType: room.gameType,
                    sceneMode: room.sceneMode,
                    players: room.players,
                    maxPlayers: room.maxPlayers,
                    status: room.status,
                  }));
                }
              }
              scheduleActiveRoomsSnapshot();
            }
          }
          if (msg.roomId && rooms.has(msg.roomId) && !sceneWaitingRooms.has(msg.roomId)) {
            const r = rooms.get(msg.roomId);
            if (r && r.status !== 'playing' && r.status !== 'starting') {
              const rLeave = findLeavingPlayer(r.players);
              if (rLeave) {
                r.players = r.players.filter((pl: any) => pl !== rLeave);
                if (r.players.length === 0) {
                  rooms.delete(msg.roomId);
                } else {
                  r.status = 'waiting';
                  delete (r as any).deal;
                }
              }
            }
          }
          for (let i = activeChallenges.length - 1; i >= 0; i--) {
            const ch = activeChallenges[i];
            if (
              (ch.senderId === msg.playerId || ch.senderName === msg.playerName) &&
              (!msg.sceneMode || ch.sceneMode === msg.sceneMode) &&
              !ch.acceptedBy
            ) {
              const removedId = ch.id;
              activeChallenges.splice(i, 1);
              broadcast({
                type: 'challenge_status_update',
                challengeId: removedId,
                status: 'removed',
                senderId: msg.playerId,
                senderName: msg.playerName,
                gameType: ch.gameType,
                sceneMode: ch.sceneMode,
              });
            }
          }
          broadcast({
            type: 'presence_counts',
            counts: presenceTracker.getRealGamePresence(),
          });
          broadcastTeahousePlayers();
          return;
        }

        if (msg.type === 'scene_chat') {
          if (!allowSocketChat(ws)) return;
          const safeAudio = typeof msg.audio === 'string' && msg.audio.length <= 450_000 ? msg.audio : null;
          const safeChatText = sanitizeWsText(msg.text, 300);
          if (!safeChatText && !safeAudio) return;

          const isChallenge = msg.action === 'challenge';
          const targetChannel = isChallenge ? 'hall' : msg.channel || 'hall';

          if (isChallenge && msg.gameType && msg.sceneMode) {
            const senderId = msg.senderId || 'P-guest';
            const senderName = sanitizeWsText(msg.sender, 24) || '牌友';

            const previousUserChallenges = activeChallenges.filter(
              (c) =>
                (c.senderId === senderId || c.senderName === senderName) &&
                c.status !== 'cancelled' &&
                c.status !== 'removed'
            );

            for (const oldCh of previousUserChallenges) {
              if (oldCh.acceptedBy) {
                const newHost = oldCh.acceptedBy;
                oldCh.senderId = newHost.id;
                oldCh.senderName = newHost.name;
                oldCh.status = 'transferred';
                oldCh.text = `⚔️ 在【${oldCh.sceneName}】由 [${newHost.name}] 继承守擂：“谁来与我一战到底！”`;
                const oldAcceptedHost = { ...newHost };
                oldCh.acceptedBy = undefined;

                broadcast({
                  type: 'challenge_status_update',
                  challengeId: oldCh.id,
                  status: 'transferred',
                  newSenderId: oldAcceptedHost.id,
                  newSenderName: oldAcceptedHost.name,
                  gameType: oldCh.gameType,
                  sceneMode: oldCh.sceneMode,
                  sceneName: oldCh.sceneName,
                  text: oldCh.text,
                });

                broadcast({
                  type: 'scene_chat_broadcast',
                  gameType: oldCh.gameType,
                  sceneMode: oldCh.sceneMode,
                  sceneName: oldCh.sceneName,
                  channel: 'hall',
                  sender: '🤖 牌楼官播 BOT',
                  senderId: 'BOT-SYSTEM',
                  avatar: '🤖',
                  text: `⚔️【擂台过继】牌友 [${senderName}] 已移步至新场景，原【${oldCh.sceneName}】战帖已顺位过继由 [${oldAcceptedHost.name}] 继承守擂！`,
                  action: 'notice',
                  timestamp: new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
                });
              } else {
                const rmIdx = activeChallenges.findIndex((c) => c.id === oldCh.id);
                if (rmIdx !== -1) {
                  activeChallenges.splice(rmIdx, 1);
                }

                broadcast({
                  type: 'challenge_status_update',
                  challengeId: oldCh.id,
                  status: 'removed',
                  senderId,
                  senderName,
                  gameType: oldCh.gameType,
                  sceneMode: oldCh.sceneMode,
                });
              }
            }

            const newCh: ActiveChallengeRecord = {
              id: msg.challengeId || `ch-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
              senderId,
              senderName,
              avatar: sanitizeWsText(msg.avatar, 12) || '👑',
              gameType: msg.gameType,
              sceneMode: msg.sceneMode,
              sceneName: sanitizeWsText(msg.sceneName, 32) || '经典分场',
              text: safeChatText ? safeChatText.slice(0, 200) : '摆下战帖，谁敢来与我一战到底！',
              status: 'open',
              timestamp: msg.timestamp || new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
              createdAt: Date.now(),
            };
            activeChallenges.unshift(newCh);
            if (activeChallenges.length > 30) activeChallenges.pop();

            msg.challengeId = newCh.id;
          }

          broadcast({
            type: 'scene_chat_broadcast',
            challengeId: msg.challengeId || null,
            gameType: msg.gameType,
            sceneMode: msg.sceneMode,
            sceneName: sanitizeWsText(msg.sceneName, 32) || null,
            channel: targetChannel,
            sender: sanitizeWsText(msg.sender, 24) || '牌友',
            senderId: msg.senderId || null,
            avatar: sanitizeWsText(msg.avatar, 12) || '👑',
            text: safeChatText || '🎤 [等候室实时语音]',
            voice: Boolean(msg.voice && safeAudio) || Boolean(msg.voice && msg.phraseText),
            phraseText: msg.phraseText ? sanitizeWsText(msg.phraseText, 200) : null,
            roleId: msg.roleId || null,
            roleName: msg.roleName ? sanitizeWsText(msg.roleName, 24) : null,
            pitch: typeof msg.pitch === 'number' ? msg.pitch : 1.0,
            rate: typeof msg.rate === 'number' ? msg.rate : 1.0,
            isFemale: Boolean(msg.isFemale),
            audio: safeAudio,
            duration: Math.max(0, Math.min(30, Number(msg.duration) || 0)),
            action: msg.action || null,
            targetPlayerId: msg.targetPlayerId || null,
            targetPlayerName: msg.targetPlayerName ? sanitizeWsText(msg.targetPlayerName, 24) : null,
            giftAmount: Number(msg.giftAmount) || 0,
            timestamp: msg.timestamp || new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
            activeChallenges: activeChallenges.slice(0, 10),
          });
          return;
        }

        if (msg.type === 'scene_accept_challenge') {
          const found = activeChallenges.find(
            (c) => c.id === msg.challengeId || (c.senderName === msg.challengerName && c.sceneMode === msg.sceneMode && c.status === 'open')
          );
          if (found) {
            found.status = 'accepted';
            found.acceptedBy = { id: msg.acceptorId || 'guest', name: sanitizeWsText(msg.acceptorName, 24) || '热血牌友' };

            broadcast({
              type: 'challenge_status_update',
              challengeId: found.id,
              status: 'accepted',
              acceptedBy: found.acceptedBy,
            });
          }

          if (msg.gameType && msg.sceneMode) {
            presenceTracker.updateClientGame((ws as any).socketId, msg.gameType, msg.sceneMode);
            if (msg.acceptorId) {
              presenceTracker.recordClient((ws as any).socketId, {
                gameType: msg.gameType,
                playerId: msg.acceptorId,
                playerName: sanitizeWsText(msg.acceptorName, 24),
                sceneMode: msg.sceneMode,
              });
            }
            broadcast({
              type: 'presence_counts',
              counts: presenceTracker.getRealGamePresence(),
            });
            broadcastTeahousePlayers();
          }

          broadcast({
            type: 'scene_chat_broadcast',
            gameType: msg.gameType,
            sceneMode: msg.sceneMode,
            sceneName: sanitizeWsText(msg.sceneName, 32),
            channel: 'hall',
            sender: '🤖 牌楼官播 BOT',
            senderId: 'BOT-SYSTEM',
            avatar: '🤖',
            text: `⚔️【应战捷报】牌友 [${sanitizeWsText(msg.acceptorName, 24) || '热血牌友'}] 已成功响应 [${sanitizeWsText(msg.challengerName, 24) || '牌友'}] 在【${sanitizeWsText(msg.sceneName, 32) || '经典分场'}】的擂台战帖，已切入相同桌台！`,
            action: 'notice',
            timestamp: new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
            activeChallenges: activeChallenges.slice(0, 10),
          });
          return;
        }

        if (msg.type === 'room_create') {
          const pId = msg.playerId || (ws as any).authenticatedUserId || `P-${Date.now()}`;
          if (msg.sessionToken && msg.playerId) {
            verifyWsSession(ws, msg.playerId, msg.sessionToken);
          }
          try {
            const db = loadDatabase();
            const prof = db.profiles?.[pId];
            if (prof?.isBanned) return;
          } catch {}
          const id = `RM-${Math.floor(207 + Math.random() * 800)}`;
          const gType = msg.gameType || 'yansan';
          const cleanPasscode = typeof msg.passcode === 'string' ? msg.passcode.trim().slice(0, 6) : '';
          const safePlayerName = sanitizeWsText(msg.playerName, 24) || '茗局雅客';
          const safeTitle = sanitizeWsText(msg.title, 40) || `${safePlayerName}的牌局`;
          const newRoom: any = {
            id,
            title: safeTitle,
            gameType: gType,
            status: 'playing',
            hasPasscode: Boolean(cleanPasscode),
            _passcode: cleanPasscode,
            players: [{ id: pId, name: safePlayerName, rating: 1900, seat: 0, socketId: (ws as any).socketId }],
            spectators: 1,
            turnSeat: 0,
            moveCount: 0,
            modeLabel: cleanPasscode ? '私密口令牌桌' : '实时 WebSocket 牌桌',
            chat: [
              {
                id: `c-${Date.now()}`,
                sender: '系统司仪',
                text: `房间 ${id} 已建立${cleanPasscode ? '（已设私密口令）' : ''}，欢迎各路牌友入席！`,
                timestamp: new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
              },
            ],
            boardState: createDefaultBoardState(id, gType),
          };
          rooms.set(id, newRoom);
          const safeRooms = Array.from(rooms.values()).map(({ _passcode: _p, ...rest }: any) => rest);
          const { _passcode: _pSelf, ...safeNewRoom } = newRoom;
          broadcast({ type: 'room_created', room: safeNewRoom, rooms: safeRooms });
          return;
        }

        if (msg.type === 'room_join') {
          const room: any = rooms.get(msg.roomId);
          if (room) {
            if (room._passcode && String(msg.passcode || '').trim() !== room._passcode) {
              ws.send(
                JSON.stringify({
                  type: 'room_join_rejected',
                  roomId: room.id,
                  reason: 'invalid_passcode',
                  error: '私密房间口令不正确，请核对后再试',
                })
              );
              return;
            }
            const playerName = msg.playerName || '牌友';
            const pId = msg.playerId || (ws as any).authenticatedUserId || `P-${Date.now()}`;
            const existingPlayer = room.players.find((p: any) => p.id === pId || p.name === playerName);
            if (!existingPlayer && room.players.length < 4) {
              room.players.push({
                id: pId,
                name: playerName,
                rating: msg.rating || 1900,
                seat: room.players.length,
                socketId: (ws as any).socketId,
              });
            } else {
              if (existingPlayer) existingPlayer.socketId = (ws as any).socketId;
              room.spectators += 1;
            }
            const safeRooms = Array.from(rooms.values()).map(({ _passcode: _p, ...rest }: any) => rest);
            const { _passcode: _pSelf, ...safeRoom } = room;
            broadcast({ type: 'room_updated', room: safeRoom, rooms: safeRooms });
          }
          return;
        }

        if (msg.type === 'room_leave') {
          const room: any = rooms.get(msg.roomId);
          if (room) {
            room.spectators = Math.max(0, room.spectators - 1);
            const safeRooms = Array.from(rooms.values()).map(({ _passcode: _p, ...rest }: any) => rest);
            const { _passcode: _pSelf, ...safeRoom } = room;
            broadcast({ type: 'room_updated', room: safeRoom, rooms: safeRooms });
          }
          return;
        }

        if (msg.type === 'room_reconnect') {
          if (msg.sessionToken && msg.playerId) {
            verifyWsSession(ws, msg.playerId, msg.sessionToken);
          }
          const room: any = rooms.get(msg.roomId);
          if (room) {
            try {
              const pId = msg.playerId || (ws as any).authenticatedUserId || (ws as any).scenePlayerId;
              const matched =
                room.players?.find(
                  (p: any) =>
                    p.socketId === (ws as any).socketId ||
                    (pId && (p.id === pId || p.originalId === pId || p.userId === pId))
                ) || (msg.playerName ? room.players?.find((p: any) => p.name === msg.playerName) : undefined);
              if (matched) {
                if (msg.sessionToken) {
                  const db = loadDatabase();
                  const prof = db.profiles?.[matched.id];
                  if (prof && prof.sessionToken && !safeTokenEqual(prof.sessionToken, msg.sessionToken)) {
                    ws.send(JSON.stringify({ type: 'reconnect_ack', failed: true, reason: 'invalid_session' }));
                    return;
                  }
                }
                matched.socketId = (ws as any).socketId;
                delete matched.isDisconnected;
                (ws as any).sceneRoomId = room.id;
                (ws as any).scenePlayerId = matched.id;
              }
              const seat = matched && typeof matched.seat === 'number' ? matched.seat : -1;
              const { deal: _d, _passcode: _pSelf, ...roomWithoutDeal } = room;
              ws.send(
                JSON.stringify({
                  type: 'reconnect_ack',
                  room: {
                    ...roomWithoutDeal,
                    deal: room.deal ? sanitizeDealForSeat(room.gameType, room.deal, seat) : undefined,
                  },
                  boardState: sanitizeBoardStateForSeat(room.boardState, room.gameType, seat),
                  serverTs: Date.now(),
                })
              );
            } catch {}
          }
          return;
        }

        if (msg.type === 'request_game_deal') {
          const targetRoomId = msg.roomId;
          const r = (sceneWaitingRooms.get(targetRoomId) as any) || rooms.get(targetRoomId);
          if (r) {
            if (!r.deal) {
              r.deal = generateRoomDeal(r.gameType, r.sceneMode, r.maxPlayers);
            }
            const pId = msg.playerId || (ws as any).scenePlayerId;
            const matched = r.players?.find(
              (pl: any) => pl.socketId === (ws as any).socketId || (pId && pl.id === pId)
            );
            const seat = matched && typeof matched.seat === 'number' ? matched.seat : (typeof msg.seat === 'number' ? msg.seat : -1);
            ws.send(
              JSON.stringify({
                type: 'game_deal',
                roomId: targetRoomId,
                gameType: r.gameType,
                sceneMode: r.sceneMode,
                deal: sanitizeDealForSeat(r.gameType, r.deal, seat),
              })
            );
          }
          return;
        }

        if (msg.type === 'game_move') {
          const targetRoomId = msg.roomId;
          const room = rooms.get(targetRoomId) || (sceneWaitingRooms.get(targetRoomId) as any);
          if (room) {
            const evt = msg.event || {};
            const p = { ...evt, ...(msg.payload || {}) };
            const action = p.action || evt.action || 'play';
            const combo = p.combo || evt.combo || null;

            const requestedSeat =
              typeof msg.seat === 'number'
                ? msg.seat
                : typeof evt.seat === 'number'
                  ? evt.seat
                  : typeof p.seat === 'number'
                    ? p.seat
                    : 0;
            const wsSocketId = (ws as any).socketId;
            const wsPlayerId = (ws as any).authenticatedUserId || (ws as any).scenePlayerId;
            const senderPlayer = Array.isArray(room.players)
              ? room.players.find(
                  (pl: any) =>
                    (pl.socketId && wsSocketId && pl.socketId === wsSocketId) ||
                    (pl.id && wsPlayerId && pl.id === wsPlayerId)
                )
              : undefined;

            const mutatingActions = new Set([
              'play',
              'pass',
              'draw',
              'discard',
              'peng',
              'gang',
              'hu',
              'draw_stalemate',
              'finish',
              'qianggong',
              'surrender',
              'shisanshui_submit_hand',
            ]);

            if (!senderPlayer && mutatingActions.has(action)) {
              return;
            }

            const seat = senderPlayer && typeof senderPlayer.seat === 'number' ? senderPlayer.seat : requestedSeat;

            if (!room.boardState) {
              room.boardState = createDefaultBoardState(room.id, room.gameType);
            }
            const bs = room.boardState;

            if (bs.phase === 'ended' && mutatingActions.has(action)) {
              return;
            }

            if (action === 'set_auto_host') {
              const isAuto = Boolean(p.isAutoManaged ?? p.isAutoHost);
              const targetPlayer = room.players?.find((pl: any, idx: number) => (typeof pl?.seat === 'number' ? pl.seat : idx) === seat);
              if (targetPlayer) {
                targetPlayer.isAutoManaged = isAuto;
              }
              const sRoom = sceneWaitingRooms.get(room.id);
              const targetScenePlayer = sRoom?.players?.find((pl: any, idx: number) => (typeof pl?.seat === 'number' ? pl.seat : idx) === seat);
              if (targetScenePlayer) {
                targetScenePlayer.isAutoManaged = isAuto;
              }
              if (!bs.autoManagedSeats) bs.autoManagedSeats = {};
              bs.autoManagedSeats[seat] = isAuto;
              if (isAuto && room.turnSeat === seat) {
                room.lastTurnTs = Date.now() - 2000;
              }
              sendSanitizedToRoom(room, (rSeat) => ({
                type: 'game_event',
                roomId: room.id,
                seat,
                event: { action: 'set_auto_host', seat, absSeat: seat, isAutoManaged: isAuto, isAutoHost: isAuto },
                payload: { action: 'set_auto_host', seat, absSeat: seat, isAutoManaged: isAuto, isAutoHost: isAuto },
                boardState: sanitizeBoardStateForSeat(bs, room.gameType, rSeat),
              }));
              scheduleActiveRoomsSnapshot();
              return;
            }

            if (room.gameType === 'mahjong') {
              handleMahjongGameMove(hubContext, ws, room, seat, action, combo, p, evt);
              scheduleActiveRoomsSnapshot();
              return;
            }

            if (room.gameType === 'yansan') {
              handleYansanGameMove(hubContext, room, seat, action, combo, p, ws);
              scheduleActiveRoomsSnapshot();
              return;
            }

            if (action === 'shisanshui_submit_hand') {
              handleShisanshuiGameMove(hubContext, ws, room, seat, p);
              scheduleActiveRoomsSnapshot();
              return;
            }

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

            if (p.action === 'finish' || (p.winner !== undefined && p.winner !== null)) {
              bs.winnerSeat = p.winner !== undefined && p.winner !== null ? Number(p.winner) : null;
              bs.phase = 'ended';
              room.status = 'finished';
              if (p.finishedSeats && Array.isArray(p.finishedSeats)) {
                bs.finishedSeats = [...p.finishedSeats];
              }
              if (p.allianceScoreResult) {
                bs.allianceScoreResult = p.allianceScoreResult;
                bs.settlement = p.allianceScoreResult;
              }
              resetSceneRoomReadyAndCleanDisconnected(room.id);
            }

            const resolvedNextTurn = p.nextTurn !== undefined ? Number(p.nextTurn) : bs.turnSeat;
            sendSanitizedToRoom(room, (rSeat) => ({
              type: 'game_event',
              roomId: room.id,
              seat,
              event: {
                action,
                seat,
                combo,
                nextTurn: resolvedNextTurn,
                winner: bs.winnerSeat ?? p.winner,
                finishedSeats: bs.finishedSeats || p.finishedSeats,
                allianceScoreResult: bs.allianceScoreResult || p.allianceScoreResult,
                ...p,
              },
              payload: {
                action,
                seat,
                combo,
                nextTurn: resolvedNextTurn,
                winner: bs.winnerSeat ?? p.winner,
                finishedSeats: bs.finishedSeats || p.finishedSeats,
                allianceScoreResult: bs.allianceScoreResult || p.allianceScoreResult,
                ...p,
              },
              moveCount: room.moveCount,
              boardState: sanitizeBoardStateForSeat(bs, room.gameType, rSeat),
            }));
            scheduleActiveRoomsSnapshot();
          }
          return;
        }

        if (msg.type === 'game_chat' || msg.type === 'room_voice') {
          if (!allowSocketChat(ws)) return;
          const targetRoomId = msg.roomId || 'RM-202';
          const room = rooms.get(targetRoomId);
          const safeAudio = typeof msg.audio === 'string' && msg.audio.length <= 450_000 ? msg.audio : null;
          const safeText = sanitizeWsText(msg.text, 120);
          if (safeText || safeAudio) {
            const chatMsg = {
              id: `chat-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
              sender: sanitizeWsText(msg.sender, 24) || '牌友',
              senderId: msg.senderId || null,
              avatar: sanitizeWsText(msg.avatar, 12) || '👤',
              text: safeText || (safeAudio ? '🎤 [牌桌实时语音]' : ''),
              prop: msg.prop || null,
              targetSeat: msg.targetSeat !== undefined ? msg.targetSeat : null,
              voice: Boolean(msg.voice && safeAudio),
              audio: safeAudio,
              duration: Math.max(0, Math.min(30, Number(msg.duration) || 0)),
              timestamp: new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
            };
            if (room) {
              room.chat = [...room.chat.slice(-49), chatMsg];
              sendSanitizedToRoom(room, () => ({ type: 'chat_broadcast', roomId: targetRoomId, message: chatMsg }));
            } else {
              broadcast({ type: 'chat_broadcast', roomId: targetRoomId, message: chatMsg });
            }
          }
          return;
        }
      } catch {}
    });
  });

  // Periodic WebSocket zombie client heartbeat sweep
  setInterval(() => {
    const now = Date.now();
    for (const client of wss.clients) {
      const wsAny = client as any;
      if (wsAny.lastHeartbeat && now - wsAny.lastHeartbeat > 60000) {
        client.terminate();
      } else {
        wsAny.isAlive = false;
        try {
          client.ping();
        } catch {}
      }
    }
  }, 25000);

  // Authoritative AFK & Settlement Timeout Watchdog
  setInterval(() => {
    const now = Date.now();
    for (const room of rooms.values()) {
      if ((room.status === 'finished' || room.boardState?.phase === 'ended') && room.settlementExpiresAt && now >= room.settlementExpiresAt) {
        const sceneRoom = sceneWaitingRooms.get(room.id);
        const currentPlayers = sceneRoom?.players || room.players || [];
        const timedOutPlayers = currentPlayers.filter((p: any) => !isBotLikePlayer(p) && !p.isReady);
        const readyPlayers = currentPlayers.filter((p: any) => !isBotLikePlayer(p) && p.isReady);
        const timedOutIds = timedOutPlayers.map((p: any) => p.id);
        const timedOutSeats = timedOutPlayers.map((p: any) => p.seat);

        if (timedOutIds.length > 0) {
          console.log(`⏱️ [结算超时踢出] 房间 ${room.id} 结算倒计时结束，自动返回大厅玩家: ${timedOutIds.join(',')}`);
          sendSanitizedToRoom(room, () => ({
            type: 'settlement_timeout',
            roomId: room.id,
            gameType: room.gameType,
            timedOutPlayerIds: timedOutIds,
            timedOutSeats,
            message: '结算超时未点击继续下一局，已自动返回大厅',
          }));
        }

        delete room.settlementExpiresAt;
        if (sceneRoom) delete (sceneRoom as any).settlementExpiresAt;

        if (readyPlayers.length === 0) {
          sceneWaitingRooms.delete(room.id);
          rooms.delete(room.id);
        } else {
          const recycledPlayers = readyPlayers.map((p: any, idx: number) => ({
            ...p,
            seat: idx,
            isReady: true,
          }));
          if (sceneRoom) {
            sceneRoom.players = recycledPlayers;
            sceneRoom.status = 'waiting';
            delete (sceneRoom as any).deal;
          }
          room.players = recycledPlayers.map((p: any) => ({ ...p }));
          room.status = 'waiting';
          room.moveCount = 0;
          room.turnSeat = 0;
          delete room.deal;
          delete room.lastTurnTs;
          delete room._coinsSettledRoundKey;
          room.boardState = createDefaultBoardState(room.id, room.gameType);
          sendSanitizedToRoom(room, () => ({
            type: 'scene_room_update',
            roomId: room.id,
            gameType: room.gameType,
            sceneMode: room.sceneMode || sceneRoom?.sceneMode || '',
            players: room.players,
            maxPlayers: room.maxPlayers || sceneRoom?.maxPlayers || 4,
            status: 'waiting',
          }));
        }
        scheduleActiveRoomsSnapshot();
        broadcast({
          type: 'presence_counts',
          counts: presenceTracker.getRealGamePresence(),
        });
        broadcastTeahousePlayers();
        continue;
      }

      if (room.status !== 'playing' || !room.boardState || room.boardState.phase !== 'playing') continue;

      const prevSeq = room.boardState?.actionSeq || 0;
      const prevPhase = room.boardState?.phase;
      const prevMoveCount = room.moveCount || 0;

      if (room.gameType === 'shisanshui') {
        tickShisanshuiRoom(hubContext, room, now);
        if ((room.boardState?.actionSeq || 0) !== prevSeq || room.boardState?.phase !== prevPhase || (room.moveCount || 0) !== prevMoveCount) {
          scheduleActiveRoomsSnapshot();
        }
        continue;
      }

      if (tickMahjongClaimWindow(hubContext, room, now)) {
        if ((room.boardState?.actionSeq || 0) !== prevSeq || room.boardState?.phase !== prevPhase || (room.moveCount || 0) !== prevMoveCount) {
          scheduleActiveRoomsSnapshot();
        }
        continue;
      }

      const currentSeat = room.turnSeat || 0;
      const curPlayer = room.players?.find((p: any) => p.seat === currentSeat);
      const isPlayerAuto = Boolean(
        curPlayer?.isAutoManaged ||
          curPlayer?.isDisconnected ||
          isBotLikePlayer(curPlayer) ||
          (room.boardState?.autoManagedSeats && room.boardState.autoManagedSeats[currentSeat])
      );
      const isConnectedManualAuto = Boolean(isPlayerAuto && !curPlayer?.isDisconnected && !isBotLikePlayer(curPlayer));
      const mjDealForThreshold = (room as any).deal;
      const mjWallEmptyPendingDraw =
        room.gameType === 'mahjong' &&
        Array.isArray(mjDealForThreshold?.remainingWall) &&
        mjDealForThreshold.remainingWall.length === 0 &&
        !room.boardState.extraState?.pendingPengSeat;
      const baseTimeoutThreshold = isConnectedManualAuto ? 2600 : isPlayerAuto ? 1200 : 60000;
      const isYansanBaoDan =
        room.gameType === 'yansan' &&
        Array.isArray((room as any).deal?.seatCards?.[currentSeat]) &&
        (room as any).deal.seatCards[currentSeat].length === 1;
      const timeoutThreshold = mjWallEmptyPendingDraw
        ? Math.min(baseTimeoutThreshold, 3500)
        : isYansanBaoDan
          ? Math.min(baseTimeoutThreshold, 2500)
          : baseTimeoutThreshold;

      if (!room.lastTurnTs || now - room.lastTurnTs <= timeoutThreshold) continue;
      room.lastTurnTs = now;

      if (!isPlayerAuto && !mjWallEmptyPendingDraw) {
        if (curPlayer) curPlayer.isAutoManaged = true;
        if (!room.boardState.autoManagedSeats) room.boardState.autoManagedSeats = {};
        room.boardState.autoManagedSeats[currentSeat] = true;
        sendSanitizedToRoom(room, (rSeat) => ({
          type: 'game_event',
          roomId: room.id,
          seat: currentSeat,
          event: { action: 'set_auto_host', seat: currentSeat, absSeat: currentSeat, isAutoManaged: true, isAutoHost: true },
          payload: { action: 'set_auto_host', seat: currentSeat, absSeat: currentSeat, isAutoManaged: true, isAutoHost: true },
          boardState: sanitizeBoardStateForSeat(room.boardState, room.gameType, rSeat),
        }));
      }

      if (room.gameType === 'mahjong') {
        tickMahjongAutoTurn(hubContext, room, currentSeat, now);
        scheduleActiveRoomsSnapshot();
        continue;
      }

      tickYansanAutoTurn(hubContext, room, currentSeat, now);
      scheduleActiveRoomsSnapshot();
    }
  }, 1000);

  initServerBookingInventory()
    .then((info) => {
      console.log(
        `[ServerBooking] 十三张预约场预发牌库已就绪: 四人场 ${info.fourBookingStock} 节 / 八人场 ${info.eightBookingStock} 节`
      );
    })
    .catch((err) => {
      console.warn('[ServerBooking] 预发牌库初始化失败:', err);
    });

  setTimeout(() => {
    runServer0600SettlementSweep()
      .then((list) => {
        if (list.length > 0) emitBookingSettlementNotifications(list);
      })
      .catch(() => {});
  }, 5000);

  setInterval(() => {
    runServer0600SettlementSweep()
      .then((list) => {
        if (list.length > 0) emitBookingSettlementNotifications(list);
      })
      .catch(() => {});
  }, 30000);

  setInterval(() => {
    const now = Date.now();
    const defaultRoomIds = new Set(['RM-202', 'RM-204', 'RM-205']);
    for (const [id, room] of rooms.entries()) {
      if (!defaultRoomIds.has(id) && room.createdAt && now - room.createdAt > 3600000 && (room.spectators <= 1 || !room.players || room.players.length === 0)) {
        rooms.delete(id);
      }
    }
    for (const [id, sRoom] of sceneWaitingRooms.entries()) {
      if (sRoom.createdAt && now - sRoom.createdAt > 3600000 && (!sRoom.players || sRoom.players.length === 0)) {
        sceneWaitingRooms.delete(id);
      }
    }
  }, 600000);

  return broadcast;
}
