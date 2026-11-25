package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	wsGUID              = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	opContinuation      = 0
	opText              = 1
	opBinary            = 2
	opClose             = 8
	opPing              = 9
	opPong              = 10
	maxWSFrameBytes     = 4 * 1024 * 1024
	settlementTimeoutMs = int64(60000)
)

type wsFrame struct {
	opcode byte
	data   []byte
}

// WSConn wraps an upgraded RFC 6455 TCP connection with a non-blocking FIFO writer loop.
type WSConn struct {
	conn                net.Conn
	bufr                *bufio.Reader
	bufw                *bufio.Writer
	id                  string
	authenticatedUserID string
	userID              string
	sceneRoomID         string
	scenePlayerID       string
	lastHeartbeat       atomic.Int64
	chatMu              sync.Mutex
	chatTimestamps      []int64

	sendCh    chan wsFrame
	closeOnce sync.Once
	closed    atomic.Bool
	writeMu   sync.Mutex
}

func newWSConn(conn net.Conn, br *bufio.Reader, bw *bufio.Writer, id string) *WSConn {
	w := &WSConn{
		conn:   conn,
		bufr:   br,
		bufw:   bw,
		id:     id,
		sendCh: make(chan wsFrame, 256),
	}
	w.lastHeartbeat.Store(nowMs())
	go w.writeLoop()
	return w
}

func (w *WSConn) writeLoop() {
	for frame := range w.sendCh {
		if w.closed.Load() {
			return
		}
		if err := w.writeFrameDirect(frame.opcode, frame.data); err != nil {
			w.Close()
			return
		}
	}
}

func (w *WSConn) writeFrameDirect(opcode byte, payload []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if w.closed.Load() {
		return fmt.Errorf("connection closed")
	}
	_ = w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	defer func() {
		_ = w.conn.SetWriteDeadline(time.Time{})
	}()

	length := len(payload)
	b0 := byte(0x80) | (opcode & 0x0F)
	var header []byte
	if length <= 125 {
		header = []byte{b0, byte(length)}
	} else if length <= 65535 {
		header = make([]byte, 4)
		header[0] = b0
		header[1] = 126
		binary.BigEndian.PutUint16(header[2:4], uint16(length))
	} else {
		header = make([]byte, 10)
		header[0] = b0
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:10], uint64(length))
	}

	if _, err := w.bufw.Write(header); err != nil {
		return err
	}
	if length > 0 {
		if _, err := w.bufw.Write(payload); err != nil {
			return err
		}
	}
	return w.bufw.Flush()
}

func (w *WSConn) WriteMessage(opcode byte, payload []byte) error {
	if w.closed.Load() {
		return fmt.Errorf("connection closed")
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	select {
	case w.sendCh <- wsFrame{opcode: opcode, data: cp}:
		return nil
	default:
		// Slow/stalled client: close to protect server memory
		w.Close()
		return fmt.Errorf("send queue full")
	}
}

func (w *WSConn) WriteText(text string) error {
	return w.WriteMessage(opText, []byte(text))
}

func (w *WSConn) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.WriteMessage(opText, data)
}

func (w *WSConn) SendPing() error {
	return w.WriteMessage(opPing, []byte("hb"))
}

func (w *WSConn) allowChat() bool {
	now := nowMs()
	w.chatMu.Lock()
	defer w.chatMu.Unlock()
	filtered := w.chatTimestamps[:0]
	for _, ts := range w.chatTimestamps {
		if now-ts < 10000 {
			filtered = append(filtered, ts)
		}
	}
	w.chatTimestamps = filtered
	if len(w.chatTimestamps) >= 15 {
		return false
	}
	w.chatTimestamps = append(w.chatTimestamps, now)
	return true
}

func (w *WSConn) ReadMessage() (byte, []byte, error) {
	var msgBuf []byte
	var msgOpcode byte

	for {
		b0, err := w.bufr.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		fin := (b0 & 0x80) != 0
		opcode := b0 & 0x0F

		b1, err := w.bufr.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		masked := (b1 & 0x80) != 0
		payloadLen := int(b1 & 0x7F)

		if payloadLen == 126 {
			var extLen uint16
			if err := binary.Read(w.bufr, binary.BigEndian, &extLen); err != nil {
				return 0, nil, err
			}
			payloadLen = int(extLen)
		} else if payloadLen == 127 {
			var extLen uint64
			if err := binary.Read(w.bufr, binary.BigEndian, &extLen); err != nil {
				return 0, nil, err
			}
			if extLen > maxWSFrameBytes {
				return 0, nil, fmt.Errorf("frame too large")
			}
			payloadLen = int(extLen)
		}

		if payloadLen < 0 || payloadLen > maxWSFrameBytes {
			return 0, nil, fmt.Errorf("frame exceeds limit")
		}

		var maskKey [4]byte
		if masked {
			if _, err := io.ReadFull(w.bufr, maskKey[:]); err != nil {
				return 0, nil, err
			}
		}

		payload := make([]byte, payloadLen)
		if payloadLen > 0 {
			if _, err := io.ReadFull(w.bufr, payload); err != nil {
				return 0, nil, err
			}
		}

		if masked {
			for i := 0; i < payloadLen; i++ {
				payload[i] ^= maskKey[i%4]
			}
		}

		w.lastHeartbeat.Store(nowMs())

		switch opcode {
		case opClose:
			w.Close()
			return opClose, payload, io.EOF
		case opPing:
			_ = w.WriteMessage(opPong, payload)
			continue
		case opPong:
			continue
		case opText, opBinary:
			if fin {
				return opcode, payload, nil
			}
			msgOpcode = opcode
			msgBuf = append(msgBuf[:0], payload...)
		case opContinuation:
			if len(msgBuf)+len(payload) > maxWSFrameBytes {
				return 0, nil, fmt.Errorf("fragmented message exceeds limit")
			}
			msgBuf = append(msgBuf, payload...)
			if fin {
				out := make([]byte, len(msgBuf))
				copy(out, msgBuf)
				return msgOpcode, out, nil
			}
		}
	}
}

func (w *WSConn) Close() {
	w.closeOnce.Do(func() {
		w.closed.Store(true)
		close(w.sendCh)
		_ = w.conn.Close()
	})
}

// ==============================================================================
// HubContext & Realtime Hub
// ==============================================================================

type HubContext struct {
	Storage *StorageManager
	Booking *BookingManager
	hub     *Hub
}

func (c *HubContext) SendToConn(conn *WSConn, payload interface{}) {
	if conn == nil || payload == nil {
		return
	}
	_ = conn.WriteJSON(payload)
}

func (c *HubContext) Broadcast(payload interface{}) {
	if c.hub == nil || payload == nil {
		return
	}
	c.hub.BroadcastJSON(payload)
}

func (c *HubContext) SendSanitizedToRoom(room *RoomState, buildPayload func(seat int, player ScenePlayer) interface{}) {
	if c.hub == nil || room == nil || buildPayload == nil {
		return
	}
	c.hub.sendSanitizedToRoomLocked(room, buildPayload)
}

func (c *HubContext) SettleRoomCoins(room *RoomState, coinsDeltaBySeat []int, gameType string, ratingDeltaBySeat []int) {
	if c.hub == nil || room == nil {
		return
	}
	dealID := ""
	if room.Deal != nil {
		dealID = room.Deal.DealID
	}
	rIdx := room.RoundIndex
	if rIdx <= 0 {
		rIdx = 1
	}
	roundKey := fmt.Sprintf("%s_%s_r%d_%s", gameType, room.ID, rIdx, dealID)
	if room.CoinsSettledKey == roundKey {
		return
	}
	room.CoinsSettledKey = roundKey

	type coinUpdateMsg struct {
		userID string
		coins  int
		delta  int
		rating int
		level  int
	}
	var updates []coinUpdateMsg

	_ = c.Storage.WithDBWrite(func(db *DatabaseSchema) bool {
		dirty := false
		for _, pl := range room.Players {
			if pl.Seat < 0 || pl.Seat >= len(coinsDeltaBySeat) || isBotLikePlayer(pl) {
				continue
			}
			delta := coinsDeltaBySeat[pl.Seat]
			prof := db.Profiles[pl.ID]
			if prof == nil && pl.OriginalID != "" {
				prof = db.Profiles[pl.OriginalID]
			}
			if prof == nil {
				continue
			}
			prof.Coins += delta
			if prof.Coins < 0 {
				prof.Coins = 0
			}
			if pl.Seat < len(ratingDeltaBySeat) {
				prof.Rating += ratingDeltaBySeat[pl.Seat]
				if prof.Rating < 1000 {
					prof.Rating = 1000
				}
				prof.Level = prof.Rating / 500
				if prof.Level < 1 {
					prof.Level = 1
				} else if prof.Level > 10 {
					prof.Level = 10
				}
			}
			prof.UpdatedAt = nowMs()
			if delta != 0 {
				gLabel := "经典烟三"
				if gameType == "mahjong" {
					gLabel = "经典国标麻将"
				} else if gameType == "shisanshui" {
					gLabel = "水镜十三水"
				}
				txType := "game_win"
				sign := "+"
				if delta < 0 {
					txType = "game_loss"
					sign = ""
				}
				recordServerPointsTx(db, prof.ID, txType, fmt.Sprintf("%s · 牌桌结算 (%s%d)", gLabel, sign, delta), delta, prof.Coins)
			}
			dirty = true
			updates = append(updates, coinUpdateMsg{
				userID: prof.ID,
				coins:  prof.Coins,
				delta:  delta,
				rating: prof.Rating,
				level:  prof.Level,
			})
		}
		return dirty
	})

	for _, u := range updates {
		c.Broadcast(map[string]interface{}{
			"type":     "user_coins_updated",
			"userId":   u.userID,
			"coins":    u.coins,
			"delta":    u.delta,
			"rating":   u.rating,
			"level":    u.level,
			"source":   "match_settlement",
			"gameType": gameType,
			"roomId":   room.ID,
			"roundKey": roundKey,
		})
	}
}

func (c *HubContext) ResetSceneRoomReady(roomID string) {
	if c.hub == nil || roomID == "" {
		return
	}
	c.hub.resetSceneRoomReadyLocked(roomID)
}

func (c *HubContext) ScheduleSnapshot() {
	if c.hub != nil {
		c.hub.scheduleSnapshot()
	}
}

func (c *HubContext) EmitBookingNotifications(notifs []BookingSettledNotification) {
	if c.hub == nil || len(notifs) == 0 {
		return
	}
	for _, item := range notifs {
		c.Broadcast(map[string]interface{}{
			"type":               "user_coins_updated",
			"userId":             item.UserID,
			"coins":              item.Coins,
			"delta":              item.Delta,
			"rating":             item.Rating,
			"level":              item.Level,
			"source":             "booking_0600_settlement",
			"sceneMode":          item.Record.SceneMode,
			"carriageNumber":     item.Record.CarriageNumber,
			"settledTablesCount": item.Record.SettledTablesCount,
		})
		c.Broadcast(map[string]interface{}{
			"type":      "booking_0600_settled",
			"userId":    item.UserID,
			"sceneMode": item.Record.SceneMode,
			"record":    item.Record,
			"coins":     item.Coins,
			"delta":     item.Delta,
		})
	}
}

type Hub struct {
	storage *StorageManager
	booking *BookingManager
	ctx     *HubContext

	clientsMu sync.RWMutex
	clients   map[string]*WSConn
	presence  map[string]*ClientPresenceInfo

	roomMu     sync.Mutex
	rooms      map[string]*RoomState
	sceneRooms map[string]*SceneWaitingRoom
	challenges []ActiveChallengeRecord

	snapshotMu    sync.Mutex
	snapshotTimer *time.Timer

	stopCh   chan struct{}
	bootTime time.Time
}

func newHub(sm *StorageManager, bm *BookingManager) *Hub {
	h := &Hub{
		storage:    sm,
		booking:    bm,
		clients:    make(map[string]*WSConn),
		presence:   make(map[string]*ClientPresenceInfo),
		rooms:      make(map[string]*RoomState),
		sceneRooms: make(map[string]*SceneWaitingRoom),
		challenges: []ActiveChallengeRecord{},
		stopCh:     make(chan struct{}),
		bootTime:   time.Now(),
	}
	h.ctx = &HubContext{
		Storage: sm,
		Booking: bm,
		hub:     h,
	}
	h.seedDefaultRooms()
	restored := loadActiveRoomsSnapshot(h.rooms, h.sceneRooms)
	if restored > 0 {
		log.Printf("[Snapshot] Restored %d active rooms from crash-recovery snapshot", restored)
	}
	return h
}

func (h *Hub) seedDefaultRooms() {
	now := nowMs()
	h.rooms["RM-202"] = &RoomState{
		ID:       "RM-202",
		Title:    "风雷堂 · 经典烟三四人赛",
		GameType: "yansan",
		Status:   "playing",
		Players: []ScenePlayer{
			{ID: "P-4", Name: "快枪手张三", Avatar: "🃏", Rating: 2010, Seat: 0},
			{ID: "P-5", Name: "关门客李四", Avatar: "🃏", Rating: 1940, Seat: 1},
			{ID: "P-6", Name: "岭南少侠", Avatar: "🃏", Rating: 1870, Seat: 2},
		},
		Spectators: 9,
		TurnSeat:   0,
		MoveCount:  6,
		ModeLabel:  "方块四首出 · 四人争锋",
		Chat:       []RoomChatMessage{{ID: "c2", Sender: "快枪手张三", Text: "方块四首出直接开顺子！", Timestamp: "10:04"}},
		BoardState: createDefaultBoardState("RM-202", "yansan"),
		CreatedAt:  now,
	}
	h.rooms["RM-204"] = &RoomState{
		ID:       "RM-204",
		Title:    "水镜阁 · 经典十三水",
		GameType: "shisanshui",
		Status:   "playing",
		Players: []ScenePlayer{
			{ID: "P-11", Name: "罗宋巧手", Avatar: "🂡", Rating: 2060, Seat: 0},
			{ID: "P-12", Name: "倒水掌门", Avatar: "🂡", Rating: 1850, Seat: 1},
			{ID: "P-13", Name: "十三飞龙", Avatar: "🂡", Rating: 2200, Seat: 2},
			{ID: "P-14", Name: "七星居士", Avatar: "🂡", Rating: 1970, Seat: 3},
		},
		Spectators: 18,
		TurnSeat:   0,
		MoveCount:  1,
		ModeLabel:  "头中尾三道 · 智能理牌",
		Chat:       []RoomChatMessage{{ID: "c4", Sender: "十三飞龙", Text: "尾道葫芦中道同花，防倒水稳如磐石。", Timestamp: "10:06"}},
		BoardState: createDefaultBoardState("RM-204", "shisanshui"),
		CreatedAt:  now,
	}
	h.rooms["RM-205"] = &RoomState{
		ID:       "RM-205",
		Title:    "雀神楼 · 经典国标麻将",
		GameType: "mahjong",
		Status:   "playing",
		Players: []ScenePlayer{
			{ID: "P-15", Name: "天和雀圣", Avatar: "🀄", Rating: 2240, Seat: 0},
			{ID: "P-16", Name: "清一色老叟", Avatar: "🀄", Rating: 2110, Seat: 1},
			{ID: "P-17", Name: "大三元道长", Avatar: "🀄", Rating: 2180, Seat: 2},
			{ID: "P-18", Name: "十三幺隐士", Avatar: "🀄", Rating: 2290, Seat: 3},
		},
		Spectators: 31,
		TurnSeat:   0,
		MoveCount:  12,
		ModeLabel:  "万条筒字 · 碰杠听胡",
		Chat:       []RoomChatMessage{{ID: "c5", Sender: "天和雀圣", Text: "自摸万字清一色，听牌在手！", Timestamp: "10:07"}},
		BoardState: createDefaultBoardState("RM-205", "mahjong"),
		CreatedAt:  now,
	}
}

func (h *Hub) startBackgroundLoops() {
	// 1. Authoritative Game Turn, AFK & Settlement Timeout Watchdog (every 1s)
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.stopCh:
				return
			case <-ticker.C:
				h.tickRooms()
			}
		}
	}()

	// 2. WebSocket Zombie Heartbeat Sweep (every 25s)
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.stopCh:
				return
			case <-ticker.C:
				h.sweepStaleSockets()
			}
		}
	}()

	// 3. Booking 06:00 Settlement Sweep (initial 5s delay + every 30s)
	go func() {
		select {
		case <-h.stopCh:
			return
		case <-time.After(5 * time.Second):
		}
		if list := h.booking.Run0600Sweep("", ""); len(list) > 0 {
			h.ctx.EmitBookingNotifications(list)
		}
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.stopCh:
				return
			case <-ticker.C:
				if list := h.booking.Run0600Sweep("", ""); len(list) > 0 {
					h.ctx.EmitBookingNotifications(list)
				}
			}
		}
	}()

	// 4. Stale Empty Room Cleanup (every 10m)
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-h.stopCh:
				return
			case <-ticker.C:
				h.cleanupEmptyRooms()
			}
		}
	}()
}

func (h *Hub) stop() {
	select {
	case <-h.stopCh:
	default:
		close(h.stopCh)
	}
	h.snapshotMu.Lock()
	if h.snapshotTimer != nil {
		h.snapshotTimer.Stop()
		h.snapshotTimer = nil
	}
	h.snapshotMu.Unlock()

	h.roomMu.Lock()
	saveActiveRoomsSnapshot(h.rooms, h.sceneRooms)
	h.roomMu.Unlock()
}

func (h *Hub) scheduleSnapshot() {
	h.snapshotMu.Lock()
	defer h.snapshotMu.Unlock()
	if h.snapshotTimer != nil {
		h.snapshotTimer.Stop()
	}
	h.snapshotTimer = time.AfterFunc(500*time.Millisecond, func() {
		h.roomMu.Lock()
		saveActiveRoomsSnapshot(h.rooms, h.sceneRooms)
		h.roomMu.Unlock()
	})
}

func (h *Hub) registerClient(c *WSConn) {
	h.clientsMu.Lock()
	h.clients[c.id] = c
	h.presence[c.id] = &ClientPresenceInfo{
		SocketID: c.id,
		GameType: "lobby",
		LastSeen: nowMs(),
	}
	online := len(h.clients)
	h.clientsMu.Unlock()

	// Send initial lobby_snapshot & teahouse_players_update
	h.roomMu.Lock()
	safeRooms := h.buildSanitizedRoomsListLocked()
	h.roomMu.Unlock()

	counts := h.getRealGamePresence()
	var teahousePlayers []TeahousePlayerItem
	h.storage.WithDBRead(func(db *DatabaseSchema) {
		teahousePlayers = h.getAllPlayers(db.Profiles)
	})

	_ = c.WriteJSON(map[string]interface{}{
		"type":        "lobby_snapshot",
		"rooms":       safeRooms,
		"onlineCount": online,
		"counts":      counts,
		"players":     teahousePlayers,
		"serverTs":    nowMs(),
	})
	_ = c.WriteJSON(map[string]interface{}{
		"type":        "teahouse_players_update",
		"count":       len(teahousePlayers),
		"onlineCount": countOnlineTeahouse(teahousePlayers),
		"players":     teahousePlayers,
	})

	h.broadcastPresenceAndPlayers()
}

func (h *Hub) unregisterClient(c *WSConn) {
	if c == nil {
		return
	}
	h.clientsMu.Lock()
	delete(h.clients, c.id)
	delete(h.presence, c.id)
	h.clientsMu.Unlock()

	c.Close()

	h.roomMu.Lock()
	h.handleClientDisconnectLocked(c)
	h.roomMu.Unlock()

	h.broadcastPresenceAndPlayers()
}

func (h *Hub) handleClientDisconnectLocked(c *WSConn) {
	sRoomID := c.sceneRoomID
	pID := c.scenePlayerID

	hasActiveOtherSocket := false
	if pID != "" {
		h.clientsMu.RLock()
		for _, other := range h.clients {
			if other != nil && other.id != c.id && other.scenePlayerID == pID {
				hasActiveOtherSocket = true
				break
			}
		}
		if !hasActiveOtherSocket {
			for _, pr := range h.presence {
				if pr != nil && pr.SocketID != c.id && pr.PlayerID == pID {
					hasActiveOtherSocket = true
					break
				}
			}
		}
		h.clientsMu.RUnlock()
	}

	var room *SceneWaitingRoom
	if sRoomID != "" {
		room = h.sceneRooms[sRoomID]
	}
	authRoom := h.rooms[sRoomID]

	if sRoomID != "" && (room != nil || authRoom != nil) {
		gType := ""
		sMode := ""
		status := ""
		if room != nil {
			gType = room.GameType
			sMode = room.SceneMode
			status = room.Status
		} else if authRoom != nil {
			gType = authRoom.GameType
			sMode = authRoom.SceneMode
			status = authRoom.Status
		}
		isShisanshuiReservation := gType == "shisanshui" && sMode == "reservation"

		if status == "finished" && !isShisanshuiReservation {
			var matchedFinishedID string
			if room != nil {
				for i := range room.Players {
					pl := &room.Players[i]
					if pl.SocketID == c.id || (pl.SocketID == "" && pID != "" && (pl.ID == pID || pl.OriginalID == pID)) {
						pl.IsDisconnected = true
						pl.SocketID = ""
						matchedFinishedID = pl.ID
					}
				}
			}
			if authRoom != nil {
				for i := range authRoom.Players {
					apl := &authRoom.Players[i]
					if apl.SocketID == c.id || (apl.SocketID == "" && ((matchedFinishedID != "" && apl.ID == matchedFinishedID) || (pID != "" && (apl.ID == pID || apl.OriginalID == pID)))) {
						apl.IsDisconnected = true
						apl.SocketID = ""
					}
				}
			}
		} else if (status == "starting" || status == "playing") && !isShisanshuiReservation {
			var matchedPl *ScenePlayer
			if room != nil {
				for i := range room.Players {
					pl := &room.Players[i]
					if pl.SocketID == c.id || (pl.SocketID == "" && pID != "" && pl.ID == pID) {
						pl.IsDisconnected = true
						pl.IsAutoManaged = true
						matchedPl = pl
						break
					}
				}
			}
			if authRoom != nil {
				for i := range authRoom.Players {
					apl := &authRoom.Players[i]
					if apl.SocketID == c.id || (apl.SocketID == "" && ((pID != "" && apl.ID == pID) || (matchedPl != nil && apl.ID == matchedPl.ID))) {
						apl.IsDisconnected = true
						apl.IsAutoManaged = true
						if matchedPl == nil {
							matchedPl = apl
						}
					}
				}
				if matchedPl != nil {
					if authRoom.BoardState == nil {
						authRoom.BoardState = createDefaultBoardState(authRoom.ID, authRoom.GameType)
					}
					if authRoom.BoardState.AutoManagedSeats == nil {
						authRoom.BoardState.AutoManagedSeats = map[int]bool{}
					}
					authRoom.BoardState.AutoManagedSeats[matchedPl.Seat] = true
					if authRoom.TurnSeat == matchedPl.Seat {
						authRoom.LastTurnTs = nowMs() - 2000
					}
				}
			}
			if matchedPl != nil {
				plSeat := matchedPl.Seat
				plName := matchedPl.Name
				targetRef := authRoom
				if targetRef == nil && room != nil {
					targetRef = &RoomState{ID: room.ID, GameType: room.GameType, SceneMode: room.SceneMode, MaxPlayers: room.MaxPlayers, Players: room.Players, Status: room.Status}
				}
				if room != nil {
					h.sendSanitizedToRoomLocked(targetRef, func(rSeat int, _ ScenePlayer) interface{} {
						m := map[string]interface{}{
							"type":       "scene_room_update",
							"roomId":     room.ID,
							"gameType":   room.GameType,
							"sceneMode":  room.SceneMode,
							"players":    room.Players,
							"maxPlayers": room.MaxPlayers,
							"status":     room.Status,
						}
						if rSeat >= 0 {
							m["assignedSeat"] = rSeat
						}
						return m
					})
				}
				if authRoom != nil {
					h.sendSanitizedToRoomLocked(authRoom, func(rSeat int, _ ScenePlayer) interface{} {
						evt := map[string]interface{}{
							"action":         "set_auto_host",
							"seat":           plSeat,
							"absSeat":        plSeat,
							"isAutoManaged":  true,
							"isAutoHost":     true,
							"isDisconnected": true,
							"playerName":     plName,
						}
						return map[string]interface{}{
							"type":       "game_event",
							"roomId":     authRoom.ID,
							"seat":       plSeat,
							"event":      evt,
							"payload":    evt,
							"boardState": sanitizeBoardStateForSeat(authRoom.BoardState, authRoom.GameType, rSeat),
						}
					})
				}
			}
		} else if room != nil {
			prevLen := len(room.Players)
			kept := make([]ScenePlayer, 0, len(room.Players))
			for _, pl := range room.Players {
				if pl.SocketID != "" {
					if pl.SocketID != c.id {
						kept = append(kept, pl)
					}
				} else if pl.ID != pID {
					kept = append(kept, pl)
				}
			}
			room.Players = kept
			if len(room.Players) != prevLen {
				if len(room.Players) == 0 {
					delete(h.sceneRooms, sRoomID)
				} else {
					for i := range room.Players {
						room.Players[i].Seat = i
					}
					if room.Status != "playing" && room.Status != "starting" {
						room.Status = "waiting"
					}
					h.sendSanitizedToRoomLocked(&RoomState{ID: room.ID, Players: room.Players}, func(_ int, _ ScenePlayer) interface{} {
						return map[string]interface{}{
							"type":       "scene_room_update",
							"roomId":     room.ID,
							"gameType":   room.GameType,
							"sceneMode":  room.SceneMode,
							"players":    room.Players,
							"maxPlayers": room.MaxPlayers,
							"status":     room.Status,
						}
					})
				}
			}
		}
	}

	if pID != "" && !hasActiveOtherSocket {
		keptCh := h.challenges[:0]
		for _, ch := range h.challenges {
			if ch.SenderID == pID && len(ch.AcceptedBy) == 0 {
				h.BroadcastJSON(map[string]interface{}{
					"type":        "challenge_status_update",
					"challengeId": ch.ID,
					"status":      "removed",
					"senderId":    pID,
					"senderName":  ch.SenderName,
					"gameType":    ch.GameType,
					"sceneMode":   ch.SceneMode,
				})
				continue
			}
			keptCh = append(keptCh, ch)
		}
		h.challenges = keptCh
	}
}

func (h *Hub) sweepStaleSockets() {
	now := nowMs()
	var toClose []*WSConn
	var toPing []*WSConn

	h.clientsMu.RLock()
	for _, c := range h.clients {
		hb := c.lastHeartbeat.Load()
		if hb > 0 && now-hb > 60000 {
			toClose = append(toClose, c)
		} else {
			toPing = append(toPing, c)
		}
	}
	h.clientsMu.RUnlock()

	for _, c := range toPing {
		_ = c.SendPing()
	}
	for _, c := range toClose {
		c.Close()
	}
}

func (h *Hub) cleanupEmptyRooms() {
	now := nowMs()
	h.roomMu.Lock()
	for id, r := range h.rooms {
		if id == "RM-202" || id == "RM-204" || id == "RM-205" {
			continue
		}
		if r.CreatedAt > 0 && now-r.CreatedAt > 3600000 && (r.Spectators <= 1 || len(r.Players) == 0) {
			delete(h.rooms, id)
		}
	}
	for id, sr := range h.sceneRooms {
		if sr.CreatedAt > 0 && now-sr.CreatedAt > 3600000 && len(sr.Players) == 0 {
			delete(h.sceneRooms, id)
		}
	}
	h.roomMu.Unlock()
}

func (h *Hub) BroadcastJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.clientsMu.RLock()
	conns := make([]*WSConn, 0, len(h.clients))
	for _, c := range h.clients {
		conns = append(conns, c)
	}
	h.clientsMu.RUnlock()

	for _, c := range conns {
		_ = c.WriteMessage(opText, data)
	}
}

func (h *Hub) findOpenSocketLocked(pl ScenePlayer, connsByID map[string]*WSConn, connsByPlayer map[string]*WSConn) *WSConn {
	if pl.SocketID != "" {
		if c, ok := connsByID[pl.SocketID]; ok && !c.closed.Load() {
			return c
		}
	}
	if pl.ID != "" {
		if c, ok := connsByPlayer[pl.ID]; ok && !c.closed.Load() {
			return c
		}
	}
	return nil
}

// sendSanitizedToRoomLocked must be called with h.roomMu held.
func (h *Hub) sendSanitizedToRoomLocked(room *RoomState, buildPayload func(seat int, player ScenePlayer) interface{}) {
	if room == nil || buildPayload == nil {
		return
	}
	combined := make([]ScenePlayer, 0, len(room.Players)+4)
	combined = append(combined, room.Players...)
	if sr := h.sceneRooms[room.ID]; sr != nil {
		combined = append(combined, sr.Players...)
	}

	h.clientsMu.RLock()
	connsByID := make(map[string]*WSConn, len(h.clients))
	connsByPlayer := make(map[string]*WSConn, len(h.clients))
	roomConns := make([]*WSConn, 0, 8)
	for _, c := range h.clients {
		if c.closed.Load() {
			continue
		}
		connsByID[c.id] = c
		if c.scenePlayerID != "" {
			connsByPlayer[c.scenePlayerID] = c
		}
		if c.sceneRoomID == room.ID {
			roomConns = append(roomConns, c)
		}
	}
	h.clientsMu.RUnlock()

	type outbound struct {
		conn    *WSConn
		payload interface{}
	}
	var messages []outbound
	sent := map[*WSConn]bool{}

	for _, pl := range combined {
		if isBotLikePlayer(pl) {
			continue
		}
		target := h.findOpenSocketLocked(pl, connsByID, connsByPlayer)
		if target != nil && !sent[target] {
			sent[target] = true
			messages = append(messages, outbound{
				conn:    target,
				payload: buildPayload(pl.Seat, pl),
			})
		}
	}

	for _, c := range roomConns {
		if sent[c] {
			continue
		}
		sent[c] = true
		seat := -1
		var matched ScenePlayer
		for _, pl := range combined {
			if pl.ID == c.scenePlayerID {
				seat = pl.Seat
				matched = pl
				break
			}
		}
		messages = append(messages, outbound{
			conn:    c,
			payload: buildPayload(seat, matched),
		})
	}

	for _, m := range messages {
		if m.payload != nil {
			_ = m.conn.WriteJSON(m.payload)
		}
	}
}

// resetSceneRoomReadyLocked must be called with h.roomMu held.
func (h *Hub) resetSceneRoomReadyLocked(roomID string) {
	authRoom := h.rooms[roomID]
	sr := h.sceneRooms[roomID]

	expiresAt := nowMs() + settlementTimeoutMs
	if authRoom != nil && authRoom.SettlementExpiresAt > 0 {
		expiresAt = authRoom.SettlementExpiresAt
	} else if sr != nil && sr.SettlementExpiresAt > 0 {
		expiresAt = sr.SettlementExpiresAt
	}

	if authRoom != nil {
		authRoom.SettlementExpiresAt = expiresAt
		for i := range authRoom.Players {
			authRoom.Players[i].IsReady = isBotLikePlayer(authRoom.Players[i])
		}
	}
	if sr != nil {
		sr.Status = "finished"
		sr.SettlementExpiresAt = expiresAt
		for i := range sr.Players {
			sr.Players[i].IsReady = isBotLikePlayer(sr.Players[i])
		}
		h.sendSanitizedToRoomLocked(&RoomState{ID: sr.ID, Players: sr.Players}, func(_ int, _ ScenePlayer) interface{} {
			return map[string]interface{}{
				"type":                "scene_room_update",
				"roomId":              sr.ID,
				"gameType":            sr.GameType,
				"sceneMode":           sr.SceneMode,
				"players":             sr.Players,
				"maxPlayers":          sr.MaxPlayers,
				"status":              sr.Status,
				"settlementExpiresAt": expiresAt,
				"serverTs":            nowMs(),
			}
		})
	}
	h.scheduleSnapshot()
}

func (h *Hub) buildSanitizedRoomsListLocked() []map[string]interface{} {
	list := make([]map[string]interface{}, 0, len(h.rooms))
	for _, r := range h.rooms {
		if r == nil {
			continue
		}
		list = append(list, map[string]interface{}{
			"id":          r.ID,
			"title":       r.Title,
			"gameType":    r.GameType,
			"sceneMode":   r.SceneMode,
			"maxPlayers":  r.MaxPlayers,
			"status":      r.Status,
			"hasPasscode": r.HasPasscode,
			"players":     r.Players,
			"spectators":  r.Spectators,
			"turnSeat":    r.TurnSeat,
			"moveCount":   r.MoveCount,
			"roundIndex":  r.RoundIndex,
			"modeLabel":   r.ModeLabel,
			"chat":        r.Chat,
			"boardState":  r.BoardState,
		})
	}
	return list
}

// ==============================================================================
// Presence & Teahouse Roster (Ported from src/presenceTracker.ts)
// ==============================================================================

func countOnlineTeahouse(players []TeahousePlayerItem) int {
	n := 0
	for _, p := range players {
		if p.IsOnline {
			n++
		}
	}
	return n
}

func maskPhoneDisplay(phone string) string {
	np := strings.TrimSpace(phone)
	if len(np) < 7 {
		return np
	}
	return np[:3] + "****" + np[len(np)-4:]
}

func (h *Hub) buildAuthoritativeSeatMap() map[string]struct{ gameType, sceneMode, status string } {
	seatMap := map[string]struct{ gameType, sceneMode, status string }{}
	h.roomMu.Lock()
	defer h.roomMu.Unlock()
	for _, room := range h.sceneRooms {
		if room == nil || room.GameType == "" || room.SceneMode == "" {
			continue
		}
		st := room.Status
		if st == "" {
			st = "waiting"
		}
		entry := struct{ gameType, sceneMode, status string }{room.GameType, room.SceneMode, st}
		for _, pl := range room.Players {
			if isBotLikePlayer(pl) {
				continue
			}
			if pl.ID != "" {
				seatMap[pl.ID] = entry
			}
			if pl.OriginalID != "" {
				seatMap[pl.OriginalID] = entry
			}
			if pl.SocketID != "" {
				seatMap["sock:"+pl.SocketID] = entry
			}
		}
	}
	return seatMap
}

func (h *Hub) getSceneWaitingStats() map[string]*SceneWaitingStat {
	stats := map[string]*SceneWaitingStat{}
	seenInScene := map[string]map[string]bool{}
	seenWaiting := map[string]map[string]bool{}

	h.roomMu.Lock()
	defer h.roomMu.Unlock()

	for _, room := range h.sceneRooms {
		if room == nil || room.SceneMode == "" {
			continue
		}
		mode := room.SceneMode
		st, ok := stats[mode]
		if !ok {
			st = &SceneWaitingStat{
				GameType:       room.GameType,
				SceneMode:      mode,
				WaitingPlayers: []SceneWaitingPlayerSummary{},
			}
			stats[mode] = st
			seenInScene[mode] = map[string]bool{}
			seenWaiting[mode] = map[string]bool{}
		}
		humans := filterHumanPlayers(room.Players)
		if len(humans) == 0 {
			continue
		}
		for _, p := range humans {
			key := p.OriginalID
			if key == "" {
				key = p.ID
			}
			if key == "" {
				key = p.Name
			}
			if key != "" {
				seenInScene[mode][key] = true
			}
		}
		if room.Status == "waiting" {
			if len(humans) > st.WaitingCount {
				st.WaitingCount = len(humans)
			}
			for _, p := range humans {
				key := p.OriginalID
				if key == "" {
					key = p.ID
				}
				if key == "" {
					key = p.Name
				}
				if key != "" && !seenWaiting[mode][key] {
					seenWaiting[mode][key] = true
					avatar := p.Avatar
					if avatar == "" {
						avatar = "👑"
					}
					name := p.Name
					if name == "" {
						name = "牌友"
					}
					st.WaitingPlayers = append(st.WaitingPlayers, SceneWaitingPlayerSummary{
						ID:     p.ID,
						Name:   name,
						Avatar: avatar,
						Coins:  p.Coins,
					})
				}
			}
		} else {
			if room.Status == "starting" || room.Status == "playing" {
				st.ActiveTablesCount++
			}
			st.PlayingCount += len(humans)
		}
	}
	for mode, st := range stats {
		st.TotalInScene = len(seenInScene[mode])
	}
	return stats
}

func (h *Hub) getAllPlayers(dbProfiles map[string]*UserProfile) []TeahousePlayerItem {
	list := make([]TeahousePlayerItem, 0, len(dbProfiles)+8)
	now := nowMs()
	seenIDs := map[string]bool{}
	seatMap := h.buildAuthoritativeSeatMap()

	h.clientsMu.RLock()
	activeSockMap := map[string]*ClientPresenceInfo{}
	presCopy := make([]*ClientPresenceInfo, 0, len(h.presence))
	for _, info := range h.presence {
		cp := *info
		presCopy = append(presCopy, &cp)
		if cp.PlayerID != "" {
			activeSockMap[cp.PlayerID] = &cp
		}
		if cp.PlayerName != "" {
			activeSockMap[cp.PlayerName] = &cp
		}
		if cp.Phone != "" {
			activeSockMap[cp.Phone] = &cp
		}
	}
	h.clientsMu.RUnlock()

	resolveLoc := func(pID string, activeSock *ClientPresenceInfo) (string, string) {
		if seatInfo, ok := seatMap[pID]; ok {
			return seatInfo.gameType, seatInfo.sceneMode
		}
		if activeSock != nil && activeSock.SocketID != "" {
			if seatInfo, ok := seatMap["sock:"+activeSock.SocketID]; ok {
				return seatInfo.gameType, seatInfo.sceneMode
			}
		}
		if activeSock != nil && (activeSock.SceneMode == "four-booking" || activeSock.SceneMode == "eight-booking") {
			return "shisanshui", activeSock.SceneMode
		}
		return "lobby", "hall"
	}

	for id, prof := range dbProfiles {
		if prof == nil || prof.IsBanned {
			continue
		}
		if prof.Name == "测试茶客" || prof.Username == "admin" {
			continue
		}
		pID := prof.ID
		if pID == "" {
			pID = id
		}
		seenIDs[pID] = true
		if prof.Phone != "" {
			seenIDs[normalizePhone(prof.Phone)] = true
		}

		var activeSock *ClientPresenceInfo
		if s, ok := activeSockMap[pID]; ok {
			activeSock = s
		} else if prof.Phone != "" {
			if s, ok := activeSockMap[prof.Phone]; ok {
				activeSock = s
			}
		}
		if activeSock == nil && prof.Name != "" {
			if s, ok := activeSockMap[prof.Name]; ok {
				activeSock = s
			}
		}

		isOnline := activeSock != nil && (now-activeSock.LastSeen < 300000)
		gType, sMode := "lobby", "hall"
		if isOnline {
			gType, sMode = resolveLoc(pID, activeSock)
		}

		pName := prof.Name
		if pName == "" {
			suffix := "9999"
			if len(prof.Phone) >= 4 {
				suffix = prof.Phone[len(prof.Phone)-4:]
			}
			pName = "茶客" + suffix
		}
		avatar := prof.Avatar
		if avatar == "" {
			avatar = "👑"
		}
		rating := prof.Rating
		if rating <= 0 {
			rating = 1800
		}
		level := prof.Level
		if level <= 0 {
			level = 1
		}
		lastSeen := prof.UpdatedAt
		if lastSeen == 0 {
			lastSeen = prof.RegisteredAt
		}
		if activeSock != nil && activeSock.LastSeen > 0 {
			lastSeen = activeSock.LastSeen
		}

		list = append(list, TeahousePlayerItem{
			ID:        pID,
			Name:      pName,
			Avatar:    avatar,
			Coins:     prof.Coins,
			Rating:    rating,
			Level:     level,
			Phone:     maskPhoneDisplay(prof.Phone),
			IsAdmin:   prof.IsAdmin,
			IsOnline:  isOnline,
			GameType:  gType,
			SceneMode: sMode,
			LastSeen:  lastSeen,
		})
	}

	for _, c := range presCopy {
		if c.PlayerID == "" && c.PlayerName == "" && c.Phone == "" {
			continue
		}
		cID := c.PlayerID
		if cID == "" {
			cID = "sock-" + c.SocketID
		}
		cName := c.PlayerName
		if cName == "" {
			cName = "在线牌友"
		}
		if seenIDs[cID] || seenIDs[cName] {
			continue
		}
		seenIDs[cID] = true
		seenIDs[cName] = true
		gType, sMode := resolveLoc(cID, c)
		avatar := c.Avatar
		if avatar == "" {
			avatar = "👑"
		}
		coins := 2000
		if c.Coins != nil {
			coins = *c.Coins
		}
		rating := 1800
		if c.Rating != nil && *c.Rating > 0 {
			rating = *c.Rating
		}
		lastSeen := c.LastSeen
		if lastSeen == 0 {
			lastSeen = now
		}
		list = append(list, TeahousePlayerItem{
			ID:        cID,
			Name:      cName,
			Avatar:    avatar,
			Coins:     coins,
			Rating:    rating,
			Level:     1,
			Phone:     maskPhoneDisplay(c.Phone),
			IsOnline:  true,
			GameType:  gType,
			SceneMode: sMode,
			LastSeen:  lastSeen,
		})
	}

	sort.SliceStable(list, func(i, j int) bool {
		if list[i].IsOnline != list[j].IsOnline {
			return list[i].IsOnline
		}
		if list[i].Coins != list[j].Coins {
			return list[i].Coins > list[j].Coins
		}
		return list[i].LastSeen > list[j].LastSeen
	})

	return list
}

func (h *Hub) getRealGamePresence() RealPresenceCounts {
	h.clientsMu.RLock()
	totalOnline := len(h.clients)
	presCopy := make([]ClientPresenceInfo, 0, len(h.presence))
	for _, info := range h.presence {
		presCopy = append(presCopy, *info)
	}
	h.clientsMu.RUnlock()

	sceneStats := h.getSceneWaitingStats()

	counts := RealPresenceCounts{
		TotalOnline:       totalOnline,
		SceneWaitingStats: sceneStats,
	}

	h.roomMu.Lock()
	defer h.roomMu.Unlock()

	for _, gt := range []string{"yansan", "shisanshui", "mahjong"} {
		distinct := map[string]bool{}
		for _, sr := range h.sceneRooms {
			if sr == nil || sr.GameType != gt {
				continue
			}
			for _, p := range sr.Players {
				if !isBotLikePlayer(p) {
					key := p.OriginalID
					if key == "" {
						key = p.ID
					}
					if key == "" {
						key = p.Name
					}
					if key != "" {
						distinct[key] = true
					}
				}
			}
		}
		if gt == "shisanshui" {
			for _, c := range presCopy {
				if c.GameType == "shisanshui" && (c.SceneMode == "four-booking" || c.SceneMode == "eight-booking") {
					key := c.PlayerID
					if key == "" {
						key = c.PlayerName
					}
					if key == "" {
						key = "sock-" + c.SocketID
					}
					distinct[key] = true
				}
			}
		}
		for _, r := range h.rooms {
			if r == nil || r.ID == "RM-202" || r.ID == "RM-204" || r.ID == "RM-205" {
				continue
			}
			if r.GameType == gt {
				for _, p := range r.Players {
					if !isBotLikePlayer(p) && p.ID != "" && !strings.HasPrefix(p.ID, "P-sim-") {
						key := p.OriginalID
						if key == "" {
							key = p.ID
						}
						if key == "" {
							key = p.Name
						}
						if key != "" {
							distinct[key] = true
						}
					}
				}
			}
		}
		switch gt {
		case "yansan":
			counts.Yansan = len(distinct)
		case "shisanshui":
			counts.Shisanshui = len(distinct)
		case "mahjong":
			counts.Mahjong = len(distinct)
		}
	}

	return counts
}

func (h *Hub) broadcastTeahousePlayers() {
	var players []TeahousePlayerItem
	h.storage.WithDBRead(func(db *DatabaseSchema) {
		players = h.getAllPlayers(db.Profiles)
	})
	h.BroadcastJSON(map[string]interface{}{
		"type":        "teahouse_players_update",
		"count":       len(players),
		"onlineCount": countOnlineTeahouse(players),
		"players":     players,
	})
}

func (h *Hub) broadcastPresenceAndPlayers() {
	h.BroadcastJSON(map[string]interface{}{
		"type":   "presence_counts",
		"counts": h.getRealGamePresence(),
	})
	h.broadcastTeahousePlayers()
}

// ==============================================================================
// Matchmaking & Room Start
// ==============================================================================

func pickBestWaitingRoomLocked(rooms map[string]*SceneWaitingRoom, gameType, sceneMode string) *SceneWaitingRoom {
	var best *SceneWaitingRoom
	bestCount := -1
	for _, r := range rooms {
		if r == nil || r.GameType != gameType || r.SceneMode != sceneMode || r.Status != "waiting" {
			continue
		}
		hc := len(filterHumanPlayers(r.Players))
		if hc < r.MaxPlayers && hc > bestCount {
			best = r
			bestCount = hc
		}
	}
	return best
}

// startRoomGameIfFullLocked must be called with h.roomMu held.
func (h *Hub) startRoomGameIfFullLocked(room *SceneWaitingRoom) bool {
	if room == nil {
		return false
	}
	humans := filterHumanPlayers(room.Players)
	if len(humans) != len(room.Players) {
		room.Players = humans
	}
	if len(room.Players) < room.MaxPlayers {
		if room.Status != "waiting" {
			room.Status = "waiting"
			room.Deal = nil
		}
		return false
	}
	if (room.Status == "starting" || room.Status == "playing") && room.Deal != nil && (room.Deal.SeatCards != nil || room.Deal.MahjongSeatCards != nil) {
		return true
	}

	validPlayers := make([]ScenePlayer, len(humans))
	copy(validPlayers, humans)
	sort.SliceStable(validPlayers, func(i, j int) bool {
		sa := validPlayers[i].Seat
		if sa < 0 {
			sa = 999
		}
		sb := validPlayers[j].Seat
		if sb < 0 {
			sb = 999
		}
		return sa < sb
	})
	if len(validPlayers) > room.MaxPlayers {
		validPlayers = validPlayers[:room.MaxPlayers]
	}
	if len(validPlayers) < room.MaxPlayers {
		room.Players = validPlayers
		room.Status = "waiting"
		room.Deal = nil
		return false
	}

	now := nowMs()
	for idx := range validPlayers {
		validPlayers[idx].Seat = idx
		validPlayers[idx].IsReady = false
		validPlayers[idx].IsDisconnected = false
		validPlayers[idx].IsAutoManaged = false
		if validPlayers[idx].ID == "" {
			validPlayers[idx].ID = fmt.Sprintf("P-usr-%d-%s", idx, randomHex(2))
		}
		if validPlayers[idx].Name == "" {
			validPlayers[idx].Name = fmt.Sprintf("茶客 #%d", idx+1)
		}
	}
	room.Players = validPlayers
	room.Status = "starting"
	room.SettlementExpiresAt = 0
	room.RoundIndex++

	var freshDeal *RoomDeal
	switch room.GameType {
	case "shisanshui":
		freshDeal = createShisanshuiDeal(room.SceneMode, room.MaxPlayers)
	case "mahjong":
		freshDeal = createMahjongDeal(room.SceneMode)
	default:
		freshDeal = createYansanDeal(room.SceneMode)
	}
	if freshDeal.DealID == "" {
		freshDeal.DealID = fmt.Sprintf("%s-%s", room.GameType, randomHex(4))
	}
	if room.GameType == "yansan" && freshDeal.SeatCards != nil {
		initCards := make([][]Card, 4)
		for s := 0; s < 4; s++ {
			hand := freshDeal.SeatCards[s]
			cp := make([]Card, len(hand))
			copy(cp, hand)
			initCards[s] = cp
		}
		freshDeal.InitialSeatCards = initCards
	}
	room.Deal = freshDeal

	defaultBoard := createDefaultBoardState(room.ID, room.GameType)
	if room.GameType == "yansan" {
		defaultBoard.TurnSeat = freshDeal.StarterSeat
		defaultBoard.ExtraState.HasPassedSeats = []bool{false, false, false, false}
		defaultBoard.ExtraState.SeatPlayedCards = [][]Card{{}, {}, {}, {}}
		defaultBoard.ExtraState.PlayHistory = []YansanPlayRecord{}
		defaultBoard.ExtraState.IsQiangGongActive = false
		defaultBoard.ExtraState.QiangGongSeat = nil
	} else if room.GameType == "mahjong" {
		defaultBoard.SeatKongScores = []int{0, 0, 0, 0}
		defaultBoard.ExtraState.KongHistory = []MahjongKongRecord{}
		defaultBoard.ExtraState.ClaimWindow = nil
		defaultBoard.ExtraState.WallRemaining = len(freshDeal.RemainingWall)
		dSeat := freshDeal.DealerSeat
		defaultBoard.TurnSeat = dSeat
		hc := make([]int, 4)
		for s := 0; s < 4; s++ {
			if l := len(freshDeal.MahjongSeatCards[s]); l > 0 {
				hc[s] = l
			} else if s == dSeat {
				hc[s] = 14
			} else {
				hc[s] = 13
			}
		}
		defaultBoard.HandsCount = hc
	} else if room.GameType == "shisanshui" && room.MaxPlayers == 8 {
		defaultBoard.HandsCount = []int{13, 13, 13, 13, 13, 13, 13, 13}
	}

	roomPlayersCopy := make([]ScenePlayer, len(room.Players))
	for i, pl := range room.Players {
		pl.Seat = i
		if pl.Rating == 0 {
			pl.Rating = 1900
		}
		roomPlayersCopy[i] = pl
	}

	authRoom, exists := h.rooms[room.ID]
	if !exists || authRoom == nil {
		authRoom = &RoomState{
			ID:         room.ID,
			Title:      room.SceneName + " · 纯真人对决",
			GameType:   room.GameType,
			SceneMode:  room.SceneMode,
			MaxPlayers: room.MaxPlayers,
			Status:     "starting",
			Players:    roomPlayersCopy,
			TurnSeat:   freshDeal.StarterSeat,
			LastTurnTs: now,
			MoveCount:  0,
			RoundIndex: room.RoundIndex,
			Deal:       freshDeal,
			ModeLabel:  room.SceneName + " · 实时对决",
			Chat:       []RoomChatMessage{},
			BoardState: defaultBoard,
			CreatedAt:  now,
		}
		h.rooms[room.ID] = authRoom
	} else {
		authRoom.Status = "starting"
		authRoom.SceneMode = room.SceneMode
		authRoom.MaxPlayers = room.MaxPlayers
		authRoom.SettlementExpiresAt = 0
		authRoom.MoveCount = 0
		authRoom.RoundIndex = room.RoundIndex
		authRoom.CoinsSettledKey = ""
		authRoom.Deal = freshDeal
		authRoom.TurnSeat = freshDeal.StarterSeat
		authRoom.LastTurnTs = now
		authRoom.BoardState = defaultBoard
		authRoom.Players = roomPlayersCopy
	}

	log.Printf("🚀 [满员自动发牌] 房间 %s (%s-%s) %d/%d 玩家席位校验完毕，定向脱敏广播 scene_room_full！",
		room.ID, room.GameType, room.SceneMode, len(room.Players), room.MaxPlayers)

	h.sendSanitizedToRoomLocked(authRoom, func(seat int, player ScenePlayer) interface{} {
		msg := map[string]interface{}{
			"type":             "scene_room_full",
			"roomId":           room.ID,
			"gameType":         room.GameType,
			"sceneMode":        room.SceneMode,
			"players":          room.Players,
			"maxPlayers":       room.MaxPlayers,
			"deal":             sanitizeDealForSeat(freshDeal, seat),
			"turnSeat":         freshDeal.StarterSeat,
			"boardState":       defaultBoard,
			"status":           "starting",
			"lockGameLaunch":   true,
			"forceReset":       true,
			"timestamp":        now,
		}
		if seat >= 0 {
			msg["assignedSeat"] = seat
		}
		if player.ID != "" {
			msg["assignedPlayerId"] = player.ID
		}
		return msg
	})

	roomID := room.ID
	expectedDeal := freshDeal
	time.AfterFunc(1400*time.Millisecond, func() {
		h.roomMu.Lock()
		sr := h.sceneRooms[roomID]
		ar := h.rooms[roomID]
		if sr != nil && sr.Deal == expectedDeal {
			sr.Status = "playing"
			for i := range sr.Players {
				sr.Players[i].IsReady = isBotLikePlayer(sr.Players[i])
			}
			if ar != nil {
				ar.Status = "playing"
				ar.LastTurnTs = nowMs()
				for i := range ar.Players {
					ar.Players[i].IsReady = isBotLikePlayer(ar.Players[i])
				}
			}
			bs := defaultBoard
			if ar != nil && ar.BoardState != nil {
				bs = ar.BoardState
			}
			target := ar
			if target == nil {
				target = &RoomState{ID: sr.ID, Players: sr.Players}
			}
			h.sendSanitizedToRoomLocked(target, func(seat int, player ScenePlayer) interface{} {
				m := map[string]interface{}{
					"type":           "scene_room_started",
					"roomId":         sr.ID,
					"gameType":       sr.GameType,
					"sceneMode":      sr.SceneMode,
					"players":        sr.Players,
					"maxPlayers":     sr.MaxPlayers,
					"deal":           sanitizeDealForSeat(expectedDeal, seat),
					"turnSeat":       expectedDeal.StarterSeat,
					"boardState":     bs,
					"status":         "playing",
					"lockGameLaunch": true,
					"forceReset":     true,
					"timestamp":      nowMs(),
				}
				if seat >= 0 {
					m["assignedSeat"] = seat
				}
				if player.ID != "" {
					m["assignedPlayerId"] = player.ID
				}
				return m
			})
			h.scheduleSnapshot()
		}
		h.roomMu.Unlock()
	})

	h.scheduleSnapshot()
	return true
}

// ==============================================================================
// Authoritative 1-Second Game & Settlement Ticker
// ==============================================================================

func (h *Hub) tickRooms() {
	now := nowMs()
	needPresenceBroadcast := false

	h.roomMu.Lock()
	// Collect IDs to avoid mutating map during iteration if needed
	roomIDs := make([]string, 0, len(h.rooms))
	for id := range h.rooms {
		roomIDs = append(roomIDs, id)
	}

	for _, id := range roomIDs {
		room := h.rooms[id]
		if room == nil {
			continue
		}

		if (room.Status == "finished" || (room.BoardState != nil && room.BoardState.Phase == "ended")) &&
			room.SettlementExpiresAt > 0 && now >= room.SettlementExpiresAt {

			sr := h.sceneRooms[room.ID]
			curPlayers := room.Players
			if sr != nil && len(sr.Players) > 0 {
				curPlayers = sr.Players
			}

			var timedOutIDs []string
			var timedOutSeats []int
			var readyPlayers []ScenePlayer

			for _, p := range curPlayers {
				if isBotLikePlayer(p) {
					continue
				}
				if p.IsReady {
					readyPlayers = append(readyPlayers, p)
				} else {
					timedOutIDs = append(timedOutIDs, p.ID)
					timedOutSeats = append(timedOutSeats, p.Seat)
				}
			}

			if len(timedOutIDs) > 0 {
				log.Printf("⏱️ [结算超时踢出] 房间 %s 结算倒计时结束，自动返回大厅玩家: %s", room.ID, strings.Join(timedOutIDs, ","))
				h.sendSanitizedToRoomLocked(room, func(_ int, _ ScenePlayer) interface{} {
					return map[string]interface{}{
						"type":              "settlement_timeout",
						"roomId":            room.ID,
						"gameType":          room.GameType,
						"timedOutPlayerIds": timedOutIDs,
						"timedOutSeats":     timedOutSeats,
						"message":           "结算超时未点击继续下一局，已自动返回大厅",
					}
				})
			}

			room.SettlementExpiresAt = 0
			if sr != nil {
				sr.SettlementExpiresAt = 0
			}

			if len(readyPlayers) == 0 {
				delete(h.sceneRooms, room.ID)
				delete(h.rooms, room.ID)
			} else {
				for idx := range readyPlayers {
					readyPlayers[idx].Seat = idx
					readyPlayers[idx].IsReady = true
				}
				if sr != nil {
					sr.Players = readyPlayers
					sr.Status = "waiting"
					sr.Deal = nil
				}
				cp := make([]ScenePlayer, len(readyPlayers))
				copy(cp, readyPlayers)
				room.Players = cp
				room.Status = "waiting"
				room.MoveCount = 0
				room.TurnSeat = 0
				room.Deal = nil
				room.LastTurnTs = 0
				room.CoinsSettledKey = ""
				room.BoardState = createDefaultBoardState(room.ID, room.GameType)

				sceneMode := room.SceneMode
				if sceneMode == "" && sr != nil {
					sceneMode = sr.SceneMode
				}
				maxP := room.MaxPlayers
				if maxP <= 0 && sr != nil && sr.MaxPlayers > 0 {
					maxP = sr.MaxPlayers
				}
				if maxP <= 0 {
					maxP = 4
				}
				h.sendSanitizedToRoomLocked(room, func(_ int, _ ScenePlayer) interface{} {
					return map[string]interface{}{
						"type":       "scene_room_update",
						"roomId":     room.ID,
						"gameType":   room.GameType,
						"sceneMode":  sceneMode,
						"players":    room.Players,
						"maxPlayers": maxP,
						"status":     "waiting",
					}
				})
			}

			h.scheduleSnapshot()
			needPresenceBroadcast = true
			continue
		}

		if room.Status != "playing" || room.BoardState == nil || room.BoardState.Phase != "playing" {
			continue
		}

		if room.GameType == "shisanshui" {
			tickShisanshuiRoom(h.ctx, room, now)
			continue
		}

		if tickMahjongClaimWindow(h.ctx, room, now) {
			continue
		}

		currentSeat := room.TurnSeat
		var curPlayer *ScenePlayer
		for i := range room.Players {
			if room.Players[i].Seat == currentSeat {
				curPlayer = &room.Players[i]
				break
			}
		}

		isAuto := false
		isDisconnOrBot := false
		if curPlayer != nil {
			isDisconnOrBot = curPlayer.IsDisconnected || isBotLikePlayer(*curPlayer)
			if curPlayer.IsAutoManaged || isDisconnOrBot {
				isAuto = true
			}
		}
		if room.BoardState.AutoManagedSeats != nil && room.BoardState.AutoManagedSeats[currentSeat] {
			isAuto = true
		}
		isConnectedManualAuto := isAuto && !isDisconnOrBot

		mjWallEmptyPendingDraw := false
		if room.GameType == "mahjong" && room.Deal != nil && len(room.Deal.RemainingWall) == 0 {
			if room.BoardState.ExtraState == nil || room.BoardState.ExtraState.PendingPengSeat == nil {
				mjWallEmptyPendingDraw = true
			}
		}

		baseTimeout := int64(60000)
		if isConnectedManualAuto {
			baseTimeout = 2600
		} else if isAuto {
			baseTimeout = 1200
		}

		isYansanBaoDan := room.GameType == "yansan" && room.Deal != nil && room.Deal.SeatCards != nil && len(room.Deal.SeatCards[currentSeat]) == 1

		threshold := baseTimeout
		if mjWallEmptyPendingDraw && threshold > 3500 {
			threshold = 3500
		} else if isYansanBaoDan && threshold > 2500 {
			threshold = 2500
		}

		if room.LastTurnTs == 0 || now-room.LastTurnTs <= threshold {
			continue
		}
		room.LastTurnTs = now

		if !isAuto && !mjWallEmptyPendingDraw {
			if curPlayer != nil {
				curPlayer.IsAutoManaged = true
			}
			if room.BoardState.AutoManagedSeats == nil {
				room.BoardState.AutoManagedSeats = map[int]bool{}
			}
			room.BoardState.AutoManagedSeats[currentSeat] = true
			h.sendSanitizedToRoomLocked(room, func(rSeat int, _ ScenePlayer) interface{} {
				evt := map[string]interface{}{
					"action":        "set_auto_host",
					"seat":          currentSeat,
					"absSeat":       currentSeat,
					"isAutoManaged": true,
					"isAutoHost":    true,
				}
				return map[string]interface{}{
					"type":       "game_event",
					"roomId":     room.ID,
					"seat":       currentSeat,
					"event":      evt,
					"payload":    evt,
					"boardState": sanitizeBoardStateForSeat(room.BoardState, room.GameType, rSeat),
				}
			})
		}

		if room.GameType == "mahjong" {
			tickMahjongAutoTurn(h.ctx, room, currentSeat, now)
		} else {
			tickYansanAutoTurn(h.ctx, room, currentSeat, now)
		}
	}
	h.roomMu.Unlock()

	if needPresenceBroadcast {
		h.broadcastPresenceAndPlayers()
	}
}

// ==============================================================================
// WebSocket Message Helpers & Dispatcher
// ==============================================================================

func sanitizeWsText(v interface{}, maxRunes int) string {
	s, _ := v.(string)
	var b strings.Builder
	for _, r := range s {
		if r < 32 && r != '\n' && r != '\t' {
			continue
		}
		if r == 127 {
			continue
		}
		b.WriteRune(r)
	}
	trimmed := strings.TrimSpace(b.String())
	if maxRunes <= 0 || utf8.RuneCountInString(trimmed) <= maxRunes {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:maxRunes])
}

func jsonString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func jsonInt(m map[string]interface{}, key string, def int) int {
	if m == nil {
		return def
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return def
}

func jsonBool(m map[string]interface{}, key string, def bool) bool {
	if m == nil {
		return def
	}
	if b, ok := m[key].(bool); ok {
		return b
	}
	return def
}

func (h *Hub) verifyWsSession(c *WSConn, playerID, sessionToken string) (*UserProfile, bool) {
	if playerID == "" || sessionToken == "" {
		return nil, false
	}
	var matched *UserProfile
	h.storage.WithDBRead(func(db *DatabaseSchema) {
		prof := db.Profiles[playerID]
		if prof != nil && prof.SessionToken != "" && safeTokenEqual(prof.SessionToken, sessionToken) && !prof.IsBanned {
			cp := *prof
			matched = &cp
		}
	})
	if matched != nil {
		c.authenticatedUserID = matched.ID
		c.userID = matched.ID
		return matched, true
	}
	return nil, false
}

func (h *Hub) handleMessage(client *WSConn, raw []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	client.lastHeartbeat.Store(nowMs())
	msgType := jsonString(msg, "type")
	now := nowMs()

	switch msgType {
	case "ping":
		h.clientsMu.RLock()
		online := len(h.clients)
		h.clientsMu.RUnlock()
		_ = client.WriteJSON(map[string]interface{}{
			"type":        "pong",
			"clientTs":    msg["clientTs"],
			"serverTs":    now,
			"onlineCount": online,
		})
		return

	case "presence_update":
		targetGame := jsonString(msg, "gameType")
		if targetGame != "yansan" && targetGame != "shisanshui" && targetGame != "mahjong" {
			targetGame = "lobby"
		}
		pID := jsonString(msg, "playerId")
		if _, hasTok := msg["sessionToken"]; hasTok {
			tok := jsonString(msg, "sessionToken")
			if _, ok := h.verifyWsSession(client, pID, tok); !ok {
				client.authenticatedUserID = ""
				client.userID = pID
			}
		}
		sMode := jsonString(msg, "sceneMode")
		h.clientsMu.Lock()
		info, ok := h.presence[client.id]
		if !ok {
			info = &ClientPresenceInfo{SocketID: client.id}
			h.presence[client.id] = info
		}
		info.GameType = targetGame
		if _, has := msg["sceneMode"]; has {
			info.SceneMode = sMode
		}
		if pID != "" {
			info.PlayerID = pID
			if n := jsonString(msg, "playerName"); n != "" {
				info.PlayerName = n
			}
			if av := jsonString(msg, "avatar"); av != "" {
				info.Avatar = av
			}
			if ph := jsonString(msg, "phone"); ph != "" {
				info.Phone = ph
			}
			if _, hasCoins := msg["coins"].(float64); hasCoins {
				cVal := jsonInt(msg, "coins", 0)
				info.Coins = &cVal
			}
			if _, hasRating := msg["rating"].(float64); hasRating {
				rVal := jsonInt(msg, "rating", 1800)
				info.Rating = &rVal
			}
		}
		info.LastSeen = now
		h.clientsMu.Unlock()

		h.broadcastPresenceAndPlayers()
		return

	case "request_teahouse_players":
		var players []TeahousePlayerItem
		h.storage.WithDBRead(func(db *DatabaseSchema) {
			players = h.getAllPlayers(db.Profiles)
		})
		_ = client.WriteJSON(map[string]interface{}{
			"type":        "teahouse_players_update",
			"count":       len(players),
			"onlineCount": countOnlineTeahouse(players),
			"players":     players,
		})
		return
	}

	if handleBookingWsMessage(h.ctx, client, msg) {
		return
	}

	switch msgType {
	case "scene_reconnect":
		h.handleSceneReconnect(client, msg)
	case "scene_player_ready", "room_restart":
		h.handleScenePlayerReady(client, msg, msgType)
	case "scene_start_game":
		roomID := jsonString(msg, "roomId")
		h.roomMu.Lock()
		if sr := h.sceneRooms[roomID]; sr != nil {
			h.startRoomGameIfFullLocked(sr)
		}
		h.roomMu.Unlock()
	case "scene_fill_bots", "scene_quick_match":
		log.Printf("⛔ [纯真人防假人] 拦截人机补位请求 (%s)", msgType)
	case "scene_join":
		h.handleSceneJoin(client, msg)
	case "scene_leave":
		h.handleSceneLeave(client, msg)
	case "scene_chat":
		h.handleSceneChat(client, msg)
	case "scene_accept_challenge":
		h.handleSceneAcceptChallenge(client, msg)
	case "room_create":
		h.handleRoomCreate(client, msg)
	case "room_join":
		h.handleRoomJoin(client, msg)
	case "room_leave":
		h.handleRoomLeave(client, msg)
	case "room_reconnect":
		h.handleRoomReconnect(client, msg)
	case "request_game_deal":
		h.handleRequestGameDeal(client, msg)
	case "game_move":
		h.handleGameMove(client, msg)
	case "game_chat", "room_voice":
		h.handleRoomChat(client, msg)
	}
}

func (h *Hub) handleSceneReconnect(client *WSConn, msg map[string]interface{}) {
	tok := jsonString(msg, "sessionToken")
	rawPID := jsonString(msg, "playerId")
	if tok != "" && rawPID != "" {
		h.verifyWsSession(client, rawPID, tok)
	}
	pID := rawPID
	if pID == "" {
		pID = client.authenticatedUserID
	}
	if pID == "" {
		pID = client.scenePlayerID
	}
	targetRoomID := jsonString(msg, "roomId")
	pName := jsonString(msg, "playerName")

	h.roomMu.Lock()
	defer h.roomMu.Unlock()

	var targetRoom *SceneWaitingRoom
	if targetRoomID != "" {
		targetRoom = h.sceneRooms[targetRoomID]
	}
	if targetRoom == nil && pID != "" {
		for _, r := range h.sceneRooms {
			for _, pl := range r.Players {
				if pl.ID == pID || pl.OriginalID == pID || pl.UserID == pID {
					targetRoom = r
					break
				}
			}
			if targetRoom != nil {
				break
			}
		}
		if targetRoom == nil && pName != "" {
			for _, r := range h.sceneRooms {
				for _, pl := range r.Players {
					if pl.Name == pName {
						targetRoom = r
						break
					}
				}
				if targetRoom != nil {
					break
				}
			}
		}
	}

	if targetRoom != nil {
		plIdx := -1
		for i, p := range targetRoom.Players {
			if pID != "" && (p.ID == pID || p.OriginalID == pID || p.UserID == pID) {
				plIdx = i
				break
			}
		}
		if plIdx == -1 && pName != "" {
			for i, p := range targetRoom.Players {
				if p.Name == pName {
					plIdx = i
					break
				}
			}
		}
		if plIdx >= 0 {
			pl := &targetRoom.Players[plIdx]
			if tok != "" {
				invalid := false
				h.storage.WithDBRead(func(db *DatabaseSchema) {
					if prof := db.Profiles[pl.ID]; prof != nil && prof.SessionToken != "" && !safeTokenEqual(prof.SessionToken, tok) {
						invalid = true
					}
				})
				if invalid {
					_ = client.WriteJSON(map[string]interface{}{"type": "scene_reconnect_ack", "failed": true, "reason": "invalid_session"})
					return
				}
			}
			authRoom := h.rooms[targetRoom.ID]
			isFinished := targetRoom.Status == "finished" || (authRoom != nil && authRoom.BoardState != nil && authRoom.BoardState.Phase == "ended")
			if isFinished {
				targetRoom.Status = "finished"
			}
			pl.IsDisconnected = false
			if !isFinished {
				pl.IsAutoManaged = false
			}
			pl.SocketID = client.id
			client.sceneRoomID = targetRoom.ID
			client.scenePlayerID = pl.ID

			if authRoom != nil {
				for i := range authRoom.Players {
					apl := &authRoom.Players[i]
					if apl.ID == pl.ID || (pID != "" && (apl.ID == pID || apl.OriginalID == pID || apl.UserID == pID)) || (pName != "" && apl.Name == pName) {
						apl.IsDisconnected = false
						if !isFinished {
							apl.IsAutoManaged = false
						}
						apl.SocketID = client.id
						break
					}
				}
				if !isFinished && authRoom.BoardState != nil && authRoom.BoardState.AutoManagedSeats != nil {
					delete(authRoom.BoardState.AutoManagedSeats, pl.Seat)
				}
			}

			var rawDeal *RoomDeal
			if authRoom != nil && authRoom.Deal != nil {
				rawDeal = authRoom.Deal
			} else {
				rawDeal = targetRoom.Deal
			}
			var settleExp int64
			if authRoom != nil && authRoom.SettlementExpiresAt > 0 {
				settleExp = authRoom.SettlementExpiresAt
			} else if targetRoom.SettlementExpiresAt > 0 {
				settleExp = targetRoom.SettlementExpiresAt
			}
			var bs *AuthoritativeBoardState
			if authRoom != nil {
				bs = authRoom.BoardState
			}

			nowTs := nowMs()
			_ = client.WriteJSON(map[string]interface{}{
				"type":                "scene_reconnect_ack",
				"room":                targetRoom,
				"boardState":          sanitizeBoardStateForSeat(bs, targetRoom.GameType, pl.Seat),
				"players":             targetRoom.Players,
				"deal":                sanitizeDealForSeat(rawDeal, pl.Seat),
				"settlementExpiresAt": settleExp,
				"serverTs":            nowTs,
			})

			roomRef := authRoom
			if roomRef == nil {
				roomRef = &RoomState{ID: targetRoom.ID, Players: targetRoom.Players}
			}
			h.sendSanitizedToRoomLocked(roomRef, func(_ int, _ ScenePlayer) interface{} {
				return map[string]interface{}{
					"type":                "scene_room_update",
					"roomId":              targetRoom.ID,
					"gameType":            targetRoom.GameType,
					"sceneMode":           targetRoom.SceneMode,
					"players":             targetRoom.Players,
					"maxPlayers":          targetRoom.MaxPlayers,
					"status":              targetRoom.Status,
					"settlementExpiresAt": settleExp,
					"serverTs":            nowTs,
				}
			})
			if !isFinished {
				h.sendSanitizedToRoomLocked(roomRef, func(rSeat int, _ ScenePlayer) interface{} {
					evt := map[string]interface{}{
						"action":         "set_auto_host",
						"seat":           pl.Seat,
						"absSeat":        pl.Seat,
						"isAutoManaged":  false,
						"isAutoHost":     false,
						"isDisconnected": false,
					}
					return map[string]interface{}{
						"type":       "game_event",
						"roomId":     targetRoom.ID,
						"seat":       pl.Seat,
						"event":      evt,
						"payload":    evt,
						"boardState": sanitizeBoardStateForSeat(bs, targetRoom.GameType, rSeat),
					}
				})
			}
			return
		}
	}

	_ = client.WriteJSON(map[string]interface{}{"type": "scene_reconnect_ack", "failed": true, "reason": "room_not_found"})
}

func (h *Hub) handleScenePlayerReady(client *WSConn, msg map[string]interface{}, msgType string) {
	targetRoomID := jsonString(msg, "roomId")
	if targetRoomID == "" {
		targetRoomID = client.sceneRoomID
	}
	pID := jsonString(msg, "playerId")
	if pID == "" {
		pID = client.scenePlayerID
	}
	pName := jsonString(msg, "playerName")

	h.roomMu.Lock()
	defer h.roomMu.Unlock()

	var room *SceneWaitingRoom
	if targetRoomID != "" {
		room = h.sceneRooms[targetRoomID]
	}
	if room == nil {
		for _, r := range h.sceneRooms {
			for _, pl := range r.Players {
				if pl.SocketID == client.id || (pID != "" && (pl.ID == pID || pl.OriginalID == pID || pl.UserID == pID)) {
					room = r
					break
				}
			}
			if room != nil {
				break
			}
		}
	}
	if room == nil {
		return
	}

	authRoom := h.rooms[room.ID]
	if authRoom != nil && ((authRoom.BoardState != nil && authRoom.BoardState.Phase == "ended") || authRoom.Status == "finished") {
		room.Status = "finished"
	}
	if room.Status == "starting" || room.Status == "playing" {
		return
	}

	plIdx := -1
	for i, p := range room.Players {
		if p.SocketID == client.id {
			plIdx = i
			break
		}
	}
	if plIdx == -1 && pID != "" {
		for i, p := range room.Players {
			if p.ID == pID || p.OriginalID == pID || p.UserID == pID {
				plIdx = i
				break
			}
		}
	}
	if plIdx == -1 && pName != "" {
		for i, p := range room.Players {
			if p.Name == pName {
				plIdx = i
				break
			}
		}
	}
	isReady := jsonBool(msg, "isReady", true)
	if plIdx >= 0 {
		room.Players[plIdx].SocketID = client.id
		room.Players[plIdx].IsReady = isReady
		plID := room.Players[plIdx].ID
		if authRoom != nil {
			for i := range authRoom.Players {
				if authRoom.Players[i].ID == plID || (pID != "" && (authRoom.Players[i].ID == pID || authRoom.Players[i].OriginalID == pID)) {
					authRoom.Players[i].IsReady = isReady
					break
				}
			}
		}
	}
	room.Players = filterHumanPlayers(room.Players)

	readyCount := 0
	for _, p := range room.Players {
		if p.IsReady {
			readyCount++
		}
	}
	log.Printf("🎮 [联机准备] 房间 %s (%s) 玩家准备进度: %d/%d", room.ID, room.GameType, readyCount, room.MaxPlayers)

	if len(room.Players) >= room.MaxPlayers && readyCount == len(room.Players) {
		log.Printf("🚀 [全员就绪再开] 房间 %s (%d/%d) 全员准备完成，生成全新发牌切片开启下一局！", room.ID, readyCount, room.MaxPlayers)
		room.Status = "waiting"
		room.Deal = nil
		room.SettlementExpiresAt = 0
		if authRoom != nil {
			authRoom.SettlementExpiresAt = 0
		}
		h.startRoomGameIfFullLocked(room)
		return
	}

	settleExp := room.SettlementExpiresAt
	if settleExp == 0 && authRoom != nil {
		settleExp = authRoom.SettlementExpiresAt
	}
	h.sendSanitizedToRoomLocked(&RoomState{ID: room.ID, Players: room.Players}, func(_ int, _ ScenePlayer) interface{} {
		return map[string]interface{}{
			"type":                "scene_room_update",
			"roomId":              room.ID,
			"gameType":            room.GameType,
			"sceneMode":           room.SceneMode,
			"players":             room.Players,
			"maxPlayers":          room.MaxPlayers,
			"status":              room.Status,
			"settlementExpiresAt": settleExp,
			"serverTs":            nowMs(),
		}
	})
	_ = msgType
}

func (h *Hub) handleSceneJoin(client *WSConn, msg map[string]interface{}) {
	gType := jsonString(msg, "gameType")
	if gType == "" {
		gType = "yansan"
	}
	sMode := jsonString(msg, "sceneMode")
	if sMode == "" {
		sMode = "standard"
	}
	sName := jsonString(msg, "sceneName")
	if sName == "" {
		sName = "经典场"
	}
	maxP := jsonInt(msg, "maxPlayers", 0)
	if maxP <= 0 {
		if gType == "shisanshui" && strings.Contains(sMode, "eight") {
			maxP = 8
		} else {
			maxP = 4
		}
	}

	p := ScenePlayer{
		ID:     fmt.Sprintf("P-%d-%s", nowMs(), randomHex(2)),
		Name:   "牌友",
		Avatar: "👑",
		Rating: 1900,
		Coins:  2000,
		Seat:   0,
	}
	if pm, ok := msg["player"].(map[string]interface{}); ok {
		if id := jsonString(pm, "id"); id != "" {
			p.ID = id
		}
		if nm := jsonString(pm, "name"); nm != "" {
			p.Name = nm
		}
		if av := jsonString(pm, "avatar"); av != "" {
			p.Avatar = av
		}
		if r := jsonInt(pm, "rating", 0); r > 0 {
			p.Rating = r
		}
		if _, hasC := pm["coins"].(float64); hasC {
			p.Coins = jsonInt(pm, "coins", 0)
		}
	}

	rawID := p.ID
	if p.ID == "" || p.ID == "P-me" || p.ID == "P-123" {
		p.ID = "P-usr-" + client.id
		if rawID != "" {
			p.OriginalID = rawID
		} else {
			p.OriginalID = "P-me"
		}
	}
	p.IsSimulated = false
	p.IsAi = false
	p.IsBot = false

	tok := jsonString(msg, "sessionToken")
	if tok != "" && rawID != "" {
		h.verifyWsSession(client, rawID, tok)
	}

	rejected := false
	h.storage.WithDBRead(func(db *DatabaseSchema) {
		prof := db.Profiles[rawID]
		if prof == nil {
			prof = db.Profiles[p.ID]
		}
		if prof == nil {
			return
		}
		if prof.IsBanned {
			reason := prof.BannedReason
			if reason == "" {
				reason = "违规操作"
			}
			_ = client.WriteJSON(map[string]interface{}{
				"type":   "scene_join_rejected",
				"reason": "banned",
				"text":   "该账号已被管理员封禁：" + reason,
			})
			rejected = true
			return
		}
		if prof.SessionToken != "" && tok != "" && client.authenticatedUserID != prof.ID {
			_ = client.WriteJSON(map[string]interface{}{
				"type":   "scene_join_rejected",
				"reason": "invalid_session",
				"text":   "登录会话已失效，请重新登录后再入座！",
			})
			rejected = true
			return
		}
		if prof.SessionToken == "" || client.authenticatedUserID == prof.ID {
			p.Coins = prof.Coins
			if prof.Rating > 0 {
				p.Rating = prof.Rating
			}
			if prof.Name != "" {
				p.Name = prof.Name
			}
			if prof.Avatar != "" {
				p.Avatar = prof.Avatar
			}
		}
	})
	if rejected {
		return
	}

	reqCoins := getSceneRequiredCoins(gType, sMode)
	if p.Coins < reqCoins || p.Coins <= 0 {
		h.clientsMu.Lock()
		if info := h.presence[client.id]; info != nil {
			info.GameType = "lobby"
			info.SceneMode = ""
			info.LastSeen = nowMs()
		}
		h.clientsMu.Unlock()
		_ = client.WriteJSON(map[string]interface{}{
			"type":          "scene_join_rejected",
			"reason":        "insufficient_coins",
			"requiredCoins": reqCoins,
			"currentCoins":  p.Coins,
			"text":          fmt.Sprintf("您的积分不足 (%d/%d 分)，无法入座此分场！", p.Coins, reqCoins),
		})
		h.broadcastPresenceAndPlayers()
		return
	}

	h.roomMu.Lock()
	// Remove or mark disconnected in other scenes/finished rooms
	for otherID, otherRoom := range h.sceneRooms {
		inOther := false
		for _, pl := range otherRoom.Players {
			if (pl.ID != "" && pl.ID == p.ID) || pl.SocketID == client.id {
				inOther = true
				break
			}
		}
		if !inOther {
			continue
		}
		isSameScene := otherRoom.GameType == gType && otherRoom.SceneMode == sMode
		authOther := h.rooms[otherID]
		isOtherFinished := otherRoom.Status == "finished" || (authOther != nil && authOther.BoardState != nil && authOther.BoardState.Phase == "ended")
		if !isSameScene || isOtherFinished {
			if !isOtherFinished && (otherRoom.Status == "playing" || otherRoom.Status == "starting") {
				for i := range otherRoom.Players {
					if (otherRoom.Players[i].ID != "" && otherRoom.Players[i].ID == p.ID) || otherRoom.Players[i].SocketID == client.id {
						otherRoom.Players[i].IsDisconnected = true
						otherRoom.Players[i].IsAutoManaged = true
					}
				}
			} else {
				kept := make([]ScenePlayer, 0, len(otherRoom.Players))
				for _, pl := range otherRoom.Players {
					if pl.ID != p.ID && pl.SocketID != client.id {
						kept = append(kept, pl)
					}
				}
				otherRoom.Players = filterHumanPlayers(kept)
				if len(otherRoom.Players) == 0 {
					delete(h.sceneRooms, otherID)
					if isOtherFinished {
						delete(h.rooms, otherID)
					}
				} else if !isOtherFinished {
					for idx := range otherRoom.Players {
						otherRoom.Players[idx].Seat = idx
					}
					otherRoom.Status = "waiting"
					h.sendSanitizedToRoomLocked(&RoomState{ID: otherRoom.ID, Players: otherRoom.Players}, func(_ int, _ ScenePlayer) interface{} {
						return map[string]interface{}{
							"type":       "scene_room_update",
							"roomId":     otherRoom.ID,
							"gameType":   otherRoom.GameType,
							"sceneMode":  otherRoom.SceneMode,
							"players":    otherRoom.Players,
							"maxPlayers": otherRoom.MaxPlayers,
							"status":     otherRoom.Status,
						}
					})
				}
			}
		}
	}

	var room *SceneWaitingRoom
	for _, r := range h.sceneRooms {
		if r.GameType == gType && r.SceneMode == sMode && (r.Status == "waiting" || r.Status == "starting" || r.Status == "playing") {
			for _, pl := range r.Players {
				if pl.SocketID == client.id {
					room = r
					break
				}
			}
			if room != nil {
				break
			}
		}
	}

	h.clientsMu.RLock()
	isSocketOpen := func(sid string) bool {
		c, ok := h.clients[sid]
		return ok && !c.closed.Load()
	}
	h.clientsMu.RUnlock()

	if room == nil && p.ID != "" {
		for _, r := range h.sceneRooms {
			if r.GameType == gType && r.SceneMode == sMode && r.Status == "waiting" && len(r.Players) < r.MaxPlayers {
				for _, pl := range r.Players {
					if pl.ID == p.ID && !isSocketOpen(pl.SocketID) {
						room = r
						break
					}
				}
				if room != nil {
					break
				}
			}
		}
	}

	if room == nil {
		room = pickBestWaitingRoomLocked(h.sceneRooms, gType, sMode)
	}
	if room == nil {
		id := fmt.Sprintf("SCENE-%s-%s-%s", gType, sMode, randomHex(4))
		room = &SceneWaitingRoom{
			ID:         id,
			GameType:   gType,
			SceneMode:  sMode,
			SceneName:  sName,
			MaxPlayers: maxP,
			Players:    []ScenePlayer{},
			Status:     "waiting",
			CreatedAt:  nowMs(),
		}
		h.sceneRooms[id] = room
	}

	room.Players = filterHumanPlayers(room.Players)

	existingIdx := -1
	for i, pl := range room.Players {
		if pl.SocketID == client.id {
			existingIdx = i
			break
		}
	}
	if existingIdx == -1 && p.ID != "" {
		for i, pl := range room.Players {
			if pl.ID == p.ID {
				if isSocketOpen(pl.SocketID) {
					if pl.OriginalID != "" {
						p.OriginalID = pl.OriginalID
					} else {
						p.OriginalID = pl.ID
					}
					if pl.OriginalName != "" {
						p.OriginalName = pl.OriginalName
					} else {
						p.OriginalName = pl.Name
					}
					p.ID = p.ID + "_tab_" + client.id
					p.Name = fmt.Sprintf("%s #%d", p.Name, len(room.Players)+1)
				} else {
					existingIdx = i
				}
				break
			}
		}
	}

	if existingIdx == -1 && (len(room.Players) >= room.MaxPlayers || room.Status == "playing" || room.Status == "starting" || room.Status == "finished") {
		newID := fmt.Sprintf("SCENE-%s-%s-%s", gType, sMode, randomHex(4))
		room = &SceneWaitingRoom{
			ID:         newID,
			GameType:   gType,
			SceneMode:  sMode,
			SceneName:  sName,
			MaxPlayers: maxP,
			Players:    []ScenePlayer{},
			Status:     "waiting",
			CreatedAt:  nowMs(),
		}
		h.sceneRooms[newID] = room
		existingIdx = -1
	}

	if existingIdx != -1 {
		p.Seat = room.Players[existingIdx].Seat
		p.SocketID = client.id
		p.IsReady = true
		room.Players[existingIdx] = p
	} else if len(room.Players) < room.MaxPlayers {
		usedSeats := map[int]bool{}
		for _, pl := range room.Players {
			usedSeats[pl.Seat] = true
		}
		freeSeat := 0
		for usedSeats[freeSeat] && freeSeat < room.MaxPlayers {
			freeSeat++
		}
		p.Seat = freeSeat
		p.SocketID = client.id
		p.IsReady = true
		room.Players = append(room.Players, p)
	}

	client.sceneRoomID = room.ID
	client.scenePlayerID = p.ID

	h.clientsMu.Lock()
	info, ok := h.presence[client.id]
	if !ok {
		info = &ClientPresenceInfo{SocketID: client.id}
		h.presence[client.id] = info
	}
	info.GameType = gType
	info.SceneMode = sMode
	if rawID != "" {
		info.PlayerID = rawID
	} else {
		info.PlayerID = p.ID
	}
	if p.OriginalName != "" {
		info.PlayerName = p.OriginalName
	} else {
		info.PlayerName = p.Name
	}
	info.LastSeen = nowMs()
	h.clientsMu.Unlock()

	isFullStarted := h.startRoomGameIfFullLocked(room)

	ack := map[string]interface{}{
		"type":             "scene_join_ack",
		"roomId":           room.ID,
		"gameType":         room.GameType,
		"sceneMode":        room.SceneMode,
		"player":           p,
		"assignedPlayerId": p.ID,
		"assignedSeat":     p.Seat,
		"players":          room.Players,
		"maxPlayers":       room.MaxPlayers,
		"status":           room.Status,
	}
	if room.Deal != nil {
		ack["deal"] = sanitizeDealForSeat(room.Deal, p.Seat)
	}
	_ = client.WriteJSON(ack)

	if !isFullStarted {
		room.Status = "waiting"
		room.Deal = nil
		h.sendSanitizedToRoomLocked(&RoomState{ID: room.ID, Players: room.Players}, func(_ int, _ ScenePlayer) interface{} {
			return map[string]interface{}{
				"type":       "scene_room_update",
				"roomId":     room.ID,
				"gameType":   room.GameType,
				"sceneMode":  room.SceneMode,
				"players":    room.Players,
				"maxPlayers": room.MaxPlayers,
				"status":     room.Status,
			}
		})
	}
	h.scheduleSnapshot()
	h.roomMu.Unlock()

	h.broadcastPresenceAndPlayers()
}

func (h *Hub) handleSceneLeave(client *WSConn, msg map[string]interface{}) {
	h.clientsMu.Lock()
	if info := h.presence[client.id]; info != nil {
		info.GameType = "lobby"
		info.LastSeen = nowMs()
	}
	h.clientsMu.Unlock()

	leavePID := jsonString(msg, "playerId")
	if leavePID == "" {
		leavePID = client.scenePlayerID
	}
	leavePName := jsonString(msg, "playerName")
	targetRoomID := jsonString(msg, "roomId")
	if targetRoomID == "" {
		targetRoomID = client.sceneRoomID
	}

	findLeaving := func(list []ScenePlayer) int {
		for i, pl := range list {
			if pl.SocketID != "" && pl.SocketID == client.id {
				return i
			}
		}
		if leavePID != "" {
			for i, pl := range list {
				if pl.ID == leavePID || pl.OriginalID == leavePID {
					return i
				}
			}
		}
		if leavePName != "" {
			for i, pl := range list {
				if pl.Name == leavePName {
					return i
				}
			}
		}
		return -1
	}

	h.roomMu.Lock()
	roomIDs := make([]string, 0, len(h.sceneRooms))
	for id := range h.sceneRooms {
		roomIDs = append(roomIDs, id)
	}
	for _, id := range roomIDs {
		room := h.sceneRooms[id]
		if room == nil {
			continue
		}
		lIdx := findLeaving(room.Players)
		isTarget := lIdx >= 0 && (targetRoomID == "" || id == targetRoomID)
		if !isTarget {
			continue
		}
		leavingPl := room.Players[lIdx]
		authRoom := h.rooms[id]
		isSSSRes := room.GameType == "shisanshui" && room.SceneMode == "reservation"
		isMatchActive := (room.Status == "starting" || room.Status == "playing") && !isSSSRes &&
			(authRoom == nil || authRoom.BoardState == nil || authRoom.BoardState.Phase != "ended")

		if isMatchActive {
			plSeat := leavingPl.Seat
			room.Players[lIdx].IsDisconnected = true
			room.Players[lIdx].IsAutoManaged = true
			room.Players[lIdx].SocketID = ""

			if authRoom != nil {
				for i := range authRoom.Players {
					if authRoom.Players[i].ID == leavingPl.ID || (authRoom.Players[i].SocketID != "" && authRoom.Players[i].SocketID == client.id) {
						authRoom.Players[i].IsDisconnected = true
						authRoom.Players[i].IsAutoManaged = true
						authRoom.Players[i].SocketID = ""
						break
					}
				}
				if authRoom.BoardState == nil {
					authRoom.BoardState = createDefaultBoardState(authRoom.ID, authRoom.GameType)
				}
				if authRoom.BoardState.AutoManagedSeats == nil {
					authRoom.BoardState.AutoManagedSeats = map[int]bool{}
				}
				authRoom.BoardState.AutoManagedSeats[plSeat] = true
				if authRoom.TurnSeat == plSeat {
					authRoom.LastTurnTs = nowMs() - 2000
				}
			}

			roomRef := authRoom
			if roomRef == nil {
				roomRef = &RoomState{ID: room.ID, Players: room.Players}
			}
			h.sendSanitizedToRoomLocked(roomRef, func(_ int, _ ScenePlayer) interface{} {
				return map[string]interface{}{
					"type":       "scene_room_update",
					"roomId":     room.ID,
					"gameType":   room.GameType,
					"sceneMode":  room.SceneMode,
					"players":    room.Players,
					"maxPlayers": room.MaxPlayers,
					"status":     room.Status,
				}
			})
			h.sendSanitizedToRoomLocked(roomRef, func(rSeat int, _ ScenePlayer) interface{} {
				evt := map[string]interface{}{
					"action":         "set_auto_host",
					"seat":           plSeat,
					"absSeat":        plSeat,
					"isAutoManaged":  true,
					"isAutoHost":     true,
					"isDisconnected": true,
					"playerName":     leavingPl.Name,
				}
				var bs *AuthoritativeBoardState
				if authRoom != nil {
					bs = authRoom.BoardState
				}
				return map[string]interface{}{
					"type":       "game_event",
					"roomId":     room.ID,
					"seat":       plSeat,
					"event":      evt,
					"payload":    evt,
					"boardState": sanitizeBoardStateForSeat(bs, room.GameType, rSeat),
				}
			})
		} else {
			wasFinished := room.Status == "finished" || (authRoom != nil && authRoom.BoardState != nil && authRoom.BoardState.Phase == "ended")
			room.Players = append(room.Players[:lIdx], room.Players[lIdx+1:]...)
			if client.sceneRoomID == id {
				client.sceneRoomID = ""
				client.scenePlayerID = ""
			}
			if len(room.Players) == 0 {
				delete(h.sceneRooms, id)
				delete(h.rooms, id)
			} else if wasFinished {
				if authRoom != nil {
					kept := make([]ScenePlayer, 0, len(authRoom.Players))
					for _, ap := range authRoom.Players {
						if ap.ID != leavingPl.ID && (ap.SocketID == "" || ap.SocketID != client.id) {
							kept = append(kept, ap)
						}
					}
					authRoom.Players = kept
				}
				settleExp := room.SettlementExpiresAt
				if settleExp == 0 && authRoom != nil {
					settleExp = authRoom.SettlementExpiresAt
				}
				h.sendSanitizedToRoomLocked(&RoomState{ID: room.ID, Players: room.Players}, func(_ int, _ ScenePlayer) interface{} {
					return map[string]interface{}{
						"type":                "scene_room_update",
						"roomId":              room.ID,
						"gameType":            room.GameType,
						"sceneMode":           room.SceneMode,
						"players":             room.Players,
						"maxPlayers":          room.MaxPlayers,
						"status":              room.Status,
						"settlementExpiresAt": settleExp,
						"serverTs":            nowMs(),
					}
				})
			} else {
				for idx := range room.Players {
					room.Players[idx].Seat = idx
				}
				if len(room.Players) < room.MaxPlayers {
					room.Status = "waiting"
					room.Deal = nil
				}
				if authRoom != nil {
					cp := make([]ScenePlayer, len(room.Players))
					copy(cp, room.Players)
					authRoom.Players = cp
					authRoom.Status = room.Status
					if room.Status == "waiting" {
						authRoom.Deal = nil
					}
				}
				h.sendSanitizedToRoomLocked(&RoomState{ID: room.ID, Players: room.Players}, func(_ int, _ ScenePlayer) interface{} {
					return map[string]interface{}{
						"type":       "scene_room_update",
						"roomId":     room.ID,
						"gameType":   room.GameType,
						"sceneMode":  room.SceneMode,
						"players":    room.Players,
						"maxPlayers": room.MaxPlayers,
						"status":     room.Status,
					}
				})
			}
		}
		h.scheduleSnapshot()
	}

	reqRoomID := jsonString(msg, "roomId")
	if reqRoomID != "" && h.sceneRooms[reqRoomID] == nil {
		if r := h.rooms[reqRoomID]; r != nil && r.Status != "playing" && r.Status != "starting" {
			if idx := findLeaving(r.Players); idx >= 0 {
				r.Players = append(r.Players[:idx], r.Players[idx+1:]...)
				if len(r.Players) == 0 {
					delete(h.rooms, reqRoomID)
				} else {
					r.Status = "waiting"
					r.Deal = nil
				}
			}
		}
	}

	msgScene := jsonString(msg, "sceneMode")
	keptCh := h.challenges[:0]
	for _, ch := range h.challenges {
		if (ch.SenderID == leavePID || ch.SenderName == leavePName) &&
			(msgScene == "" || ch.SceneMode == msgScene) && len(ch.AcceptedBy) == 0 {
			h.BroadcastJSON(map[string]interface{}{
				"type":        "challenge_status_update",
				"challengeId": ch.ID,
				"status":      "removed",
				"senderId":    leavePID,
				"senderName":  leavePName,
				"gameType":    ch.GameType,
				"sceneMode":   ch.SceneMode,
			})
			continue
		}
		keptCh = append(keptCh, ch)
	}
	h.challenges = keptCh
	h.roomMu.Unlock()

	h.broadcastPresenceAndPlayers()
}

func (h *Hub) handleSceneChat(client *WSConn, msg map[string]interface{}) {
	if !client.allowChat() {
		return
	}
	var safeAudio interface{}
	if a, ok := msg["audio"].(string); ok && len(a) > 0 && len(a) <= 450000 {
		safeAudio = a
	}
	safeText := sanitizeWsText(msg["text"], 300)
	if safeText == "" && safeAudio == nil {
		return
	}

	action := jsonString(msg, "action")
	isChallenge := action == "challenge"
	targetChannel := jsonString(msg, "channel")
	if targetChannel == "" || isChallenge {
		targetChannel = "hall"
	}

	gType := jsonString(msg, "gameType")
	sMode := jsonString(msg, "sceneMode")
	sName := sanitizeWsText(msg["sceneName"], 32)
	sender := sanitizeWsText(msg["sender"], 24)
	if sender == "" {
		sender = "牌友"
	}
	senderID := jsonString(msg, "senderId")
	avatar := sanitizeWsText(msg["avatar"], 12)
	if avatar == "" {
		avatar = "👑"
	}
	challengeID := jsonString(msg, "challengeId")
	ts := jsonString(msg, "timestamp")
	if ts == "" {
		ts = time.Now().Format("15:04")
	}

	h.roomMu.Lock()
	if isChallenge && gType != "" && sMode != "" {
		if senderID == "" {
			senderID = "P-guest"
		}
		kept := make([]ActiveChallengeRecord, 0, len(h.challenges))
		for i := range h.challenges {
			oldCh := h.challenges[i]
			if (oldCh.SenderID == senderID || oldCh.SenderName == sender) &&
				oldCh.Status != "cancelled" && oldCh.Status != "removed" {
				if len(oldCh.AcceptedBy) > 0 {
					newHostID := oldCh.AcceptedBy["id"]
					newHostName := oldCh.AcceptedBy["name"]
					oldCh.SenderID = newHostID
					oldCh.SenderName = newHostName
					oldCh.Status = "transferred"
					oldCh.Text = fmt.Sprintf("⚔️ 在【%s】由 [%s] 继承守擂：“谁来与我一战到底！”", oldCh.SceneName, newHostName)
					oldCh.AcceptedBy = nil
					kept = append(kept, oldCh)

					h.BroadcastJSON(map[string]interface{}{
						"type":          "challenge_status_update",
						"challengeId":   oldCh.ID,
						"status":        "transferred",
						"newSenderId":   newHostID,
						"newSenderName": newHostName,
						"gameType":      oldCh.GameType,
						"sceneMode":     oldCh.SceneMode,
						"sceneName":     oldCh.SceneName,
						"text":          oldCh.Text,
					})
					h.BroadcastJSON(map[string]interface{}{
						"type":      "scene_chat_broadcast",
						"gameType":  oldCh.GameType,
						"sceneMode": oldCh.SceneMode,
						"sceneName": oldCh.SceneName,
						"channel":   "hall",
						"sender":    "🤖 牌楼官播 BOT",
						"senderId":  "BOT-SYSTEM",
						"avatar":    "🤖",
						"text":      fmt.Sprintf("⚔️【擂台过继】牌友 [%s] 已移步至新场景，原【%s】战帖已顺位过继由 [%s] 继承守擂！", sender, oldCh.SceneName, newHostName),
						"action":    "notice",
						"timestamp": time.Now().Format("15:04"),
					})
				} else {
					h.BroadcastJSON(map[string]interface{}{
						"type":        "challenge_status_update",
						"challengeId": oldCh.ID,
						"status":      "removed",
						"senderId":    senderID,
						"senderName":  sender,
						"gameType":    oldCh.GameType,
						"sceneMode":   oldCh.SceneMode,
					})
				}
				continue
			}
			kept = append(kept, oldCh)
		}

		if challengeID == "" {
			challengeID = fmt.Sprintf("ch-%d-%s", nowMs(), randomHex(2))
		}
		chText := safeText
		if chText == "" {
			chText = "摆下战帖，谁敢来与我一战到底！"
		}
		if len([]rune(chText)) > 200 {
			chText = string([]rune(chText)[:200])
		}
		scName := sName
		if scName == "" {
			scName = "经典分场"
		}
		newCh := ActiveChallengeRecord{
			ID:         challengeID,
			SenderID:   senderID,
			SenderName: sender,
			Avatar:     avatar,
			GameType:   gType,
			SceneMode:  sMode,
			SceneName:  scName,
			Text:       chText,
			Status:     "open",
			Timestamp:  ts,
			CreatedAt:  nowMs(),
		}
		h.challenges = append([]ActiveChallengeRecord{newCh}, kept...)
		if len(h.challenges) > 30 {
			h.challenges = h.challenges[:30]
		}
	}

	topCount := len(h.challenges)
	if topCount > 10 {
		topCount = 10
	}
	topCh := make([]ActiveChallengeRecord, topCount)
	copy(topCh, h.challenges[:topCount])
	h.roomMu.Unlock()

	displayText := safeText
	if displayText == "" {
		displayText = "🎤 [等候室实时语音]"
	}
	phraseText := sanitizeWsText(msg["phraseText"], 200)
	isVoice := jsonBool(msg, "voice", false) && (safeAudio != nil || phraseText != "")
	pitch := 1.0
	if p, ok := msg["pitch"].(float64); ok {
		pitch = p
	}
	rate := 1.0
	if r, ok := msg["rate"].(float64); ok {
		rate = r
	}
	dur := jsonInt(msg, "duration", 0)
	if dur < 0 {
		dur = 0
	} else if dur > 30 {
		dur = 30
	}

	h.BroadcastJSON(map[string]interface{}{
		"type":             "scene_chat_broadcast",
		"challengeId":      nullableStr(challengeID),
		"gameType":         gType,
		"sceneMode":        sMode,
		"sceneName":        nullableStr(sName),
		"channel":          targetChannel,
		"sender":           sender,
		"senderId":         nullableStr(senderID),
		"avatar":           avatar,
		"text":             displayText,
		"voice":            isVoice,
		"phraseText":       nullableStr(phraseText),
		"roleId":           msg["roleId"],
		"roleName":         nullableStr(sanitizeWsText(msg["roleName"], 24)),
		"pitch":            pitch,
		"rate":             rate,
		"isFemale":         jsonBool(msg, "isFemale", false),
		"audio":            safeAudio,
		"duration":         dur,
		"action":           nullableStr(action),
		"targetPlayerId":   msg["targetPlayerId"],
		"targetPlayerName": nullableStr(sanitizeWsText(msg["targetPlayerName"], 24)),
		"giftAmount":       jsonInt(msg, "giftAmount", 0),
		"timestamp":        ts,
		"activeChallenges": topCh,
	})
}

func nullableStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func (h *Hub) handleSceneAcceptChallenge(client *WSConn, msg map[string]interface{}) {
	chID := jsonString(msg, "challengeId")
	challengerName := jsonString(msg, "challengerName")
	sMode := jsonString(msg, "sceneMode")
	gType := jsonString(msg, "gameType")
	acceptorID := jsonString(msg, "acceptorId")
	if acceptorID == "" {
		acceptorID = "guest"
	}
	acceptorName := sanitizeWsText(msg["acceptorName"], 24)
	if acceptorName == "" {
		acceptorName = "热血牌友"
	}

	h.roomMu.Lock()
	for i := range h.challenges {
		c := &h.challenges[i]
		if c.ID == chID || (c.SenderName == challengerName && c.SceneMode == sMode && c.Status == "open") {
			c.Status = "accepted"
			c.AcceptedBy = map[string]string{"id": acceptorID, "name": acceptorName}
			h.BroadcastJSON(map[string]interface{}{
				"type":        "challenge_status_update",
				"challengeId": c.ID,
				"status":      "accepted",
				"acceptedBy":  c.AcceptedBy,
			})
			break
		}
	}
	topCount := len(h.challenges)
	if topCount > 10 {
		topCount = 10
	}
	topCh := make([]ActiveChallengeRecord, topCount)
	copy(topCh, h.challenges[:topCount])
	h.roomMu.Unlock()

	if gType != "" && sMode != "" {
		h.clientsMu.Lock()
		info, ok := h.presence[client.id]
		if !ok {
			info = &ClientPresenceInfo{SocketID: client.id}
			h.presence[client.id] = info
		}
		info.GameType = gType
		info.SceneMode = sMode
		if rawAccID := jsonString(msg, "acceptorId"); rawAccID != "" {
			info.PlayerID = rawAccID
			info.PlayerName = acceptorName
		}
		info.LastSeen = nowMs()
		h.clientsMu.Unlock()
		h.broadcastPresenceAndPlayers()
	}

	sName := sanitizeWsText(msg["sceneName"], 32)
	if sName == "" {
		sName = "经典分场"
	}
	cName := sanitizeWsText(challengerName, 24)
	if cName == "" {
		cName = "牌友"
	}
	h.BroadcastJSON(map[string]interface{}{
		"type":             "scene_chat_broadcast",
		"gameType":         gType,
		"sceneMode":        sMode,
		"sceneName":        sName,
		"channel":          "hall",
		"sender":           "🤖 牌楼官播 BOT",
		"senderId":         "BOT-SYSTEM",
		"avatar":           "🤖",
		"text":             fmt.Sprintf("⚔️【应战捷报】牌友 [%s] 已成功响应 [%s] 在【%s】的擂台战帖，已切入相同桌台！", acceptorName, cName, sName),
		"action":           "notice",
		"timestamp":        time.Now().Format("15:04"),
		"activeChallenges": topCh,
	})
}

func (h *Hub) handleRoomCreate(client *WSConn, msg map[string]interface{}) {
	pID := jsonString(msg, "playerId")
	tok := jsonString(msg, "sessionToken")
	if tok != "" && pID != "" {
		h.verifyWsSession(client, pID, tok)
	}
	if pID == "" {
		pID = client.authenticatedUserID
	}
	if pID == "" {
		pID = fmt.Sprintf("P-%d", nowMs())
	}

	banned := false
	h.storage.WithDBRead(func(db *DatabaseSchema) {
		if prof := db.Profiles[pID]; prof != nil && prof.IsBanned {
			banned = true
		}
	})
	if banned {
		return
	}

	id := fmt.Sprintf("RM-%d", 207+rand.Intn(800))
	gType := jsonString(msg, "gameType")
	if gType == "" {
		gType = "yansan"
	}
	cleanPass := strings.TrimSpace(jsonString(msg, "passcode"))
	if len([]rune(cleanPass)) > 6 {
		cleanPass = string([]rune(cleanPass)[:6])
	}
	pName := sanitizeWsText(msg["playerName"], 24)
	if pName == "" {
		pName = "茗局雅客"
	}
	title := sanitizeWsText(msg["title"], 40)
	if title == "" {
		title = pName + "的牌局"
	}
	modeLabel := "实时 WebSocket 牌桌"
	passNote := ""
	if cleanPass != "" {
		modeLabel = "私密口令牌桌"
		passNote = "（已设私密口令）"
	}

	newRoom := &RoomState{
		ID:          id,
		Title:       title,
		GameType:    gType,
		Status:      "playing",
		HasPasscode: cleanPass != "",
		Passcode:    cleanPass,
		Players:     []ScenePlayer{{ID: pID, Name: pName, Rating: 1900, Seat: 0, SocketID: client.id}},
		Spectators:  1,
		TurnSeat:    0,
		MoveCount:   0,
		ModeLabel:   modeLabel,
		Chat: []RoomChatMessage{{
			ID:        fmt.Sprintf("c-%d", nowMs()),
			Sender:    "系统司仪",
			Text:      fmt.Sprintf("房间 %s 已建立%s，欢迎各路牌友入席！", id, passNote),
			Timestamp: time.Now().Format("15:04"),
		}},
		BoardState: createDefaultBoardState(id, gType),
		CreatedAt:  nowMs(),
	}

	h.roomMu.Lock()
	h.rooms[id] = newRoom
	safeList := h.buildSanitizedRoomsListLocked()
	h.roomMu.Unlock()

	h.BroadcastJSON(map[string]interface{}{
		"type":  "room_created",
		"room":  newRoom,
		"rooms": safeList,
	})
}

func (h *Hub) handleRoomJoin(client *WSConn, msg map[string]interface{}) {
	roomID := jsonString(msg, "roomId")
	h.roomMu.Lock()
	room := h.rooms[roomID]
	if room == nil {
		h.roomMu.Unlock()
		return
	}
	if room.Passcode != "" && strings.TrimSpace(jsonString(msg, "passcode")) != room.Passcode {
		h.roomMu.Unlock()
		_ = client.WriteJSON(map[string]interface{}{
			"type":   "room_join_rejected",
			"roomId": roomID,
			"reason": "invalid_passcode",
			"error":  "私密房间口令不正确，请核对后再试",
		})
		return
	}
	pName := sanitizeWsText(msg["playerName"], 24)
	if pName == "" {
		pName = "牌友"
	}
	pID := jsonString(msg, "playerId")
	if pID == "" {
		pID = client.authenticatedUserID
	}
	if pID == "" {
		pID = fmt.Sprintf("P-%d", nowMs())
	}
	rating := jsonInt(msg, "rating", 1900)

	existingIdx := -1
	for i, p := range room.Players {
		if p.ID == pID || p.Name == pName {
			existingIdx = i
			break
		}
	}
	if existingIdx == -1 && len(room.Players) < 4 {
		room.Players = append(room.Players, ScenePlayer{
			ID:       pID,
			Name:     pName,
			Rating:   rating,
			Seat:     len(room.Players),
			SocketID: client.id,
		})
	} else {
		if existingIdx >= 0 {
			room.Players[existingIdx].SocketID = client.id
		}
		room.Spectators++
	}
	safeList := h.buildSanitizedRoomsListLocked()
	h.roomMu.Unlock()

	h.BroadcastJSON(map[string]interface{}{
		"type":  "room_updated",
		"room":  room,
		"rooms": safeList,
	})
}

func (h *Hub) handleRoomLeave(_ *WSConn, msg map[string]interface{}) {
	roomID := jsonString(msg, "roomId")
	h.roomMu.Lock()
	room := h.rooms[roomID]
	if room == nil {
		h.roomMu.Unlock()
		return
	}
	if room.Spectators > 0 {
		room.Spectators--
	}
	safeList := h.buildSanitizedRoomsListLocked()
	h.roomMu.Unlock()

	h.BroadcastJSON(map[string]interface{}{
		"type":  "room_updated",
		"room":  room,
		"rooms": safeList,
	})
}

func (h *Hub) handleRoomReconnect(client *WSConn, msg map[string]interface{}) {
	tok := jsonString(msg, "sessionToken")
	rawPID := jsonString(msg, "playerId")
	if tok != "" && rawPID != "" {
		h.verifyWsSession(client, rawPID, tok)
	}
	roomID := jsonString(msg, "roomId")
	h.roomMu.Lock()
	defer h.roomMu.Unlock()
	room := h.rooms[roomID]
	if room == nil {
		return
	}
	pID := rawPID
	if pID == "" {
		pID = client.authenticatedUserID
	}
	if pID == "" {
		pID = client.scenePlayerID
	}
	pName := jsonString(msg, "playerName")

	matchedIdx := -1
	for i, p := range room.Players {
		if p.SocketID == client.id || (pID != "" && (p.ID == pID || p.OriginalID == pID || p.UserID == pID)) {
			matchedIdx = i
			break
		}
	}
	if matchedIdx == -1 && pName != "" {
		for i, p := range room.Players {
			if p.Name == pName {
				matchedIdx = i
				break
			}
		}
	}

	seat := -1
	if matchedIdx >= 0 {
		matched := &room.Players[matchedIdx]
		if tok != "" {
			invalid := false
			h.storage.WithDBRead(func(db *DatabaseSchema) {
				if prof := db.Profiles[matched.ID]; prof != nil && prof.SessionToken != "" && !safeTokenEqual(prof.SessionToken, tok) {
					invalid = true
				}
			})
			if invalid {
				_ = client.WriteJSON(map[string]interface{}{"type": "reconnect_ack", "failed": true, "reason": "invalid_session"})
				return
			}
		}
		matched.SocketID = client.id
		matched.IsDisconnected = false
		client.sceneRoomID = room.ID
		client.scenePlayerID = matched.ID
		seat = matched.Seat
	}

	_ = client.WriteJSON(map[string]interface{}{
		"type": "reconnect_ack",
		"room": map[string]interface{}{
			"id":          room.ID,
			"title":       room.Title,
			"gameType":    room.GameType,
			"sceneMode":   room.SceneMode,
			"maxPlayers":  room.MaxPlayers,
			"status":      room.Status,
			"hasPasscode": room.HasPasscode,
			"players":     room.Players,
			"spectators":  room.Spectators,
			"turnSeat":    room.TurnSeat,
			"moveCount":   room.MoveCount,
			"roundIndex":  room.RoundIndex,
			"modeLabel":   room.ModeLabel,
			"chat":        room.Chat,
			"boardState":  room.BoardState,
			"deal":        sanitizeDealForSeat(room.Deal, seat),
		},
		"boardState": sanitizeBoardStateForSeat(room.BoardState, room.GameType, seat),
		"serverTs":   nowMs(),
	})
}

func (h *Hub) handleRequestGameDeal(client *WSConn, msg map[string]interface{}) {
	roomID := jsonString(msg, "roomId")
	h.roomMu.Lock()
	defer h.roomMu.Unlock()

	r := h.rooms[roomID]
	sr := h.sceneRooms[roomID]
	var deal *RoomDeal
	gType, sMode := "", ""
	var players []ScenePlayer
	if r != nil {
		if r.Deal == nil {
			switch r.GameType {
			case "shisanshui":
				r.Deal = createShisanshuiDeal(r.SceneMode, r.MaxPlayers)
			case "mahjong":
				r.Deal = createMahjongDeal(r.SceneMode)
			default:
				r.Deal = createYansanDeal(r.SceneMode)
			}
		}
		deal = r.Deal
		gType, sMode = r.GameType, r.SceneMode
		players = r.Players
	} else if sr != nil {
		if sr.Deal == nil {
			switch sr.GameType {
			case "shisanshui":
				sr.Deal = createShisanshuiDeal(sr.SceneMode, sr.MaxPlayers)
			case "mahjong":
				sr.Deal = createMahjongDeal(sr.SceneMode)
			default:
				sr.Deal = createYansanDeal(sr.SceneMode)
			}
		}
		deal = sr.Deal
		gType, sMode = sr.GameType, sr.SceneMode
		players = sr.Players
	} else {
		return
	}

	pID := jsonString(msg, "playerId")
	if pID == "" {
		pID = client.scenePlayerID
	}
	seat := jsonInt(msg, "seat", -1)
	for _, pl := range players {
		if pl.SocketID == client.id || (pID != "" && pl.ID == pID) {
			seat = pl.Seat
			break
		}
	}
	_ = client.WriteJSON(map[string]interface{}{
		"type":      "game_deal",
		"roomId":    roomID,
		"gameType":  gType,
		"sceneMode": sMode,
		"deal":      sanitizeDealForSeat(deal, seat),
	})
}

func (h *Hub) handleGameMove(client *WSConn, msg map[string]interface{}) {
	targetRoomID := jsonString(msg, "roomId")
	h.roomMu.Lock()
	defer h.roomMu.Unlock()

	room := h.rooms[targetRoomID]
	if room == nil {
		if sr := h.sceneRooms[targetRoomID]; sr != nil {
			room = &RoomState{
				ID:         sr.ID,
				GameType:   sr.GameType,
				SceneMode:  sr.SceneMode,
				MaxPlayers: sr.MaxPlayers,
				Status:     sr.Status,
				Players:    sr.Players,
				RoundIndex: sr.RoundIndex,
				Deal:       sr.Deal,
				BoardState: createDefaultBoardState(sr.ID, sr.GameType),
			}
			h.rooms[targetRoomID] = room
		}
	}
	if room == nil {
		return
	}

	evt, _ := msg["event"].(map[string]interface{})
	payloadMap, _ := msg["payload"].(map[string]interface{})
	merged := map[string]interface{}{}
	for k, v := range evt {
		merged[k] = v
	}
	for k, v := range payloadMap {
		merged[k] = v
	}
	action := jsonString(merged, "action")
	if action == "" {
		action = "play"
	}

	requestedSeat := jsonInt(msg, "seat", -1)
	if requestedSeat < 0 {
		requestedSeat = jsonInt(evt, "seat", -1)
	}
	if requestedSeat < 0 {
		requestedSeat = jsonInt(merged, "seat", 0)
	}

	wsPlayerID := client.authenticatedUserID
	if wsPlayerID == "" {
		wsPlayerID = client.scenePlayerID
	}
	var senderPlayer *ScenePlayer
	for i := range room.Players {
		pl := &room.Players[i]
		if (pl.SocketID != "" && pl.SocketID == client.id) || (pl.ID != "" && wsPlayerID != "" && pl.ID == wsPlayerID) {
			senderPlayer = pl
			break
		}
	}

	mutating := map[string]bool{
		"play": true, "pass": true, "draw": true, "discard": true,
		"peng": true, "gang": true, "hu": true, "draw_stalemate": true,
		"finish": true, "qianggong": true, "surrender": true, "shisanshui_submit_hand": true,
	}
	if senderPlayer == nil && mutating[action] {
		return
	}

	seat := requestedSeat
	if senderPlayer != nil {
		seat = senderPlayer.Seat
	}

	if room.BoardState == nil {
		room.BoardState = createDefaultBoardState(room.ID, room.GameType)
	}
	bs := room.BoardState
	if bs.Phase == "ended" && mutating[action] {
		return
	}

	if action == "set_auto_host" {
		isAuto := false
		if b, ok := merged["isAutoManaged"].(bool); ok {
			isAuto = b
		} else if b, ok := merged["isAutoHost"].(bool); ok {
			isAuto = b
		}
		for i := range room.Players {
			if room.Players[i].Seat == seat {
				room.Players[i].IsAutoManaged = isAuto
				break
			}
		}
		if sr := h.sceneRooms[room.ID]; sr != nil {
			for i := range sr.Players {
				if sr.Players[i].Seat == seat {
					sr.Players[i].IsAutoManaged = isAuto
					break
				}
			}
		}
		if bs.AutoManagedSeats == nil {
			bs.AutoManagedSeats = map[int]bool{}
		}
		bs.AutoManagedSeats[seat] = isAuto
		if isAuto && room.TurnSeat == seat {
			room.LastTurnTs = nowMs() - 2000
		}
		h.sendSanitizedToRoomLocked(room, func(rSeat int, _ ScenePlayer) interface{} {
			ev := map[string]interface{}{
				"action":        "set_auto_host",
				"seat":          seat,
				"absSeat":       seat,
				"isAutoManaged": isAuto,
				"isAutoHost":    isAuto,
			}
			return map[string]interface{}{
				"type":       "game_event",
				"roomId":     room.ID,
				"seat":       seat,
				"event":      ev,
				"payload":    ev,
				"boardState": sanitizeBoardStateForSeat(bs, room.GameType, rSeat),
			}
		})
		return
	}

	if room.GameType == "mahjong" {
		handleMahjongGameMove(h.ctx, client, room, seat, action, merged)
		return
	}
	if room.GameType == "yansan" {
		var rawCombo json.RawMessage
		if cObj := merged["combo"]; cObj != nil {
			rawCombo, _ = json.Marshal(cObj)
		} else if cObj := evt["combo"]; cObj != nil {
			rawCombo, _ = json.Marshal(cObj)
		}
		handleYansanGameMove(h.ctx, client, room, seat, action, rawCombo, merged)
		return
	}
	if action == "shisanshui_submit_hand" {
		handleShisanshuiGameMove(h.ctx, client, room, seat, merged)
		return
	}

	// Generic fallback move
	room.MoveCount++
	bs.ActionSeq++
	bs.MoveCount = room.MoveCount
	bs.UpdatedAt = nowMs()
	room.LastTurnTs = bs.UpdatedAt
	if m := jsonInt(merged, "multiplier", 0); m > 0 {
		bs.Multiplier = m
	}
	if _, hasNext := merged["nextTurn"]; hasNext {
		bs.TurnSeat = jsonInt(merged, "nextTurn", bs.TurnSeat)
		room.TurnSeat = bs.TurnSeat
	}
	if cObj := merged["combo"]; cObj != nil {
		if rawC, err := json.Marshal(cObj); err == nil {
			bs.LastCombo = rawC
		}
		bs.DiscardsHistory = append(bs.DiscardsHistory, cObj)
		if len(bs.DiscardsHistory) > 100 {
			bs.DiscardsHistory = bs.DiscardsHistory[len(bs.DiscardsHistory)-100:]
		}
	}
	if action == "finish" || (merged["winner"] != nil) {
		wSeat := jsonInt(merged, "winner", seat)
		bs.WinnerSeat = &wSeat
		bs.Phase = "ended"
		room.Status = "finished"
		if merged["allianceScoreResult"] != nil {
			bs.AllianceScoreResult = merged["allianceScoreResult"]
			bs.Settlement = merged["allianceScoreResult"]
		}
		h.resetSceneRoomReadyLocked(room.ID)
	}
	outEvt := map[string]interface{}{}
	for k, v := range merged {
		outEvt[k] = v
	}
	outEvt["action"] = action
	outEvt["seat"] = seat
	outEvt["nextTurn"] = bs.TurnSeat

	h.sendSanitizedToRoomLocked(room, func(rSeat int, _ ScenePlayer) interface{} {
		return map[string]interface{}{
			"type":       "game_event",
			"roomId":     room.ID,
			"seat":       seat,
			"event":      outEvt,
			"payload":    outEvt,
			"moveCount":  room.MoveCount,
			"boardState": sanitizeBoardStateForSeat(bs, room.GameType, rSeat),
		}
	})
	h.scheduleSnapshot()
}

func (h *Hub) handleRoomChat(client *WSConn, msg map[string]interface{}) {
	if !client.allowChat() {
		return
	}
	targetRoomID := jsonString(msg, "roomId")
	if targetRoomID == "" {
		targetRoomID = "RM-202"
	}
	var audioPtr *string
	if a, ok := msg["audio"].(string); ok && len(a) > 0 && len(a) <= 450000 {
		audioPtr = &a
	}
	safeText := sanitizeWsText(msg["text"], 120)
	if safeText == "" && audioPtr == nil {
		return
	}
	if safeText == "" && audioPtr != nil {
		safeText = "🎤 [牌桌实时语音]"
	}
	sender := sanitizeWsText(msg["sender"], 24)
	if sender == "" {
		sender = "牌友"
	}
	avatar := sanitizeWsText(msg["avatar"], 12)
	if avatar == "" {
		avatar = "👤"
	}
	var senderIDPtr *string
	if sid := jsonString(msg, "senderId"); sid != "" {
		senderIDPtr = &sid
	}
	var propPtr *string
	if p := jsonString(msg, "prop"); p != "" {
		propPtr = &p
	}
	var targetSeatPtr *int
	if _, ok := msg["targetSeat"].(float64); ok {
		tsVal := jsonInt(msg, "targetSeat", 0)
		targetSeatPtr = &tsVal
	}
	dur := jsonInt(msg, "duration", 0)
	if dur < 0 {
		dur = 0
	} else if dur > 30 {
		dur = 30
	}

	chatMsg := RoomChatMessage{
		ID:         fmt.Sprintf("chat-%d-%s", nowMs(), randomHex(2)),
		Sender:     sender,
		SenderID:   senderIDPtr,
		Avatar:     avatar,
		Text:       safeText,
		Prop:       propPtr,
		TargetSeat: targetSeatPtr,
		Voice:      jsonBool(msg, "voice", false) && audioPtr != nil,
		Audio:      audioPtr,
		Duration:   dur,
		Timestamp:  time.Now().Format("15:04"),
	}

	h.roomMu.Lock()
	room := h.rooms[targetRoomID]
	if room != nil {
		if len(room.Chat) >= 50 {
			room.Chat = append(room.Chat[len(room.Chat)-49:], chatMsg)
		} else {
			room.Chat = append(room.Chat, chatMsg)
		}
		h.sendSanitizedToRoomLocked(room, func(_ int, _ ScenePlayer) interface{} {
			return map[string]interface{}{
				"type":    "chat_broadcast",
				"roomId":  targetRoomID,
				"message": chatMsg,
			}
		})
		h.roomMu.Unlock()
		return
	}
	h.roomMu.Unlock()

	h.BroadcastJSON(map[string]interface{}{
		"type":    "chat_broadcast",
		"roomId":  targetRoomID,
		"message": chatMsg,
	})
}
