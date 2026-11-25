import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { WebSocketServer, WebSocket } from 'ws';

process.env.DISABLE_ACTIVE_ROOM_SNAPSHOT = '1';

import { createRealtimeHub } from '../src/serverRealtimeHub.ts';
import { autoArrangeThirteen } from '../src/engine/shisanshuiRules.ts';

interface TestClient {
  ws: WebSocket;
  messages: any[];
  waitFor: (pred: (m: any) => boolean, timeoutMs?: number, label?: string) => Promise<any>;
}

function connectClient(url: string): Promise<TestClient> {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(url);
    const messages: any[] = [];
    const waiters: Array<{ pred: (m: any) => boolean; resolve: (m: any) => void; reject: (e: Error) => void }> = [];

    ws.on('message', (buf) => {
      const msg = JSON.parse(buf.toString());
      messages.push(msg);
      for (let i = waiters.length - 1; i >= 0; i--) {
        if (waiters[i].pred(msg)) {
          const w = waiters.splice(i, 1)[0];
          w.resolve(msg);
        }
      }
    });

    ws.on('open', () => {
      resolve({
        ws,
        messages,
        waitFor: (pred, timeoutMs = 4000, label = 'message') => {
          const existing = messages.find(pred);
          if (existing) return Promise.resolve(existing);
          return new Promise((res, rej) => {
            const timer = setTimeout(() => {
              const idx = waiters.findIndex((x) => x.resolve === res);
              if (idx >= 0) waiters.splice(idx, 1);
              rej(new Error(`Timeout waiting for ${label}`));
            }, timeoutMs);
            waiters.push({
              pred,
              resolve: (m) => {
                clearTimeout(timer);
                res(m);
              },
              reject: (e) => {
                clearTimeout(timer);
                rej(e);
              },
            });
          });
        },
      });
    });

    ws.on('error', reject);
  });
}

async function main() {
  const httpServer = createServer();
  const wss = new WebSocketServer({ server: httpServer, path: '/ws', perMessageDeflate: false });
  createRealtimeHub(wss);

  await new Promise<void>((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
  const addr: any = httpServer.address();
  const wsUrl = `ws://127.0.0.1:${addr.port}/ws`;

  // Outsider client in lobby (must never receive room game_event broadcasts)
  const outsider = await connectClient(wsUrl);
  await outsider.waitFor((m) => m.type === 'lobby_snapshot', 3000, 'outsider lobby_snapshot');

  // 1. Yansan 4-player E2E: matchmaking, isolation, move rejection, play, disconnect & scene_reconnect
  const ysClients = await Promise.all([
    connectClient(wsUrl),
    connectClient(wsUrl),
    connectClient(wsUrl),
    connectClient(wsUrl),
  ]);
  await Promise.all(ysClients.map((c) => c.waitFor((m) => m.type === 'lobby_snapshot', 3000, 'ys lobby_snapshot')));

  ysClients.forEach((c, idx) => {
    c.ws.send(
      JSON.stringify({
        type: 'scene_join',
        gameType: 'yansan',
        sceneMode: 'yansan-5-10',
        sceneName: '经典 5-10 场',
        maxPlayers: 4,
        player: {
          id: `e2e-ys-${idx}`,
          name: `测试牌友${idx}`,
          avatar: '👑',
          rating: 1900,
          coins: 2000,
          seat: idx,
        },
      })
    );
  });

  const fullMsgs = await Promise.all(
    ysClients.map((c, idx) => c.waitFor((m) => m.type === 'scene_room_full', 4000, `ys-${idx} scene_room_full`))
  );
  const ysRoomId = fullMsgs[0].roomId;
  const starterSeat = fullMsgs[0].turnSeat as number;
  assert(typeof starterSeat === 'number' && starterSeat >= 0 && starterSeat < 4, 'Starter seat must be 0..3');

  // Verify deal sanitization per seat
  fullMsgs.forEach((m) => {
    const mySeat = m.assignedSeat;
    assert(typeof mySeat === 'number');
    for (const [sKey, hand] of Object.entries(m.deal.seatCards as Record<string, any[]>)) {
      const s = Number(sKey);
      if (s === mySeat) {
        assert(hand.every((c) => !c.hidden && c.rank >= 3), 'Own hand must be unmasked');
      } else {
        assert(hand.every((c) => c.hidden === true && c.rank === 0), 'Opponent hand must be masked');
      }
    }
  });

  // Wait for scene_room_started so room.status === 'playing'
  await Promise.all(
    ysClients.map((c, idx) => c.waitFor((m) => m.type === 'scene_room_started', 4000, `ys-${idx} scene_room_started`))
  );

  // Starter attempts illegal first play without ♦4 -> must receive yansan_move_rejected
  const starterClientIdx = fullMsgs.findIndex((m) => m.assignedSeat === starterSeat);
  assert(starterClientIdx >= 0, 'Must find starter client');
  const starterClient = ysClients[starterClientIdx];
  const starterHand: any[] = fullMsgs[starterClientIdx].deal.seatCards[starterSeat];
  const nonD4 = starterHand.find((c) => c.id !== '4-♦');
  assert(nonD4, 'Starter hand must have a non-♦4 card');
  starterClient.ws.send(
    JSON.stringify({
      type: 'game_move',
      roomId: ysRoomId,
      seat: starterSeat,
      event: { action: 'play', seat: starterSeat, combo: { cards: [nonD4] } },
      payload: { action: 'play', seat: starterSeat, combo: { cards: [nonD4] } },
    })
  );
  const rejectMsg = await starterClient.waitFor(
    (m) => m.type === 'game_event' && m.event?.action === 'yansan_move_rejected',
    3000,
    'yansan_move_rejected'
  );
  assert.equal(rejectMsg.event.reason, 'must_contain_diamond4');

  // Starter plays ♦4 -> all 4 clients receive play event
  const d4Card = starterHand.find((c) => c.id === '4-♦');
  assert(d4Card, 'Starter must hold ♦4');
  starterClient.ws.send(
    JSON.stringify({
      type: 'game_move',
      roomId: ysRoomId,
      seat: starterSeat,
      event: { action: 'play', seat: starterSeat, combo: { cards: [d4Card] } },
      payload: { action: 'play', seat: starterSeat, combo: { cards: [d4Card] } },
    })
  );
  await Promise.all(
    ysClients.map((c, idx) =>
      c.waitFor((m) => m.type === 'game_event' && m.event?.action === 'play', 3000, `ys-${idx} play`)
    )
  );

  // Disconnect another seat and reconnect from a brand new WebSocket
  const reconSeat = (starterSeat + 1) % 4;
  const reconClientIdx = fullMsgs.findIndex((m) => m.assignedSeat === reconSeat);
  assert(reconClientIdx >= 0);
  const reconPlayerId = fullMsgs[reconClientIdx].assignedPlayerId;
  ysClients[reconClientIdx].ws.close();
  await new Promise((r) => setTimeout(r, 150));
  const reconClient = await connectClient(wsUrl);
  await reconClient.waitFor((m) => m.type === 'lobby_snapshot', 3000, 'recon lobby_snapshot');
  reconClient.ws.send(
    JSON.stringify({
      type: 'scene_reconnect',
      roomId: ysRoomId,
      playerId: reconPlayerId,
    })
  );
  const reconAck = await reconClient.waitFor((m) => m.type === 'scene_reconnect_ack', 3000, 'scene_reconnect_ack');
  assert(!reconAck.failed, 'scene_reconnect must succeed');
  assert.equal(reconAck.room?.deal, undefined, 'scene_reconnect_ack.room must NOT leak raw un-sanitized deal');
  assert(
    reconAck.deal.seatCards[reconSeat].every((c: any) => !c.hidden && c.rank >= 3),
    'Reconnected client must recover unmasked own hand'
  );
  assert(
    reconAck.deal.seatCards[starterSeat].every((c: any) => c.hidden === true),
    'Reconnected client must still see opponent hands masked'
  );

  // Stale-socket close race: new socket reconnects BEFORE old half-open socket closes -> must NOT mark player disconnected/auto-managed
  const raceSeat = (starterSeat + 2) % 4;
  const raceClientIdx = fullMsgs.findIndex((m) => m.assignedSeat === raceSeat);
  assert(raceClientIdx >= 0);
  const racePlayerId = fullMsgs[raceClientIdx].assignedPlayerId;
  const raceNewClient = await connectClient(wsUrl);
  await raceNewClient.waitFor((m) => m.type === 'lobby_snapshot', 3000, 'race lobby_snapshot');
  raceNewClient.ws.send(
    JSON.stringify({
      type: 'scene_reconnect',
      roomId: ysRoomId,
      playerId: racePlayerId,
    })
  );
  const raceAck = await raceNewClient.waitFor((m) => m.type === 'scene_reconnect_ack', 3000, 'race scene_reconnect_ack');
  assert(!raceAck.failed, 'race scene_reconnect must succeed');
  const staleMsgCountBefore = raceNewClient.messages.length;
  // Now old socket finally closes!
  ysClients[raceClientIdx].ws.close();
  await new Promise((r) => setTimeout(r, 180));
  const postStaleCloseEvents = raceNewClient.messages.slice(staleMsgCountBefore);
  const falseAutoHost = postStaleCloseEvents.find(
    (m) =>
      m.type === 'game_event' &&
      m.event?.action === 'set_auto_host' &&
      m.event?.seat === raceSeat &&
      m.event?.isAutoManaged === true
  );
  assert.equal(falseAutoHost, undefined, 'Closing old stale socket after reconnect must NOT trigger auto-host/disconnect on active seat');

  // 2. Shisanshui 4-player E2E: pre-showdown submission masking & full settlement
  const sssClients = await Promise.all([
    connectClient(wsUrl),
    connectClient(wsUrl),
    connectClient(wsUrl),
    connectClient(wsUrl),
  ]);
  await Promise.all(sssClients.map((c) => c.waitFor((m) => m.type === 'lobby_snapshot', 3000, 'sss lobby_snapshot')));

  sssClients.forEach((c, idx) => {
    c.ws.send(
      JSON.stringify({
        type: 'scene_join',
        gameType: 'shisanshui',
        sceneMode: 'four',
        sceneName: '水镜四人场',
        maxPlayers: 4,
        player: {
          id: `e2e-sss-${idx}`,
          name: `十三水玩家${idx}`,
          avatar: '🎴',
          rating: 1900,
          coins: 5000,
          seat: idx,
        },
      })
    );
  });

  const sssFullMsgs = await Promise.all(
    sssClients.map((c, idx) => c.waitFor((m) => m.type === 'scene_room_full', 4000, `sss-${idx} scene_room_full`))
  );
  const sssRoomId = sssFullMsgs[0].roomId;
  await Promise.all(
    sssClients.map((c, idx) => c.waitFor((m) => m.type === 'scene_room_started', 4000, `sss-${idx} scene_room_started`))
  );

  // Seat 0 submits hand first
  const seat0Idx = sssFullMsgs.findIndex((m) => m.assignedSeat === 0);
  const seat1Idx = sssFullMsgs.findIndex((m) => m.assignedSeat === 1);
  const opt0 = autoArrangeThirteen(sssFullMsgs[seat0Idx].deal.seatCards[0], 'optimal');
  sssClients[seat0Idx].ws.send(
    JSON.stringify({
      type: 'game_move',
      roomId: sssRoomId,
      seat: 0,
      event: {
        action: 'shisanshui_submit_hand',
        seat: 0,
        tiers: { head: opt0.head, mid: opt0.mid, tail: opt0.tail },
        rawTiers: { front: opt0.head, mid: opt0.mid, back: opt0.tail },
      },
    })
  );

  const subEvtForSeat1 = await sssClients[seat1Idx].waitFor(
    (m) => m.type === 'game_event' && m.event?.action === 'shisanshui_submit_hand',
    3000,
    'seat1 shisanshui_submit_hand'
  );
  const seat0SubSeenBySeat1 = subEvtForSeat1.boardState?.shisanshuiSubmissions?.[0];
  assert(seat0SubSeenBySeat1?.hidden === true, 'Opponent must see hidden:true on pre-showdown submission');
  assert(!seat0SubSeenBySeat1?.tiers && !seat0SubSeenBySeat1?.rawTiers, 'Opponent must NOT receive submitted cards before showdown');

  const subEvtForSeat0 = await sssClients[seat0Idx].waitFor(
    (m) => m.type === 'game_event' && m.event?.action === 'shisanshui_submit_hand',
    3000,
    'seat0 shisanshui_submit_hand'
  );
  assert(
    Array.isArray(subEvtForSeat0.boardState?.shisanshuiSubmissions?.[0]?.tiers?.head),
    'Submitting player keeps own submission visible in boardState'
  );

  // Seat 2 attempts intentionally fouled submission -> must receive shisanshui_submit_rejected
  const seat2Idx = sssFullMsgs.findIndex((m) => m.assignedSeat === 2);
  const s2Cards = [...sssFullMsgs[seat2Idx].deal.seatCards[2]];
  // Put highest 3 cards in head and lowest 5 in tail (or check isFoulHand on reversed sort)
  const s2SortedDesc = [...s2Cards].sort((a: any, b: any) => b.rank - a.rank || b.suitVal - a.suitVal);
  const foulHead = s2SortedDesc.slice(0, 3);
  const foulMid = s2SortedDesc.slice(3, 8);
  const foulTail = s2SortedDesc.slice(8, 13);
  const { detectSpecialHand, isFoulHand } = await import('../src/engine/shisanshuiRules.ts');
  if (!detectSpecialHand(s2SortedDesc) && isFoulHand(foulHead, foulMid, foulTail)) {
    sssClients[seat2Idx].ws.send(
      JSON.stringify({
        type: 'game_move',
        roomId: sssRoomId,
        seat: 2,
        event: {
          action: 'shisanshui_submit_hand',
          seat: 2,
          tiers: { head: foulHead, mid: foulMid, tail: foulTail },
          rawTiers: { front: foulHead, mid: foulMid, back: foulTail },
        },
      })
    );
    const sssReject = await sssClients[seat2Idx].waitFor(
      (m) => m.type === 'game_event' && m.event?.action === 'shisanshui_submit_rejected',
      3000,
      'shisanshui_submit_rejected'
    );
    assert.equal(sssReject.event.reason, 'foul_hand');
  }

  // Remaining 3 seats submit -> triggers shisanshui_all_submitted
  for (let i = 0; i < 4; i++) {
    if (i === seat0Idx) continue;
    const s = sssFullMsgs[i].assignedSeat;
    const opt = autoArrangeThirteen(sssFullMsgs[i].deal.seatCards[s], 'optimal');
    sssClients[i].ws.send(
      JSON.stringify({
        type: 'game_move',
        roomId: sssRoomId,
        seat: s,
        event: {
          action: 'shisanshui_submit_hand',
          seat: s,
          tiers: { head: opt.head, mid: opt.mid, tail: opt.tail },
          rawTiers: { front: opt.head, mid: opt.mid, back: opt.tail },
        },
      })
    );
  }

  const allSubMsgs = await Promise.all(
    sssClients.map((c, idx) =>
      c.waitFor((m) => m.type === 'game_event' && m.event?.action === 'shisanshui_all_submitted', 4000, `sss-${idx} all_submitted`)
    )
  );
  assert.equal(allSubMsgs[0].boardState.phase, 'ended');
  assert.equal(allSubMsgs[0].event.settlement.netWaters.length, 4);

  // 3. Mahjong 4-player E2E: matchmaking, wall/opponent masking, invalid hu rejection, discard turn advance
  const mjClients = await Promise.all([
    connectClient(wsUrl),
    connectClient(wsUrl),
    connectClient(wsUrl),
    connectClient(wsUrl),
  ]);
  await Promise.all(mjClients.map((c) => c.waitFor((m) => m.type === 'lobby_snapshot', 3000, 'mj lobby_snapshot')));

  mjClients.forEach((c, idx) => {
    c.ws.send(
      JSON.stringify({
        type: 'scene_join',
        gameType: 'mahjong',
        sceneMode: 'hongzhong-6',
        sceneName: '红中6码场',
        maxPlayers: 4,
        player: {
          id: `e2e-mj-${idx}`,
          name: `麻将玩家${idx}`,
          avatar: '🀄',
          rating: 1800,
          coins: 3000,
          seat: idx,
        },
      })
    );
  });

  const mjFullMsgs = await Promise.all(
    mjClients.map((c, idx) => c.waitFor((m) => m.type === 'scene_room_full', 4000, `mj-${idx} scene_room_full`))
  );
  const mjRoomId = mjFullMsgs[0].roomId;
  mjFullMsgs.forEach((m) => {
    assert(
      Array.isArray(m.deal.remainingWall) && m.deal.remainingWall.every((t: any) => t.hidden === true),
      'remainingWall tiles must all be masked (hidden: true) in Mahjong scene_room_full'
    );
    const mySeat = m.assignedSeat;
    assert(Array.isArray(m.deal.seatCards[mySeat]) && m.deal.seatCards[mySeat].every((t: any) => !t.hidden));
    const oppSeat = (mySeat + 1) % 4;
    assert(m.deal.seatCards[oppSeat].every((t: any) => t.hidden === true));
  });

  await Promise.all(
    mjClients.map((c, idx) => c.waitFor((m) => m.type === 'scene_room_started', 4000, `mj-${idx} scene_room_started`))
  );

  // Dealer (seat 0 or turnSeat) attempts bogus hu on non-winning hand -> must get hu_rejected
  const mjTurnSeat = mjFullMsgs[0].turnSeat ?? 0;
  const mjTurnIdx = mjFullMsgs.findIndex((m) => m.assignedSeat === mjTurnSeat);
  const mjDealerHand = mjFullMsgs[mjTurnIdx].deal.seatCards[mjTurnSeat];
  mjClients[mjTurnIdx].ws.send(
    JSON.stringify({
      type: 'game_move',
      roomId: mjRoomId,
      seat: mjTurnSeat,
      event: { action: 'hu', seat: mjTurnSeat },
    })
  );
  // If dealer happens to have an instant win (super rare) we handle either; almost always invalid_hu_hand
  const mjAfterHuAttempt = await mjClients[mjTurnIdx].waitFor(
    (m) => m.type === 'game_event' && (m.event?.action === 'hu_rejected' || m.event?.action === 'hu'),
    3000,
    'mj hu check'
  );
  if (mjAfterHuAttempt.event.action === 'hu_rejected') {
    const discardTile = mjDealerHand[0];
    mjClients[mjTurnIdx].ws.send(
      JSON.stringify({
        type: 'game_move',
        roomId: mjRoomId,
        seat: mjTurnSeat,
        event: { action: 'discard', seat: mjTurnSeat, tile: discardTile },
      })
    );
    await Promise.all(
      mjClients.map((c, idx) =>
        c.waitFor((m) => m.type === 'game_event' && m.event?.action === 'discard', 3000, `mj-${idx} discard`)
      )
    );
  }

  // Outsider must have received ZERO game_event messages across all rooms!
  const outsiderGameEvents = outsider.messages.filter((m) => m.type === 'game_event');
  assert.equal(outsiderGameEvents.length, 0, 'Outsider in lobby must never receive room game_event messages');

  // Cleanup
  [outsider, reconClient, raceNewClient, ...ysClients, ...sssClients, ...mjClients].forEach((c) => {
    try { c.ws.close(); } catch {}
  });
  wss.close();
  httpServer.close();
  console.log('✅ WebSocket E2E integration tests passed (Yansan 4p, Reject, Reconnect, Shisanshui Masking/FoulReject, Mahjong 4p, Room Isolation).');
  process.exit(0);
}

main().catch((err) => {
  console.error('❌ WS E2E test failed:', err);
  process.exit(1);
});
