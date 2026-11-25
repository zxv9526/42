import type { WebSocket } from 'ws';
import { loadDatabase } from '../../serverStorage.ts';
import {
  ackServerUnreadSettlements,
  advanceServerBookingCarriage,
  getServerBookingState,
  pickServerBookingTable,
  runServer0600SettlementSweep,
  selectServerBookingSeat,
  simulateServerBookingSettlement,
  submitServerBookingHand,
  type SettledUserNotification,
} from '../../games/shisanshui/serverBookingService.ts';
import type { RealtimeHubContext } from '../realtimeTypes.ts';

export function emitBookingSettlementNotificationsHelper(
  broadcast: (payload: unknown) => void,
  notifications: SettledUserNotification[]
) {
  for (const item of notifications) {
    broadcast({
      type: 'user_coins_updated',
      userId: item.userId,
      coins: item.coins,
      delta: item.delta,
      rating: item.rating,
      level: item.level,
      source: 'booking_0600_settlement',
      sceneMode: item.record.sceneMode,
      carriageNumber: item.record.carriageNumber,
      settledTablesCount: item.record.settledTablesCount,
    });
    broadcast({
      type: 'booking_0600_settled',
      userId: item.userId,
      sceneMode: item.record.sceneMode,
      record: item.record,
      coins: item.coins,
      delta: item.delta,
    });
  }
}

export async function handleBookingWsMessage(
  ws: WebSocket,
  msg: any,
  ctx: RealtimeHubContext
): Promise<boolean> {
  const { broadcast, emitBookingSettlementNotifications } = ctx;

  const resolveBookingUserId = (rawId?: string, token?: string): { ok: boolean; userId: string; error?: string } => {
    const uid = rawId || (ws as any).authenticatedUserId || (ws as any).userId || 'guest';
    const db = loadDatabase();
    if (token && uid) {
      const prof = db.profiles?.[uid];
      if (prof && prof.sessionToken && prof.sessionToken === token && !prof.isBanned) {
        (ws as any).authenticatedUserId = prof.id;
        return { ok: true, userId: prof.id };
      }
    }
    if (uid && db.profiles?.[uid]?.password && (ws as any).authenticatedUserId !== uid) {
      return { ok: false, userId: uid, error: '账号会话校验未通过，请重新登录或刷新重连' };
    }
    return { ok: true, userId: (ws as any).authenticatedUserId || uid };
  };

  if (msg.type === 'booking_get_carriage') {
    const sceneMode = msg.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
    const authCheck = resolveBookingUserId(msg.playerId || msg.userId, msg.sessionToken);
    if (!authCheck.ok) {
      ws.send(JSON.stringify({ type: 'booking_error', action: 'get_carriage', error: authCheck.error, serverTs: Date.now() }));
      return true;
    }
    const userId = authCheck.userId;
    const state = await getServerBookingState({
      sceneMode,
      userId,
      playerName: msg.playerName || '茶客',
      avatar: msg.avatar || '👑',
      requestedCarriageId: msg.carriageId || undefined,
      requestedCarriageNumber: msg.carriageNumber ? Number(msg.carriageNumber) : undefined,
    });
    if (state.settledNotifications?.length) {
      emitBookingSettlementNotifications(state.settledNotifications);
    }
    ws.send(
      JSON.stringify({
        type: 'booking_carriage_state',
        sceneMode,
        carriage: state.carriage,
        archivedCarriages: state.archivedCarriages,
        settlementHistory: state.settlementHistory,
        unreadSettlements: state.unreadSettlements,
        serverTs: Date.now(),
      })
    );
    return true;
  }

  if (msg.type === 'booking_select_seat') {
    const sceneMode = msg.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
    const authCheck = resolveBookingUserId(msg.playerId || msg.userId, msg.sessionToken);
    if (!authCheck.ok) {
      ws.send(JSON.stringify({ type: 'booking_error', action: 'select_seat', error: authCheck.error, serverTs: Date.now() }));
      return true;
    }
    const userId = authCheck.userId;
    const res = await selectServerBookingSeat({
      sceneMode,
      userId,
      playerName: msg.playerName || '茶客',
      avatar: msg.avatar || '👑',
      seat: msg.seat || '东',
      carriageId: msg.carriageId || undefined,
      carriageNumber: msg.carriageNumber ? Number(msg.carriageNumber) : undefined,
    });
    if (!res.ok) {
      ws.send(
        JSON.stringify({
          type: 'booking_error',
          action: 'select_seat',
          error: res.error,
          carriage: (res as any).carriage || null,
          serverTs: Date.now(),
        })
      );
      return true;
    }
    ws.send(
      JSON.stringify({
        type: 'booking_seat_selected',
        sceneMode,
        carriage: res.carriage,
        archivedCarriages: res.archivedCarriages,
        activeTableIndex: res.carriage.currentActiveTableIndex,
        serverTs: Date.now(),
      })
    );
    broadcast({
      type: 'booking_carriage_occupants_updated',
      sceneMode,
      carriageId: res.carriageId,
      carriageNumber: res.carriageNumber,
      seatOccupants: res.rawSeatOccupants,
    });
    return true;
  }

  if (msg.type === 'booking_pick_table') {
    const sceneMode = msg.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
    const authCheck = resolveBookingUserId(msg.playerId || msg.userId, msg.sessionToken);
    if (!authCheck.ok) {
      ws.send(JSON.stringify({ type: 'booking_error', action: 'pick_table', error: authCheck.error, serverTs: Date.now() }));
      return true;
    }
    const userId = authCheck.userId;
    const res = await pickServerBookingTable({
      sceneMode,
      userId,
      carriageId: msg.carriageId || undefined,
      carriageNumber: msg.carriageNumber ? Number(msg.carriageNumber) : undefined,
      specificTableIndex: msg.tableIndex ? Number(msg.tableIndex) : undefined,
    });
    if (!res.ok) {
      ws.send(
        JSON.stringify({
          type: 'booking_error',
          action: 'pick_table',
          error: res.error,
          serverTs: Date.now(),
        })
      );
      return true;
    }
    ws.send(
      JSON.stringify({
        type: 'booking_table_picked',
        sceneMode,
        carriage: res.carriage,
        archivedCarriages: res.archivedCarriages,
        activeTableIndex: res.carriage.currentActiveTableIndex,
        serverTs: Date.now(),
      })
    );
    return true;
  }

  if (msg.type === 'booking_submit_hand' || msg.type === 'booking_submit') {
    const sceneMode = msg.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
    const authCheck = resolveBookingUserId(msg.playerId || msg.userId, msg.sessionToken);
    if (!authCheck.ok) {
      ws.send(JSON.stringify({ type: 'booking_error', action: 'submit_hand', error: authCheck.error, serverTs: Date.now() }));
      return true;
    }
    const userId = authCheck.userId;
    const res = await submitServerBookingHand({
      sceneMode,
      userId,
      playerName: msg.playerName || '茶客',
      avatar: msg.avatar || '👑',
      carriageId: msg.carriageId || undefined,
      carriageNumber: msg.carriageNumber ? Number(msg.carriageNumber) : undefined,
      tableIndex: msg.tableIndex ? Number(msg.tableIndex) : undefined,
      playerSeat: msg.playerSeat,
      handTiers: msg.playerHand,
      isGraceSubmission: Boolean(msg.isGraceSubmission),
    });
    if (!res.ok) {
      ws.send(
        JSON.stringify({
          type: 'booking_error',
          action: 'submit_hand',
          error: res.error,
          serverTs: Date.now(),
        })
      );
      return true;
    }
    if (res.settledNotifications?.length) {
      emitBookingSettlementNotifications(res.settledNotifications);
    }
    ws.send(
      JSON.stringify({
        type: 'booking_submit_ack',
        success: true,
        sceneMode,
        carriage: res.carriage,
        submittedTable: res.submittedTable,
        remainingCount: res.remainingCount,
        isCarriageCompleted: res.isCarriageCompleted,
        archivedCarriages: res.archivedCarriages,
        settlementHistory: res.settlementHistory,
        serverTs: Date.now(),
      })
    );
    broadcast({
      type: 'booking_carriage_occupants_updated',
      sceneMode,
      carriageId: res.carriageId,
      carriageNumber: res.carriageNumber,
      tableIndex: res.tableIndex,
      seat: res.seat,
      seatOccupants: res.seatOccupants,
    });
    return true;
  }

  if (msg.type === 'booking_advance_carriage') {
    const sceneMode = msg.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
    const authCheck = resolveBookingUserId(msg.playerId || msg.userId, msg.sessionToken);
    if (!authCheck.ok) {
      ws.send(JSON.stringify({ type: 'booking_error', action: 'advance_carriage', error: authCheck.error, serverTs: Date.now() }));
      return true;
    }
    const userId = authCheck.userId;
    const res = await advanceServerBookingCarriage({
      sceneMode,
      userId,
      playerName: msg.playerName || '茶客',
      avatar: msg.avatar || '👑',
      createFresh: Boolean(msg.createFresh),
    });
    ws.send(
      JSON.stringify({
        type: 'booking_carriage_advanced',
        sceneMode,
        carriage: res.carriage,
        archivedCarriages: res.archivedCarriages,
        serverTs: Date.now(),
      })
    );
    if (res.releasedSeatInfo) {
      broadcast({
        type: 'booking_carriage_occupants_updated',
        sceneMode,
        carriageId: res.releasedSeatInfo.carriageId,
        carriageNumber: res.releasedSeatInfo.carriageNumber,
        seatOccupants: res.releasedSeatInfo.seatOccupants,
      });
    }
    return true;
  }

  if (msg.type === 'booking_simulate_settle') {
    const sceneMode = msg.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
    const authCheck = resolveBookingUserId(msg.playerId || msg.userId, msg.sessionToken);
    if (!authCheck.ok) {
      ws.send(JSON.stringify({ type: 'booking_error', action: 'simulate_settle', error: authCheck.error, serverTs: Date.now() }));
      return true;
    }
    const userId = authCheck.userId;
    const res = await simulateServerBookingSettlement({
      sceneMode,
      userId,
      carriageId: msg.carriageId || undefined,
      carriageNumber: msg.carriageNumber ? Number(msg.carriageNumber) : undefined,
    });
    if (!res.ok) {
      ws.send(
        JSON.stringify({
          type: 'booking_error',
          action: 'simulate_settle',
          error: res.error,
          serverTs: Date.now(),
        })
      );
      return true;
    }
    ws.send(
      JSON.stringify({
        type: 'booking_simulation_result',
        sceneMode,
        record: res.record,
        carriage: res.carriage,
        archivedCarriages: res.archivedCarriages,
        serverTs: Date.now(),
      })
    );
    return true;
  }

  if (msg.type === 'booking_settle_0600') {
    const userId = msg.playerId || msg.userId;
    const notifications = await runServer0600SettlementSweep({
      onlyUserId: userId || undefined,
      forceIncludeGraceForUserId: msg.forceIncludeGrace ? userId : undefined,
    });
    if (notifications.length > 0) {
      emitBookingSettlementNotifications(notifications);
    }
    if (userId) {
      const sceneMode = msg.sceneMode === 'eight-booking' ? 'eight-booking' : 'four-booking';
      const refreshed = await getServerBookingState({
        sceneMode,
        userId,
        requestedCarriageId: msg.carriageId || undefined,
        requestedCarriageNumber: msg.carriageNumber ? Number(msg.carriageNumber) : undefined,
      });
      ws.send(
        JSON.stringify({
          type: 'booking_carriage_state',
          sceneMode,
          carriage: refreshed.carriage,
          archivedCarriages: refreshed.archivedCarriages,
          settlementHistory: refreshed.settlementHistory,
          unreadSettlements: refreshed.unreadSettlements,
          serverTs: Date.now(),
        })
      );
    }
    return true;
  }

  if (msg.type === 'booking_ack_settlement') {
    const userId = msg.playerId || msg.userId;
    if (userId) {
      await ackServerUnreadSettlements(userId);
    }
    return true;
  }

  return false;
}
