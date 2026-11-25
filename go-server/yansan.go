package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"sort"
)

// ==============================================================================
// Yansan (经典烟三) Authoritative Rules, Smart AI & Server Engine
// ==============================================================================

type YansanWireCombo struct {
	Type           string `json:"type"`
	ComboTier      int    `json:"comboTier,omitempty"`
	Rank           int    `json:"rank"`
	SuitVal        int    `json:"suitVal,omitempty"`
	Length         int    `json:"length,omitempty"`
	Cards          []Card `json:"cards"`
	Seat           *int   `json:"seat,omitempty"`
	Label          string `json:"label,omitempty"`
	PrimaryRank    int    `json:"primaryRank,omitempty"`
	PrimarySuitVal int    `json:"primarySuitVal,omitempty"`
	FlushSuit      int    `json:"flushSuit,omitempty"`
}

func (c *YansanWireCombo) toYansanCombo() *YansanCombo {
	if c == nil {
		return nil
	}
	cardsCopy := make([]Card, len(c.Cards))
	copy(cardsCopy, c.Cards)
	return &YansanCombo{
		Type:      c.Type,
		ComboTier: c.ComboTier,
		Rank:      c.Rank,
		SuitVal:   c.SuitVal,
		Length:    c.Length,
		Cards:     cardsCopy,
		Seat:      c.Seat,
		Label:     c.Label,
	}
}

func wireFromYansanCombo(c *YansanCombo) *YansanWireCombo {
	if c == nil {
		return nil
	}
	w := &YansanWireCombo{
		Type:      c.Type,
		ComboTier: c.ComboTier,
		Rank:      c.Rank,
		SuitVal:   c.SuitVal,
		Length:    c.Length,
		Cards:     c.Cards,
		Seat:      c.Seat,
		Label:     c.Label,
	}
	w.PrimaryRank = getYansanRankWeight(c.Rank)
	w.PrimarySuitVal = c.SuitVal
	if c.Type == "5_card" && (c.ComboTier == 2 || c.ComboTier == 5) {
		w.FlushSuit = c.SuitVal
	}
	return w
}

func getYansanComboFromRaw(raw json.RawMessage) *YansanWireCombo {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var wc YansanWireCombo
	if err := json.Unmarshal(raw, &wc); err != nil || wc.Type == "" {
		return nil
	}
	return &wc
}

func setLastComboRaw(bs *AuthoritativeBoardState, c *YansanWireCombo) {
	if bs == nil {
		return
	}
	if c == nil {
		bs.LastCombo = json.RawMessage("null")
		return
	}
	b, err := json.Marshal(c)
	if err == nil {
		bs.LastCombo = b
	}
}

func getYansanRankLabel(rank int) string {
	switch rank {
	case 15:
		return "2"
	case 14:
		return "A"
	case 13:
		return "K"
	case 12:
		return "Q"
	case 11:
		return "J"
	default:
		return itoa(rank)
	}
}

func getYansanComboLabel(c *YansanWireCombo) string {
	if c == nil {
		return ""
	}
	rLabel := getYansanRankLabel(c.Rank)
	switch c.Type {
	case "single":
		suit := ""
		if len(c.Cards) > 0 {
			suit = c.Cards[0].Suit
		}
		return "单张 " + suit + rLabel
	case "pair":
		return "对" + rLabel
	case "triplet":
		return "三条 " + rLabel
	case "5_card":
		switch c.ComboTier {
		case 5:
			suit := ""
			if len(c.Cards) > 0 {
				suit = c.Cards[0].Suit
			}
			return "同花顺 (" + suit + ")"
		case 4:
			return "四带一 (" + rLabel + ")"
		case 3:
			return "俘虏/葫芦 (" + rLabel + ")"
		case 2:
			suit := ""
			if len(c.Cards) > 0 {
				suit = c.Cards[0].Suit
			}
			return "同花 (" + suit + rLabel + ")"
		default:
			return "顺子 (至" + rLabel + ")"
		}
	}
	if c.Label != "" {
		return c.Label
	}
	return "组合牌"
}

func getYansanRankWeight(rank int) int {
	switch rank {
	case 3:
		return 13
	case 15:
		return 12
	case 14:
		return 11
	case 13:
		return 10
	case 12:
		return 9
	case 11:
		return 8
	case 10:
		return 7
	case 9:
		return 6
	case 8:
		return 5
	case 7:
		return 4
	case 6:
		return 3
	case 5:
		return 2
	case 4:
		return 1
	}
	return 0
}

func getYansanCardPower(c Card) int {
	return getYansanRankWeight(c.Rank)*10 + c.SuitVal
}

func sortYansanCardsDesc(cards []Card) []Card {
	out := make([]Card, len(cards))
	copy(out, cards)
	sort.Slice(out, func(i, j int) bool {
		return getYansanCardPower(out[i]) > getYansanCardPower(out[j])
	})
	return out
}

func createYansanDeal(sceneMode string) *RoomDeal {
	suits := []struct {
		sym string
		val int
		red bool
	}{
		{"♠", 4, false},
		{"♥", 3, true},
		{"♣", 2, false},
		{"♦", 1, true},
	}
	ranks := []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	deck := make([]Card, 0, 52)
	for _, s := range suits {
		for _, r := range ranks {
			lbl := getYansanRankLabel(r)
			deck = append(deck, Card{
				ID:      lbl + "-" + s.sym,
				Rank:    r,
				SuitVal: s.val,
				Label:   lbl,
				Suit:    s.sym,
				IsRed:   s.red,
			})
		}
	}
	// Fisher-Yates shuffle
	for i := len(deck) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}

	seatCards := map[int][]Card{
		0: {}, 1: {}, 2: {}, 3: {},
	}
	for idx, c := range deck {
		s := idx % 4
		seatCards[s] = append(seatCards[s], c)
	}
	starterSeat := 0
	spade3Seat := 0
	spadeASeat := 0
	initSeats := make([][]Card, 4)
	for s := 0; s < 4; s++ {
		sorted := sortYansanCardsDesc(seatCards[s])
		seatCards[s] = sorted
		cp := make([]Card, len(sorted))
		copy(cp, sorted)
		initSeats[s] = cp
		for _, c := range sorted {
			if c.Rank == 4 && c.Suit == "♦" {
				starterSeat = s
			}
			if c.Rank == 3 && c.Suit == "♠" {
				spade3Seat = s
			}
			if c.Rank == 14 && c.Suit == "♠" {
				spadeASeat = s
			}
		}
	}

	return &RoomDeal{
		GameType:         "yansan",
		SceneMode:        sceneMode,
		DealID:           "ys-" + itoa(int(nowMs()&0x7fffffff)) + "-" + randomHex(3),
		DeckCount:        1,
		StarterSeat:      starterSeat,
		Spade3Seat:       &spade3Seat,
		SpadeASeat:       &spadeASeat,
		Deck:             deck,
		SeatCards:        seatCards,
		InitialSeatCards: initSeats,
		CreatedAt:        nowMs(),
	}
}

func recognizeYansanCombo(cards []Card) *YansanWireCombo {
	if len(cards) == 0 {
		return nil
	}
	sorted := sortYansanCardsDesc(cards)
	count := len(sorted)
	rankCounts := map[int]int{}
	for _, c := range sorted {
		rankCounts[c.Rank]++
	}

	var result *YansanWireCombo
	if count == 1 {
		result = &YansanWireCombo{
			Type:    "single",
			Rank:    sorted[0].Rank,
			SuitVal: sorted[0].SuitVal,
			Length:  1,
			Cards:   sorted,
		}
	} else if count == 2 && len(rankCounts) == 1 {
		highSuit := sorted[0].SuitVal
		for _, c := range sorted {
			if c.SuitVal > highSuit {
				highSuit = c.SuitVal
			}
		}
		result = &YansanWireCombo{
			Type:    "pair",
			Rank:    sorted[0].Rank,
			SuitVal: highSuit,
			Length:  2,
			Cards:   sorted,
		}
	} else if count == 3 && len(rankCounts) == 1 {
		result = &YansanWireCombo{
			Type:   "triplet",
			Rank:   sorted[0].Rank,
			Length: 3,
			Cards:  sorted,
		}
	} else if count == 5 {
		isFlush := true
		for i := 1; i < 5; i++ {
			if sorted[i].Suit != sorted[0].Suit {
				isFlush = false
				break
			}
		}
		ranks := make([]int, 5)
		for i := 0; i < 5; i++ {
			ranks[i] = sorted[i].Rank
		}
		sort.Ints(ranks)

		isNormalSeq := true
		hasTwo := false
		for i := 0; i < 5; i++ {
			if ranks[i] == 15 {
				hasTwo = true
			}
			if i > 0 && ranks[i] != ranks[i-1]+1 {
				isNormalSeq = false
			}
		}
		if hasTwo {
			isNormalSeq = false
		}
		isA2345 := ranks[0] == 3 && ranks[1] == 4 && ranks[2] == 5 && ranks[3] == 14 && ranks[4] == 15
		is10JQKA := ranks[0] == 10 && ranks[1] == 11 && ranks[2] == 12 && ranks[3] == 13 && ranks[4] == 14
		isLegalStraight := (isNormalSeq || isA2345) && !is10JQKA

		fourRank := 0
		threeRank := 0
		for r, cnt := range rankCounts {
			if cnt == 4 {
				fourRank = r
			} else if cnt == 3 {
				threeRank = r
			}
		}

		if isFlush && isLegalStraight {
			result = &YansanWireCombo{
				Type:      "5_card",
				ComboTier: 5,
				Rank:      sorted[0].Rank,
				SuitVal:   sorted[0].SuitVal,
				Length:    5,
				Cards:     sorted,
				FlushSuit: sorted[0].SuitVal,
			}
		} else if fourRank > 0 {
			result = &YansanWireCombo{
				Type:      "5_card",
				ComboTier: 4,
				Rank:      fourRank,
				Length:    5,
				Cards:     sorted,
			}
		} else if threeRank > 0 && len(rankCounts) == 2 {
			result = &YansanWireCombo{
				Type:      "5_card",
				ComboTier: 3,
				Rank:      threeRank,
				Length:    5,
				Cards:     sorted,
			}
		} else if isFlush {
			result = &YansanWireCombo{
				Type:      "5_card",
				ComboTier: 2,
				Rank:      sorted[0].Rank,
				SuitVal:   sorted[0].SuitVal,
				Length:    5,
				Cards:     sorted,
				FlushSuit: sorted[0].SuitVal,
			}
		} else if isLegalStraight {
			result = &YansanWireCombo{
				Type:      "5_card",
				ComboTier: 1,
				Rank:      sorted[0].Rank,
				SuitVal:   sorted[0].SuitVal,
				Length:    5,
				Cards:     sorted,
			}
		}
	}

	if result != nil {
		result.PrimaryRank = getYansanRankWeight(result.Rank)
		result.PrimarySuitVal = result.SuitVal
		result.Label = getYansanComboLabel(result)
	}
	return result
}

func canBeatYansanCombo(candidate, target *YansanWireCombo) bool {
	if candidate == nil {
		return false
	}
	if target == nil {
		return true
	}
	if candidate.Type != target.Type {
		return false
	}
	if candidate.Type == "single" || candidate.Type == "pair" {
		cw := getYansanRankWeight(candidate.Rank)
		tw := getYansanRankWeight(target.Rank)
		if cw != tw {
			return cw > tw
		}
		return candidate.SuitVal > target.SuitVal
	}
	if candidate.Type == "triplet" {
		return getYansanRankWeight(candidate.Rank) > getYansanRankWeight(target.Rank)
	}
	if candidate.Type == "5_card" {
		ct := candidate.ComboTier
		if ct == 0 {
			ct = 1
		}
		tt := target.ComboTier
		if tt == 0 {
			tt = 1
		}
		if ct != tt {
			return ct > tt
		}
		if ct == 5 || ct == 2 {
			cs := candidate.SuitVal
			ts := target.SuitVal
			if cs != ts {
				return cs > ts
			}
			return getYansanRankWeight(candidate.Rank) > getYansanRankWeight(target.Rank)
		}
		if ct == 4 || ct == 3 {
			return getYansanRankWeight(candidate.Rank) > getYansanRankWeight(target.Rank)
		}
		cw := getYansanRankWeight(candidate.Rank)
		tw := getYansanRankWeight(target.Rank)
		if cw != tw {
			return cw > tw
		}
		return candidate.SuitVal > target.SuitVal
	}
	return false
}

func buildFullYansanDeckMap(playedCards []Card, handCards []Card) []Card {
	used := map[string]bool{}
	for _, c := range playedCards {
		used[c.ID] = true
	}
	for _, c := range handCards {
		used[c.ID] = true
	}
	suits := []struct {
		sym string
		val int
		red bool
	}{
		{"♠", 4, false}, {"♥", 3, true}, {"♣", 2, false}, {"♦", 1, true},
	}
	rem := make([]Card, 0, 52)
	for _, s := range suits {
		for r := 3; r <= 15; r++ {
			lbl := getYansanRankLabel(r)
			id := lbl + "-" + s.sym
			if !used[id] {
				rem = append(rem, Card{
					ID: id, Rank: r, SuitVal: s.val, Label: lbl, Suit: s.sym, IsRed: s.red,
				})
			}
		}
	}
	return rem
}

func isYansanUnbeatableCombo(combo *YansanWireCombo, playedCards []Card, handCards []Card) bool {
	if combo == nil || len(combo.Cards) == 0 {
		return false
	}
	allUnseen := buildFullYansanDeckMap(playedCards, handCards)
	switch combo.Type {
	case "single":
		myPow := getYansanCardPower(combo.Cards[0])
		for _, c := range allUnseen {
			if getYansanCardPower(c) > myPow {
				return false
			}
		}
		return true
	case "pair":
		byRank := map[int][]Card{}
		for _, c := range allUnseen {
			byRank[c.Rank] = append(byRank[c.Rank], c)
		}
		for rk, list := range byRank {
			if len(list) < 2 {
				continue
			}
			maxSuit := 0
			for _, c := range list {
				if c.SuitVal > maxSuit {
					maxSuit = c.SuitVal
				}
			}
			cand := &YansanWireCombo{Type: "pair", Rank: rk, SuitVal: maxSuit}
			if canBeatYansanCombo(cand, combo) {
				return false
			}
		}
		return true
	case "triplet":
		byRank := map[int]int{}
		for _, c := range allUnseen {
			byRank[c.Rank]++
		}
		for rk, cnt := range byRank {
			if cnt >= 3 {
				cand := &YansanWireCombo{Type: "triplet", Rank: rk}
				if canBeatYansanCombo(cand, combo) {
					return false
				}
			}
		}
		return true
	case "5_card":
		if len(allUnseen) < 5 {
			return true
		}
		// Check higher combo tiers
		// Check Tier 5 straight flush in unseen cards
		bySuit := map[string][]Card{"♠": {}, "♥": {}, "♣": {}, "♦": {}}
		byRank := map[int][]Card{}
		for _, c := range allUnseen {
			bySuit[c.Suit] = append(bySuit[c.Suit], c)
			byRank[c.Rank] = append(byRank[c.Rank], c)
		}
		// Any straight flush possible?
		for _, list := range bySuit {
			if len(list) < 5 {
				continue
			}
			rankSet := map[int]Card{}
			for _, c := range list {
				rankSet[c.Rank] = c
			}
			// Check A2345 (3,4,5,14,15)
			if c3, ok3 := rankSet[3]; ok3 {
				if c4, ok4 := rankSet[4]; ok4 {
					if c5, ok5 := rankSet[5]; ok5 {
						if cA, okA := rankSet[14]; okA {
							if c2, ok2 := rankSet[15]; ok2 {
								sf := recognizeYansanCombo([]Card{c3, c4, c5, cA, c2})
								if canBeatYansanCombo(sf, combo) {
									return false
								}
							}
						}
					}
				}
			}
			for start := 3; start <= 9; start++ {
				cards5 := make([]Card, 0, 5)
				ok := true
				for k := 0; k < 5; k++ {
					if c, exists := rankSet[start+k]; exists {
						cards5 = append(cards5, c)
					} else {
						ok = false
						break
					}
				}
				if ok {
					sf := recognizeYansanCombo(cards5)
					if canBeatYansanCombo(sf, combo) {
						return false
					}
				}
			}
		}
		if combo.ComboTier >= 5 {
			return true
		}
		// Check Tier 4 four-with-one
		for rk, list := range byRank {
			if len(list) == 4 && len(allUnseen) >= 5 {
				cand := &YansanWireCombo{Type: "5_card", ComboTier: 4, Rank: rk}
				if canBeatYansanCombo(cand, combo) {
					return false
				}
			}
		}
		if combo.ComboTier >= 4 {
			return true
		}
		// Check Tier 3 full house
		var triRanks, pairRanks []int
		for rk, list := range byRank {
			if len(list) >= 3 {
				triRanks = append(triRanks, rk)
			}
			if len(list) >= 2 {
				pairRanks = append(pairRanks, rk)
			}
		}
		for _, tr := range triRanks {
			for _, pr := range pairRanks {
				if pr == tr {
					continue
				}
				cand := &YansanWireCombo{Type: "5_card", ComboTier: 3, Rank: tr}
				if canBeatYansanCombo(cand, combo) {
					return false
				}
			}
		}
		if combo.ComboTier >= 3 {
			return true
		}
		// Check Tier 2 flush
		for _, list := range bySuit {
			if len(list) >= 5 {
				sortedList := sortYansanCardsDesc(list)
				cand := recognizeYansanCombo(sortedList[:5])
				if cand != nil {
					cand.ComboTier = 2
					if canBeatYansanCombo(cand, combo) {
						return false
					}
				}
			}
		}
		if combo.ComboTier >= 2 {
			return true
		}
		// Check Tier 1 straight
		if _, ok3 := byRank[3]; ok3 {
			if _, ok4 := byRank[4]; ok4 {
				if _, ok5 := byRank[5]; ok5 {
					if _, okA := byRank[14]; okA {
						if _, ok2 := byRank[15]; ok2 {
							maxPowCard := byRank[3][0]
							for _, c := range byRank[3] {
								if c.SuitVal > maxPowCard.SuitVal {
									maxPowCard = c
								}
							}
							cand := &YansanWireCombo{Type: "5_card", ComboTier: 1, Rank: maxPowCard.Rank, SuitVal: maxPowCard.SuitVal}
							if canBeatYansanCombo(cand, combo) {
								return false
							}
						}
					}
				}
			}
		}
		for start := 3; start <= 9; start++ {
			ok := true
			for k := 0; k < 5; k++ {
				if len(byRank[start+k]) == 0 {
					ok = false
					break
				}
			}
			if ok {
				topList := byRank[start+4]
				best := topList[0]
				for _, c := range topList {
					if c.SuitVal > best.SuitVal {
						best = c
					}
				}
				cand := &YansanWireCombo{Type: "5_card", ComboTier: 1, Rank: best.Rank, SuitVal: best.SuitVal}
				if canBeatYansanCombo(cand, combo) {
					return false
				}
			}
		}
		return true
	}
	return false
}

func validateYansanComboStrict(cards []Card) (bool, *YansanWireCombo, string) {
	if len(cards) == 0 {
		return false, nil, "请至少选择一张牌"
	}
	if len(cards) == 4 || len(cards) > 5 {
		return false, nil, "烟三不支持4张或超过5张的牌型（仅支持单张、对子、三条、五张组合）"
	}
	c := recognizeYansanCombo(cards)
	if c == nil {
		return false, nil, "所选牌不符合烟三合法牌型规则（顺子不可含10JQKA）"
	}
	return true, c, ""
}

func getYansanBaseStake(sceneMode string) int {
	switch sceneMode {
	case "yansan-20-40":
		return 20
	case "yansan-10-20":
		return 10
	default:
		return 5
	}
}

func calculateYansanSettlement(winnerSeat, spade3Seat, spadeASeat int, isQiangGong bool, qiangGongSeat *int, sceneMode string, finishedSeats []int) *YansanSettlement {
	lowStake := getYansanBaseStake(sceneMode)
	highStake := lowStake * 2
	seatNames := []string{"本家", "西席", "北席", "东席"}
	var scores [4]int
	var winnerAlliance string
	var summary string
	isTie := false
	var soloSeatPtr *int
	resolvedWinner := winnerSeat

	if isQiangGong && qiangGongSeat != nil {
		qg := *qiangGongSeat
		s := qg
		soloSeatPtr = &s
		eachStake := highStake * 2
		totalStake := eachStake * 3
		isSoloWin := winnerSeat == qg
		if isSoloWin {
			winnerAlliance = seatNames[qg] + " (强攻独胜)"
			for i := 0; i < 4; i++ {
				if i == qg {
					scores[i] = totalStake
				} else {
					scores[i] = -eachStake
				}
			}
			summary = seatNames[qg] + " 开启强攻率先出清，全胜翻倍！独赢 +" + itoa(totalStake) + " 分，其余三家各输 -" + itoa(eachStake) + " 分"
		} else {
			winnerAlliance = seatNames[winnerSeat] + " (防守同盟)"
			for i := 0; i < 4; i++ {
				if i == qg {
					scores[i] = -totalStake
				} else {
					scores[i] = eachStake
				}
			}
			summary = "强攻被破！" + seatNames[winnerSeat] + " 率先出清，强攻者 " + seatNames[qg] + " 翻倍全输 -" + itoa(totalStake) + " 分，其余三家各得 +" + itoa(eachStake) + " 分"
		}
	} else if spade3Seat != spadeASeat {
		isSpade := func(s int) bool {
			return s == spade3Seat || s == spadeASeat
		}
		p1 := winnerSeat
		p2 := -1
		p3 := -1
		if len(finishedSeats) > 0 {
			p1 = finishedSeats[0]
		}
		if len(finishedSeats) > 1 {
			p2 = finishedSeats[1]
		}
		if len(finishedSeats) > 2 {
			p3 = finishedSeats[2]
		}
		spadeWinPoints := 0
		if p2 != -1 && isSpade(p1) == isSpade(p2) {
			if isSpade(p1) && isSpade(p2) {
				spadeWinPoints = highStake
				winnerAlliance = "黑桃同盟 (♠3 & ♠A)"
				summary = "黑桃同盟 (♠3 & ♠A) 双跑全胜！包揽前两名率先出清，各得 +" + itoa(highStake) + " 分，平民各输 -" + itoa(highStake) + " 分"
			} else {
				spadeWinPoints = -highStake
				winnerAlliance = "平民同盟"
				summary = "平民同盟 双跑全胜！包揽前两名率先出清，各得 +" + itoa(highStake) + " 分，黑桃同盟各输 -" + itoa(highStake) + " 分"
			}
		} else if p3 != -1 {
			p1IsSpade := isSpade(p1)
			p3IsSpade := isSpade(p3)
			if p1IsSpade && p3IsSpade {
				spadeWinPoints = lowStake
				winnerAlliance = "黑桃同盟 (♠3 & ♠A)"
				summary = "黑桃同盟 (♠3 & ♠A) 单胜！夺得第一名及第三名出清，各得 +" + itoa(lowStake) + " 分，平民各输 -" + itoa(lowStake) + " 分"
			} else if !p1IsSpade && !p3IsSpade {
				spadeWinPoints = -lowStake
				winnerAlliance = "平民同盟"
				summary = "平民同盟 单胜！夺得第一名及第三名出清，各得 +" + itoa(lowStake) + " 分，黑桃同盟各输 -" + itoa(lowStake) + " 分"
			} else {
				spadeWinPoints = 0
				winnerAlliance = "双方平局 (打和)"
				isTie = true
				resolvedWinner = -1
				summary = "势均力敌 · 本局打和！一方拿下头彩但另一方包揽二三名，双方势均力敌，大家都没有输赢 (各 0 分)"
			}
		} else {
			if isSpade(p1) {
				spadeWinPoints = lowStake
				winnerAlliance = "黑桃同盟 (♠3 & ♠A)"
				summary = "黑桃同盟率先出清，各得 +" + itoa(lowStake) + " 分，平民同盟各输 -" + itoa(lowStake) + " 分"
			} else {
				spadeWinPoints = -lowStake
				winnerAlliance = "平民同盟"
				summary = "平民同盟率先出清，各得 +" + itoa(lowStake) + " 分，黑桃同盟各输 -" + itoa(lowStake) + " 分"
			}
		}
		for i := 0; i < 4; i++ {
			if isSpade(i) {
				scores[i] = spadeWinPoints
			} else {
				scores[i] = -spadeWinPoints
			}
		}
	} else {
		solo := spade3Seat
		soloSeatPtr = &solo
		soloRank := -1
		for idx, fs := range finishedSeats {
			if fs == solo {
				soloRank = idx
				break
			}
		}
		var soloScore, eachFarmerScore int
		switch soloRank {
		case 0:
			soloScore = highStake * 3
			eachFarmerScore = -highStake
			winnerAlliance = seatNames[solo] + " (双煞全胜)"
			summary = seatNames[solo] + " (持双煞♠3♠A) 第一个打完手牌夺得头游，1打3全胜大捷！独赢 +" + itoa(soloScore) + " 分，其余三家各输 -" + itoa(highStake) + " 分"
			resolvedWinner = solo
		case 1:
			soloScore = lowStake * 3
			eachFarmerScore = -lowStake
			winnerAlliance = seatNames[solo] + " (双煞半胜)"
			summary = seatNames[solo] + " (双煞) 第二个打完手牌出清，1打3半赢！独赢 +" + itoa(soloScore) + " 分，其余三家各输 -" + itoa(lowStake) + " 分"
			if len(finishedSeats) > 0 {
				resolvedWinner = finishedSeats[0]
			}
		case 2:
			soloScore = -(lowStake * 3)
			eachFarmerScore = lowStake
			winnerAlliance = "平民同盟 (半胜)"
			summary = "前两名平民率先出清，" + seatNames[solo] + " (双煞) 第三个打完手牌出清半输，扣 -" + itoa(lowStake*3) + " 分，其余三家各得 +" + itoa(lowStake) + " 分"
			if len(finishedSeats) > 0 {
				resolvedWinner = finishedSeats[0]
			}
		default:
			soloScore = -(highStake * 3)
			eachFarmerScore = highStake
			winnerAlliance = "平民同盟 (全胜)"
			summary = "其他三名平民全部打完手牌包揽前三，只剩下双煞 " + seatNames[solo] + "，双煞全输！扣 -" + itoa(highStake*3) + " 分，其余三家各得 +" + itoa(highStake) + " 分"
			if len(finishedSeats) > 0 {
				resolvedWinner = finishedSeats[0]
			}
		}
		for i := 0; i < 4; i++ {
			if i == solo {
				scores[i] = soloScore
			} else {
				scores[i] = eachFarmerScore
			}
		}
	}

	return &YansanSettlement{
		WinnerSeat:     resolvedWinner,
		WinnerAlliance: winnerAlliance,
		IsQiangGong:    isQiangGong,
		QiangGongSeat:  qiangGongSeat,
		SeatScores:     scores,
		Summary:        summary,
		IsTie:          isTie,
		Spade3Seat:     spade3Seat,
		SpadeASeat:     spadeASeat,
		SoloSeat:       soloSeatPtr,
	}
}

func calculateYansanSurrenderSettlement(sceneMode string, surrenderSeat int) *YansanSettlement {
	eachStake := getYansanBaseStake(sceneMode)
	totalStake := eachStake * 3
	seatNames := []string{"本家", "西席", "北席", "东席"}
	var scores [4]int
	for i := 0; i < 4; i++ {
		if i == surrenderSeat {
			scores[i] = -totalStake
		} else {
			scores[i] = eachStake
		}
	}
	return &YansanSettlement{
		WinnerSeat:     (surrenderSeat + 1) % 4,
		WinnerAlliance: "胜方同盟 (对手投降)",
		IsQiangGong:    false,
		QiangGongSeat:  nil,
		SeatScores:     scores,
		Summary:        seatNames[surrenderSeat] + " 在起手手牌未动时选择投降认输！输给其他玩家各 " + itoa(eachStake) + " 分，共输 -" + itoa(totalStake) + " 分，其余三家各得 +" + itoa(eachStake) + " 分",
	}
}

// ==============================================================================
// Smart AI Solver & Alliance Inference
// ==============================================================================

func getYansanAllCombosFromHand(hand []Card) []*YansanWireCombo {
	sorted := sortYansanCardsDesc(hand)
	var out []*YansanWireCombo

	// Singles (ascending power)
	for i := len(sorted) - 1; i >= 0; i-- {
		if c := recognizeYansanCombo([]Card{sorted[i]}); c != nil {
			out = append(out, c)
		}
	}
	// Pairs & Triplets
	byRank := map[int][]Card{}
	for _, c := range sorted {
		byRank[c.Rank] = append(byRank[c.Rank], c)
	}
	var ranksAsc []int
	for r := range byRank {
		ranksAsc = append(ranksAsc, r)
	}
	sort.Slice(ranksAsc, func(i, j int) bool {
		return getYansanRankWeight(ranksAsc[i]) < getYansanRankWeight(ranksAsc[j])
	})

	for _, r := range ranksAsc {
		list := byRank[r]
		if len(list) >= 2 {
			// Weakest pair first (smallest suits)
			n := len(list)
			if c := recognizeYansanCombo([]Card{list[n-2], list[n-1]}); c != nil {
				out = append(out, c)
			}
			// Strongest pair too if > 2 cards
			if n > 2 {
				if c := recognizeYansanCombo([]Card{list[0], list[1]}); c != nil {
					out = append(out, c)
				}
			}
		}
		if len(list) >= 3 {
			n := len(list)
			if c := recognizeYansanCombo([]Card{list[n-3], list[n-2], list[n-1]}); c != nil {
				out = append(out, c)
			}
		}
	}

	// 5-card straights
	// A2345
	if len(byRank[3]) > 0 && len(byRank[4]) > 0 && len(byRank[5]) > 0 && len(byRank[14]) > 0 && len(byRank[15]) > 0 {
		if c := recognizeYansanCombo([]Card{byRank[3][0], byRank[4][0], byRank[5][0], byRank[14][0], byRank[15][0]}); c != nil {
			out = append(out, c)
		}
	}
	for start := 3; start <= 9; start++ {
		if len(byRank[start]) > 0 && len(byRank[start+1]) > 0 && len(byRank[start+2]) > 0 && len(byRank[start+3]) > 0 && len(byRank[start+4]) > 0 {
			c5 := []Card{
				byRank[start][len(byRank[start])-1],
				byRank[start+1][len(byRank[start+1])-1],
				byRank[start+2][len(byRank[start+2])-1],
				byRank[start+3][len(byRank[start+3])-1],
				byRank[start+4][len(byRank[start+4])-1],
			}
			if c := recognizeYansanCombo(c5); c != nil {
				out = append(out, c)
			}
		}
	}
	// Flushes
	bySuit := map[string][]Card{}
	for _, c := range sorted {
		bySuit[c.Suit] = append(bySuit[c.Suit], c)
	}
	for _, list := range bySuit {
		if len(list) >= 5 {
			n := len(list)
			// Smallest 5
			if c := recognizeYansanCombo(list[n-5 : n]); c != nil {
				out = append(out, c)
			}
			// Largest 5
			if n > 5 {
				if c := recognizeYansanCombo(list[:5]); c != nil {
					out = append(out, c)
				}
			}
		}
	}
	// Full houses
	for _, r3 := range ranksAsc {
		if len(byRank[r3]) < 3 {
			continue
		}
		for _, r2 := range ranksAsc {
			if r2 == r3 || len(byRank[r2]) < 2 {
				continue
			}
			l3 := byRank[r3]
			l2 := byRank[r2]
			c5 := []Card{l3[len(l3)-3], l3[len(l3)-2], l3[len(l3)-1], l2[len(l2)-2], l2[len(l2)-1]}
			if c := recognizeYansanCombo(c5); c != nil {
				out = append(out, c)
			}
		}
	}
	// Four with one
	for _, r4 := range ranksAsc {
		if len(byRank[r4]) != 4 {
			continue
		}
		for _, rk := range ranksAsc {
			if rk == r4 || len(byRank[rk]) == 0 {
				continue
			}
			lk := byRank[rk]
			c5 := append(append([]Card{}, byRank[r4]...), lk[len(lk)-1])
			if c := recognizeYansanCombo(c5); c != nil {
				out = append(out, c)
			}
			break
		}
	}
	return out
}

func getYansanHintCombos(hand []Card, target *YansanWireCombo, isFirstPlay bool, hasDiamond4 bool) []*YansanWireCombo {
	all := getYansanAllCombosFromHand(hand)
	var valid []*YansanWireCombo
	for _, cand := range all {
		if isFirstPlay && hasDiamond4 {
			hasD4 := false
			for _, card := range cand.Cards {
				if card.Rank == 4 && card.Suit == "♦" {
					hasD4 = true
					break
				}
			}
			if !hasD4 {
				continue
			}
		}
		if canBeatYansanCombo(cand, target) {
			valid = append(valid, cand)
		}
	}
	return valid
}

func removeCardsFromHand(hand []Card, remove []Card) []Card {
	rm := map[string]bool{}
	for _, c := range remove {
		rm[c.ID] = true
	}
	out := make([]Card, 0, len(hand))
	for _, c := range hand {
		if !rm[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

func findYansanWinningSequence(hand []Card, target *YansanWireCombo, playedCards []Card, isFirstPlay bool, hasD4 bool, depth int) []*YansanWireCombo {
	if len(hand) == 0 {
		return []*YansanWireCombo{}
	}
	if whole := recognizeYansanCombo(hand); whole != nil {
		if isFirstPlay && hasD4 {
			okD4 := false
			for _, c := range whole.Cards {
				if c.Rank == 4 && c.Suit == "♦" {
					okD4 = true
					break
				}
			}
			if !okD4 {
				goto TryMulti
			}
		}
		if canBeatYansanCombo(whole, target) {
			return []*YansanWireCombo{whole}
		}
	}
TryMulti:
	if depth <= 1 {
		return nil
	}
	cands := getYansanHintCombos(hand, target, isFirstPlay, hasD4)
	// Try biggest moves first
	for i := len(cands) - 1; i >= 0; i-- {
		c := cands[i]
		if !isYansanUnbeatableCombo(c, playedCards, hand) {
			continue
		}
		rem := removeCardsFromHand(hand, c.Cards)
		nextPlayed := append(append([]Card{}, playedCards...), c.Cards...)
		if sub := findYansanWinningSequence(rem, nil, nextPlayed, false, false, depth-1); sub != nil {
			return append([]*YansanWireCombo{c}, sub...)
		}
	}
	return nil
}

func inferYansanAllySeats(observerSeat int, hand []Card, deal *RoomDeal, bs *AuthoritativeBoardState) [4]float64 {
	var scores [4]float64
	scores[observerSeat] = 1.0
	if bs != nil && bs.ExtraState != nil && bs.ExtraState.IsQiangGongActive && bs.ExtraState.QiangGongSeat != nil {
		qg := *bs.ExtraState.QiangGongSeat
		for s := 0; s < 4; s++ {
			if s == observerSeat {
				continue
			}
			if observerSeat == qg {
				scores[s] = -1.0
			} else if s == qg {
				scores[s] = -1.0
			} else {
				scores[s] = 1.0
			}
		}
		return scores
	}

	// Check visible spade 3 / spade A in own hand or played cards
	spade3Known := -1
	spadeAKnown := -1
	for _, c := range hand {
		if c.Rank == 3 && c.Suit == "♠" {
			spade3Known = observerSeat
		}
		if c.Rank == 14 && c.Suit == "♠" {
			spadeAKnown = observerSeat
		}
	}
	if bs != nil && bs.ExtraState != nil {
		for s, list := range bs.ExtraState.SeatPlayedCards {
			if s < 0 || s >= 4 {
				continue
			}
			for _, c := range list {
				if c.Rank == 3 && c.Suit == "♠" {
					spade3Known = s
				}
				if c.Rank == 14 && c.Suit == "♠" {
					spadeAKnown = s
				}
			}
		}
		for _, rec := range bs.ExtraState.PlayHistory {
			if rec.IsPass && rec.TargetCombo != nil && rec.TargetCombo.Seat != nil {
				ts := *rec.TargetCombo.Seat
				if ts >= 0 && ts < 4 && ts != rec.Seat {
					scores[ts] += 0.12
				}
			} else if !rec.IsPass && rec.TargetCombo != nil && rec.TargetCombo.Seat != nil {
				ts := *rec.TargetCombo.Seat
				if ts >= 0 && ts < 4 && ts != rec.Seat {
					if rec.Seat == observerSeat {
						scores[ts] -= 0.15
					}
				}
			}
		}
	}
	if spade3Known >= 0 && spadeAKnown >= 0 {
		obsInSpade := observerSeat == spade3Known || observerSeat == spadeAKnown
		for s := 0; s < 4; s++ {
			if s == observerSeat {
				continue
			}
			sInSpade := s == spade3Known || s == spadeAKnown
			if obsInSpade == sInSpade {
				scores[s] = 1.0
			} else {
				scores[s] = -1.0
			}
		}
		return scores
	}
	if spade3Known == observerSeat && spadeAKnown >= 0 {
		scores[spadeAKnown] = 1.0
	} else if spadeAKnown == observerSeat && spade3Known >= 0 {
		scores[spade3Known] = 1.0
	}
	_ = deal
	return scores
}

func chooseYansanAISmartPlay(hand []Card, target *YansanWireCombo, isFirstPlay bool, seat int, room *RoomState) *YansanWireCombo {
	if len(hand) == 0 {
		return nil
	}
	hasD4 := false
	for _, c := range hand {
		if c.Rank == 4 && c.Suit == "♦" {
			hasD4 = true
			break
		}
	}
	var played []Card
	if room != nil && room.BoardState != nil && room.BoardState.ExtraState != nil {
		played = room.BoardState.ExtraState.PlayedCardsHistory
	}

	// 1. Guaranteed winning sequence in <=3 steps
	if seq := findYansanWinningSequence(hand, target, played, isFirstPlay, hasD4, 3); len(seq) > 0 {
		return seq[0]
	}

	cands := getYansanHintCombos(hand, target, isFirstPlay, hasD4)
	if len(cands) == 0 {
		return nil
	}

	// Alliance awareness
	allyScores := inferYansanAllySeats(seat, hand, room.Deal, room.BoardState)
	if target != nil && target.Seat != nil && *target.Seat != seat {
		ts := *target.Seat
		if ts >= 0 && ts < 4 {
			if allyScores[ts] >= 0.75 && len(hand) > 2 {
				// Yield to confirmed teammate unless teammate still has many cards and target is weak
				if target.Type == "5_card" || getYansanRankWeight(target.Rank) >= 9 {
					return nil
				}
			}
		}
	}

	// Check next player (counterclockwise) low card count (报单防线)
	nextSeat := (seat - 1 + 4) % 4
	nextCount := 13
	if room != nil && room.BoardState != nil && nextSeat < len(room.BoardState.HandsCount) {
		nextCount = room.BoardState.HandsCount[nextSeat]
	}
	nextIsEnemy := allyScores[nextSeat] < 0.3

	if target == nil {
		// Leading: prefer 5-card or triplet or pair before single; if enemy has 1 card, avoid small single
		sort.SliceStable(cands, func(i, j int) bool {
			li, lj := len(cands[i].Cards), len(cands[j].Cards)
			if li != lj {
				return li > lj
			}
			return getYansanRankWeight(cands[i].Rank) < getYansanRankWeight(cands[j].Rank)
		})
		if nextCount == 1 && nextIsEnemy {
			// Avoid small single if we have multi-card or pick highest single
			for _, c := range cands {
				if len(c.Cards) > 1 {
					return c
				}
			}
			// Must play single: pick strongest single!
			best := cands[0]
			for _, c := range cands {
				if getYansanCardPower(c.Cards[0]) > getYansanCardPower(best.Cards[0]) {
					best = c
				}
			}
			return best
		}
		return cands[0]
	}

	// Following: if enemy next has <= 2 cards and playing single, block with big single
	if nextCount == 1 && nextIsEnemy && target.Type == "single" {
		best := cands[len(cands)-1]
		for _, c := range cands {
			if getYansanCardPower(c.Cards[0]) > getYansanCardPower(best.Cards[0]) {
				best = c
			}
		}
		return best
	}

	// Otherwise play weakest valid response without breaking bigger combos if avoidable
	return cands[0]
}

// ==============================================================================
// Yansan Authoritative Server Move Handler & Auto-Turn
// ==============================================================================

func resolveYansanEffectiveTarget(bs *AuthoritativeBoardState, currentSeat int) *YansanWireCombo {
	if bs == nil {
		return nil
	}
	last := getYansanComboFromRaw(bs.LastCombo)
	if last == nil {
		return nil
	}
	if last.Seat != nil && *last.Seat == currentSeat {
		return nil
	}
	return last
}

func countActivePassesAndNextYansanSeat(currentSeat int, finishedSeats []int) int {
	fin := map[int]bool{}
	for _, s := range finishedSeats {
		fin[s] = true
	}
	for i := 1; i <= 4; i++ {
		cand := (currentSeat - i + 4) % 4
		if !fin[cand] {
			return cand
		}
	}
	return (currentSeat - 1 + 4) % 4
}

func settleYansanRoomMatch(ctx *HubContext, room *RoomState, winnerSeat int, isQiangGong bool, qgSeat *int) {
	if room == nil || room.BoardState == nil || room.Deal == nil {
		return
	}
	bs := room.BoardState
	s3 := 0
	sA := 0
	if room.Deal.Spade3Seat != nil {
		s3 = *room.Deal.Spade3Seat
	}
	if room.Deal.SpadeASeat != nil {
		sA = *room.Deal.SpadeASeat
	}
	firstFin := winnerSeat
	if len(bs.FinishedSeats) > 0 {
		firstFin = bs.FinishedSeats[0]
	}
	settle := calculateYansanSettlement(firstFin, s3, sA, isQiangGong, qgSeat, room.SceneMode, bs.FinishedSeats)

	wSeat := settle.WinnerSeat
	bs.WinnerSeat = &wSeat
	bs.Phase = "ended"
	bs.AllianceScoreResult = settle
	bs.Settlement = settle
	room.Status = "finished"
	if bs.ExtraState != nil {
		bs.ExtraState.IsQiangGongActive = isQiangGong
		bs.ExtraState.QiangGongSeat = qgSeat
	}

	var ratingDelta [4]int
	var coinsDelta [4]int
	for i := 0; i < 4; i++ {
		coinsDelta[i] = settle.SeatScores[i]
		ratingDelta[i] = int(math.Round(float64(settle.SeatScores[i]) * 0.8))
	}

	ctx.SettleRoomCoins(room, coinsDelta[:], "yansan", ratingDelta[:])
	ctx.ResetSceneRoomReady(room.ID)
}

func sendYansanReject(ctx *HubContext, sender *WSConn, room *RoomState, seat int, reason string) {
	if sender == nil || room == nil || room.BoardState == nil {
		return
	}
	bs := room.BoardState
	var seatHand []Card
	if room.Deal != nil && room.Deal.SeatCards != nil {
		seatHand = room.Deal.SeatCards[seat]
	}
	var lastComboVal interface{}
	if len(bs.LastCombo) > 0 && string(bs.LastCombo) != "null" {
		var tmp interface{}
		if json.Unmarshal(bs.LastCombo, &tmp) == nil {
			lastComboVal = tmp
		}
	}
	rejectEvt := map[string]interface{}{
		"action":    "yansan_move_rejected",
		"seat":      seat,
		"absSeat":   seat,
		"turnSeat":  bs.TurnSeat,
		"lastCombo": lastComboVal,
		"seatHand":  seatHand,
		"handsCount": bs.HandsCount,
		"finishedSeats": bs.FinishedSeats,
		"reason":    reason,
	}
	if bs.ExtraState != nil {
		rejectEvt["playedCardsHistory"] = bs.ExtraState.PlayedCardsHistory
		rejectEvt["seatPlayedCards"] = bs.ExtraState.SeatPlayedCards
		rejectEvt["isQiangGongActive"] = bs.ExtraState.IsQiangGongActive
		rejectEvt["qiangGongSeat"] = bs.ExtraState.QiangGongSeat
		rejectEvt["hasPassedSeats"] = bs.ExtraState.HasPassedSeats
		rejectEvt["consecutivePasses"] = bs.ExtraState.ConsecutivePasses
	}
	payload := map[string]interface{}{
		"type":       "game_event",
		"roomId":     room.ID,
		"seat":       seat,
		"event":      rejectEvt,
		"payload":    rejectEvt,
		"boardState": sanitizeBoardStateForSeat(bs, "yansan", seat),
	}
	if room.Deal != nil {
		payload["deal"] = sanitizeDealForSeat(room.Deal, seat)
	}
	ctx.SendToConn(sender, payload)
}

func handleYansanGameMove(ctx *HubContext, sender *WSConn, room *RoomState, seat int, action string, rawCombo json.RawMessage, payloadMap map[string]interface{}) {
	if room == nil || room.BoardState == nil {
		return
	}
	bs := room.BoardState
	if bs.ExtraState == nil {
		bs.ExtraState = &ExtraBoardState{
			PlayedCardsHistory: []Card{},
			SeatPlayedCards:    [][]Card{{}, {}, {}, {}},
			PlayHistory:        []YansanPlayRecord{},
			HasPassedSeats:     []bool{false, false, false, false},
		}
	}
	ex := bs.ExtraState
	for len(ex.HasPassedSeats) < 4 {
		ex.HasPassedSeats = append(ex.HasPassedSeats, false)
	}
	for len(ex.SeatPlayedCards) < 4 {
		ex.SeatPlayedCards = append(ex.SeatPlayedCards, []Card{})
	}
	for len(bs.HandsCount) < 4 {
		bs.HandsCount = append(bs.HandsCount, 13)
	}

	s3Seat := 0
	sASeat := 0
	if room.Deal != nil {
		if room.Deal.Spade3Seat != nil {
			s3Seat = *room.Deal.Spade3Seat
		}
		if room.Deal.SpadeASeat != nil {
			sASeat = *room.Deal.SpadeASeat
		}
	}

	hasCard := func(cards []Card, rank int, suit string) bool {
		for _, c := range cards {
			if c.Rank == rank && c.Suit == suit {
				return true
			}
		}
		return false
	}

	switch action {
	case "qianggong":
		var initHand []Card
		if room.Deal != nil {
			if seat >= 0 && seat < len(room.Deal.InitialSeatCards) && len(room.Deal.InitialSeatCards[seat]) > 0 {
				initHand = room.Deal.InitialSeatCards[seat]
			} else if room.Deal.SeatCards != nil {
				initHand = room.Deal.SeatCards[seat]
			}
		}
		if ex.IsQiangGongActive || ex.HasPassedSeats[seat] || !hasCard(initHand, 3, "♠") || !hasCard(initHand, 14, "♠") {
			sendYansanReject(ctx, sender, room, seat, "invalid_qianggong")
			return
		}
		ex.IsQiangGongActive = true
		sCopy := seat
		ex.QiangGongSeat = &sCopy
		d4Seat := 0
		if room.Deal != nil {
			d4Seat = room.Deal.StarterSeat
			if len(room.Deal.InitialSeatCards) == 4 {
				if room.Deal.SeatCards == nil {
					room.Deal.SeatCards = map[int][]Card{}
				}
				for s := 0; s < 4; s++ {
					cp := make([]Card, len(room.Deal.InitialSeatCards[s]))
					copy(cp, room.Deal.InitialSeatCards[s])
					room.Deal.SeatCards[s] = cp
					if hasCard(cp, 4, "♦") {
						d4Seat = s
					}
				}
			}
		}
		bs.HandsCount = []int{13, 13, 13, 13}
		bs.FinishedSeats = []int{}
		setLastComboRaw(bs, nil)
		bs.DiscardsHistory = []interface{}{}
		ex.PlayedCardsHistory = []Card{}
		ex.SeatPlayedCards = [][]Card{{}, {}, {}, {}}
		ex.PlayHistory = []YansanPlayRecord{}
		ex.ConsecutivePasses = 0
		ex.HasPassedSeats = []bool{false, false, false, false}
		bs.TurnSeat = d4Seat
		room.TurnSeat = d4Seat
		bs.Multiplier *= 2
		room.MoveCount++
		bs.MoveCount = room.MoveCount
		bs.ActionSeq++
		bs.UpdatedAt = nowMs()
		room.LastTurnTs = nowMs()

		evt := map[string]interface{}{
			"action":        "qianggong",
			"seat":          seat,
			"qiangGongSeat": seat,
			"starterSeat":   d4Seat,
			"nextTurn":      d4Seat,
			"multiplier":    bs.Multiplier,
		}
		ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
			return map[string]interface{}{
				"type":       "game_event",
				"roomId":     room.ID,
				"seat":       seat,
				"event":      evt,
				"payload":    evt,
				"moveCount":  room.MoveCount,
				"boardState": sanitizeBoardStateForSeat(bs, "yansan", rSeat),
			}
		})
		ctx.ScheduleSnapshot()
		return

	case "surrender":
		var hand []Card
		if room.Deal != nil && room.Deal.SeatCards != nil {
			hand = room.Deal.SeatCards[seat]
		}
		if ex.IsQiangGongActive || ex.HasPassedSeats[seat] || len(hand) != 13 || !hasCard(hand, 3, "♠") || !hasCard(hand, 14, "♠") {
			sendYansanReject(ctx, sender, room, seat, "invalid_surrender")
			return
		}
		stl := calculateYansanSurrenderSettlement(room.SceneMode, seat)
		wSeat := stl.WinnerSeat
		bs.WinnerSeat = &wSeat
		bs.Phase = "ended"
		room.Status = "finished"
		bs.AllianceScoreResult = stl
		bs.Settlement = stl
		room.MoveCount++
		bs.MoveCount = room.MoveCount
		bs.ActionSeq++
		bs.UpdatedAt = nowMs()

		var coinsDelta [4]int
		var ratingDelta [4]int
		for i := 0; i < 4; i++ {
			coinsDelta[i] = stl.SeatScores[i]
			ratingDelta[i] = int(math.Round(float64(stl.SeatScores[i]) * 0.8))
		}
		ctx.SettleRoomCoins(room, coinsDelta[:], "yansan", ratingDelta[:])
		ctx.ResetSceneRoomReady(room.ID)

		evt := map[string]interface{}{
			"action":               "surrender",
			"seat":                 seat,
			"winner":               stl.WinnerSeat,
			"finishedSeats":        []int{stl.WinnerSeat},
			"allianceScoreResult":  stl,
			"fromServerSettlement": true,
		}
		ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
			return map[string]interface{}{
				"type":       "game_event",
				"roomId":     room.ID,
				"seat":       seat,
				"event":      evt,
				"payload":    evt,
				"moveCount":  room.MoveCount,
				"boardState": sanitizeBoardStateForSeat(bs, "yansan", rSeat),
			}
		})
		ctx.ScheduleSnapshot()
		return

	case "pass":
		if bs.TurnSeat != seat {
			sendYansanReject(ctx, sender, room, seat, "not_your_turn")
			return
		}
		effTarget := resolveYansanEffectiveTarget(bs, seat)
		if effTarget == nil || len(ex.PlayHistory) == 0 {
			sendYansanReject(ctx, sender, room, seat, "cannot_pass_on_lead")
			return
		}
		ex.HasPassedSeats[seat] = true
		ex.ConsecutivePasses++
		ex.PlayHistory = append(ex.PlayHistory, YansanPlayRecord{
			Seat:        seat,
			Cards:       []Card{},
			Combo:       nil,
			TargetCombo: effTarget.toYansanCombo(),
			IsPass:      true,
			ActionSeq:   bs.ActionSeq + 1,
			Timestamp:   nowMs(),
		})
		if len(ex.PlayHistory) > 120 {
			ex.PlayHistory = ex.PlayHistory[len(ex.PlayHistory)-120:]
		}

		activeCount := 4 - len(bs.FinishedSeats)
		if activeCount < 1 {
			activeCount = 1
		}
		leaderFinished := false
		if effTarget.Seat != nil {
			for _, fs := range bs.FinishedSeats {
				if fs == *effTarget.Seat {
					leaderFinished = true
					break
				}
			}
		}
		passThreshold := activeCount - 1
		if leaderFinished {
			passThreshold = activeCount
		}
		if passThreshold < 1 {
			passThreshold = 1
		}

		nextSeat := countActivePassesAndNextYansanSeat(seat, bs.FinishedSeats)
		roundCleared := ex.ConsecutivePasses >= passThreshold || (effTarget.Seat != nil && *effTarget.Seat == nextSeat)
		if roundCleared {
			setLastComboRaw(bs, nil)
			ex.ConsecutivePasses = 0
		}

		room.MoveCount++
		bs.MoveCount = room.MoveCount
		bs.ActionSeq++
		bs.TurnSeat = nextSeat
		room.TurnSeat = nextSeat
		bs.UpdatedAt = nowMs()
		room.LastTurnTs = nowMs()

		evt := map[string]interface{}{
			"action":        "pass",
			"seat":          seat,
			"nextTurn":      nextSeat,
			"finishedSeats": bs.FinishedSeats,
			"roundCleared":  roundCleared,
		}
		ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
			return map[string]interface{}{
				"type":       "game_event",
				"roomId":     room.ID,
				"seat":       seat,
				"event":      evt,
				"payload":    evt,
				"moveCount":  room.MoveCount,
				"boardState": sanitizeBoardStateForSeat(bs, "yansan", rSeat),
			}
		})
		ctx.ScheduleSnapshot()
		return

	case "play", "finish":
		if bs.TurnSeat != seat {
			sendYansanReject(ctx, sender, room, seat, "not_your_turn")
			return
		}
		var playedCards []Card
		if wc := getYansanComboFromRaw(rawCombo); wc != nil && len(wc.Cards) > 0 {
			playedCards = wc.Cards
		} else if rawCards, ok := payloadMap["cards"]; ok {
			b, _ := json.Marshal(rawCards)
			_ = json.Unmarshal(b, &playedCards)
		}
		if len(playedCards) == 0 {
			sendYansanReject(ctx, sender, room, seat, "invalid_combo_or_hand")
			return
		}

		// Verify player actually holds these cards in Deal.SeatCards[seat]
		if room.Deal == nil || room.Deal.SeatCards == nil {
			sendYansanReject(ctx, sender, room, seat, "deal_not_ready")
			return
		}
		hand := room.Deal.SeatCards[seat]
		handByID := map[string]Card{}
		for _, hc := range hand {
			handByID[hc.ID] = hc
		}
		verifiedCards := make([]Card, 0, len(playedCards))
		seenID := map[string]bool{}
		for _, pc := range playedCards {
			hc, exists := handByID[pc.ID]
			if !exists || seenID[pc.ID] {
				sendYansanReject(ctx, sender, room, seat, "invalid_combo_or_hand")
				return
			}
			seenID[pc.ID] = true
			verifiedCards = append(verifiedCards, hc)
		}

		okCombo, validCombo, _ := validateYansanComboStrict(verifiedCards)
		if !okCombo || validCombo == nil {
			sendYansanReject(ctx, sender, room, seat, "invalid_combo_or_hand")
			return
		}

		// Check diamond-4 rule on very first move
		if len(ex.PlayedCardsHistory) == 0 && len(ex.PlayHistory) == 0 {
			if hasCard(hand, 4, "♦") && !hasCard(verifiedCards, 4, "♦") {
				sendYansanReject(ctx, sender, room, seat, "must_contain_diamond4")
				return
			}
		}

		effTarget := resolveYansanEffectiveTarget(bs, seat)
		if !canBeatYansanCombo(validCombo, effTarget) {
			sendYansanReject(ctx, sender, room, seat, "cannot_beat_target")
			return
		}

		// Remove played cards from seat hand
		newHand := removeCardsFromHand(hand, verifiedCards)
		room.Deal.SeatCards[seat] = newHand
		bs.HandsCount[seat] = len(newHand)

		sCopy := seat
		validCombo.Seat = &sCopy
		ex.PlayedCardsHistory = append(ex.PlayedCardsHistory, verifiedCards...)
		ex.SeatPlayedCards[seat] = append(ex.SeatPlayedCards[seat], verifiedCards...)
		ex.PlayHistory = append(ex.PlayHistory, YansanPlayRecord{
			Seat:        seat,
			Cards:       verifiedCards,
			Combo:       validCombo.toYansanCombo(),
			TargetCombo: effTarget.toYansanCombo(),
			IsPass:      false,
			ActionSeq:   bs.ActionSeq + 1,
			Timestamp:   nowMs(),
		})
		if len(ex.PlayHistory) > 120 {
			ex.PlayHistory = ex.PlayHistory[len(ex.PlayHistory)-120:]
		}
		ex.ConsecutivePasses = 0

		bs.DiscardsHistory = append(bs.DiscardsHistory, validCombo)
		if len(bs.DiscardsHistory) > 100 {
			bs.DiscardsHistory = bs.DiscardsHistory[len(bs.DiscardsHistory)-100:]
		}

		if len(newHand) == 0 {
			already := false
			for _, fs := range bs.FinishedSeats {
				if fs == seat {
					already = true
					break
				}
			}
			if !already {
				bs.FinishedSeats = append(bs.FinishedSeats, seat)
			}
		}

		finished := bs.FinishedSeats
		is2v2 := s3Seat != sASeat
		isSpadeTeam := func(s int) bool {
			return s == s3Seat || s == sASeat
		}
		shouldEnd := false
		if ex.IsQiangGongActive {
			shouldEnd = len(finished) >= 1
		} else if is2v2 {
			if len(finished) >= 2 && isSpadeTeam(finished[0]) == isSpadeTeam(finished[1]) {
				shouldEnd = true
			} else if len(finished) >= 3 {
				shouldEnd = true
			}
		} else {
			soloFinished := false
			for _, fs := range finished {
				if fs == s3Seat {
					soloFinished = true
					break
				}
			}
			if soloFinished || len(finished) >= 3 {
				shouldEnd = true
			}
		}

		room.MoveCount++
		bs.MoveCount = room.MoveCount
		bs.ActionSeq++
		bs.UpdatedAt = nowMs()
		room.LastTurnTs = nowMs()

		nextSeat := countActivePassesAndNextYansanSeat(seat, bs.FinishedSeats)
		resolvedNext := nextSeat
		unbeatableAutoClear := false

		if !shouldEnd {
			isUnbeatable := isYansanUnbeatableCombo(validCombo, ex.PlayedCardsHistory, newHand)
			if len(newHand) == 0 && (isUnbeatable || hasCard(verifiedCards, 3, "♠")) {
				setLastComboRaw(bs, nil)
				ex.ConsecutivePasses = 0
				resolvedNext = nextSeat
				unbeatableAutoClear = true
			} else if len(newHand) > 0 && isUnbeatable {
				setLastComboRaw(bs, nil)
				ex.ConsecutivePasses = 0
				resolvedNext = seat
				unbeatableAutoClear = true
			} else {
				setLastComboRaw(bs, validCombo)
			}
		} else {
			setLastComboRaw(bs, validCombo)
		}

		bs.TurnSeat = resolvedNext
		room.TurnSeat = resolvedNext

		playEvt := map[string]interface{}{
			"action":             "play",
			"seat":               seat,
			"combo":              validCombo,
			"authoritativeCombo": validCombo,
			"nextTurn":           resolvedNext,
			"finishedSeats":      bs.FinishedSeats,
			"unbeatable":         unbeatableAutoClear,
			"isAutoCleared":      unbeatableAutoClear,
		}
		ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
			return map[string]interface{}{
				"type":       "game_event",
				"roomId":     room.ID,
				"seat":       seat,
				"event":      playEvt,
				"payload":    playEvt,
				"moveCount":  room.MoveCount,
				"boardState": sanitizeBoardStateForSeat(bs, "yansan", rSeat),
			}
		})

		if shouldEnd {
			firstFin := seat
			if len(finished) > 0 {
				firstFin = finished[0]
			}
			settleYansanRoomMatch(ctx, room, firstFin, ex.IsQiangGongActive, ex.QiangGongSeat)
			finEvt := map[string]interface{}{
				"action":               "finish",
				"seat":                 firstFin,
				"winner":               bs.WinnerSeat,
				"finishedSeats":        bs.FinishedSeats,
				"spade3Seat":           s3Seat,
				"spadeASeat":           sASeat,
				"allianceScoreResult":  bs.AllianceScoreResult,
				"fromServerSettlement": true,
			}
			ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
				return map[string]interface{}{
					"type":       "game_event",
					"roomId":     room.ID,
					"seat":       firstFin,
					"event":      finEvt,
					"payload":    finEvt,
					"moveCount":  room.MoveCount,
					"boardState": sanitizeBoardStateForSeat(bs, "yansan", rSeat),
				}
			})
		}
		ctx.ScheduleSnapshot()
	}
}

func tickYansanAutoTurn(ctx *HubContext, room *RoomState, currentSeat int, now int64) {
	if room == nil || room.BoardState == nil || room.Deal == nil || room.Deal.SeatCards == nil {
		return
	}
	bs := room.BoardState
	hand := room.Deal.SeatCards[currentSeat]
	if len(hand) == 0 {
		return
	}
	isFirstPlay := room.MoveCount == 0 && (bs.ExtraState == nil || len(bs.ExtraState.PlayHistory) == 0)
	effTarget := resolveYansanEffectiveTarget(bs, currentSeat)
	chosen := chooseYansanAISmartPlay(hand, effTarget, isFirstPlay, currentSeat, room)
	if chosen == nil {
		if effTarget != nil {
			handleYansanGameMove(ctx, nil, room, currentSeat, "pass", nil, map[string]interface{}{"action": "pass", "seat": currentSeat})
			return
		}
		// Fallback: lead smallest single (or diamond-4 on turn 0)
		sorted := sortYansanCardsDesc(hand)
		pick := sorted[len(sorted)-1]
		if isFirstPlay {
			for _, c := range sorted {
				if c.Rank == 4 && c.Suit == "♦" {
					pick = c
					break
				}
			}
		}
		chosen = recognizeYansanCombo([]Card{pick})
	}
	if chosen != nil {
		b, _ := json.Marshal(chosen)
		action := "play"
		if len(chosen.Cards) == len(hand) {
			action = "finish"
		}
		handleYansanGameMove(ctx, nil, room, currentSeat, action, b, map[string]interface{}{
			"action": action,
			"seat":   currentSeat,
			"combo":  chosen,
		})
	}
	_ = now
}
