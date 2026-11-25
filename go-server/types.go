package main

import (
	"crypto/subtle"
	"encoding/json"
	"strings"
)

// ==============================================================================
// 1. Core Card, Tile & Game Data Structures
// ==============================================================================

type Card struct {
	ID      string `json:"id"`
	Rank    int    `json:"rank"`
	SuitVal int    `json:"suitVal"`
	Label   string `json:"label"`
	Suit    string `json:"suit"`
	IsRed   bool   `json:"isRed"`
	Hidden  bool   `json:"hidden,omitempty"`
}

type MahjongTile struct {
	ID          string `json:"id"`
	Type        string `json:"type"`  // "wan" | "tiao" | "tong" | "zi"
	Value       int    `json:"value"` // 1..9 or 1..7
	Text        string `json:"text"`
	Color       string `json:"color,omitempty"`
	IsRedDragon bool   `json:"isRedDragon,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
	DiscardBy   *int   `json:"discardBy,omitempty"`
}

type YansanCombo struct {
	Type      string `json:"type"` // "single" | "pair" | "triplet" | "5_card"
	ComboTier int    `json:"comboTier,omitempty"`
	Rank      int    `json:"rank"`
	SuitVal   int    `json:"suitVal,omitempty"`
	Length    int    `json:"length,omitempty"`
	Cards     []Card `json:"cards"`
	Seat      *int   `json:"seat,omitempty"`
	Label     string `json:"label,omitempty"`
}

type YansanPlayRecord struct {
	Seat        int          `json:"seat"`
	Cards       []Card       `json:"cards"`
	Combo       *YansanCombo `json:"combo"`
	TargetCombo *YansanCombo `json:"targetCombo"`
	IsPass      bool         `json:"isPass"`
	ActionSeq   int          `json:"actionSeq,omitempty"`
	Timestamp   int64        `json:"timestamp,omitempty"`
}

type YansanSettlement struct {
	WinnerSeat     int    `json:"winnerSeat"`
	WinnerAlliance string `json:"winnerAlliance"`
	IsQiangGong    bool   `json:"isQiangGong"`
	QiangGongSeat  *int   `json:"qiangGongSeat"`
	SeatScores     [4]int `json:"seatScores"`
	Summary        string `json:"summary"`
	IsTie          bool   `json:"isTie,omitempty"`
	Spade3Seat     int    `json:"spade3Seat,omitempty"`
	SpadeASeat     int    `json:"spadeASeat,omitempty"`
	SoloSeat       *int   `json:"soloSeat,omitempty"`
}

type MahjongMeld struct {
	Type     string        `json:"type"` // "peng" | "gang"
	Tile     MahjongTile   `json:"tile"`
	Tiles    []MahjongTile `json:"tiles"`
	Seat     int           `json:"seat"`
	GangType string        `json:"gangType,omitempty"` // "ming" | "an" | "bu"
}

type MahjongClaimWindow struct {
	DiscardID     string `json:"discardId"`
	DiscarderSeat int    `json:"discarderSeat"`
	PendingSeats  []int  `json:"pendingSeats"`
	PassedSeats   []int  `json:"passedSeats"`
	CreatedAt     int64  `json:"createdAt"`
}

type MahjongKongRecord struct {
	GangType string       `json:"gangType"`
	Seat     int          `json:"seat"`
	FromSeat *int         `json:"fromSeat"`
	Tile     MahjongTile  `json:"tile"`
	Deltas   [4]int       `json:"deltas"`
}

type SSSSpecialHand struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	BonusWater int    `json:"bonusWater"`
	Desc       string `json:"desc"`
}

type SSSTiers struct {
	Head []Card `json:"head"`
	Mid  []Card `json:"mid"`
	Tail []Card `json:"tail"`
}

type SSSRawTiers struct {
	Front       []Card          `json:"front"`
	Mid         []Card          `json:"mid"`
	Back        []Card          `json:"back"`
	FrontLabel  string          `json:"frontLabel,omitempty"`
	MidLabel    string          `json:"midLabel,omitempty"`
	BackLabel   string          `json:"backLabel,omitempty"`
	SpecialHand *SSSSpecialHand `json:"specialHand,omitempty"`
}

type ShisanshuiSubmission struct {
	Seat          int          `json:"seat"`
	Tiers         *SSSTiers    `json:"tiers,omitempty"`
	RawTiers      *SSSRawTiers `json:"rawTiers,omitempty"`
	IsSpecial     bool         `json:"isSpecial"`
	AutoSubmitted bool         `json:"autoSubmitted,omitempty"`
	SubmittedAt   int64        `json:"submittedAt"`
	Hidden        bool         `json:"hidden,omitempty"`
}

type RoomDeal struct {
	GameType         string                `json:"gameType"`
	SceneMode        string                `json:"sceneMode,omitempty"`
	DealID           string                `json:"dealId"`
	DeckCount        int                   `json:"deckCount,omitempty"`
	StarterSeat      int                   `json:"starterSeat,omitempty"`
	DealerSeat       int                   `json:"dealerSeat,omitempty"`
	Spade3Seat       *int                  `json:"spade3Seat,omitempty"`
	SpadeASeat       *int                  `json:"spadeASeat,omitempty"`
	Dice             map[string]int        `json:"dice,omitempty"`
	Deck             []Card                `json:"deck,omitempty"`
	SeatCards        map[int][]Card        `json:"seatCards,omitempty"`
	InitialSeatCards [][]Card              `json:"initialSeatCards,omitempty"`
	MahjongSeatCards map[int][]MahjongTile `json:"mahjongSeatCards,omitempty"`
	RemainingWall    []MahjongTile         `json:"remainingWall,omitempty"`
	WallCount        int                   `json:"wallCount,omitempty"`
	CreatedAt        int64                 `json:"createdAt"`
}

type ExtraBoardState struct {
	PlayedCardsHistory []Card             `json:"playedCardsHistory,omitempty"`
	SeatPlayedCards    [][]Card           `json:"seatPlayedCards,omitempty"`
	PlayHistory        []YansanPlayRecord `json:"playHistory,omitempty"`
	HasPassedSeats     []bool             `json:"hasPassedSeats,omitempty"`
	ConsecutivePasses  int                `json:"consecutivePasses,omitempty"`
	IsQiangGongActive  bool               `json:"isQiangGongActive,omitempty"`
	QiangGongSeat      *int               `json:"qiangGongSeat,omitempty"`

	MahjongDiscards []MahjongTile       `json:"mahjongDiscards,omitempty"`
	MahjongMelds    [][]MahjongMeld     `json:"mahjongMelds,omitempty"`
	LastDiscarded   *MahjongTile        `json:"lastDiscarded,omitempty"`
	LastDrawnSeat   *int                `json:"lastDrawnSeat,omitempty"`
	LastDrawnTile   *MahjongTile        `json:"lastDrawnTile,omitempty"`
	PendingPengSeat *int                `json:"pendingPengSeat,omitempty"`
	ClaimWindow     *MahjongClaimWindow `json:"claimWindow,omitempty"`
	KongHistory     []MahjongKongRecord `json:"kongHistory,omitempty"`
	WallRemaining   int                 `json:"wallRemaining,omitempty"`
}

type AuthoritativeBoardState struct {
	RoomID                string                          `json:"roomId"`
	GameType              string                          `json:"gameType"`
	Phase                 string                          `json:"phase"`
	TurnSeat              int                             `json:"turnSeat"`
	ActionSeq             int                             `json:"actionSeq"`
	MoveCount             int                             `json:"moveCount"`
	Multiplier            int                             `json:"multiplier"`
	HandsCount            []int                           `json:"handsCount"`
	LastCombo             json.RawMessage                 `json:"lastCombo"`
	WinnerSeat            *int                            `json:"winnerSeat"`
	IsDraw                bool                            `json:"isDraw,omitempty"`
	WinDesc               string                          `json:"winDesc,omitempty"`
	FinishedSeats         []int                           `json:"finishedSeats,omitempty"`
	DiscardsHistory       []interface{}                   `json:"discardsHistory,omitempty"`
	SeatKongScores        []int                           `json:"seatKongScores,omitempty"`
	AutoManagedSeats      map[int]bool                    `json:"autoManagedSeats,omitempty"`
	ShisanshuiSubmissions map[string]ShisanshuiSubmission `json:"shisanshuiSubmissions,omitempty"`
	ShisanshuiSettlement  interface{}                     `json:"shisanshuiSettlement,omitempty"`
	Settlement            interface{}                     `json:"settlement,omitempty"`
	AllianceScoreResult   interface{}                     `json:"allianceScoreResult,omitempty"`
	ExtraState            *ExtraBoardState                `json:"extraState,omitempty"`
	UpdatedAt             int64                           `json:"updatedAt"`
}

type ScenePlayer struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Avatar        string `json:"avatar"`
	Rating        int    `json:"rating"`
	Coins         int    `json:"coins"`
	Seat          int    `json:"seat"`
	IsSimulated   bool   `json:"isSimulated,omitempty"`
	IsAi          bool   `json:"isAi,omitempty"`
	IsBot         bool   `json:"isBot,omitempty"`
	IsReady       bool   `json:"isReady,omitempty"`
	SocketID      string `json:"socketId,omitempty"`
	IsDisconnected bool  `json:"isDisconnected,omitempty"`
	IsAutoManaged bool   `json:"isAutoManaged,omitempty"`
	OriginalID    string `json:"originalId,omitempty"`
	OriginalName  string `json:"originalName,omitempty"`
	UserID        string `json:"userId,omitempty"`
}

type SceneWaitingRoom struct {
	ID                  string        `json:"id"`
	GameType            string        `json:"gameType"`
	SceneMode           string        `json:"sceneMode"`
	SceneName           string        `json:"sceneName"`
	MaxPlayers          int           `json:"maxPlayers"`
	Players             []ScenePlayer `json:"players"`
	Status              string        `json:"status"`
	CreatedAt           int64         `json:"createdAt"`
	LastTurnTs          int64         `json:"lastTurnTs,omitempty"`
	RoundIndex          int           `json:"roundIndex,omitempty"`
	SettlementExpiresAt int64         `json:"settlementExpiresAt,omitempty"`
	Deal                *RoomDeal     `json:"-"`
}

type RoomChatMessage struct {
	ID         string  `json:"id"`
	Sender     string  `json:"sender"`
	SenderID   *string `json:"senderId,omitempty"`
	Avatar     string  `json:"avatar,omitempty"`
	Text       string  `json:"text"`
	Prop       *string `json:"prop,omitempty"`
	TargetSeat *int    `json:"targetSeat,omitempty"`
	Voice      bool    `json:"voice,omitempty"`
	Audio      *string `json:"audio,omitempty"`
	AudioURL   *string `json:"audioUrl,omitempty"`
	Duration   int     `json:"duration,omitempty"`
	Timestamp  string  `json:"timestamp"`
}

type RoomState struct {
	ID                  string                   `json:"id"`
	Title               string                   `json:"title"`
	GameType            string                   `json:"gameType"`
	SceneMode           string                   `json:"sceneMode,omitempty"`
	MaxPlayers          int                      `json:"maxPlayers,omitempty"`
	Status              string                   `json:"status"`
	HasPasscode         bool                     `json:"hasPasscode,omitempty"`
	Passcode            string                   `json:"-"`
	Players             []ScenePlayer            `json:"players"`
	Spectators          int                      `json:"spectators"`
	TurnSeat            int                      `json:"turnSeat"`
	LastTurnTs          int64                    `json:"lastTurnTs,omitempty"`
	MoveCount           int                      `json:"moveCount"`
	RoundIndex          int                      `json:"roundIndex,omitempty"`
	ModeLabel           string                   `json:"modeLabel"`
	Chat                []RoomChatMessage        `json:"chat"`
	BoardState          *AuthoritativeBoardState `json:"boardState,omitempty"`
	SettlementExpiresAt int64                    `json:"settlementExpiresAt,omitempty"`
	CreatedAt           int64                    `json:"createdAt,omitempty"`
	CoinsSettledKey     string                   `json:"-"`
	Deal                *RoomDeal                `json:"-"`
}

type ClientPresenceInfo struct {
	SocketID   string `json:"socketId"`
	GameType   string `json:"gameType"`
	PlayerID   string `json:"playerId,omitempty"`
	PlayerName string `json:"playerName,omitempty"`
	Avatar     string `json:"avatar,omitempty"`
	Coins      *int   `json:"coins,omitempty"`
	Rating     *int   `json:"rating,omitempty"`
	Phone      string `json:"phone,omitempty"`
	SceneMode  string `json:"sceneMode,omitempty"`
	LastSeen   int64  `json:"lastSeen"`
}

type TeahousePlayerItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Avatar    string `json:"avatar"`
	Coins     int    `json:"coins"`
	Rating    int    `json:"rating"`
	Level     int    `json:"level"`
	Phone     string `json:"phone,omitempty"`
	IsAdmin   bool   `json:"isAdmin,omitempty"`
	IsOnline  bool   `json:"isOnline"`
	GameType  string `json:"gameType"`
	SceneMode string `json:"sceneMode,omitempty"`
	LastSeen  int64  `json:"lastSeen,omitempty"`
}

type SceneWaitingPlayerSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
	Coins  int    `json:"coins"`
}

type SceneWaitingStat struct {
	GameType          string                      `json:"gameType"`
	SceneMode         string                      `json:"sceneMode"`
	WaitingCount      int                         `json:"waitingCount"`
	PlayingCount      int                         `json:"playingCount"`
	TotalInScene      int                         `json:"totalInScene"`
	ActiveTablesCount int                         `json:"activeTablesCount"`
	WaitingPlayers    []SceneWaitingPlayerSummary `json:"waitingPlayers"`
}

type RealPresenceCounts struct {
	Yansan            int                          `json:"yansan"`
	Shisanshui        int                          `json:"shisanshui"`
	Mahjong           int                          `json:"mahjong"`
	TotalOnline       int                          `json:"totalOnline"`
	SceneWaitingStats map[string]*SceneWaitingStat `json:"sceneWaitingStats,omitempty"`
}

func getSceneRequiredCoins(gameType, sceneMode string) int {
	switch gameType {
	case "yansan":
		if sceneMode == "yansan-20-40" {
			return 500
		}
		if sceneMode == "yansan-10-20" {
			return 200
		}
		return 100
	case "shisanshui":
		if sceneMode == "eight-realtime" || sceneMode == "eight-booking" || sceneMode == "shisanshui-20-40" {
			return 500
		}
		if sceneMode == "shisanshui-10-20" {
			return 200
		}
		return 100
	case "mahjong":
		if sceneMode == "mahjong-20-40" {
			return 500
		}
		if sceneMode == "mahjong-10-20" || sceneMode == "hongzhong-10" {
			return 200
		}
		return 100
	default:
		return 100
	}
}

type ActiveChallengeRecord struct {
	ID         string            `json:"id"`
	SenderID   string            `json:"senderId"`
	SenderName string            `json:"senderName"`
	Avatar     string            `json:"avatar"`
	GameType   string            `json:"gameType"`
	SceneMode  string            `json:"sceneMode"`
	SceneName  string            `json:"sceneName"`
	Text       string            `json:"text"`
	Status     string            `json:"status"`
	AcceptedBy map[string]string `json:"acceptedBy,omitempty"`
	Timestamp  string            `json:"timestamp"`
	CreatedAt  int64             `json:"createdAt"`
}

// ==============================================================================
// 2. Database & User Schema
// ==============================================================================

type GameStat struct {
	Played       int `json:"played"`
	Won          int `json:"won"`
	HighestScore int `json:"highestScore"`
}

type UserProfile struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Username        string              `json:"username,omitempty"`
	Password        string              `json:"password,omitempty"`
	Avatar          string              `json:"avatar"`
	Phone           string              `json:"phone,omitempty"`
	IsAdmin         bool                `json:"isAdmin,omitempty"`
	IsBanned        bool                `json:"isBanned,omitempty"`
	BannedReason    string              `json:"bannedReason,omitempty"`
	Coins           int                 `json:"coins"`
	Rating          int                 `json:"rating"`
	Level           int                 `json:"level"`
	LastCheckinDate string              `json:"lastCheckinDate"`
	Stats           map[string]GameStat `json:"stats"`
	UpdatedAt       int64               `json:"updatedAt,omitempty"`
	RegisteredAt    int64               `json:"registeredAt,omitempty"`
	SessionToken    string              `json:"sessionToken,omitempty"`
}

type MatchRecord struct {
	ID          string `json:"id"`
	PlayerID    string `json:"playerId,omitempty"`
	GameType    string `json:"gameType"`
	GameName    string `json:"gameName"`
	Result      string `json:"result"`
	CoinsDelta  int    `json:"coinsDelta"`
	RatingDelta int    `json:"ratingDelta"`
	Details     string `json:"details"`
	Timestamp   string `json:"timestamp"`
	UpdatedAt   int64  `json:"updatedAt,omitempty"`
}

type AdminLog struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Operator  string `json:"operator"`
	Target    string `json:"target"`
	Amount    int    `json:"amount,omitempty"`
	Details   string `json:"details"`
	Timestamp string `json:"timestamp"`
}

type SystemAnnouncement struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Author    string `json:"author"`
	Timestamp string `json:"timestamp"`
}

type PointsTx struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Title        string `json:"title"`
	Amount       int    `json:"amount"`
	BalanceAfter int    `json:"balanceAfter"`
	Timestamp    string `json:"timestamp"`
	CreatedAt    int64  `json:"createdAt"`
	Meta         string `json:"meta,omitempty"`
}

type DatabaseSchema struct {
	Profiles            map[string]*UserProfile `json:"profiles"`
	Records             []MatchRecord           `json:"records"`
	AuthorizedPhones    []string                `json:"authorizedPhones"`
	AdminPhones         []string                `json:"adminPhones"`
	RevokedPhones       []string                `json:"revokedPhones,omitempty"`
	AdminLogs           []AdminLog              `json:"adminLogs"`
	SystemAnnouncements []SystemAnnouncement    `json:"systemAnnouncements"`
	PointsLedger        map[string][]PointsTx   `json:"pointsLedger,omitempty"`
}

// ==============================================================================
// 3. Sanitization & Security Helpers
// ==============================================================================

func safeTokenEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func sanitizeProfileForClient(p *UserProfile) map[string]interface{} {
	if p == nil {
		return nil
	}
	return map[string]interface{}{
		"id":              p.ID,
		"name":            p.Name,
		"username":        p.Username,
		"avatar":          p.Avatar,
		"phone":           p.Phone,
		"isAdmin":         p.IsAdmin,
		"isBanned":        p.IsBanned,
		"bannedReason":    p.BannedReason,
		"coins":           p.Coins,
		"rating":          p.Rating,
		"level":           p.Level,
		"lastCheckinDate": p.LastCheckinDate,
		"stats":           p.Stats,
		"updatedAt":       p.UpdatedAt,
		"registeredAt":    p.RegisteredAt,
		"hasPassword":     p.Password != "",
	}
}

func isBotLikePlayer(p ScenePlayer) bool {
	if p.IsAi || p.IsSimulated || p.IsBot {
		return true
	}
	return strings.HasPrefix(p.ID, "BOT-") || strings.HasPrefix(p.ID, "sim-") || strings.HasPrefix(p.ID, "P-sim-")
}

func filterHumanPlayers(players []ScenePlayer) []ScenePlayer {
	out := make([]ScenePlayer, 0, len(players))
	for _, p := range players {
		if !isBotLikePlayer(p) && (p.ID != "" || p.Name != "") {
			out = append(out, p)
		}
	}
	return out
}

func maskHiddenMahjongTile(tile *MahjongTile) *MahjongTile {
	if tile == nil {
		return nil
	}
	id := tile.ID
	if id == "" {
		id = "hidden-tile"
	}
	return &MahjongTile{
		ID:     id,
		Type:   "wan",
		Value:  1,
		Text:   "一万",
		Color:  "#DC2626",
		Hidden: true,
	}
}

func sanitizeDealForSeat(deal *RoomDeal, recipientSeat int) map[string]interface{} {
	if deal == nil {
		return nil
	}
	res := map[string]interface{}{
		"gameType":  deal.GameType,
		"dealId":    deal.DealID,
		"createdAt": deal.CreatedAt,
	}
	if deal.SceneMode != "" {
		res["sceneMode"] = deal.SceneMode
	}
	if deal.DeckCount > 0 {
		res["deckCount"] = deal.DeckCount
	}
	if deal.StarterSeat >= 0 {
		res["starterSeat"] = deal.StarterSeat
	}
	if deal.GameType == "mahjong" {
		res["dealerSeat"] = deal.DealerSeat
		if deal.Dice != nil {
			res["dice"] = deal.Dice
		}
		seatTiles := map[int][]MahjongTile{}
		for s, hand := range deal.MahjongSeatCards {
			if s == recipientSeat {
				cp := make([]MahjongTile, len(hand))
				copy(cp, hand)
				seatTiles[s] = cp
			} else {
				masked := make([]MahjongTile, len(hand))
				for idx := range hand {
					masked[idx] = MahjongTile{
						ID:     "hidden-" + itoa(s) + "-" + itoa(idx),
						Type:   "wan",
						Value:  1,
						Text:   "一万",
						Color:  "#DC2626",
						Hidden: true,
					}
				}
				seatTiles[s] = masked
			}
		}
		res["seatCards"] = seatTiles
		maskedWall := make([]MahjongTile, len(deal.RemainingWall))
		for idx := range deal.RemainingWall {
			maskedWall[idx] = MahjongTile{
				ID:     "hidden-wall-" + itoa(idx),
				Type:   "wan",
				Value:  1,
				Text:   "一万",
				Color:  "#DC2626",
				Hidden: true,
			}
		}
		res["remainingWall"] = maskedWall
		res["wallCount"] = len(deal.RemainingWall)
		return res
	}

	seatCards := map[int][]Card{}
	for s, hand := range deal.SeatCards {
		if s == recipientSeat {
			cp := make([]Card, len(hand))
			copy(cp, hand)
			seatCards[s] = cp
		} else {
			masked := make([]Card, len(hand))
			for idx := range hand {
				masked[idx] = Card{
					ID:      "hidden-" + itoa(s) + "-" + itoa(idx),
					Rank:    0,
					SuitVal: 0,
					Label:   "?",
					Suit:    "?",
					IsRed:   false,
					Hidden:  true,
				}
			}
			seatCards[s] = masked
		}
	}
	res["seatCards"] = seatCards
	return res
}

func sanitizeBoardStateForSeat(bs *AuthoritativeBoardState, gameType string, recipientSeat int) *AuthoritativeBoardState {
	if bs == nil || bs.Phase == "ended" {
		return bs
	}
	if gameType == "shisanshui" && len(bs.ShisanshuiSubmissions) > 0 {
		clone := *bs
		masked := make(map[string]ShisanshuiSubmission, len(bs.ShisanshuiSubmissions))
		for k, sub := range bs.ShisanshuiSubmissions {
			if sub.Seat == recipientSeat {
				masked[k] = sub
			} else {
				masked[k] = ShisanshuiSubmission{
					Seat:          sub.Seat,
					IsSpecial:     sub.IsSpecial,
					AutoSubmitted: sub.AutoSubmitted,
					SubmittedAt:   sub.SubmittedAt,
					Hidden:        true,
				}
			}
		}
		clone.ShisanshuiSubmissions = masked
		return &clone
	}
	if gameType == "mahjong" && bs.ExtraState != nil && bs.ExtraState.LastDrawnTile != nil {
		if bs.ExtraState.LastDrawnSeat != nil && *bs.ExtraState.LastDrawnSeat == recipientSeat {
			return bs
		}
		clone := *bs
		exCopy := *bs.ExtraState
		exCopy.LastDrawnTile = maskHiddenMahjongTile(bs.ExtraState.LastDrawnTile)
		clone.ExtraState = &exCopy
		return &clone
	}
	return bs
}

func createDefaultBoardState(roomID, gameType string) *AuthoritativeBoardState {
	if gameType == "" {
		gameType = "yansan"
	}
	hands := []int{13, 13, 13, 13}
	if gameType == "shisanshui" && strings.Contains(roomID, "eight") {
		hands = []int{13, 13, 13, 13, 13, 13, 13, 13}
	}
	bs := &AuthoritativeBoardState{
		RoomID:          roomID,
		GameType:        gameType,
		Phase:           "playing",
		TurnSeat:        0,
		ActionSeq:       1,
		MoveCount:       0,
		Multiplier:      1,
		HandsCount:      hands,
		DiscardsHistory: []interface{}{},
		UpdatedAt:       nowMs(),
	}
	switch gameType {
	case "mahjong":
		bs.SeatKongScores = []int{0, 0, 0, 0}
		bs.ExtraState = &ExtraBoardState{
			MahjongDiscards: []MahjongTile{},
			MahjongMelds:    [][]MahjongMeld{{}, {}, {}, {}},
			KongHistory:     []MahjongKongRecord{},
			WallRemaining:   83,
		}
	case "yansan":
		bs.ExtraState = &ExtraBoardState{
			PlayedCardsHistory: []Card{},
			SeatPlayedCards:    [][]Card{{}, {}, {}, {}},
			PlayHistory:        []YansanPlayRecord{},
			HasPassedSeats:     []bool{false, false, false, false},
		}
	default:
		bs.ShisanshuiSubmissions = map[string]ShisanshuiSubmission{}
		bs.ExtraState = &ExtraBoardState{}
	}
	return bs
}
