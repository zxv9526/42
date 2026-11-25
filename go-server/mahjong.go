package main

import (
	"encoding/json"
	"math/rand"
	"sort"
	"strings"
)

// ==============================================================================
// Mahjong (经典国标麻将 / 红中赖子) Rules, Settlement & Authoritative Engine
// ==============================================================================

func isHongzhong(t MahjongTile) bool {
	return t.Type == "zi" && (t.Value == 5 || t.Text == "红中" || t.IsRedDragon)
}

func isMahjongTileMatch(a, b MahjongTile) bool {
	return a.Type == b.Type && a.Value == b.Value
}

func isYiWuJiuMa(t MahjongTile) bool {
	if isHongzhong(t) {
		return true
	}
	if t.Type == "zi" {
		return t.Value == 1 || t.Value == 5 || t.Text == "东风" || t.Text == "红中"
	}
	return t.Value == 1 || t.Value == 5 || t.Value == 9
}

func sortMahjongTiles(tiles []MahjongTile) []MahjongTile {
	out := make([]MahjongTile, len(tiles))
	copy(out, tiles)
	order := map[string]int{"wan": 0, "tiao": 1, "tong": 2, "zi": 3}
	sort.SliceStable(out, func(i, j int) bool {
		oa, okA := order[out[i].Type]
		if !okA {
			oa = 9
		}
		ob, okB := order[out[j].Type]
		if !okB {
			ob = 9
		}
		if oa != ob {
			return oa < ob
		}
		return out[i].Value < out[j].Value
	})
	return out
}

func createMahjongDeal(sceneMode string) *RoomDeal {
	numNames := []string{"", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
	typeNames := map[string]string{"wan": "万", "tiao": "条", "tong": "筒"}
	ziNames := []string{"", "东风", "南风", "西风", "北风", "红中", "发财", "白板"}
	colors := map[string]string{"wan": "#DC2626", "tiao": "#16A34A", "tong": "#2563EB", "zi": "#78350F"}

	pool := make([]MahjongTile, 0, 136)
	for _, t := range []string{"wan", "tiao", "tong"} {
		for v := 1; v <= 9; v++ {
			for k := 0; k < 4; k++ {
				pool = append(pool, MahjongTile{
					ID:    t + "-" + itoa(v) + "-" + itoa(k),
					Type:  t,
					Value: v,
					Text:  numNames[v] + typeNames[t],
					Color: colors[t],
				})
			}
		}
	}
	for v := 1; v <= 7; v++ {
		for k := 0; k < 4; k++ {
			c := colors["zi"]
			if v == 5 {
				c = "#DC2626"
			}
			pool = append(pool, MahjongTile{
				ID:          "zi-" + itoa(v) + "-" + itoa(k),
				Type:        "zi",
				Value:       v,
				Text:        ziNames[v],
				Color:       c,
				IsRedDragon: v == 5,
			})
		}
	}

	for i := len(pool) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		pool[i], pool[j] = pool[j], pool[i]
	}

	d1 := 1 + rand.Intn(6)
	d2 := 1 + rand.Intn(6)
	dealerSeat := 0

	seatCards := map[int][]MahjongTile{
		0: append([]MahjongTile{}, pool[0:14]...),
		1: append([]MahjongTile{}, pool[14:27]...),
		2: append([]MahjongTile{}, pool[27:40]...),
		3: append([]MahjongTile{}, pool[40:53]...),
	}
	for s := 0; s < 4; s++ {
		seatCards[s] = sortMahjongTiles(seatCards[s])
	}
	wall := append([]MahjongTile{}, pool[53:]...)

	return &RoomDeal{
		GameType:         "mahjong",
		SceneMode:        sceneMode,
		DealID:           "mj-deal-" + itoa(int(nowMs()&0x7fffffff)) + "-" + randomHex(3),
		DealerSeat:       dealerSeat,
		StarterSeat:      dealerSeat,
		Dice:             map[string]int{"d1": d1, "d2": d2, "sum": d1 + d2},
		MahjongSeatCards: seatCards,
		RemainingWall:    wall,
		WallCount:        len(wall),
		CreatedAt:        nowMs(),
	}
}

func mjTileKey(t string, v int) string {
	return t + "_" + itoa(v)
}

func parseMJTileKey(k string) (string, int) {
	idx := strings.IndexByte(k, '_')
	if idx <= 0 {
		return k, 0
	}
	v := 0
	for _, r := range k[idx+1:] {
		if r >= '0' && r <= '9' {
			v = v*10 + int(r-'0')
		}
	}
	return k[:idx], v
}

func checkMeldsOnly(m map[string]int, wildcards int) bool {
	var keys []string
	for k, cnt := range m {
		if cnt > 0 {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return wildcards%3 == 0
	}
	sort.Strings(keys)
	k := keys[0]
	t, val := parseMJTileKey(k)

	// 1. Try triplet
	if m[k] >= 3 {
		m[k] -= 3
		if checkMeldsOnly(m, wildcards) {
			m[k] += 3
			return true
		}
		m[k] += 3
	}
	if m[k] >= 2 && wildcards >= 1 {
		m[k] -= 2
		if checkMeldsOnly(m, wildcards-1) {
			m[k] += 2
			return true
		}
		m[k] += 2
	}
	if m[k] >= 1 && wildcards >= 2 {
		m[k] -= 1
		if checkMeldsOnly(m, wildcards-2) {
			m[k] += 1
			return true
		}
		m[k] += 1
	}

	// 2. Try sequence
	if t != "zi" {
		k2 := mjTileKey(t, val+1)
		k3 := mjTileKey(t, val+2)
		if m[k] >= 1 && m[k2] >= 1 && m[k3] >= 1 {
			m[k]--
			m[k2]--
			m[k3]--
			if checkMeldsOnly(m, wildcards) {
				m[k]++
				m[k2]++
				m[k3]++
				return true
			}
			m[k]++
			m[k2]++
			m[k3]++
		}
		if wildcards >= 1 {
			if m[k] >= 1 && m[k2] >= 1 {
				m[k]--
				m[k2]--
				if checkMeldsOnly(m, wildcards-1) {
					m[k]++
					m[k2]++
					return true
				}
				m[k]++
				m[k2]++
			}
			if m[k] >= 1 && m[k3] >= 1 {
				m[k]--
				m[k3]--
				if checkMeldsOnly(m, wildcards-1) {
					m[k]++
					m[k3]++
					return true
				}
				m[k]++
				m[k3]++
			}
		}
	}
	return false
}

func decomposeMahjongMelds(normalTiles []MahjongTile, wildcards int) bool {
	m := map[string]int{}
	keySet := map[string]bool{}
	for _, t := range normalTiles {
		k := mjTileKey(t.Type, t.Value)
		m[k]++
		keySet[k] = true
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, pk := range keys {
		if m[pk] >= 2 {
			m[pk] -= 2
			if checkMeldsOnly(m, wildcards) {
				return true
			}
			m[pk] += 2
		}
		if m[pk] >= 1 && wildcards >= 1 {
			m[pk] -= 1
			if checkMeldsOnly(m, wildcards-1) {
				return true
			}
			m[pk] += 1
		}
	}
	if wildcards >= 2 {
		if checkMeldsOnly(m, wildcards-2) {
			return true
		}
	}
	return false
}

func checkMahjongWin(tiles []MahjongTile, isHongzhongMode bool) (bool, string, int) {
	if len(tiles)%3 != 2 {
		return false, "", 0
	}
	hzCount := 0
	var normalTiles []MahjongTile
	for _, t := range tiles {
		if isHongzhongMode && isHongzhong(t) {
			hzCount++
		} else {
			normalTiles = append(normalTiles, t)
		}
	}
	if isHongzhongMode && hzCount >= 4 {
		return true, "【四红中直接天胡满贯】", 8
	}
	if len(tiles) == 14 {
		counts := map[string]int{}
		for _, t := range normalTiles {
			counts[mjTileKey(t.Type, t.Value)]++
		}
		pairs := 0
		singles := 0
		for _, cnt := range counts {
			pairs += cnt / 2
			if cnt%2 == 1 {
				singles++
			}
		}
		minS := singles
		if hzCount < minS {
			minS = hzCount
		}
		remHz := hzCount - singles
		if remHz < 0 {
			remHz = 0
		}
		if pairs+minS+remHz/2 >= 7 {
			return true, "【七对自摸】", 4
		}
	}

	if decomposeMahjongMelds(normalTiles, hzCount) {
		isQing := true
		if len(normalTiles) > 0 {
			ft := normalTiles[0].Type
			if ft == "zi" {
				isQing = false
			} else {
				for _, t := range normalTiles {
					if t.Type != ft {
						isQing = false
						break
					}
				}
			}
		}
		if isQing {
			return true, "【清一色自摸大胡】", 6
		}
		return true, "【自摸和牌】", 2
	}
	return false, "", 0
}

type MahjongSettlementResult struct {
	WinnerSeat        int             `json:"winnerSeat"`
	WinningHand       []MahjongTile   `json:"winningHand"`
	WinningMelds      [][]MahjongTile `json:"winningMelds"`
	MaCount           int             `json:"maCount"`
	MaTiles           []MahjongTile   `json:"maTiles"`
	HitMaCount        int             `json:"hitMaCount"`
	PayoutPerOpponent int             `json:"payoutPerOpponent"`
	IsFourHongzhong   bool            `json:"isFourHongzhong"`
	IsFourHz          bool            `json:"isFourHz"`
	NetCoinsPerSeat   [4]int          `json:"netCoinsPerSeat"`
	SeatKongScores    [4]int          `json:"seatKongScores"`
}

func computeMahjongSettlement(winnerSeat int, winningHand []MahjongTile, melds []MahjongMeld, remainingWall []MahjongTile, sceneMode string, seatKongScores [4]int, isFourHzOverride *bool, maTilesOverride []MahjongTile) MahjongSettlementResult {
	sortedHand := sortMahjongTiles(winningHand)
	var winningMelds [][]MahjongTile
	for _, m := range melds {
		if len(m.Tiles) > 0 {
			winningMelds = append(winningMelds, sortMahjongTiles(m.Tiles))
		} else {
			cnt := 3
			if m.Type == "gang" {
				cnt = 4
			}
			arr := make([]MahjongTile, cnt)
			for i := 0; i < cnt; i++ {
				tCopy := m.Tile
				tCopy.ID = m.Tile.ID + "-m-" + itoa(i)
				arr[i] = tCopy
			}
			winningMelds = append(winningMelds, arr)
		}
	}

	isHzMode := strings.Contains(sceneMode, "hongzhong") || sceneMode == ""
	baseMaCount := 6
	if strings.Contains(sceneMode, "10") {
		baseMaCount = 10
	}
	hzCount := 0
	for _, t := range sortedHand {
		if isHongzhong(t) {
			hzCount++
		}
	}
	isFourHz := isHzMode && hzCount >= 4
	if isFourHzOverride != nil {
		isFourHz = *isFourHzOverride
	}

	maCount := baseMaCount
	hitMaCount := 0
	payoutPerOpponent := 0
	var maTiles []MahjongTile

	if isFourHz {
		hitMaCount = baseMaCount
		payoutPerOpponent = 10 + hitMaCount*10
		take := maCount
		if take > len(remainingWall) {
			take = len(remainingWall)
		}
		maTiles = append(maTiles, remainingWall[:take]...)
		for len(maTiles) < maCount {
			maTiles = append(maTiles, MahjongTile{
				ID:          "hz-win-" + itoa(len(maTiles)),
				Type:        "zi",
				Value:       5,
				Text:        "红中",
				Color:       "#DC2626",
				IsRedDragon: true,
			})
		}
	} else {
		if isHzMode && hzCount == 0 {
			maCount = baseMaCount + 2
		}
		if len(maTilesOverride) > 0 {
			end := maCount
			if end > len(maTilesOverride) {
				end = len(maTilesOverride)
			}
			maTiles = append(maTiles, maTilesOverride[:end]...)
		} else {
			take := maCount
			if take > len(remainingWall) {
				take = len(remainingWall)
			}
			maTiles = append(maTiles, remainingWall[:take]...)
			suitTypes := []string{"wan", "tiao", "tong"}
			suitTexts := map[string][]string{
				"wan":  {"", "一万", "二万", "三万", "四万", "五万", "六万", "七万", "八万", "九万"},
				"tiao": {"", "一条", "二条", "三条", "四条", "五条", "六条", "七条", "八条", "九条"},
				"tong": {"", "一筒", "二筒", "三筒", "四筒", "五筒", "六筒", "七筒", "八筒", "九筒"},
			}
			idx := 0
			for len(maTiles) < maCount {
				st := suitTypes[idx%3]
				val := (idx % 9) + 1
				maTiles = append(maTiles, MahjongTile{
					ID:    "fallback-ma-" + itoa(len(maTiles)),
					Type:  st,
					Value: val,
					Text:  suitTexts[st][val],
				})
				idx++
			}
		}
		for _, mt := range maTiles {
			if isYiWuJiuMa(mt) {
				hitMaCount++
			}
		}
		payoutPerOpponent = 10 + hitMaCount*10
	}

	var netCoins [4]int
	for s := 0; s < 4; s++ {
		zimoDelta := -payoutPerOpponent
		if s == winnerSeat {
			zimoDelta = payoutPerOpponent * 3
		}
		netCoins[s] = zimoDelta + seatKongScores[s]
	}

	return MahjongSettlementResult{
		WinnerSeat:        winnerSeat,
		WinningHand:       sortedHand,
		WinningMelds:      winningMelds,
		MaCount:           maCount,
		MaTiles:           maTiles,
		HitMaCount:        hitMaCount,
		PayoutPerOpponent: payoutPerOpponent,
		IsFourHongzhong:   isFourHz,
		IsFourHz:          isFourHz,
		NetCoinsPerSeat:   netCoins,
		SeatKongScores:    seatKongScores,
	}
}

// ==============================================================================
// Authoritative Mahjong Server Move Engine
// ==============================================================================

func executeMahjongWinSettlement(ctx *HubContext, room *RoomState, winnerSeat int, isFourHz bool, pPayload map[string]interface{}) {
	if room == nil || room.BoardState == nil || room.BoardState.Phase == "ended" {
		return
	}
	bs := room.BoardState
	expiresAt := nowMs() + 30000
	bs.WinnerSeat = &winnerSeat
	bs.Phase = "ended"
	if bs.ExtraState != nil {
		bs.ExtraState.ClaimWindow = nil
		bs.ExtraState.PendingPengSeat = nil
	}
	room.Status = "finished"
	room.SettlementExpiresAt = expiresAt

	var winnerHand []MahjongTile
	if room.Deal != nil && room.Deal.MahjongSeatCards != nil {
		for _, t := range room.Deal.MahjongSeatCards[winnerSeat] {
			if !t.Hidden {
				winnerHand = append(winnerHand, t)
			}
		}
	}
	if len(winnerHand) == 0 && pPayload != nil {
		if whRaw, ok := pPayload["winningHand"]; ok && whRaw != nil {
			b, _ := json.Marshal(whRaw)
			_ = json.Unmarshal(b, &winnerHand)
		}
	}

	var remWall []MahjongTile
	if room.Deal != nil {
		remWall = room.Deal.RemainingWall
	}
	var winnerMelds []MahjongMeld
	if bs.ExtraState != nil && winnerSeat >= 0 && winnerSeat < len(bs.ExtraState.MahjongMelds) {
		winnerMelds = bs.ExtraState.MahjongMelds[winnerSeat]
	}
	var kongScores [4]int
	for i := 0; i < 4 && i < len(bs.SeatKongScores); i++ {
		kongScores[i] = bs.SeatKongScores[i]
	}
	var maOverride []MahjongTile
	if pPayload != nil {
		if rawMa, ok := pPayload["maTiles"]; ok && rawMa != nil {
			b, _ := json.Marshal(rawMa)
			_ = json.Unmarshal(b, &maOverride)
		}
	}
	sMode := room.SceneMode
	if sMode == "" {
		sMode = "hongzhong-6"
	}

	stl := computeMahjongSettlement(winnerSeat, winnerHand, winnerMelds, remWall, sMode, kongScores, &isFourHz, maOverride)
	if stl.IsFourHongzhong {
		bs.WinDesc = "摸到四张红中直接胡牌"
	} else {
		bs.WinDesc = "自摸和牌"
	}

	var kongHistory []MahjongKongRecord
	if bs.ExtraState != nil {
		kongHistory = append([]MahjongKongRecord{}, bs.ExtraState.KongHistory...)
	}

	bs.Settlement = map[string]interface{}{
		"winnerSeat":          stl.WinnerSeat,
		"winningHand":         stl.WinningHand,
		"winningMelds":        stl.WinningMelds,
		"maCount":             stl.MaCount,
		"maTiles":             stl.MaTiles,
		"hitMaCount":          stl.HitMaCount,
		"payoutPerOpponent":   stl.PayoutPerOpponent,
		"isFourHongzhong":     stl.IsFourHongzhong,
		"isFourHz":            stl.IsFourHongzhong,
		"netCoinsPerSeat":     stl.NetCoinsPerSeat,
		"seatKongScores":      stl.SeatKongScores,
		"kongHistory":         kongHistory,
		"settlementExpiresAt": expiresAt,
		"winDesc":             bs.WinDesc,
	}

	var ratingDelta [4]int
	for i := 0; i < 4; i++ {
		if stl.NetCoinsPerSeat[i] > 0 {
			ratingDelta[i] = 15
		} else if stl.NetCoinsPerSeat[i] < 0 {
			ratingDelta[i] = -5
		}
	}
	ctx.SettleRoomCoins(room, stl.NetCoinsPerSeat[:], "mahjong", ratingDelta[:])

	reason := "zimo_hu"
	if stl.IsFourHongzhong {
		reason = "four_hongzhong"
	}
	huEvt := map[string]interface{}{
		"action":              "hu",
		"seat":                winnerSeat,
		"absSeat":             winnerSeat,
		"winner":              winnerSeat,
		"isFourHz":            stl.IsFourHongzhong,
		"winningHand":         stl.WinningHand,
		"winningMelds":        stl.WinningMelds,
		"maCount":             stl.MaCount,
		"maTiles":             stl.MaTiles,
		"hitMaCount":          stl.HitMaCount,
		"payoutPerOpponent":   stl.PayoutPerOpponent,
		"netCoinsPerSeat":     stl.NetCoinsPerSeat,
		"seatKongScores":      stl.SeatKongScores,
		"kongHistory":         kongHistory,
		"rewardCoins":         stl.NetCoinsPerSeat[winnerSeat],
		"reason":              reason,
		"settlementExpiresAt": expiresAt,
		"serverTs":            nowMs(),
	}
	ctx.SendSanitizedToRoom(room, func(_ int, _ ScenePlayer) interface{} {
		return map[string]interface{}{
			"type":       "game_event",
			"roomId":     room.ID,
			"seat":       winnerSeat,
			"event":      huEvt,
			"payload":    huEvt,
			"boardState": bs,
		}
	})
	ctx.ResetSceneRoomReady(room.ID)
}

func executeMahjongDrawStalemate(ctx *HubContext, room *RoomState, triggerSeat int, reasonMsg string) {
	if room == nil || room.BoardState == nil || room.BoardState.Phase == "ended" {
		return
	}
	if reasonMsg == "" {
		reasonMsg = "牌墙抓毕，本局荒庄（流局）！"
	}
	bs := room.BoardState
	nowTs := nowMs()
	expiresAt := nowTs + 30000
	bs.Phase = "ended"
	bs.IsDraw = true
	if bs.ExtraState == nil {
		bs.ExtraState = &ExtraBoardState{}
	}
	bs.ExtraState.WallRemaining = 0
	bs.ExtraState.PendingPengSeat = nil
	bs.ExtraState.ClaimWindow = nil
	room.Status = "finished"
	room.SettlementExpiresAt = expiresAt
	room.MoveCount++
	bs.ActionSeq++
	bs.MoveCount = room.MoveCount
	bs.UpdatedAt = nowTs

	var kongScores [4]int
	hasKong := false
	for i := 0; i < 4 && i < len(bs.SeatKongScores); i++ {
		kongScores[i] = bs.SeatKongScores[i]
		if kongScores[i] != 0 {
			hasKong = true
		}
	}
	if hasKong {
		var zeroRating [4]int
		ctx.SettleRoomCoins(room, kongScores[:], "mahjong", zeroRating[:])
	}

	kongHistory := append([]MahjongKongRecord{}, bs.ExtraState.KongHistory...)
	bs.Settlement = map[string]interface{}{
		"isDraw":              true,
		"message":             reasonMsg,
		"seatKongScores":      kongScores,
		"netCoinsPerSeat":     kongScores,
		"kongHistory":         kongHistory,
		"settlementExpiresAt": expiresAt,
	}

	ctx.ResetSceneRoomReady(room.ID)

	stEvt := map[string]interface{}{
		"action":              "draw_stalemate",
		"isDraw":              true,
		"reason":              "wall_exhausted",
		"message":             reasonMsg,
		"msg":                 reasonMsg,
		"seatKongScores":      kongScores,
		"netCoinsPerSeat":     kongScores,
		"kongHistory":         kongHistory,
		"settlementExpiresAt": expiresAt,
		"serverTs":            nowTs,
	}
	ctx.SendSanitizedToRoom(room, func(_ int, _ ScenePlayer) interface{} {
		return map[string]interface{}{
			"type":       "game_event",
			"roomId":     room.ID,
			"seat":       triggerSeat,
			"event":      stEvt,
			"payload":    stEvt,
			"moveCount":  room.MoveCount,
			"boardState": bs,
		}
	})
}

func computeMahjongClaimPendingSeats(room *RoomState, bs *AuthoritativeBoardState, discardSeat int, discTile *MahjongTile) []int {
	if discTile == nil || room.Deal == nil || room.Deal.MahjongSeatCards == nil {
		return nil
	}
	isHzMode := room.SceneMode == "" || strings.Contains(room.SceneMode, "hongzhong")
	if isHzMode && isHongzhong(*discTile) {
		return nil
	}

	var pending []int
	for s := 0; s < 4; s++ {
		if s == discardSeat {
			continue
		}
		var pl *ScenePlayer
		for i := range room.Players {
			if room.Players[i].Seat == s {
				pl = &room.Players[i]
				break
			}
		}
		isAuto := (pl != nil && (pl.IsAutoManaged || pl.IsDisconnected || isBotLikePlayer(*pl))) ||
			(bs.AutoManagedSeats != nil && bs.AutoManagedSeats[s])
		if isAuto {
			continue
		}
		hand := room.Deal.MahjongSeatCards[s]
		if len(hand)%3 != 1 {
			continue
		}
		matchCount := 0
		for _, c := range hand {
			if isMahjongTileMatch(c, *discTile) {
				matchCount++
			}
		}
		if matchCount >= 2 {
			pending = append(pending, s)
		}
	}
	return pending
}

func sendMahjongReject(ctx *HubContext, sender *WSConn, room *RoomState, seat int, rejectAction, reason string) {
	if sender == nil || room == nil || room.BoardState == nil {
		return
	}
	bs := room.BoardState
	var seatHand []MahjongTile
	if room.Deal != nil && room.Deal.MahjongSeatCards != nil {
		seatHand = room.Deal.MahjongSeatCards[seat]
	}
	wallRem := 0
	if bs.ExtraState != nil {
		wallRem = bs.ExtraState.WallRemaining
	}
	var pend interface{}
	if bs.ExtraState != nil && bs.ExtraState.PendingPengSeat != nil {
		pend = *bs.ExtraState.PendingPengSeat
	}
	evt := map[string]interface{}{
		"action":          rejectAction,
		"seat":            seat,
		"absSeat":         seat,
		"turnSeat":        bs.TurnSeat,
		"pendingPengSeat": pend,
		"wallRemaining":   wallRem,
		"seatHand":        seatHand,
		"reason":          reason,
	}
	ctx.SendToConn(sender, map[string]interface{}{
		"type":       "game_event",
		"roomId":     room.ID,
		"seat":       seat,
		"event":      evt,
		"payload":    evt,
		"boardState": bs,
	})
}

func handleMahjongGameMove(ctx *HubContext, sender *WSConn, room *RoomState, seat int, action string, p map[string]interface{}) {
	if room == nil || room.BoardState == nil {
		return
	}
	bs := room.BoardState
	if bs.ExtraState == nil {
		bs.ExtraState = &ExtraBoardState{
			MahjongDiscards: []MahjongTile{},
			MahjongMelds:    [][]MahjongMeld{{}, {}, {}, {}},
			KongHistory:     []MahjongKongRecord{},
		}
	}
	ex := bs.ExtraState
	for len(ex.MahjongMelds) < 4 {
		ex.MahjongMelds = append(ex.MahjongMelds, []MahjongMeld{})
	}
	for len(bs.SeatKongScores) < 4 {
		bs.SeatKongScores = append(bs.SeatKongScores, 0)
	}
	for len(bs.HandsCount) < 4 {
		bs.HandsCount = append(bs.HandsCount, 13)
	}
	isHzMode := room.SceneMode == "" || strings.Contains(room.SceneMode, "hongzhong")

	var tilePtr *MahjongTile
	if rawT, ok := p["tile"]; ok && rawT != nil {
		b, _ := json.Marshal(rawT)
		var mt MahjongTile
		if json.Unmarshal(b, &mt) == nil && mt.Type != "" {
			tilePtr = &mt
		}
	}

	switch action {
	case "discard":
		if (ex.PendingPengSeat != nil && *ex.PendingPengSeat != seat) || bs.TurnSeat != seat {
			sendMahjongReject(ctx, sender, room, seat, "discard_rejected", "not_your_turn")
			return
		}
		if tilePtr == nil {
			sendMahjongReject(ctx, sender, room, seat, "discard_rejected", "missing_tile")
			return
		}
		var discTile MahjongTile
		if room.Deal != nil && room.Deal.MahjongSeatCards != nil {
			hand := room.Deal.MahjongSeatCards[seat]
			if len(hand)%3 != 2 {
				sendMahjongReject(ctx, sender, room, seat, "discard_rejected", "invalid_hand_count")
				return
			}
			fIdx := -1
			for i, c := range hand {
				if tilePtr.ID != "" && c.ID == tilePtr.ID {
					fIdx = i
					break
				}
			}
			if fIdx < 0 {
				for i, c := range hand {
					if isMahjongTileMatch(c, *tilePtr) {
						fIdx = i
						break
					}
				}
			}
			if fIdx < 0 {
				sendMahjongReject(ctx, sender, room, seat, "discard_rejected", "tile_not_in_hand")
				return
			}
			discTile = hand[fIdx]
			hand = append(hand[:fIdx], hand[fIdx+1:]...)
			room.Deal.MahjongSeatCards[seat] = hand
			bs.HandsCount[seat] = len(hand)
		} else {
			discTile = *tilePtr
			if bs.HandsCount[seat] > 0 {
				bs.HandsCount[seat]--
			}
		}

		sCopy := seat
		discTile.DiscardBy = &sCopy
		ex.MahjongDiscards = append(ex.MahjongDiscards, discTile)
		ex.LastDiscarded = &discTile
		ex.PendingPengSeat = nil
		ex.LastDrawnSeat = nil
		ex.LastDrawnTile = nil

		nextTurn := (seat + 1) % 4
		bs.TurnSeat = nextTurn
		room.TurnSeat = nextTurn
		if room.Deal != nil {
			ex.WallRemaining = len(room.Deal.RemainingWall)
		}

		pendingSeats := computeMahjongClaimPendingSeats(room, bs, seat, &discTile)
		if len(pendingSeats) > 0 {
			ex.ClaimWindow = &MahjongClaimWindow{
				DiscardID:     discTile.ID,
				DiscarderSeat: seat,
				PendingSeats:  pendingSeats,
				PassedSeats:   []int{},
				CreatedAt:     nowMs(),
			}
		} else {
			ex.ClaimWindow = nil
		}

		room.MoveCount++
		bs.ActionSeq++
		bs.MoveCount = room.MoveCount
		bs.UpdatedAt = nowMs()
		room.LastTurnTs = nowMs()

		evt := map[string]interface{}{
			"action":            "discard",
			"seat":              seat,
			"tile":              discTile,
			"nextTurn":          nextTurn,
			"wallRemaining":     ex.WallRemaining,
			"pendingClaimSeats": pendingSeats,
			"hasClaimWindow":    len(pendingSeats) > 0,
		}
		ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
			return map[string]interface{}{
				"type":       "game_event",
				"roomId":     room.ID,
				"seat":       seat,
				"event":      evt,
				"payload":    evt,
				"moveCount":  room.MoveCount,
				"boardState": sanitizeBoardStateForSeat(bs, "mahjong", rSeat),
			}
		})

		if len(pendingSeats) == 0 {
			if room.Deal != nil && len(room.Deal.RemainingWall) == 0 {
				executeMahjongDrawStalemate(ctx, room, nextTurn, "牌墙抓毕，本局荒庄（流局）！")
			} else {
				bs.ActionSeq++
				cr := map[string]interface{}{
					"action":        "claim_resolved",
					"discardId":     discTile.ID,
					"nextTurn":      nextTurn,
					"wallRemaining": ex.WallRemaining,
				}
				ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
					return map[string]interface{}{
						"type":       "game_event",
						"roomId":     room.ID,
						"seat":       nextTurn,
						"event":      cr,
						"payload":    cr,
						"moveCount":  room.MoveCount,
						"boardState": sanitizeBoardStateForSeat(bs, "mahjong", rSeat),
					}
				})
			}
		}
		ctx.ScheduleSnapshot()

	case "draw":
		if ex.ClaimWindow != nil {
			passedSet := map[int]bool{}
			for _, ps := range ex.ClaimWindow.PassedSeats {
				passedSet[ps] = true
			}
			for _, ps := range ex.ClaimWindow.PendingSeats {
				if !passedSet[ps] {
					sendMahjongReject(ctx, sender, room, seat, "draw_rejected", "claim_window_open")
					return
				}
			}
		}
		if ex.PendingPengSeat != nil && *ex.PendingPengSeat != seat {
			sendMahjongReject(ctx, sender, room, seat, "draw_rejected", "pending_peng")
			return
		}
		if bs.TurnSeat != seat {
			sendMahjongReject(ctx, sender, room, seat, "draw_rejected", "not_your_turn")
			return
		}
		if room.Deal == nil || room.Deal.MahjongSeatCards == nil {
			return
		}
		hand := room.Deal.MahjongSeatCards[seat]
		if len(hand)%3 != 1 {
			sendMahjongReject(ctx, sender, room, seat, "draw_rejected", "already_drew")
			return
		}
		if len(room.Deal.RemainingWall) == 0 {
			executeMahjongDrawStalemate(ctx, room, seat, "牌墙抓毕，本局荒庄（流局）！")
			return
		}
		drawn := room.Deal.RemainingWall[0]
		room.Deal.RemainingWall = room.Deal.RemainingWall[1:]
		hand = append(hand, drawn)
		room.Deal.MahjongSeatCards[seat] = hand
		ex.WallRemaining = len(room.Deal.RemainingWall)
		bs.HandsCount[seat] = len(hand)
		sCopy := seat
		ex.LastDrawnSeat = &sCopy
		ex.LastDrawnTile = &drawn
		bs.TurnSeat = seat
		room.TurnSeat = seat
		room.MoveCount++
		bs.ActionSeq++
		bs.MoveCount = room.MoveCount
		bs.UpdatedAt = nowMs()
		room.LastTurnTs = nowMs()

		ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
			var masked interface{} = drawn
			if rSeat != seat {
				masked = maskHiddenMahjongTile(&drawn)
			}
			evt := map[string]interface{}{
				"action":        "draw",
				"seat":          seat,
				"tile":          masked,
				"wallRemaining": ex.WallRemaining,
			}
			return map[string]interface{}{
				"type":       "game_event",
				"roomId":     room.ID,
				"seat":       seat,
				"event":      evt,
				"payload":    evt,
				"moveCount":  room.MoveCount,
				"boardState": sanitizeBoardStateForSeat(bs, "mahjong", rSeat),
			}
		})
		ctx.ScheduleSnapshot()

	case "peng":
		lastDisc := ex.LastDiscarded
		if lastDisc == nil || ex.PendingPengSeat != nil || (lastDisc.DiscardBy != nil && *lastDisc.DiscardBy == seat) ||
			tilePtr == nil || !isMahjongTileMatch(*tilePtr, *lastDisc) || (isHzMode && isHongzhong(*lastDisc)) {
			sendMahjongReject(ctx, sender, room, seat, "claim_rejected", "discard_not_available")
			return
		}
		if room.Deal == nil || room.Deal.MahjongSeatCards == nil {
			return
		}
		hand := room.Deal.MahjongSeatCards[seat]
		var matchIdx []int
		for i := len(hand) - 1; i >= 0 && len(matchIdx) < 2; i-- {
			if isMahjongTileMatch(hand[i], *lastDisc) {
				matchIdx = append(matchIdx, i)
			}
		}
		if len(hand)%3 != 1 || len(matchIdx) < 2 {
			sendMahjongReject(ctx, sender, room, seat, "claim_rejected", "insufficient_matching_tiles")
			return
		}

		ex.ClaimWindow = nil
		var canceledSeat *int
		var canceledTile *MahjongTile
		if ex.LastDrawnSeat != nil && *ex.LastDrawnSeat != seat {
			dSeat := *ex.LastDrawnSeat
			dHand := room.Deal.MahjongSeatCards[dSeat]
			if len(dHand)%3 == 2 {
				rIdx := len(dHand) - 1
				if ex.LastDrawnTile != nil {
					for i, c := range dHand {
						if c.ID == ex.LastDrawnTile.ID {
							rIdx = i
							break
						}
					}
				}
				ret := dHand[rIdx]
				dHand = append(dHand[:rIdx], dHand[rIdx+1:]...)
				room.Deal.MahjongSeatCards[dSeat] = dHand
				bs.HandsCount[dSeat] = len(dHand)
				room.Deal.RemainingWall = append([]MahjongTile{ret}, room.Deal.RemainingWall...)
				ex.WallRemaining = len(room.Deal.RemainingWall)
				canceledSeat = &dSeat
				canceledTile = &ret
			}
			ex.LastDrawnSeat = nil
			ex.LastDrawnTile = nil
		}

		// Pop claimed discard from MahjongDiscards
		claimedTile := *lastDisc
		for i := len(ex.MahjongDiscards) - 1; i >= 0; i-- {
			if ex.MahjongDiscards[i].ID == lastDisc.ID {
				claimedTile = ex.MahjongDiscards[i]
				ex.MahjongDiscards = append(ex.MahjongDiscards[:i], ex.MahjongDiscards[i+1:]...)
				break
			}
		}
		var removedTiles []MahjongTile
		for _, idx := range matchIdx {
			removedTiles = append(removedTiles, hand[idx])
			hand = append(hand[:idx], hand[idx+1:]...)
		}
		room.Deal.MahjongSeatCards[seat] = hand
		bs.HandsCount[seat] = len(hand)

		meldTiles := append(removedTiles, claimedTile)
		ex.MahjongMelds[seat] = append(ex.MahjongMelds[seat], MahjongMeld{
			Type:  "peng",
			Tile:  claimedTile,
			Tiles: meldTiles,
			Seat:  seat,
		})
		ex.LastDiscarded = nil
		sCopy := seat
		ex.PendingPengSeat = &sCopy
		bs.TurnSeat = seat
		room.TurnSeat = seat
		room.MoveCount++
		bs.ActionSeq++
		bs.MoveCount = room.MoveCount
		bs.UpdatedAt = nowMs()
		room.LastTurnTs = nowMs()

		evt := map[string]interface{}{
			"action":        "peng",
			"seat":          seat,
			"tile":          claimedTile,
			"meldTiles":     meldTiles,
			"wallRemaining": ex.WallRemaining,
		}
		if canceledSeat != nil {
			evt["canceledDrawSeat"] = *canceledSeat
			if canceledTile != nil {
				evt["canceledDrawTile"] = *canceledTile
			}
		}
		ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
			return map[string]interface{}{
				"type":       "game_event",
				"roomId":     room.ID,
				"seat":       seat,
				"event":      evt,
				"payload":    evt,
				"moveCount":  room.MoveCount,
				"boardState": sanitizeBoardStateForSeat(bs, "mahjong", rSeat),
			}
		})
		ctx.ScheduleSnapshot()

	case "gang":
		if room.Deal == nil || room.Deal.MahjongSeatCards == nil {
			return
		}
		if len(room.Deal.RemainingWall) == 0 {
			sendMahjongReject(ctx, sender, room, seat, "claim_rejected", "wall_empty_no_gang")
			return
		}
		lastDisc := ex.LastDiscarded
		hand := room.Deal.MahjongSeatCards[seat]
		gangType, _ := p["gangType"].(string)
		if gangType == "" || gangType == "an_or_bu" {
			hasPeng := false
			if tilePtr != nil {
				for _, m := range ex.MahjongMelds[seat] {
					if m.Type == "peng" && isMahjongTileMatch(m.Tile, *tilePtr) {
						hasPeng = true
						break
					}
				}
			}
			if lastDisc != nil && (lastDisc.DiscardBy == nil || *lastDisc.DiscardBy != seat) && (gangType == "" && bs.TurnSeat != seat) {
				gangType = "ming"
			} else if hasPeng {
				gangType = "bu"
			} else {
				gangType = "an"
			}
		}

		var targetTile MahjongTile
		needRemove := 3
		var fromSeatPtr *int
		switch gangType {
		case "ming":
			if lastDisc == nil || ex.PendingPengSeat != nil || lastDisc.DiscardBy == nil || *lastDisc.DiscardBy == seat ||
				tilePtr == nil || !isMahjongTileMatch(*tilePtr, *lastDisc) || (isHzMode && isHongzhong(*lastDisc)) {
				sendMahjongReject(ctx, sender, room, seat, "claim_rejected", "discard_not_available")
				return
			}
			cnt := 0
			for _, c := range hand {
				if isMahjongTileMatch(c, *lastDisc) {
					cnt++
				}
			}
			if len(hand)%3 != 1 || cnt < 3 {
				sendMahjongReject(ctx, sender, room, seat, "claim_rejected", "insufficient_matching_tiles")
				return
			}
			targetTile = *lastDisc
			fs := *lastDisc.DiscardBy
			fromSeatPtr = &fs
			needRemove = 3
		case "an":
			if bs.TurnSeat != seat || (ex.PendingPengSeat != nil && *ex.PendingPengSeat != seat) ||
				tilePtr == nil || (isHzMode && isHongzhong(*tilePtr)) {
				sendMahjongReject(ctx, sender, room, seat, "claim_rejected", "invalid_an_gang")
				return
			}
			cnt := 0
			for _, c := range hand {
				if isMahjongTileMatch(c, *tilePtr) {
					cnt++
				}
			}
			if len(hand)%3 != 2 || cnt != 4 {
				sendMahjongReject(ctx, sender, room, seat, "claim_rejected", "insufficient_matching_tiles")
				return
			}
			targetTile = *tilePtr
			needRemove = 4
		case "bu":
			if bs.TurnSeat != seat || (ex.PendingPengSeat != nil && *ex.PendingPengSeat != seat) || tilePtr == nil {
				sendMahjongReject(ctx, sender, room, seat, "claim_rejected", "invalid_bu_gang")
				return
			}
			hasPeng := false
			for _, m := range ex.MahjongMelds[seat] {
				if m.Type == "peng" && isMahjongTileMatch(m.Tile, *tilePtr) {
					hasPeng = true
					break
				}
			}
			cnt := 0
			for _, c := range hand {
				if isMahjongTileMatch(c, *tilePtr) {
					cnt++
				}
			}
			if !hasPeng || len(hand)%3 != 2 || cnt < 1 {
				sendMahjongReject(ctx, sender, room, seat, "claim_rejected", "invalid_bu_gang")
				return
			}
			targetTile = *tilePtr
			needRemove = 1
		}

		ex.ClaimWindow = nil
		var canceledSeat *int
		var canceledTile *MahjongTile
		if gangType == "ming" && ex.LastDrawnSeat != nil && *ex.LastDrawnSeat != seat {
			dSeat := *ex.LastDrawnSeat
			dHand := room.Deal.MahjongSeatCards[dSeat]
			if len(dHand)%3 == 2 {
				rIdx := len(dHand) - 1
				if ex.LastDrawnTile != nil {
					for i, c := range dHand {
						if c.ID == ex.LastDrawnTile.ID {
							rIdx = i
							break
						}
					}
				}
				ret := dHand[rIdx]
				dHand = append(dHand[:rIdx], dHand[rIdx+1:]...)
				room.Deal.MahjongSeatCards[dSeat] = dHand
				bs.HandsCount[dSeat] = len(dHand)
				room.Deal.RemainingWall = append([]MahjongTile{ret}, room.Deal.RemainingWall...)
				ex.WallRemaining = len(room.Deal.RemainingWall)
				canceledSeat = &dSeat
				canceledTile = &ret
			}
			ex.LastDrawnSeat = nil
			ex.LastDrawnTile = nil
			hand = room.Deal.MahjongSeatCards[seat]
		}

		var removed []MahjongTile
		for i := len(hand) - 1; i >= 0 && len(removed) < needRemove; i-- {
			if isMahjongTileMatch(hand[i], targetTile) {
				removed = append(removed, hand[i])
				hand = append(hand[:i], hand[i+1:]...)
			}
		}

		var meldTiles []MahjongTile
		if gangType == "bu" {
			for i, m := range ex.MahjongMelds[seat] {
				if m.Type == "peng" && isMahjongTileMatch(m.Tile, targetTile) {
					meldTiles = append(append([]MahjongTile{}, m.Tiles...), removed[0])
					ex.MahjongMelds[seat][i] = MahjongMeld{
						Type: "gang", Tile: removed[0], Tiles: meldTiles, Seat: seat, GangType: "bu",
					}
					break
				}
			}
		} else {
			if gangType == "ming" && lastDisc != nil {
				for i := len(ex.MahjongDiscards) - 1; i >= 0; i-- {
					if ex.MahjongDiscards[i].ID == lastDisc.ID {
						ex.MahjongDiscards = append(ex.MahjongDiscards[:i], ex.MahjongDiscards[i+1:]...)
						break
					}
				}
				meldTiles = append(removed, *lastDisc)
			} else {
				meldTiles = removed
			}
			ex.MahjongMelds[seat] = append(ex.MahjongMelds[seat], MahjongMeld{
				Type: "gang", Tile: targetTile, Tiles: meldTiles, Seat: seat, GangType: gangType,
			})
		}
		ex.LastDiscarded = nil
		ex.PendingPengSeat = nil

		// Draw supplemental tile from back of wall
		lastWallIdx := len(room.Deal.RemainingWall) - 1
		suppTile := room.Deal.RemainingWall[lastWallIdx]
		room.Deal.RemainingWall = room.Deal.RemainingWall[:lastWallIdx]
		hand = append(hand, suppTile)
		room.Deal.MahjongSeatCards[seat] = hand
		bs.HandsCount[seat] = len(hand)
		ex.WallRemaining = len(room.Deal.RemainingWall)
		sCopy := seat
		ex.LastDrawnSeat = &sCopy
		ex.LastDrawnTile = &suppTile

		// Kong scoring: ming (+10 from discarder), an/bu (+30, -10 each)
		var deltas [4]int
		if gangType == "ming" && fromSeatPtr != nil {
			fs := *fromSeatPtr
			deltas[seat] = 10
			deltas[fs] = -10
		} else {
			for s := 0; s < 4; s++ {
				if s == seat {
					deltas[s] = 30
				} else {
					deltas[s] = -10
				}
			}
		}
		for s := 0; s < 4; s++ {
			bs.SeatKongScores[s] += deltas[s]
		}
		ex.KongHistory = append(ex.KongHistory, MahjongKongRecord{
			GangType: gangType,
			Seat:     seat,
			FromSeat: fromSeatPtr,
			Tile:     targetTile,
			Deltas:   deltas,
		})

		bs.TurnSeat = seat
		room.TurnSeat = seat
		ex.PendingPengSeat = &sCopy
		room.MoveCount++
		bs.ActionSeq++
		bs.MoveCount = room.MoveCount
		bs.UpdatedAt = nowMs()
		room.LastTurnTs = nowMs()

		ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
			var maskedSupp interface{} = suppTile
			if rSeat != seat {
				maskedSupp = maskHiddenMahjongTile(&suppTile)
			}
			evt := map[string]interface{}{
				"action":         "gang",
				"seat":           seat,
				"gangType":       gangType,
				"tile":           targetTile,
				"meldTiles":      meldTiles,
				"suppTile":       maskedSupp,
				"wallRemaining":  ex.WallRemaining,
				"kongDeltas":     deltas,
				"seatKongScores": bs.SeatKongScores,
			}
			if canceledSeat != nil {
				evt["canceledDrawSeat"] = *canceledSeat
				if canceledTile != nil {
					evt["canceledDrawTile"] = *canceledTile
				}
			}
			return map[string]interface{}{
				"type":       "game_event",
				"roomId":     room.ID,
				"seat":       seat,
				"event":      evt,
				"payload":    evt,
				"moveCount":  room.MoveCount,
				"boardState": sanitizeBoardStateForSeat(bs, "mahjong", rSeat),
			}
		})
		ctx.ScheduleSnapshot()

	case "pass":
		if ex.ClaimWindow == nil {
			return
		}
		already := false
		for _, ps := range ex.ClaimWindow.PassedSeats {
			if ps == seat {
				already = true
				break
			}
		}
		if !already {
			ex.ClaimWindow.PassedSeats = append(ex.ClaimWindow.PassedSeats, seat)
		}
		// Check if all pending seats passed
		passed := map[int]bool{}
		for _, ps := range ex.ClaimWindow.PassedSeats {
			passed[ps] = true
		}
		allPassed := true
		for _, ps := range ex.ClaimWindow.PendingSeats {
			if !passed[ps] {
				allPassed = false
				break
			}
		}
		if allPassed {
			discId := ex.ClaimWindow.DiscardID
			ex.ClaimWindow = nil
			bs.ActionSeq++
			bs.UpdatedAt = nowMs()
			room.LastTurnTs = nowMs()
			if room.Deal != nil && len(room.Deal.RemainingWall) == 0 {
				executeMahjongDrawStalemate(ctx, room, bs.TurnSeat, "牌墙抓毕，本局荒庄（流局）！")
				return
			}
			cr := map[string]interface{}{
				"action":        "claim_resolved",
				"discardId":     discId,
				"nextTurn":      bs.TurnSeat,
				"wallRemaining": ex.WallRemaining,
			}
			ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
				return map[string]interface{}{
					"type":       "game_event",
					"roomId":     room.ID,
					"seat":       bs.TurnSeat,
					"event":      cr,
					"payload":    cr,
					"moveCount":  room.MoveCount,
					"boardState": sanitizeBoardStateForSeat(bs, "mahjong", rSeat),
				}
			})
			ctx.ScheduleSnapshot()
		}

	case "hu":
		if room.Deal == nil || room.Deal.MahjongSeatCards == nil {
			return
		}
		hand := room.Deal.MahjongSeatCards[seat]
		seatMelds := ex.MahjongMelds[seat]
		expectedTotal := len(hand) + len(seatMelds)*3
		hzCnt := 0
		if isHzMode {
			for _, t := range hand {
				if isHongzhong(t) {
					hzCnt++
				}
			}
		}
		isFourHzAuth := isHzMode && hzCnt >= 4
		win := false
		if len(hand)%3 == 2 {
			win, _, _ = checkMahjongWin(hand, isHzMode)
		}
		if expectedTotal != 14 || len(hand)%3 != 2 || (!isFourHzAuth && !win) {
			sendMahjongReject(ctx, sender, room, seat, "hu_rejected", "invalid_hu_hand")
			return
		}
		if bs.TurnSeat != seat && (ex.PendingPengSeat == nil || *ex.PendingPengSeat != seat) {
			sendMahjongReject(ctx, sender, room, seat, "hu_rejected", "not_your_turn")
			return
		}
		room.MoveCount++
		bs.ActionSeq++
		bs.MoveCount = room.MoveCount
		bs.UpdatedAt = nowMs()
		executeMahjongWinSettlement(ctx, room, seat, isFourHzAuth, p)

	case "draw_stalemate":
		if room.Deal != nil && len(room.Deal.RemainingWall) > 0 {
			sendMahjongReject(ctx, sender, room, seat, "draw_rejected", "wall_not_empty")
			return
		}
		executeMahjongDrawStalemate(ctx, room, seat, "牌墙抓毕，本局荒庄（流局）！")
	}
}

func tickMahjongClaimWindow(ctx *HubContext, room *RoomState, now int64) bool {
	if room == nil || room.GameType != "mahjong" || room.BoardState == nil || room.BoardState.ExtraState == nil {
		return false
	}
	cw := room.BoardState.ExtraState.ClaimWindow
	if cw == nil {
		return false
	}
	if now-cw.CreatedAt < 10500 {
		return true
	}
	discId := cw.DiscardID
	room.BoardState.ExtraState.ClaimWindow = nil
	room.BoardState.ActionSeq++
	room.BoardState.UpdatedAt = now
	room.LastTurnTs = now
	if room.Deal != nil && len(room.Deal.RemainingWall) == 0 {
		executeMahjongDrawStalemate(ctx, room, room.BoardState.TurnSeat, "牌墙抓毕，本局荒庄（流局）！")
		return true
	}
	cr := map[string]interface{}{
		"action":        "claim_resolved",
		"discardId":     discId,
		"nextTurn":      room.BoardState.TurnSeat,
		"wallRemaining": room.BoardState.ExtraState.WallRemaining,
	}
	ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
		return map[string]interface{}{
			"type":       "game_event",
			"roomId":     room.ID,
			"seat":       room.BoardState.TurnSeat,
			"event":      cr,
			"payload":    cr,
			"moveCount":  room.MoveCount,
			"boardState": sanitizeBoardStateForSeat(room.BoardState, "mahjong", rSeat),
		}
	})
	return true
}

func pickMahjongAutoDiscard(hand []MahjongTile, isHzMode bool) MahjongTile {
	counts := map[string]int{}
	for _, t := range hand {
		counts[mjTileKey(t.Type, t.Value)]++
	}
	bestTile := hand[len(hand)-1]
	bestScore := 99999
	for _, t := range hand {
		if isHzMode && isHongzhong(t) {
			continue
		}
		sc := 0
		k := mjTileKey(t.Type, t.Value)
		sc += (counts[k] - 1) * 30
		if t.Type != "zi" {
			if counts[mjTileKey(t.Type, t.Value-1)] > 0 {
				sc += 20
			}
			if counts[mjTileKey(t.Type, t.Value+1)] > 0 {
				sc += 20
			}
			if counts[mjTileKey(t.Type, t.Value-2)] > 0 {
				sc += 8
			}
			if counts[mjTileKey(t.Type, t.Value+2)] > 0 {
				sc += 8
			}
		} else {
			sc -= 10
		}
		if sc < bestScore {
			bestScore = sc
			bestTile = t
		}
	}
	return bestTile
}

func tickMahjongAutoTurn(ctx *HubContext, room *RoomState, currentSeat int, now int64) {
	if room == nil || room.BoardState == nil || room.Deal == nil || room.Deal.MahjongSeatCards == nil {
		return
	}
	bs := room.BoardState
	if bs.ExtraState != nil && bs.ExtraState.ClaimWindow != nil {
		return
	}
	hand := room.Deal.MahjongSeatCards[currentSeat]
	isHzMode := room.SceneMode == "" || strings.Contains(room.SceneMode, "hongzhong")

	if len(hand)%3 == 1 {
		if len(room.Deal.RemainingWall) == 0 {
			executeMahjongDrawStalemate(ctx, room, currentSeat, "牌墙已摸空，荒牌流局！")
			return
		}
		handleMahjongGameMove(ctx, nil, room, currentSeat, "draw", map[string]interface{}{"action": "draw", "seat": currentSeat})
		hand = room.Deal.MahjongSeatCards[currentSeat]
	}

	if win, _, _ := checkMahjongWin(hand, isHzMode); win {
		hzCnt := 0
		for _, t := range hand {
			if isHongzhong(t) {
				hzCnt++
			}
		}
		executeMahjongWinSettlement(ctx, room, currentSeat, isHzMode && hzCnt >= 4, map[string]interface{}{"action": "hu", "seat": currentSeat})
		return
	}

	if len(hand)%3 == 2 {
		disc := pickMahjongAutoDiscard(hand, isHzMode)
		handleMahjongGameMove(ctx, nil, room, currentSeat, "discard", map[string]interface{}{
			"action":      "discard",
			"seat":        currentSeat,
			"tile":        disc,
			"autoDiscard": true,
		})
	}
	_ = now
}
