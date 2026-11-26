package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"sort"
)

// ==============================================================================
// Shisanshui (水镜十三水 / 十三张) Authoritative Rules, Solver & Server Engine
// ==============================================================================

const (
	sssMultCat  int64 = 10_000_000_000
	sssMultSuit int64 = 1_000_000_000
	sssMultR1   int64 = 100_000_000
	sssMultR2   int64 = 1_000_000
	sssMultK1   int64 = 10_000
	sssMultK2   int64 = 100
	sssMultK3   int64 = 10
)

type SSSEvaluatedTier struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	ScoreRank   int64  `json:"scoreRank"`
	Cards       []Card `json:"cards"`
	DetailLabel string `json:"detailLabel"`
}

type SSSLayout struct {
	Head        []Card          `json:"head"`
	Mid         []Card          `json:"mid"`
	Tail        []Card          `json:"tail"`
	SpecialHand *SSSSpecialHand `json:"specialHand,omitempty"`
}

type SSSMatchupSummary struct {
	AgainstSeat int      `json:"againstSeat"`
	AgainstName string   `json:"againstName"`
	NetWater    int      `json:"netWater"`
	IsShot      bool     `json:"isShot"`
	ShotBy      bool     `json:"shotBy"`
	Details     []string `json:"details"`
}

type SSSPlayerResult struct {
	Seat            int                 `json:"seat"`
	Name            string              `json:"name"`
	TotalWater      int                 `json:"totalWater"`
	ScoreDelta      int                 `json:"scoreDelta"`
	RatingDelta     int                 `json:"ratingDelta"`
	IsFoul          bool                `json:"isFoul"`
	IsSpecial       bool                `json:"isSpecial"`
	SpecialHand     *SSSSpecialHand     `json:"specialHand,omitempty"`
	IsGrandSlam     bool                `json:"isGrandSlam"`
	ShotCount       int                 `json:"shotCount"`
	HeadEval        *SSSEvaluatedTier   `json:"headEval,omitempty"`
	MidEval         *SSSEvaluatedTier   `json:"midEval,omitempty"`
	TailEval        *SSSEvaluatedTier   `json:"tailEval,omitempty"`
	Head            []Card              `json:"head"`
	Mid             []Card              `json:"mid"`
	Tail            []Card              `json:"tail"`
	Matchups        []SSSMatchupSummary `json:"matchups"`
	SummaryTags     []string            `json:"summaryTags"`
}

var (
	sssCombos13_5 [][5]int
	sssCombos8_5  [][5]int
)

func init() {
	for a := 0; a < 9; a++ {
		for b := a + 1; b < 10; b++ {
			for c := b + 1; c < 11; c++ {
				for d := c + 1; d < 12; d++ {
					for e := d + 1; e < 13; e++ {
						sssCombos13_5 = append(sssCombos13_5, [5]int{a, b, c, d, e})
					}
				}
			}
		}
	}
	for a := 0; a < 4; a++ {
		for b := a + 1; b < 5; b++ {
			for c := b + 1; c < 6; c++ {
				for d := c + 1; d < 7; d++ {
					for e := d + 1; e < 8; e++ {
						sssCombos8_5 = append(sssCombos8_5, [5]int{a, b, c, d, e})
					}
				}
			}
		}
	}
}

func sssRankChar(r int) string {
	switch r {
	case 14:
		return "A"
	case 13:
		return "K"
	case 12:
		return "Q"
	case 11:
		return "J"
	case 10:
		return "10"
	default:
		return itoa(r)
	}
}

func sortSSSCardsDesc(cards []Card) []Card {
	out := make([]Card, len(cards))
	copy(out, cards)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank > out[j].Rank
		}
		return out[i].SuitVal > out[j].SuitVal
	})
	return out
}

func createShisanshuiDeal(sceneMode string, maxPlayers int) *RoomDeal {
	isEight := maxPlayers == 8 || sceneMode == "eight-realtime" || sceneMode == "eight-booking"
	players := 4
	deckCount := 1
	if isEight {
		players = 8
		deckCount = 2
	}
	suits := []struct {
		sym string
		val int
		red bool
	}{
		{"♠", 4, false}, {"♥", 3, true}, {"♣", 2, false}, {"♦", 1, true},
	}
	deck := make([]Card, 0, deckCount*52)
	for d := 0; d < deckCount; d++ {
		for _, s := range suits {
			for r := 2; r <= 14; r++ {
				lbl := sssRankChar(r)
				deck = append(deck, Card{
					ID:      lbl + "-" + s.sym + "-" + itoa(d),
					Rank:    r,
					SuitVal: s.val,
					Label:   lbl,
					Suit:    s.sym,
					IsRed:   s.red,
				})
			}
		}
	}
	for i := len(deck) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}

	seatCards := map[int][]Card{}
	for s := 0; s < players; s++ {
		start := s * 13
		hand := make([]Card, 13)
		copy(hand, deck[start:start+13])
		seatCards[s] = sortSSSCardsDesc(hand)
	}

	return &RoomDeal{
		GameType:    "shisanshui",
		SceneMode:   sceneMode,
		DealID:      "sss-" + itoa(int(nowMs()&0x7fffffff)) + "-" + randomHex(3),
		DeckCount:   deckCount,
		StarterSeat: 0,
		Deck:        deck,
		SeatCards:   seatCards,
		CreatedAt:   nowMs(),
	}
}

func evaluate5CardTier(cards []Card) SSSEvaluatedTier {
	if len(cards) != 5 {
		return SSSEvaluatedTier{Name: "散牌", Type: "high_card", ScoreRank: 0, Cards: cards, DetailLabel: "缺牌"}
	}
	sorted := sortSSSCardsDesc(cards)
	isSameSuit := true
	for i := 1; i < 5; i++ {
		if sorted[i].Suit != sorted[0].Suit {
			isSameSuit = false
			break
		}
	}

	isStraight := false
	straightTierRank := 0.0
	straightHighCardRank := 0
	standardStraight := true
	for i := 1; i < 5; i++ {
		if sorted[i].Rank != sorted[i-1].Rank-1 {
			standardStraight = false
			break
		}
	}
	if standardStraight {
		isStraight = true
		top := sorted[0].Rank
		straightHighCardRank = top
		if top == 14 && sorted[1].Rank == 13 {
			straightTierRank = 14.5
		} else {
			straightTierRank = float64(top)
		}
	} else if sorted[0].Rank == 14 && sorted[1].Rank == 5 && sorted[2].Rank == 4 && sorted[3].Rank == 3 && sorted[4].Rank == 2 {
		isStraight = true
		straightTierRank = 14.2
		straightHighCardRank = 14
	}

	counts := map[int]int{}
	for _, c := range sorted {
		counts[c.Rank]++
	}
	type rankEntry struct {
		rank  int
		count int
	}
	entries := make([]rankEntry, 0, len(counts))
	for r, c := range counts {
		entries = append(entries, rankEntry{rank: r, count: c})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].rank > entries[j].rank
	})

	// 1. Five of a kind
	if entries[0].count >= 5 {
		rk := entries[0].rank
		return SSSEvaluatedTier{
			Name:        "五同(五条)",
			Type:        "five_of_a_kind",
			ScoreRank:   10*sssMultCat + int64(rk)*sssMultR1 + int64(sorted[0].SuitVal),
			Cards:       sorted,
			DetailLabel: "五条" + sssRankChar(rk),
		}
	}

	// 2. Straight Flush
	if isSameSuit && isStraight {
		topCard := sorted[0]
		for _, c := range sorted {
			if c.Rank == straightHighCardRank {
				topCard = c
				break
			}
		}
		return SSSEvaluatedTier{
			Name:        sorted[0].Suit + "同花顺",
			Type:        "straight_flush",
			ScoreRank:   9*sssMultCat + int64(math.Floor(straightTierRank*100))*sssMultR2 + int64(topCard.SuitVal)*sssMultK1,
			Cards:       sorted,
			DetailLabel: sorted[0].Suit + "同花顺",
		}
	}

	// 3. Quads
	if entries[0].count == 4 {
		qRank := entries[0].rank
		kicker := 0
		if len(entries) > 1 {
			kicker = entries[1].rank
		}
		qSuit := 1
		for _, c := range sorted {
			if c.Rank == qRank {
				qSuit = c.SuitVal
				break
			}
		}
		return SSSEvaluatedTier{
			Name:        "铁支(四条)",
			Type:        "quads",
			ScoreRank:   8*sssMultCat + int64(qRank)*sssMultR1 + int64(kicker)*sssMultR2 + int64(qSuit),
			Cards:       sorted,
			DetailLabel: "铁支" + sssRankChar(qRank),
		}
	}

	// 4. Full House
	if entries[0].count == 3 && len(entries) > 1 && entries[1].count >= 2 {
		tr := entries[0].rank
		pr := entries[1].rank
		tSuit := 1
		for _, c := range sorted {
			if c.Rank == tr {
				tSuit = c.SuitVal
				break
			}
		}
		return SSSEvaluatedTier{
			Name:        "葫芦",
			Type:        "full_house",
			ScoreRank:   7*sssMultCat + int64(tr)*sssMultR1 + int64(pr)*sssMultR2 + int64(tSuit),
			Cards:       sorted,
			DetailLabel: sssRankChar(tr) + "带" + sssRankChar(pr) + "葫芦",
		}
	}

	// 5. Flush
	if isSameSuit {
		sv := sorted[0].SuitVal
		return SSSEvaluatedTier{
			Name: sorted[0].Suit + "同花",
			Type: "flush",
			ScoreRank: 6*sssMultCat +
				int64(sv)*sssMultSuit +
				int64(sorted[0].Rank)*sssMultR2 +
				int64(sorted[1].Rank)*sssMultK1 +
				int64(sorted[2].Rank)*sssMultK2 +
				int64(sorted[3].Rank)*sssMultK3 +
				int64(sorted[4].Rank),
			Cards:       sorted,
			DetailLabel: sorted[0].Suit + "同花" + sssRankChar(sorted[0].Rank) + "领衔",
		}
	}

	// 6. Straight
	if isStraight {
		topCard := sorted[0]
		for _, c := range sorted {
			if c.Rank == straightHighCardRank {
				topCard = c
				break
			}
		}
		lbl := sssRankChar(straightHighCardRank) + "高"
		if straightTierRank == 14.5 {
			lbl = "10JQKA"
		} else if straightTierRank == 14.2 || straightTierRank == 5.2 {
			lbl = "A2345"
		}
		return SSSEvaluatedTier{
			Name:        "顺子",
			Type:        "straight",
			ScoreRank:   5*sssMultCat + int64(math.Floor(straightTierRank*100))*sssMultR2 + int64(topCard.SuitVal)*sssMultK1,
			Cards:       sorted,
			DetailLabel: topCard.Suit + lbl + "顺子",
		}
	}

	// 7. Triplet
	if entries[0].count == 3 {
		tr := entries[0].rank
		var kickers []int
		tSuit := 1
		for _, c := range sorted {
			if c.Rank == tr {
				if c.SuitVal > tSuit {
					tSuit = c.SuitVal
				}
			} else {
				kickers = append(kickers, c.Rank)
			}
		}
		k1, k2 := 0, 0
		if len(kickers) > 0 {
			k1 = kickers[0]
		}
		if len(kickers) > 1 {
			k2 = kickers[1]
		}
		return SSSEvaluatedTier{
			Name:        "三条",
			Type:        "triplet",
			ScoreRank:   4*sssMultCat + int64(tr)*sssMultR1 + int64(k1)*sssMultR2 + int64(k2)*sssMultK1 + int64(tSuit),
			Cards:       sorted,
			DetailLabel: "三条" + sssRankChar(tr),
		}
	}

	// 8. Two Pairs
	if entries[0].count == 2 && len(entries) > 1 && entries[1].count == 2 {
		hp := entries[0].rank
		lp := entries[1].rank
		if lp > hp {
			hp, lp = lp, hp
		}
		kicker := 0
		hpSuit := 1
		for _, c := range sorted {
			if c.Rank == hp {
				if c.SuitVal > hpSuit {
					hpSuit = c.SuitVal
				}
			} else if c.Rank != lp {
				kicker = c.Rank
			}
		}
		return SSSEvaluatedTier{
			Name:        "两对",
			Type:        "two_pairs",
			ScoreRank:   3*sssMultCat + int64(hp)*sssMultR1 + int64(lp)*sssMultR2 + int64(kicker)*sssMultK1 + int64(hpSuit),
			Cards:       sorted,
			DetailLabel: sssRankChar(hp) + "配" + sssRankChar(lp) + "两对",
		}
	}

	// 9. One Pair
	if entries[0].count == 2 {
		pr := entries[0].rank
		var kickers []int
		pSuit := 1
		for _, c := range sorted {
			if c.Rank == pr {
				if c.SuitVal > pSuit {
					pSuit = c.SuitVal
				}
			} else {
				kickers = append(kickers, c.Rank)
			}
		}
		k1, k2, k3 := 0, 0, 0
		if len(kickers) > 0 {
			k1 = kickers[0]
		}
		if len(kickers) > 1 {
			k2 = kickers[1]
		}
		if len(kickers) > 2 {
			k3 = kickers[2]
		}
		return SSSEvaluatedTier{
			Name: "对子",
			Type: "pair",
			ScoreRank: 2*sssMultCat +
				int64(pr)*sssMultR1 +
				int64(k1)*sssMultR2 +
				int64(k2)*sssMultK1 +
				int64(k3)*sssMultK2 +
				int64(pSuit),
			Cards:       sorted,
			DetailLabel: "对" + sssRankChar(pr),
		}
	}

	// 10. High card
	return SSSEvaluatedTier{
		Name: "乌龙(散牌)",
		Type: "high_card",
		ScoreRank: 1*sssMultCat +
			int64(sorted[0].Rank)*sssMultR1 +
			int64(sorted[1].Rank)*sssMultR2 +
			int64(sorted[2].Rank)*sssMultK1 +
			int64(sorted[3].Rank)*sssMultK2 +
			int64(sorted[4].Rank)*sssMultK3 +
			int64(sorted[0].SuitVal),
		Cards:       sorted,
		DetailLabel: sssRankChar(sorted[0].Rank) + "高散牌",
	}
}

func evaluate3CardTier(cards []Card) SSSEvaluatedTier {
	if len(cards) != 3 {
		return SSSEvaluatedTier{Name: "散牌", Type: "high_card", ScoreRank: 0, Cards: cards, DetailLabel: "缺牌"}
	}
	sorted := sortSSSCardsDesc(cards)
	if sorted[0].Rank == sorted[1].Rank && sorted[1].Rank == sorted[2].Rank {
		rk := sorted[0].Rank
		name := "冲三条"
		if rk == 14 {
			name = "冲天三条A"
		}
		return SSSEvaluatedTier{
			Name:        name,
			Type:        "triplet",
			ScoreRank:   4*sssMultCat + int64(rk)*sssMultR1 + int64(sorted[0].SuitVal),
			Cards:       sorted,
			DetailLabel: "冲三" + sssRankChar(rk),
		}
	}
	if sorted[0].Rank == sorted[1].Rank {
		pr := sorted[0].Rank
		k := sorted[2].Rank
		name := "头道对子"
		if pr == 14 {
			name = "头道冲对A"
		}
		return SSSEvaluatedTier{
			Name:        name,
			Type:        "pair",
			ScoreRank:   2*sssMultCat + int64(pr)*sssMultR1 + int64(k)*sssMultR2 + int64(sorted[0].SuitVal),
			Cards:       sorted,
			DetailLabel: "对" + sssRankChar(pr) + "带" + sssRankChar(k),
		}
	}
	if sorted[1].Rank == sorted[2].Rank {
		pr := sorted[1].Rank
		k := sorted[0].Rank
		name := "头道对子"
		if pr == 14 {
			name = "头道冲对A"
		}
		return SSSEvaluatedTier{
			Name:        name,
			Type:        "pair",
			ScoreRank:   2*sssMultCat + int64(pr)*sssMultR1 + int64(k)*sssMultR2 + int64(sorted[1].SuitVal),
			Cards:       sorted,
			DetailLabel: "对" + sssRankChar(pr) + "带" + sssRankChar(k),
		}
	}
	return SSSEvaluatedTier{
		Name:        "头道乌龙",
		Type:        "high_card",
		ScoreRank:   1*sssMultCat + int64(sorted[0].Rank)*sssMultR1 + int64(sorted[1].Rank)*sssMultR2 + int64(sorted[2].Rank)*sssMultK1 + int64(sorted[0].SuitVal),
		Cards:       sorted,
		DetailLabel: sssRankChar(sorted[0].Rank) + "高散牌",
	}
}

func isFoulHand(head, mid, tail []Card) bool {
	if len(head) != 3 || len(mid) != 5 || len(tail) != 5 {
		return false
	}
	hEval := evaluate3CardTier(head)
	mEval := evaluate5CardTier(mid)
	tEval := evaluate5CardTier(tail)
	if mEval.ScoreRank > tEval.ScoreRank {
		return true
	}
	if hEval.ScoreRank > mEval.ScoreRank {
		return true
	}
	return false
}

func getSSSTierBonusWater(tierName string, eval SSSEvaluatedTier) (int, string) {
	switch tierName {
	case "head":
		if eval.Type == "triplet" {
			return 2, "头道冲三条 (+3水)"
		}
	case "mid":
		switch eval.Type {
		case "five_of_a_kind":
			return 9, "中道五同至尊 (+10水)"
		case "straight_flush":
			return 9, "中道同花顺 (+10水)"
		case "quads":
			return 7, "中道四带一铁支 (+8水)"
		case "full_house":
			return 1, "中道葫芦 (+2水)"
		}
	case "tail":
		switch eval.Type {
		case "five_of_a_kind":
			return 4, "尾道五同至尊 (+5水)"
		case "straight_flush":
			return 4, "尾道同花顺 (+5水)"
		case "quads":
			return 3, "尾道四带一铁支 (+4水)"
		}
	}
	return 0, ""
}

// ==============================================================================
// Special Natural Hands Detection
// ==============================================================================

func is3CardStraight(cards []Card) bool {
	if len(cards) != 3 {
		return false
	}
	s := sortSSSCardsDesc(cards)
	if s[0].Rank == s[1].Rank+1 && s[1].Rank == s[2].Rank+1 {
		return true
	}
	return s[0].Rank == 14 && s[1].Rank == 3 && s[2].Rank == 2
}

func is3CardFlush(cards []Card) bool {
	return len(cards) == 3 && cards[0].Suit == cards[1].Suit && cards[1].Suit == cards[2].Suit
}

func checkThreeFlushesLayout(cards []Card) (bool, *SSSLayout) {
	bySuit := map[string][]Card{"♠": {}, "♥": {}, "♣": {}, "♦": {}}
	for _, c := range cards {
		bySuit[c.Suit] = append(bySuit[c.Suit], c)
	}
	suits := []string{"♠", "♥", "♣", "♦"}
	// Valid distributions summing to (5,5,3)
	var list5 [][]Card
	var list3 [][]Card
	for _, s := range suits {
		l := sortSSSCardsDesc(bySuit[s])
		if len(l) == 0 {
			continue
		}
		if len(l) == 3 {
			list3 = append(list3, l)
		} else if len(l) == 5 {
			list5 = append(list5, l)
		} else if len(l) == 8 {
			// Split 5+3
			list5 = append(list5, l[:5])
			list3 = append(list3, l[5:8])
		} else if len(l) == 10 {
			list5 = append(list5, l[:5], l[5:10])
		} else if len(l) == 13 {
			return true, &SSSLayout{Head: l[10:13], Mid: l[5:10], Tail: l[0:5]}
		} else {
			return false, nil
		}
	}
	if len(list5) == 2 && len(list3) == 1 {
		mEval := evaluate5CardTier(list5[0])
		tEval := evaluate5CardTier(list5[1])
		head, mid, tail := list3[0], list5[1], list5[0]
		if mEval.ScoreRank > tEval.ScoreRank {
			mid, tail = list5[0], list5[1]
		} else {
			mid, tail = list5[0], list5[1]
			if mEval.ScoreRank > tEval.ScoreRank {
				mid, tail = tail, mid
			}
		}
		if evaluate5CardTier(mid).ScoreRank > evaluate5CardTier(tail).ScoreRank {
			mid, tail = tail, mid
		}
		return true, &SSSLayout{Head: head, Mid: mid, Tail: tail}
	}
	return false, nil
}

func extract5StraightCards(pool []Card) [][]Card {
	var res [][]Card
	byRank := map[int][]Card{}
	for _, c := range pool {
		byRank[c.Rank] = append(byRank[c.Rank], c)
	}
	for start := 2; start <= 10; start++ {
		ok := true
		var five []Card
		for k := 0; k < 5; k++ {
			if len(byRank[start+k]) == 0 {
				ok = false
				break
			}
			five = append(five, byRank[start+k][0])
		}
		if ok {
			res = append(res, five)
		}
	}
	if len(byRank[14]) > 0 && len(byRank[2]) > 0 && len(byRank[3]) > 0 && len(byRank[4]) > 0 && len(byRank[5]) > 0 {
		res = append(res, []Card{byRank[14][0], byRank[5][0], byRank[4][0], byRank[3][0], byRank[2][0]})
	}
	return res
}

func checkThreeStraightsLayout(cards []Card) (bool, *SSSLayout) {
	firstList := extract5StraightCards(cards)
	for _, s1 := range firstList {
		rem1 := removeCardsFromHand(cards, s1)
		secondList := extract5StraightCards(rem1)
		for _, s2 := range secondList {
			rem2 := removeCardsFromHand(rem1, s2)
			if len(rem2) == 3 && is3CardStraight(rem2) {
				mid, tail := s1, s2
				if evaluate5CardTier(mid).ScoreRank > evaluate5CardTier(tail).ScoreRank {
					mid, tail = tail, mid
				}
				return true, &SSSLayout{Head: rem2, Mid: mid, Tail: tail}
			}
		}
	}
	return false, nil
}

func checkThreeStraightFlushesLayout(cards []Card) (bool, *SSSLayout) {
	bySuit := map[string][]Card{"♠": {}, "♥": {}, "♣": {}, "♦": {}}
	for _, c := range cards {
		bySuit[c.Suit] = append(bySuit[c.Suit], c)
	}
	type sf5 struct {
		cards []Card
		ids   map[string]bool
	}
	var sfList []sf5
	for _, sym := range []string{"♠", "♥", "♣", "♦"} {
		list := bySuit[sym]
		if len(list) < 5 {
			continue
		}
		rankMap := map[int][]Card{}
		for _, c := range list {
			rankMap[c.Rank] = append(rankMap[c.Rank], c)
		}
		for start := 2; start <= 10; start++ {
			if len(rankMap[start]) > 0 && len(rankMap[start+1]) > 0 && len(rankMap[start+2]) > 0 && len(rankMap[start+3]) > 0 && len(rankMap[start+4]) > 0 {
				five := []Card{rankMap[start][0], rankMap[start+1][0], rankMap[start+2][0], rankMap[start+3][0], rankMap[start+4][0]}
				ids := map[string]bool{}
				for _, f := range five {
					ids[f.ID] = true
				}
				sfList = append(sfList, sf5{cards: five, ids: ids})
			}
		}
		if len(rankMap[14]) > 0 && len(rankMap[2]) > 0 && len(rankMap[3]) > 0 && len(rankMap[4]) > 0 && len(rankMap[5]) > 0 {
			five := []Card{rankMap[14][0], rankMap[5][0], rankMap[4][0], rankMap[3][0], rankMap[2][0]}
			ids := map[string]bool{}
			for _, f := range five {
				ids[f.ID] = true
			}
			sfList = append(sfList, sf5{cards: five, ids: ids})
		}
	}
	for i := 0; i < len(sfList); i++ {
		s1 := sfList[i]
		for j := i + 1; j < len(sfList); j++ {
			s2 := sfList[j]
			overlap := false
			for id := range s1.ids {
				if s2.ids[id] {
					overlap = true
					break
				}
			}
			if overlap {
				continue
			}
			var rem []Card
			for _, c := range cards {
				if !s1.ids[c.ID] && !s2.ids[c.ID] {
					rem = append(rem, c)
				}
			}
			if len(rem) == 3 && is3CardFlush(rem) && is3CardStraight(rem) {
				e1 := evaluate5CardTier(s1.cards)
				e2 := evaluate5CardTier(s2.cards)
				tail, mid := s1.cards, s2.cards
				if e2.ScoreRank > e1.ScoreRank {
					tail, mid = s2.cards, s1.cards
				}
				if !isFoulHand(rem, mid, tail) {
					return true, &SSSLayout{Head: rem, Mid: mid, Tail: tail}
				}
			}
		}
	}
	return false, nil
}

func detectSSSSpecialHand(cards []Card) *SSSSpecialHand {
	if len(cards) != 13 {
		return nil
	}
	sorted := sortSSSCardsDesc(cards)
	byRank := map[int]int{}
	bySuit := map[string]int{}
	for _, c := range sorted {
		byRank[c.Rank]++
		bySuit[c.Suit]++
	}
	// 1. 至尊青龙 (36水): 13张同花顺 (2..A)
	if len(byRank) == 13 && len(bySuit) == 1 {
		return &SSSSpecialHand{Type: "supreme_dragon", Name: "至尊青龙", BonusWater: 36, Desc: "同花2至A一条龙，直接通杀全场 (+36水)"}
	}
	// 2. 一条龙 (26水): 2..A 各1张
	if len(byRank) == 13 {
		return &SSSSpecialHand{Type: "dragon", Name: "一条龙", BonusWater: 26, Desc: "2至A十三张通顺无重 (+26水)"}
	}
	// 3. 十二皇族 (24水): 至少12张 J,Q,K,A
	royalCount := 0
	for _, c := range sorted {
		if c.Rank >= 11 {
			royalCount++
		}
	}
	if royalCount >= 12 {
		return &SSSSpecialHand{Type: "twelve_royals", Name: "十二皇族", BonusWater: 24, Desc: "十二张以上J/Q/K/A皇室大牌 (+24水)"}
	}
	// 4. 三同花顺 (20水)
	if ok, _ := checkThreeStraightFlushesLayout(sorted); ok {
		return &SSSSpecialHand{Type: "three_straight_flushes", Name: "三同花顺", BonusWater: 20, Desc: "头中尾三道尽皆同花顺 (+20水)"}
	}
	// 5. 三分天下/三铁支 (16水)
	quadCount := 0
	for _, cnt := range byRank {
		if cnt == 4 {
			quadCount++
		}
	}
	if quadCount >= 3 {
		return &SSSSpecialHand{Type: "three_quads", Name: "三分天下(三铁支)", BonusWater: 16, Desc: "坐拥三组四条铁支横扫千军 (+16水)"}
	}
	// 6. 凑一色 (10水): 全红或全黑
	allRed := true
	allBlack := true
	for _, c := range sorted {
		if c.IsRed {
			allBlack = false
		} else {
			allRed = false
		}
	}
	if allRed || allBlack {
		return &SSSSpecialHand{Type: "all_one_color", Name: "凑一色", BonusWater: 10, Desc: "十三张全红或全黑 (+10水)"}
	}
	// 7. 全大/全小 (10水): 全>=8 或 全<=8
	allBig := true
	allSmall := true
	for _, c := range sorted {
		if c.Rank < 8 {
			allBig = false
		}
		if c.Rank > 8 {
			allSmall = false
		}
	}
	if allBig || allSmall {
		return &SSSSpecialHand{Type: "all_big_small", Name: "全大 / 全小", BonusWater: 10, Desc: "清一色8以上全大或8以下全小 (+10水)"}
	}
	// 8. 五对三条 (8水) & 六对半 (6水)
	pairGroups := 0
	tripGroups := 0
	for _, cnt := range byRank {
		if cnt == 2 {
			pairGroups++
		} else if cnt == 3 {
			tripGroups++
		} else if cnt == 4 {
			pairGroups += 2
		} else if cnt == 5 {
			pairGroups += 1
			tripGroups += 1
		}
	}
	if pairGroups == 5 && tripGroups == 1 {
		return &SSSSpecialHand{Type: "five_pairs_one_triplet", Name: "五对三条", BonusWater: 8, Desc: "五副对子加一组三条 (+8水)"}
	}
	if pairGroups == 6 && tripGroups == 0 {
		return &SSSSpecialHand{Type: "six_and_half_pairs", Name: "六对半", BonusWater: 6, Desc: "六组对子加单牌 (+6水)"}
	}
	// 9. 三同花 (6水)
	if ok, _ := checkThreeFlushesLayout(sorted); ok {
		return &SSSSpecialHand{Type: "three_flushes", Name: "三同花", BonusWater: 6, Desc: "头中尾三道各成同花 (+6水)"}
	}
	// 10. 三顺子 (6水)
	if ok, _ := checkThreeStraightsLayout(sorted); ok {
		return &SSSSpecialHand{Type: "three_straights", Name: "三顺子", BonusWater: 6, Desc: "头中尾三道皆为顺子 (+6水)"}
	}
	return nil
}

func evaluateSSSLayoutFitness(head, mid, tail []Card) float64 {
	if isFoulHand(head, mid, tail) {
		return -1_000_000_000
	}
	hEval := evaluate3CardTier(head)
	mEval := evaluate5CardTier(mid)
	tEval := evaluate5CardTier(tail)
	hb, _ := getSSSTierBonusWater("head", hEval)
	mb, _ := getSSSTierBonusWater("mid", mEval)
	tb, _ := getSSSTierBonusWater("tail", tEval)

	var score float64
	score += float64(hb*3000 + mb*2500 + tb*2000)

	// Head base weight (high impact because 3 cards often wins with pair/A)
	switch hEval.Type {
	case "triplet":
		score += 4200 + float64(head[0].Rank)*40
	case "pair":
		score += 1800 + float64(hEval.ScoreRank%sssMultCat)/float64(sssMultR1)*30
	default:
		score += float64(head[0].Rank) * 18
	}

	// Mid weight
	mCat := float64(mEval.ScoreRank / sssMultCat)
	score += mCat*1200 + float64(mEval.ScoreRank%sssMultCat)/float64(sssMultR1)*15

	// Tail weight
	tCat := float64(tEval.ScoreRank / sssMultCat)
	score += tCat*900 + float64(tEval.ScoreRank%sssMultCat)/float64(sssMultR1)*10

	return score
}

func autoArrangeThirteen(cards []Card) SSSLayout {
	if len(cards) != 13 {
		return SSSLayout{Head: cards}
	}
	sorted := sortSSSCardsDesc(cards)
	sp := detectSSSSpecialHand(sorted)
	if sp != nil {
		if sp.Type == "three_flushes" {
			if ok, lay := checkThreeFlushesLayout(sorted); ok && lay != nil {
				lay.SpecialHand = sp
				return *lay
			}
		}
		if sp.Type == "three_straight_flushes" {
			if ok, lay := checkThreeStraightFlushesLayout(sorted); ok && lay != nil {
				lay.SpecialHand = sp
				return *lay
			}
		}
		if sp.Type == "three_straights" {
			if ok, lay := checkThreeStraightsLayout(sorted); ok && lay != nil {
				lay.SpecialHand = sp
				return *lay
			}
		}
		return SSSLayout{
			Head:        sorted[10:13],
			Mid:         sorted[5:10],
			Tail:        sorted[0:5],
			SpecialHand: sp,
		}
	}

	bestScore := -1e18
	var bestHead, bestMid, bestTail []Card
	var tail [5]Card
	var mid [5]Card
	var rem8 [8]Card
	var head [3]Card

	for _, c13 := range sssCombos13_5 {
		tail[0], tail[1], tail[2], tail[3], tail[4] = sorted[c13[0]], sorted[c13[1]], sorted[c13[2]], sorted[c13[3]], sorted[c13[4]]
		tEval := evaluate5CardTier(tail[:])
		if tEval.ScoreRank < 2*sssMultCat && bestScore > 0 {
			continue
		}
		// Build remaining 8
		ri := 0
		ci := 0
		for i := 0; i < 13; i++ {
			if ci < 5 && c13[ci] == i {
				ci++
				continue
			}
			rem8[ri] = sorted[i]
			ri++
		}
		for _, c8 := range sssCombos8_5 {
			mid[0], mid[1], mid[2], mid[3], mid[4] = rem8[c8[0]], rem8[c8[1]], rem8[c8[2]], rem8[c8[3]], rem8[c8[4]]
			hi := 0
			mi := 0
			for i := 0; i < 8; i++ {
				if mi < 5 && c8[mi] == i {
					mi++
					continue
				}
				head[hi] = rem8[i]
				hi++
			}
			fit := evaluateSSSLayoutFitness(head[:], mid[:], tail[:])
			if fit > bestScore {
				bestScore = fit
				bestHead = append([]Card{}, head[:]...)
				bestMid = append([]Card{}, mid[:]...)
				bestTail = append([]Card{}, tail[:]...)
			}
		}
	}
	if len(bestHead) != 3 {
		return SSSLayout{
			Head: sorted[10:13],
			Mid:  sorted[5:10],
			Tail: sorted[0:5],
		}
	}
	return SSSLayout{
		Head: bestHead,
		Mid:  bestMid,
		Tail: bestTail,
	}
}

func breakSpecialHandTie(aCards, bCards []Card, sp *SSSSpecialHand) int {
	sa := sortSSSCardsDesc(aCards)
	sb := sortSSSCardsDesc(bCards)
	for i := 0; i < len(sa) && i < len(sb); i++ {
		if sa[i].Rank != sb[i].Rank {
			if sa[i].Rank > sb[i].Rank {
				return 1
			}
			return -1
		}
	}
	for i := 0; i < len(sa) && i < len(sb); i++ {
		if sa[i].SuitVal != sb[i].SuitVal {
			if sa[i].SuitVal > sb[i].SuitVal {
				return 1
			}
			return -1
		}
	}
	_ = sp
	return 0
}

type sssPlayerPrep struct {
	seat        int
	name        string
	head        []Card
	mid         []Card
	tail        []Card
	allCards    []Card
	isFoul      bool
	isSpecial   bool
	specialHand *SSSSpecialHand
	headEval    SSSEvaluatedTier
	midEval     SSSEvaluatedTier
	tailEval    SSSEvaluatedTier
}

func prepareSSSPlayer(seat int, name string, sub ShisanshuiSubmission) sssPlayerPrep {
	var head, mid, tail []Card
	if sub.Tiers != nil {
		head = sub.Tiers.Head
		mid = sub.Tiers.Mid
		tail = sub.Tiers.Tail
	} else if sub.RawTiers != nil {
		head = sub.RawTiers.Front
		mid = sub.RawTiers.Mid
		tail = sub.RawTiers.Back
	}
	all := append(append(append([]Card{}, head...), mid...), tail...)
	var sp *SSSSpecialHand
	if sub.IsSpecial {
		sp = detectSSSSpecialHand(all)
	}
	hEval := evaluate3CardTier(head)
	mEval := evaluate5CardTier(mid)
	tEval := evaluate5CardTier(tail)
	foul := sp == nil && isFoulHand(head, mid, tail)
	return sssPlayerPrep{
		seat:        seat,
		name:        name,
		head:        head,
		mid:         mid,
		tail:        tail,
		allCards:    all,
		isFoul:      foul,
		isSpecial:   sp != nil,
		specialHand: sp,
		headEval:    hEval,
		midEval:     mEval,
		tailEval:    tEval,
	}
}

func compareTwoSSSPlayers(p1, p2 sssPlayerPrep) (netWater int, isShot bool, shotBy bool, details []string) {
	// Case 1: Special hands
	if p1.specialHand != nil || p2.specialHand != nil {
		if p1.specialHand != nil && p2.specialHand == nil {
			w := p1.specialHand.BonusWater
			if p2.isFoul {
				w += 3
			}
			return w, false, false, []string{p1.specialHand.Name + " 通杀 (+" + itoa(w) + "水)"}
		}
		if p2.specialHand != nil && p1.specialHand == nil {
			w := p2.specialHand.BonusWater
			if p1.isFoul {
				w += 3
			}
			return -w, false, false, []string{"被对手 " + p2.specialHand.Name + " 击败 (-" + itoa(w) + "水)"}
		}
		if p1.specialHand != nil && p2.specialHand != nil {
			if p1.specialHand.BonusWater > p2.specialHand.BonusWater {
				diff := p1.specialHand.BonusWater - p2.specialHand.BonusWater
				return diff, false, false, []string{p1.specialHand.Name + " 压制 " + p2.specialHand.Name + " (净差+" + itoa(diff) + "水)"}
			}
			if p2.specialHand.BonusWater > p1.specialHand.BonusWater {
				diff := p2.specialHand.BonusWater - p1.specialHand.BonusWater
				return -diff, false, false, []string{p2.specialHand.Name + " 压制 " + p1.specialHand.Name + " (净差-" + itoa(diff) + "水)"}
			}
			tie := breakSpecialHandTie(p1.allCards, p2.allCards, p1.specialHand)
			if tie > 0 {
				w := p1.specialHand.BonusWater
				return w, false, false, []string{"同特殊牌型比大胜出 (+" + itoa(w) + "水)"}
			}
			if tie < 0 {
				w := p2.specialHand.BonusWater
				return -w, false, false, []string{"同特殊牌型比大落败 (-" + itoa(w) + "水)"}
			}
			return 0, false, false, []string{"同特殊牌型平水 (0水)"}
		}
	}

	// Case 2: Foul checks (base 3 doubles to 6, tier bonuses added once)
	if p1.isFoul && p2.isFoul {
		return 0, false, false, []string{"双方相公倒水互免 (0水)"}
	}
	if p1.isFoul {
		hb, _ := getSSSTierBonusWater("head", p2.headEval)
		mb, _ := getSSSTierBonusWater("mid", p2.midEval)
		tb, _ := getSSSTierBonusWater("tail", p2.tailEval)
		loss := 6 + hb + mb + tb
		return -loss, false, true, []string{"己方倒水相公被通打 (-" + itoa(loss) + "水)"}
	}
	if p2.isFoul {
		hb, _ := getSSSTierBonusWater("head", p1.headEval)
		mb, _ := getSSSTierBonusWater("mid", p1.midEval)
		tb, _ := getSSSTierBonusWater("tail", p1.tailEval)
		gain := 6 + hb + mb + tb
		return gain, true, false, []string{"对手倒水相公全收打枪 (+" + itoa(gain) + "水)"}
	}

	// Case 3: Normal 3-tier comparison (only base water doubles on 3-tier shot; bonuses stay 1x)
	rawBase := 0
	rawBonus := 0
	p1Wins := 0
	p2Wins := 0

	// Head
	if p1.headEval.ScoreRank > p2.headEval.ScoreRank {
		b1, _ := getSSSTierBonusWater("head", p1.headEval)
		b2, _ := getSSSTierBonusWater("head", p2.headEval)
		nb := b1 - b2
		if nb < 0 {
			nb = 0
		}
		rawBase++
		rawBonus += nb
		p1Wins++
		details = append(details, "头道胜 "+p1.headEval.DetailLabel+" (+"+itoa(1+nb)+")")
	} else if p2.headEval.ScoreRank > p1.headEval.ScoreRank {
		b1, _ := getSSSTierBonusWater("head", p1.headEval)
		b2, _ := getSSSTierBonusWater("head", p2.headEval)
		nb := b2 - b1
		if nb < 0 {
			nb = 0
		}
		rawBase--
		rawBonus -= nb
		p2Wins++
		details = append(details, "头道负 "+p2.headEval.DetailLabel+" (-"+itoa(1+nb)+")")
	} else {
		details = append(details, "头道打平 (0)")
	}

	// Mid
	if p1.midEval.ScoreRank > p2.midEval.ScoreRank {
		b1, _ := getSSSTierBonusWater("mid", p1.midEval)
		b2, _ := getSSSTierBonusWater("mid", p2.midEval)
		nb := b1 - b2
		if nb < 0 {
			nb = 0
		}
		rawBase++
		rawBonus += nb
		p1Wins++
		details = append(details, "中道胜 "+p1.midEval.DetailLabel+" (+"+itoa(1+nb)+")")
	} else if p2.midEval.ScoreRank > p1.midEval.ScoreRank {
		b1, _ := getSSSTierBonusWater("mid", p1.midEval)
		b2, _ := getSSSTierBonusWater("mid", p2.midEval)
		nb := b2 - b1
		if nb < 0 {
			nb = 0
		}
		rawBase--
		rawBonus -= nb
		p2Wins++
		details = append(details, "中道负 "+p2.midEval.DetailLabel+" (-"+itoa(1+nb)+")")
	} else {
		details = append(details, "中道打平 (0)")
	}

	// Tail
	if p1.tailEval.ScoreRank > p2.tailEval.ScoreRank {
		b1, _ := getSSSTierBonusWater("tail", p1.tailEval)
		b2, _ := getSSSTierBonusWater("tail", p2.tailEval)
		nb := b1 - b2
		if nb < 0 {
			nb = 0
		}
		rawBase++
		rawBonus += nb
		p1Wins++
		details = append(details, "尾道胜 "+p1.tailEval.DetailLabel+" (+"+itoa(1+nb)+")")
	} else if p2.tailEval.ScoreRank > p1.tailEval.ScoreRank {
		b1, _ := getSSSTierBonusWater("tail", p1.tailEval)
		b2, _ := getSSSTierBonusWater("tail", p2.tailEval)
		nb := b2 - b1
		if nb < 0 {
			nb = 0
		}
		rawBase--
		rawBonus -= nb
		p2Wins++
		details = append(details, "尾道负 "+p2.tailEval.DetailLabel+" (-"+itoa(1+nb)+")")
	} else {
		details = append(details, "尾道打平 (0)")
	}

	multiplier := 1
	if p1Wins == 3 {
		isShot = true
		multiplier = 2
		details = append(details, "🔫 打枪三道全胜基础翻倍 (×2)！")
	} else if p2Wins == 3 {
		shotBy = true
		multiplier = 2
		details = append(details, "💥 被对手三道打枪基础翻倍 (×2)！")
	}
	return rawBase*multiplier + rawBonus, isShot, shotBy, details
}

func settleAllSSSPlayers(preps []sssPlayerPrep, pointValuePerWater int) []SSSPlayerResult {
	n := len(preps)
	results := make([]SSSPlayerResult, n)
	pairNet := make([][]int, n)
	pairShot := make([][]bool, n)
	pairShotBy := make([][]bool, n)
	pairDetails := make([][][]string, n)
	for i := 0; i < n; i++ {
		pairNet[i] = make([]int, n)
		pairShot[i] = make([]bool, n)
		pairShotBy[i] = make([]bool, n)
		pairDetails[i] = make([][]string, n)
	}

	shotCounts := make([]int, n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			w, sh, shBy, det := compareTwoSSSPlayers(preps[i], preps[j])
			pairNet[i][j] = w
			pairNet[j][i] = -w
			pairShot[i][j] = sh
			pairShotBy[i][j] = shBy
			pairShot[j][i] = shBy
			pairShotBy[j][i] = sh
			pairDetails[i][j] = det
			pairDetails[j][i] = det
			if sh {
				shotCounts[i]++
			}
			if shBy {
				shotCounts[j]++
			}
		}
	}

	// Grand slam (全垒打): shot all N-1 opponents (for N>=4)
	grandSlamIdx := -1
	for i := 0; i < n; i++ {
		if n >= 4 && shotCounts[i] == n-1 && !preps[i].isFoul {
			grandSlamIdx = i
			break
		}
	}
	if grandSlamIdx >= 0 {
		for j := 0; j < n; j++ {
			if j == grandSlamIdx {
				continue
			}
			pairNet[grandSlamIdx][j] *= 2
			pairNet[j][grandSlamIdx] *= 2
		}
	}

	for i := 0; i < n; i++ {
		totalW := 0
		matchups := make([]SSSMatchupSummary, 0, n-1)
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			totalW += pairNet[i][j]
			matchups = append(matchups, SSSMatchupSummary{
				AgainstSeat: preps[j].seat,
				AgainstName: preps[j].name,
				NetWater:    pairNet[i][j],
				IsShot:      pairShot[i][j],
				ShotBy:      pairShotBy[i][j],
				Details:     pairDetails[i][j],
			})
		}
		var tags []string
		if preps[i].isFoul {
			tags = append(tags, "倒水相公")
		}
		if preps[i].specialHand != nil {
			tags = append(tags, preps[i].specialHand.Name)
		}
		if grandSlamIdx == i {
			tags = append(tags, "🏆 全垒打通杀 (×2)")
		} else if shotCounts[i] > 0 {
			tags = append(tags, "🔫 打枪 "+itoa(shotCounts[i])+" 家")
		}
		hCopy := preps[i].headEval
		mCopy := preps[i].midEval
		tCopy := preps[i].tailEval
		results[i] = SSSPlayerResult{
			Seat:        preps[i].seat,
			Name:        preps[i].name,
			TotalWater:  totalW,
			ScoreDelta:  totalW * pointValuePerWater,
			RatingDelta: int(math.Round(float64(totalW) * 2.5)),
			IsFoul:      preps[i].isFoul,
			IsSpecial:   preps[i].isSpecial,
			SpecialHand: preps[i].specialHand,
			IsGrandSlam: grandSlamIdx == i,
			ShotCount:   shotCounts[i],
			HeadEval:    &hCopy,
			MidEval:     &mCopy,
			TailEval:    &tCopy,
			Head:        preps[i].head,
			Mid:         preps[i].mid,
			Tail:        preps[i].tail,
			Matchups:    matchups,
			SummaryTags: tags,
		}
	}
	return results
}

// ==============================================================================
// Shisanshui Authoritative Realtime Server Engine
// ==============================================================================

func getSSSTotalSeats(room *RoomState) int {
	if room != nil && room.MaxPlayers == 8 {
		return 8
	}
	if room != nil && (room.SceneMode == "eight-realtime" || room.SceneMode == "eight-booking") {
		return 8
	}
	if room != nil && room.Deal != nil && room.Deal.DeckCount == 2 {
		return 8
	}
	return 4
}

func settleShisanshuiRound(ctx *HubContext, room *RoomState, isTimeout bool) {
	if room == nil || room.BoardState == nil {
		return
	}
	bs := room.BoardState
	if bs.Phase == "ended" {
		return
	}
	if bs.ShisanshuiSubmissions == nil {
		bs.ShisanshuiSubmissions = map[string]ShisanshuiSubmission{}
	}
	totalSeats := getSSSTotalSeats(room)
	for s := 0; s < totalSeats; s++ {
		if _, ok := bs.ShisanshuiSubmissions[itoa(s)]; !ok {
			var hand []Card
			if room.Deal != nil && room.Deal.SeatCards != nil {
				hand = room.Deal.SeatCards[s]
			}
			lay := autoArrangeThirteen(hand)
			hE := evaluate3CardTier(lay.Head)
			mE := evaluate5CardTier(lay.Mid)
			tE := evaluate5CardTier(lay.Tail)
			bs.ShisanshuiSubmissions[itoa(s)] = ShisanshuiSubmission{
				Seat: s,
				Tiers: &SSSTiers{Head: lay.Head, Mid: lay.Mid, Tail: lay.Tail},
				RawTiers: &SSSRawTiers{
					Front:       lay.Head,
					Mid:         lay.Mid,
					Back:        lay.Tail,
					FrontLabel:  hE.Name,
					MidLabel:    mE.Name,
					BackLabel:   tE.Name,
					SpecialHand: lay.SpecialHand,
				},
				IsSpecial:     lay.SpecialHand != nil,
				AutoSubmitted: true,
				SubmittedAt:   nowMs(),
			}
		}
	}

	preps := make([]sssPlayerPrep, 0, totalSeats)
	for s := 0; s < totalSeats; s++ {
		sub := bs.ShisanshuiSubmissions[itoa(s)]
		pName := "牌友" + itoa(s+1)
		for _, pl := range room.Players {
			if pl.Seat == s && pl.Name != "" {
				pName = pl.Name
				break
			}
		}
		preps = append(preps, prepareSSSPlayer(s, pName, sub))
	}

	results := settleAllSSSPlayers(preps, 1)
	var winnerSeat *int
	bestWater := -999999
	coinsDelta := make([]int, totalSeats)
	ratingDelta := make([]int, totalSeats)
	netWaters := make([]int, totalSeats)
	grandSlamSeats := make([]bool, totalSeats)
	perSeatOppResults := map[int][]map[string]interface{}{}

	for _, r := range results {
		if r.Seat >= 0 && r.Seat < totalSeats {
			coinsDelta[r.Seat] = r.ScoreDelta
			ratingDelta[r.Seat] = r.RatingDelta
			netWaters[r.Seat] = r.TotalWater
			grandSlamSeats[r.Seat] = r.IsGrandSlam
			if r.TotalWater > bestWater {
				bestWater = r.TotalWater
				ws := r.Seat
				winnerSeat = &ws
			}
			oppList := make([]map[string]interface{}, 0, len(r.Matchups))
			for _, m := range r.Matchups {
				var oppSpecial *SSSSpecialHand
				if m.AgainstSeat >= 0 && m.AgainstSeat < len(preps) {
					oppSpecial = preps[m.AgainstSeat].specialHand
				}
				summaryStr := ""
				if len(m.Details) > 0 {
					summaryStr = m.Details[0]
				}
				oppList = append(oppList, map[string]interface{}{
					"seat":       m.AgainstSeat,
					"name":       m.AgainstName,
					"netWater":   m.NetWater,
					"isShot":     m.IsShot,
					"isFoulOpp":  preps[m.AgainstSeat].isFoul,
					"summary":    summaryStr,
					"specialMe":  r.SpecialHand,
					"specialOpp": oppSpecial,
				})
			}
			perSeatOppResults[r.Seat] = oppList
		}
	}

	settlementPayload := map[string]interface{}{
		"netWaters":         netWaters,
		"grandSlamSeats":    grandSlamSeats,
		"perSeatOppResults": perSeatOppResults,
		"results":           results,
		"winnerSeat":        winnerSeat,
		"coinsDelta":        coinsDelta,
		"ratingDelta":       ratingDelta,
		"totalSeats":        totalSeats,
		"settledAt":         nowMs(),
	}

	bs.Phase = "ended"
	bs.WinnerSeat = winnerSeat
	bs.ShisanshuiSettlement = settlementPayload
	bs.Settlement = settlementPayload
	bs.ActionSeq++
	bs.UpdatedAt = nowMs()
	room.Status = "finished"

	ctx.SettleRoomCoins(room, coinsDelta, "shisanshui", ratingDelta)
	ctx.ResetSceneRoomReady(room.ID)

	evt := map[string]interface{}{
		"action":               "shisanshui_all_submitted",
		"submissions":          bs.ShisanshuiSubmissions,
		"settlement":           settlementPayload,
		"shisanshuiSettlement": settlementPayload,
		"winner":               winnerSeat,
	}
	if isTimeout {
		evt["timeout"] = true
	}
	ctx.SendSanitizedToRoom(room, func(_ int, _ ScenePlayer) interface{} {
		return map[string]interface{}{
			"type":       "game_event",
			"roomId":     room.ID,
			"event":      evt,
			"payload":    evt,
			"moveCount":  room.MoveCount,
			"boardState": bs,
		}
	})
	ctx.ScheduleSnapshot()
}

func validateSSSPayloadCards(hand []Card, tiers *SSSTiers, rawTiers *SSSRawTiers) bool {
	if len(hand) != 13 {
		return true // allow if deal not tracked
	}
	var head, mid, tail []Card
	if tiers != nil {
		head, mid, tail = tiers.Head, tiers.Mid, tiers.Tail
	} else if rawTiers != nil {
		head, mid, tail = rawTiers.Front, rawTiers.Mid, rawTiers.Back
	}
	if len(head) != 3 || len(mid) != 5 || len(tail) != 5 {
		return false
	}
	handCount := map[string]int{}
	for _, c := range hand {
		handCount[c.ID]++
	}
	used := map[string]int{}
	for _, list := range [][]Card{head, mid, tail} {
		for _, c := range list {
			used[c.ID]++
			if used[c.ID] > handCount[c.ID] {
				return false
			}
		}
	}
	return true
}

func sendSSSReject(ctx *HubContext, sender *WSConn, room *RoomState, seat int, reason, message string) {
	if sender == nil || room == nil {
		return
	}
	evt := map[string]interface{}{
		"action":  "shisanshui_submit_rejected",
		"reason":  reason,
		"message": message,
	}
	ctx.SendToConn(sender, map[string]interface{}{
		"type":       "game_event",
		"roomId":     room.ID,
		"seat":       seat,
		"event":      evt,
		"payload":    evt,
		"boardState": room.BoardState,
	})
}

func handleShisanshuiGameMove(ctx *HubContext, sender *WSConn, room *RoomState, seat int, payloadMap map[string]interface{}) {
	if room == nil || room.BoardState == nil {
		return
	}
	bs := room.BoardState
	if bs.ShisanshuiSubmissions == nil {
		bs.ShisanshuiSubmissions = map[string]ShisanshuiSubmission{}
	}
	totalSeats := getSSSTotalSeats(room)
	if seat < 0 || seat >= totalSeats {
		return
	}
	if _, already := bs.ShisanshuiSubmissions[itoa(seat)]; already {
		return
	}

	var tiers *SSSTiers
	if rawT, ok := payloadMap["tiers"]; ok && rawT != nil {
		b, _ := json.Marshal(rawT)
		var t SSSTiers
		if json.Unmarshal(b, &t) == nil && len(t.Head) == 3 && len(t.Mid) == 5 && len(t.Tail) == 5 {
			tiers = &t
		}
	}
	var rawTiers *SSSRawTiers
	if rawRT, ok := payloadMap["rawTiers"]; ok && rawRT != nil {
		b, _ := json.Marshal(rawRT)
		var rt SSSRawTiers
		if json.Unmarshal(b, &rt) == nil && len(rt.Front) == 3 && len(rt.Mid) == 5 && len(rt.Back) == 5 {
			rawTiers = &rt
		}
	}

	if tiers == nil && rawTiers != nil {
		tiers = &SSSTiers{Head: rawTiers.Front, Mid: rawTiers.Mid, Tail: rawTiers.Back}
	}
	if rawTiers == nil && tiers != nil {
		rawTiers = &SSSRawTiers{Front: tiers.Head, Mid: tiers.Mid, Back: tiers.Tail}
	}

	if tiers == nil {
		sendSSSReject(ctx, sender, room, seat, "invalid_count", "三道摆牌张数必须为 3 / 5 / 5")
		return
	}

	if room.Deal != nil && room.Deal.SeatCards != nil {
		if hand, ok := room.Deal.SeatCards[seat]; ok && len(hand) == 13 {
			if !validateSSSPayloadCards(hand, tiers, rawTiers) {
				sendSSSReject(ctx, sender, room, seat, "invalid_cards", "所提交牌组与服务器所发手牌不符")
				return
			}
		}
	}

	all13 := append(append(append([]Card{}, tiers.Head...), tiers.Mid...), tiers.Tail...)
	serverSpecial := detectSSSSpecialHand(all13)
	if serverSpecial == nil && isFoulHand(tiers.Head, tiers.Mid, tiers.Tail) {
		sendSSSReject(ctx, sender, room, seat, "foul_hand", "当前摆牌倒水相公（尾道需 >= 中道 >= 头道），请调整后提交")
		return
	}

	hE := evaluate3CardTier(tiers.Head)
	mE := evaluate5CardTier(tiers.Mid)
	tE := evaluate5CardTier(tiers.Tail)
	if rawTiers == nil {
		rawTiers = &SSSRawTiers{Front: tiers.Head, Mid: tiers.Mid, Back: tiers.Tail}
	}
	rawTiers.FrontLabel = hE.Name
	rawTiers.MidLabel = mE.Name
	rawTiers.BackLabel = tE.Name
	rawTiers.SpecialHand = serverSpecial

	autoSubmitted := false
	if v, ok := payloadMap["autoSubmitted"].(bool); ok {
		autoSubmitted = v
	}

	bs.ShisanshuiSubmissions[itoa(seat)] = ShisanshuiSubmission{
		Seat:          seat,
		Tiers:         tiers,
		RawTiers:      rawTiers,
		IsSpecial:     serverSpecial != nil,
		AutoSubmitted: autoSubmitted,
		SubmittedAt:   nowMs(),
	}

	room.MoveCount++
	bs.MoveCount = room.MoveCount
	bs.ActionSeq++
	bs.UpdatedAt = nowMs()
	room.LastTurnTs = nowMs()

	submittedCount := len(bs.ShisanshuiSubmissions)
	evt := map[string]interface{}{
		"action":         "shisanshui_submit_hand",
		"seat":           seat,
		"submittedCount": submittedCount,
		"targetCount":    totalSeats,
		"isSpecial":      serverSpecial != nil,
		"autoSubmitted":  autoSubmitted,
	}
	ctx.SendSanitizedToRoom(room, func(rSeat int, _ ScenePlayer) interface{} {
		return map[string]interface{}{
			"type":       "game_event",
			"roomId":     room.ID,
			"seat":       seat,
			"event":      evt,
			"payload":    evt,
			"moveCount":  room.MoveCount,
			"boardState": sanitizeBoardStateForSeat(bs, "shisanshui", rSeat),
		}
	})

	if submittedCount >= totalSeats {
		settleShisanshuiRound(ctx, room, false)
	} else {
		ctx.ScheduleSnapshot()
	}
}

func tickShisanshuiRoom(ctx *HubContext, room *RoomState, now int64) {
	if room == nil || room.BoardState == nil || room.BoardState.Phase != "playing" {
		return
	}
	bs := room.BoardState
	if bs.ShisanshuiSubmissions == nil {
		bs.ShisanshuiSubmissions = map[string]ShisanshuiSubmission{}
	}
	totalSeats := getSSSTotalSeats(room)
	submittedCount := len(bs.ShisanshuiSubmissions)
	if submittedCount >= totalSeats {
		settleShisanshuiRound(ctx, room, false)
		return
	}
	onlyAutoRemaining := submittedCount > 0
	if onlyAutoRemaining {
		for s := 0; s < totalSeats; s++ {
			if _, ok := bs.ShisanshuiSubmissions[itoa(s)]; ok {
				continue
			}
			var pl *ScenePlayer
			for i := range room.Players {
				if room.Players[i].Seat == s {
					pl = &room.Players[i]
					break
				}
			}
			isAutoSeat := (pl != nil && (pl.IsAutoManaged || pl.IsDisconnected || isBotLikePlayer(*pl))) ||
				(bs.AutoManagedSeats != nil && bs.AutoManagedSeats[s])
			if pl != nil && !isAutoSeat {
				onlyAutoRemaining = false
				break
			}
		}
	}
	if onlyAutoRemaining || (room.LastTurnTs > 0 && now-room.LastTurnTs > 60000) {
		room.LastTurnTs = now
		settleShisanshuiRound(ctx, room, !onlyAutoRemaining)
	}
}
