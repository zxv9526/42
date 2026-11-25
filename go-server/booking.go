package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ==============================================================================
// Shisanshui Booking Train (十三张预约专列 10桌车厢赛 & 每日06:00权威结算)
// ==============================================================================

const (
	bookingStoreFile           = "data/booking_carriages_v2.json"
	bookingPredealtTarget      = 50
	bookingPredealtReplenish   = 10
	bookingPointPerWater       = 1
	bookingGraceWindowMs int64 = 70000
)

var (
	bookingSeats4 = []string{"东", "南", "西", "北"}
	bookingSeats8 = []string{"东", "东南", "南", "西南", "西", "西北", "北", "东北"}
)

type BookingSubmittedHand struct {
	Front       []Card          `json:"front"`
	Mid         []Card          `json:"mid"`
	Back        []Card          `json:"back"`
	FrontLabel  string          `json:"frontLabel"`
	MidLabel    string          `json:"midLabel"`
	BackLabel   string          `json:"backLabel"`
	SpecialHand *SSSSpecialHand `json:"specialHand,omitempty"`
}

type BookingSeatOccupant struct {
	Seat           string `json:"seat"`
	PlayerID       string `json:"playerId"`
	PlayerName     string `json:"playerName"`
	Avatar         string `json:"avatar,omitempty"`
	SelectedAt     int64  `json:"selectedAt"`
	SubmittedCount int    `json:"submittedCount"`
}

type BookingOpponentSummary struct {
	OppSeat         int    `json:"oppSeat"`
	SeatLabel       string `json:"seatLabel"`
	OppName         string `json:"oppName"`
	IsRealPlayer    bool   `json:"isRealPlayer"`
	FrontLabel      string `json:"frontLabel"`
	MidLabel        string `json:"midLabel"`
	BackLabel       string `json:"backLabel"`
	SpecialHandName string `json:"specialHandName,omitempty"`
	FinalWater      int    `json:"finalWater"`
	IsShot          bool   `json:"isShot"`
	IsFoulOpp       bool   `json:"isFoulOpp"`
}

type BookingTableSettlement struct {
	IsSettled          bool                     `json:"isSettled"`
	TotalWater         int                      `json:"totalWater"`
	NetScore           int                      `json:"netScore"`
	IsGrandSlam        bool                     `json:"isGrandSlam"`
	OpponentsBreakdown []interface{}            `json:"opponentsBreakdown"`
	OpponentSummaries  []BookingOpponentSummary `json:"opponentSummaries,omitempty"`
	SettledAt          int64                    `json:"settledAt,omitempty"`
}

type ServerTableSeatSubmission struct {
	PlayerID         string                  `json:"playerId"`
	PlayerName       string                  `json:"playerName"`
	Avatar           string                  `json:"avatar,omitempty"`
	PlayerSeat       string                  `json:"playerSeat"`
	PlayerHand       BookingSubmittedHand    `json:"playerHand"`
	SubmittedAt      int64                   `json:"submittedAt"`
	GraceFor0600Cycle string                 `json:"graceFor0600Cycle,omitempty"`
	SettledCycleKey  string                  `json:"settledCycleKey,omitempty"`
	Settlement       *BookingTableSettlement `json:"settlement,omitempty"`
}

type ServerMultiBookingTable struct {
	TableIndex      int                                  `json:"tableIndex"`
	TableName       string                               `json:"tableName"`
	TableDeckSeed   string                               `json:"tableDeckSeed"`
	SeatCards       map[string][]Card                    `json:"seatCards"`
	SeatSubmissions map[string]ServerTableSeatSubmission `json:"seatSubmissions"`
}

type ServerPlayerCarriageProgress struct {
	PlayerID                string                           `json:"playerId"`
	PlayerName              string                           `json:"playerName"`
	Avatar                  string                           `json:"avatar,omitempty"`
	SelectedSeat            string                           `json:"selectedSeat"`
	UnplayedTableIndices    []int                            `json:"unplayedTableIndices"`
	CurrentActiveTableIndex *int                             `json:"currentActiveTableIndex"`
	IsCompleted             bool                             `json:"isCompleted"`
	CompletedAt             int64                            `json:"completedAt,omitempty"`
	SettledCycleKey         string                           `json:"settledCycleKey,omitempty"`
	SimulationCache         map[int]*BookingTableSettlement  `json:"simulationCache,omitempty"`
}

type PredealtTableHands struct {
	TableIndex    int               `json:"tableIndex"`
	TableName     string            `json:"tableName"`
	TableDeckSeed string            `json:"tableDeckSeed"`
	SeatCards     map[string][]Card `json:"seatCards"`
}

type PredealtCarriageUnit struct {
	PredealtID string               `json:"predealtId"`
	Seq        int                  `json:"seq"`
	SceneMode  string               `json:"sceneMode"`
	CreatedAt  int64                `json:"createdAt"`
	Tables     []PredealtTableHands `json:"tables"`
}

type ServerMultiBookingCarriage struct {
	CarriageID     string                                   `json:"carriageId"`
	PredealtID     string                                   `json:"predealtId,omitempty"`
	CarriageNumber int                                      `json:"carriageNumber"`
	GroupIndex     int                                      `json:"groupIndex"`
	SceneMode      string                                   `json:"sceneMode"`
	CreatedAt      int64                                    `json:"createdAt"`
	SeatOccupants  map[string]*BookingSeatOccupant          `json:"seatOccupants"`
	PlayerProgress map[string]*ServerPlayerCarriageProgress `json:"playerProgress"`
	Tables         []*ServerMultiBookingTable               `json:"tables"`
}

type BookingTableSummaryItem struct {
	TableIndex      int    `json:"tableIndex"`
	PlayerSeat      string `json:"playerSeat"`
	TotalWater      int    `json:"totalWater"`
	SpecialHandName string `json:"specialHandName,omitempty"`
}

type BookingSettlementRecord struct {
	ID                 string                    `json:"id"`
	DateStr            string                    `json:"dateStr"`
	SceneMode          string                    `json:"sceneMode"`
	CarriageNumber     int                       `json:"carriageNumber"`
	SettledTablesCount int                       `json:"settledTablesCount"`
	TotalWater         int                       `json:"totalWater"`
	NetScore           int                       `json:"netScore"`
	SettledAt          int64                     `json:"settledAt"`
	IsSimulation       bool                      `json:"isSimulation"`
	SettledCycleKey    string                    `json:"settledCycleKey,omitempty"`
	CoinsCredited      bool                      `json:"coinsCredited,omitempty"`
	TablesSummary      []BookingTableSummaryItem `json:"tablesSummary"`
}

type ServerBookingStore struct {
	Version               int                                   `json:"version"`
	UpdatedAt             int64                                 `json:"updatedAt"`
	Carriages             map[string]*ServerMultiBookingCarriage `json:"carriages"`
	UserCurrentCarriage   map[string]int                        `json:"userCurrentCarriage"`
	UserCurrentCarriageID map[string]string                     `json:"userCurrentCarriageId"`
	UserSettlementHistory map[string][]BookingSettlementRecord  `json:"userSettlementHistory"`
	UserUnreadSettlements map[string][]BookingSettlementRecord  `json:"userUnreadSettlements"`
	PredealtPool          map[string][]PredealtCarriageUnit     `json:"predealtPool"`
	PredealtSeq           map[string]int                        `json:"predealtSeq"`
}

// Client-facing projected table / carriage views (matches shisanshuiBookingTrain.ts)
type ClientBookingTable struct {
	TableIndex                  int                     `json:"tableIndex"`
	TableName                   string                  `json:"tableName"`
	IsSubmitted                 bool                    `json:"isSubmitted"`
	SubmittedAt                 int64                   `json:"submittedAt,omitempty"`
	PlayerSeat                  string                  `json:"playerSeat"`
	PlayerHand                  *BookingSubmittedHand   `json:"playerHand,omitempty"`
	TableDeckSeed               string                  `json:"tableDeckSeed"`
	SeatCards                   map[string][]Card       `json:"seatCards"`
	Settlement                  *BookingTableSettlement `json:"settlement,omitempty"`
	RealOpponentsSubmittedSeats []string                `json:"realOpponentsSubmittedSeats,omitempty"`
	SettledCycleKey             string                  `json:"settledCycleKey,omitempty"`
	GraceFor0600Cycle           string                  `json:"graceFor0600Cycle,omitempty"`
}

type ClientBookingCarriage struct {
	CarriageID              string                          `json:"carriageId,omitempty"`
	PredealtID              string                          `json:"predealtId,omitempty"`
	CarriageNumber          int                             `json:"carriageNumber"`
	GroupIndex              int                             `json:"groupIndex,omitempty"`
	SceneMode               string                          `json:"sceneMode"`
	SelectedSeat            *string                         `json:"selectedSeat"`
	Tables                  []ClientBookingTable            `json:"tables"`
	UnplayedTableIndices    []int                           `json:"unplayedTableIndices"`
	CurrentActiveTableIndex *int                            `json:"currentActiveTableIndex"`
	IsCompleted             bool                            `json:"isCompleted"`
	CompletedAt             int64                           `json:"completedAt,omitempty"`
	SeatOccupants           map[string]*BookingSeatOccupant `json:"seatOccupants,omitempty"`
	SettledCycleKey         string                          `json:"settledCycleKey,omitempty"`
}

type BookingSettledNotification struct {
	UserID string                  `json:"userId"`
	Coins  int                     `json:"coins"`
	Delta  int                     `json:"delta"`
	Rating int                     `json:"rating"`
	Level  int                     `json:"level"`
	Record BookingSettlementRecord `json:"record"`
}

type BookingManager struct {
	mu      sync.Mutex
	storage *StorageManager
}

func newBookingManager(sm *StorageManager) *BookingManager {
	return &BookingManager{storage: sm}
}

func normalizeBookingSceneMode(raw string) string {
	if raw == "eight-booking" {
		return "eight-booking"
	}
	return "four-booking"
}

func getBookingSeats(sceneMode string) []string {
	if sceneMode == "eight-booking" {
		return bookingSeats8
	}
	return bookingSeats4
}

func buildBookingCarriageID(sceneMode string, carriageNumber, groupIndex int) string {
	return sceneMode + "_car_" + itoa(carriageNumber) + "_g" + itoa(groupIndex)
}

func bookingUserModeKey(userID, sceneMode string) string {
	return userID + "::" + sceneMode
}

func defaultBookingStore() *ServerBookingStore {
	return &ServerBookingStore{
		Version:               3,
		UpdatedAt:             nowMs(),
		Carriages:             map[string]*ServerMultiBookingCarriage{},
		UserCurrentCarriage:   map[string]int{},
		UserCurrentCarriageID: map[string]string{},
		UserSettlementHistory: map[string][]BookingSettlementRecord{},
		UserUnreadSettlements: map[string][]BookingSettlementRecord{},
		PredealtPool: map[string][]PredealtCarriageUnit{
			"four-booking":  {},
			"eight-booking": {},
		},
		PredealtSeq: map[string]int{
			"four-booking":  0,
			"eight-booking": 0,
		},
	}
}

func (bm *BookingManager) loadStoreLocked() *ServerBookingStore {
	for _, cand := range []string{bookingStoreFile, bookingStoreFile + ".bak"} {
		raw, err := os.ReadFile(cand)
		if err != nil {
			continue
		}
		var st ServerBookingStore
		if err := json.Unmarshal(raw, &st); err == nil {
			if st.Carriages == nil {
				st.Carriages = map[string]*ServerMultiBookingCarriage{}
			}
			if st.UserCurrentCarriage == nil {
				st.UserCurrentCarriage = map[string]int{}
			}
			if st.UserCurrentCarriageID == nil {
				st.UserCurrentCarriageID = map[string]string{}
			}
			if st.UserSettlementHistory == nil {
				st.UserSettlementHistory = map[string][]BookingSettlementRecord{}
			}
			if st.UserUnreadSettlements == nil {
				st.UserUnreadSettlements = map[string][]BookingSettlementRecord{}
			}
			if st.PredealtPool == nil {
				st.PredealtPool = map[string][]PredealtCarriageUnit{}
			}
			if st.PredealtPool["four-booking"] == nil {
				st.PredealtPool["four-booking"] = []PredealtCarriageUnit{}
			}
			if st.PredealtPool["eight-booking"] == nil {
				st.PredealtPool["eight-booking"] = []PredealtCarriageUnit{}
			}
			if st.PredealtSeq == nil {
				st.PredealtSeq = map[string]int{"four-booking": 0, "eight-booking": 0}
			}
			return &st
		}
	}
	return defaultBookingStore()
}

func (bm *BookingManager) saveStoreLocked(st *ServerBookingStore) {
	st.Version = 3
	st.UpdatedAt = nowMs()
	_ = os.MkdirAll(filepath.Dir(bookingStoreFile), 0755)
	if _, err := os.Stat(bookingStoreFile); err == nil {
		if old, err := os.ReadFile(bookingStoreFile); err == nil {
			_ = os.WriteFile(bookingStoreFile+".bak", old, 0644)
		}
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	tmp := bookingStoreFile + ".tmp." + randomHex(3)
	if os.WriteFile(tmp, raw, 0644) == nil {
		_ = os.Rename(tmp, bookingStoreFile)
	}
}

func generatePredealtUnit(st *ServerBookingStore, sceneMode string) PredealtCarriageUnit {
	seq := st.PredealtSeq[sceneMode] + 1
	st.PredealtSeq[sceneMode] = seq
	seats := getBookingSeats(sceneMode)
	maxP := len(seats)

	tables := make([]PredealtTableHands, 10)
	for t := 0; t < 10; t++ {
		deal := createShisanshuiDeal(sceneMode, maxP)
		seatMap := map[string][]Card{}
		for idx, sl := range seats {
			hand := deal.SeatCards[idx]
			seatMap[sl] = hand
		}
		tables[t] = PredealtTableHands{
			TableIndex:    t + 1,
			TableName:     "第 " + itoa(t+1) + " 桌",
			TableDeckSeed: deal.DealID,
			SeatCards:     seatMap,
		}
	}
	return PredealtCarriageUnit{
		PredealtID: sceneMode + "-pd-" + itoa(seq) + "-" + randomHex(3),
		Seq:        seq,
		SceneMode:  sceneMode,
		CreatedAt:  nowMs(),
		Tables:     tables,
	}
}

func ensurePredealtPool(st *ServerBookingStore, sceneMode string) {
	pool := st.PredealtPool[sceneMode]
	if len(pool) < bookingPredealtReplenish {
		for len(pool) < bookingPredealtTarget {
			pool = append(pool, generatePredealtUnit(st, sceneMode))
		}
		st.PredealtPool[sceneMode] = pool
	}
}

func (bm *BookingManager) InitInventory() (int, int) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	st := bm.loadStoreLocked()
	ensurePredealtPool(st, "four-booking")
	ensurePredealtPool(st, "eight-booking")
	bm.saveStoreLocked(st)
	return len(st.PredealtPool["four-booking"]), len(st.PredealtPool["eight-booking"])
}

func createCarriageEntity(st *ServerBookingStore, sceneMode string, carriageNumber, groupIndex int) *ServerMultiBookingCarriage {
	ensurePredealtPool(st, sceneMode)
	pool := st.PredealtPool[sceneMode]
	var unit PredealtCarriageUnit
	if len(pool) > 0 {
		unit = pool[0]
		st.PredealtPool[sceneMode] = pool[1:]
		ensurePredealtPool(st, sceneMode)
	} else {
		unit = generatePredealtUnit(st, sceneMode)
	}

	tables := make([]*ServerMultiBookingTable, len(unit.Tables))
	for i, pt := range unit.Tables {
		tables[i] = &ServerMultiBookingTable{
			TableIndex:      pt.TableIndex,
			TableName:       pt.TableName,
			TableDeckSeed:   pt.TableDeckSeed,
			SeatCards:       pt.SeatCards,
			SeatSubmissions: map[string]ServerTableSeatSubmission{},
		}
	}
	cid := buildBookingCarriageID(sceneMode, carriageNumber, groupIndex)
	car := &ServerMultiBookingCarriage{
		CarriageID:     cid,
		PredealtID:     unit.PredealtID,
		CarriageNumber: carriageNumber,
		GroupIndex:     groupIndex,
		SceneMode:      sceneMode,
		CreatedAt:      nowMs(),
		SeatOccupants:  map[string]*BookingSeatOccupant{},
		PlayerProgress: map[string]*ServerPlayerCarriageProgress{},
		Tables:         tables,
	}
	st.Carriages[cid] = car
	return car
}

func getOrAssignCarriageForUser(st *ServerBookingStore, sceneMode, userID string, requestedCarriageID string, requestedNum int, createFresh bool) *ServerMultiBookingCarriage {
	uKey := bookingUserModeKey(userID, sceneMode)
	if requestedCarriageID != "" && !createFresh {
		if c, ok := st.Carriages[requestedCarriageID]; ok && c != nil && c.SceneMode == sceneMode {
			return c
		}
	}
	if !createFresh && requestedNum <= 0 {
		if cid := st.UserCurrentCarriageID[uKey]; cid != "" {
			if c, ok := st.Carriages[cid]; ok && c != nil && c.SceneMode == sceneMode {
				return c
			}
		}
	}

	targetNum := requestedNum
	if targetNum <= 0 {
		if prev := st.UserCurrentCarriage[uKey]; prev > 0 {
			targetNum = prev
		} else {
			targetNum = 1
		}
	}

	if !createFresh {
		// Look for carriage where user already has progress at targetNum
		for _, c := range st.Carriages {
			if c != nil && c.SceneMode == sceneMode && c.CarriageNumber == targetNum {
				if _, has := c.PlayerProgress[userID]; has {
					st.UserCurrentCarriage[uKey] = c.CarriageNumber
					st.UserCurrentCarriageID[uKey] = c.CarriageID
					return c
				}
			}
		}
	}

	seats := getBookingSeats(sceneMode)
	// Find non-full cohort at targetNum (preferably not full, and without user if createFresh)
	var candidates []*ServerMultiBookingCarriage
	for _, c := range st.Carriages {
		if c != nil && c.SceneMode == sceneMode && c.CarriageNumber == targetNum {
			candidates = append(candidates, c)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].GroupIndex < candidates[j].GroupIndex
	})

	maxGroup := 0
	for _, c := range candidates {
		if c.GroupIndex > maxGroup {
			maxGroup = c.GroupIndex
		}
		if createFresh {
			if _, has := c.PlayerProgress[userID]; has {
				continue
			}
		}
		if len(c.SeatOccupants) < len(seats) {
			st.UserCurrentCarriage[uKey] = c.CarriageNumber
			st.UserCurrentCarriageID[uKey] = c.CarriageID
			return c
		}
	}

	car := createCarriageEntity(st, sceneMode, targetNum, maxGroup+1)
	st.UserCurrentCarriage[uKey] = car.CarriageNumber
	st.UserCurrentCarriageID[uKey] = car.CarriageID
	return car
}

func projectCarriageForUser(car *ServerMultiBookingCarriage, userID string, isSim bool) ClientBookingCarriage {
	prog := car.PlayerProgress[userID]
	var selectedSeat *string
	unplayed := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	var activeIdx *int
	isCompleted := false
	var completedAt int64
	settledCycleKey := ""

	if prog != nil {
		if prog.SelectedSeat != "" {
			s := prog.SelectedSeat
			selectedSeat = &s
		}
		if prog.UnplayedTableIndices != nil {
			unplayed = append([]int{}, prog.UnplayedTableIndices...)
		}
		activeIdx = prog.CurrentActiveTableIndex
		isCompleted = prog.IsCompleted
		completedAt = prog.CompletedAt
		settledCycleKey = prog.SettledCycleKey
	}

	tables := make([]ClientBookingTable, len(car.Tables))
	for i, tbl := range car.Tables {
		seatLabel := ""
		if selectedSeat != nil {
			seatLabel = *selectedSeat
		}
		var sub *ServerTableSeatSubmission
		if seatLabel != "" {
			if s, ok := tbl.SeatSubmissions[seatLabel]; ok && s.PlayerID == userID {
				tmp := s
				sub = &tmp
			}
		}

		// Mask other seats' cards until submitted or settled
		projectedCards := map[string][]Card{}
		for sl, cards := range tbl.SeatCards {
			if seatLabel == sl || (sub != nil && sub.Settlement != nil) {
				projectedCards[sl] = cards
			} else {
				masked := make([]Card, len(cards))
				for idx := range cards {
					masked[idx] = Card{ID: "hidden-" + sl + "-" + itoa(idx), Label: "?", Suit: "?", Hidden: true}
				}
				projectedCards[sl] = masked
			}
		}

		var realOppSeats []string
		for sl := range tbl.SeatSubmissions {
			if sl != seatLabel {
				realOppSeats = append(realOppSeats, sl)
			}
		}

		var stl *BookingTableSettlement
		if sub != nil && sub.Settlement != nil {
			stl = sub.Settlement
		} else if isSim && prog != nil && prog.SimulationCache != nil {
			if simStl, ok := prog.SimulationCache[tbl.TableIndex]; ok {
				stl = simStl
			}
		}

		ct := ClientBookingTable{
			TableIndex:                  tbl.TableIndex,
			TableName:                   tbl.TableName,
			IsSubmitted:                 sub != nil,
			PlayerSeat:                  seatLabel,
			TableDeckSeed:               tbl.TableDeckSeed,
			SeatCards:                   projectedCards,
			Settlement:                  stl,
			RealOpponentsSubmittedSeats: realOppSeats,
		}
		if sub != nil {
			ct.SubmittedAt = sub.SubmittedAt
			hCopy := sub.PlayerHand
			ct.PlayerHand = &hCopy
			ct.SettledCycleKey = sub.SettledCycleKey
			ct.GraceFor0600Cycle = sub.GraceFor0600Cycle
		}
		tables[i] = ct
	}

	return ClientBookingCarriage{
		CarriageID:              car.CarriageID,
		PredealtID:              car.PredealtID,
		CarriageNumber:          car.CarriageNumber,
		GroupIndex:              car.GroupIndex,
		SceneMode:               car.SceneMode,
		SelectedSeat:            selectedSeat,
		Tables:                  tables,
		UnplayedTableIndices:    unplayed,
		CurrentActiveTableIndex: activeIdx,
		IsCompleted:             isCompleted,
		CompletedAt:             completedAt,
		SeatOccupants:           car.SeatOccupants,
		SettledCycleKey:         settledCycleKey,
	}
}

func buildArchivedCarriages(st *ServerBookingStore, sceneMode, userID, activeCid string) []ClientBookingCarriage {
	var list []ClientBookingCarriage
	for _, car := range st.Carriages {
		if car == nil || car.SceneMode != sceneMode || car.CarriageID == activeCid {
			continue
		}
		if _, has := car.PlayerProgress[userID]; has {
			list = append(list, projectCarriageForUser(car, userID, false))
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CarriageNumber > list[j].CarriageNumber
	})
	return list
}

func settleSingleBookingTableForUser(car *ServerMultiBookingCarriage, tbl *ServerMultiBookingTable, userID string, sub ServerTableSeatSubmission) BookingTableSettlement {
	seats := getBookingSeats(car.SceneMode)
	preps := make([]sssPlayerPrep, 0, len(seats))
	isReal := make([]bool, len(seats))
	seatLabels := make([]string, len(seats))
	myIdx := -1

	for idx, sl := range seats {
		seatLabels[idx] = sl
		if sl == sub.PlayerSeat {
			myIdx = idx
			isReal[idx] = true
			preps = append(preps, prepareSSSPlayer(idx, sub.PlayerName, ShisanshuiSubmission{
				Seat:      idx,
				RawTiers:  &SSSRawTiers{Front: sub.PlayerHand.Front, Mid: sub.PlayerHand.Mid, Back: sub.PlayerHand.Back, SpecialHand: sub.PlayerHand.SpecialHand},
				IsSpecial: sub.PlayerHand.SpecialHand != nil,
			}))
		} else if oppSub, ok := tbl.SeatSubmissions[sl]; ok {
			isReal[idx] = true
			preps = append(preps, prepareSSSPlayer(idx, oppSub.PlayerName, ShisanshuiSubmission{
				Seat:      idx,
				RawTiers:  &SSSRawTiers{Front: oppSub.PlayerHand.Front, Mid: oppSub.PlayerHand.Mid, Back: oppSub.PlayerHand.Back, SpecialHand: oppSub.PlayerHand.SpecialHand},
				IsSpecial: oppSub.PlayerHand.SpecialHand != nil,
			}))
		} else {
			cards := tbl.SeatCards[sl]
			lay := autoArrangeThirteen(cards)
			preps = append(preps, prepareSSSPlayer(idx, "守擂 AI ("+sl+")", ShisanshuiSubmission{
				Seat:      idx,
				RawTiers:  &SSSRawTiers{Front: lay.Head, Mid: lay.Mid, Back: lay.Tail, SpecialHand: lay.SpecialHand},
				IsSpecial: lay.SpecialHand != nil,
			}))
		}
	}

	if myIdx < 0 {
		return BookingTableSettlement{}
	}
	results := settleAllSSSPlayers(preps, bookingPointPerWater)
	myRes := results[myIdx]

	var oppSummaries []BookingOpponentSummary
	for _, m := range myRes.Matchups {
		oppIdx := m.AgainstSeat
		oppPrep := preps[oppIdx]
		spName := ""
		if oppPrep.specialHand != nil {
			spName = oppPrep.specialHand.Name
		}
		oppSummaries = append(oppSummaries, BookingOpponentSummary{
			OppSeat:         oppIdx,
			SeatLabel:       seatLabels[oppIdx],
			OppName:         oppPrep.name,
			IsRealPlayer:    isReal[oppIdx],
			FrontLabel:      oppPrep.headEval.DetailLabel,
			MidLabel:        oppPrep.midEval.DetailLabel,
			BackLabel:       oppPrep.tailEval.DetailLabel,
			SpecialHandName: spName,
			FinalWater:      m.NetWater,
			IsShot:          m.IsShot,
			IsFoulOpp:       oppPrep.isFoul,
		})
	}

	return BookingTableSettlement{
		IsSettled:          true,
		TotalWater:         myRes.TotalWater,
		NetScore:           myRes.ScoreDelta,
		IsGrandSlam:        myRes.IsGrandSlam,
		OpponentsBreakdown: []interface{}{},
		OpponentSummaries:  oppSummaries,
		SettledAt:          nowMs(),
	}
}

func getLatest0600CycleCutoff(now time.Time) (cycleKey string, cutoffMs int64, dateStr string) {
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, now.Location())
	if now.Before(cutoff) {
		cutoff = cutoff.AddDate(0, 0, -1)
	}
	return cutoff.Format("2006-01-02") + "_0600", cutoff.UnixMilli(), cutoff.Format("2006-01-02")
}

func run0600SweepLocked(bm *BookingManager, st *ServerBookingStore, onlyUserID string, forceIncludeGraceUserID string) []BookingSettledNotification {
	now := time.Now()
	nowMillis := now.UnixMilli()
	cycleKey, cutoffMs, dateStr := getLatest0600CycleCutoff(now)

	type pendingUserCarriage struct {
		userID string
		car    *ServerMultiBookingCarriage
		tables []*ServerMultiBookingTable
	}
	var toSettle []pendingUserCarriage

	for _, car := range st.Carriages {
		if car == nil {
			continue
		}
		for uid, prog := range car.PlayerProgress {
			if prog == nil || prog.SelectedSeat == "" {
				continue
			}
			if onlyUserID != "" && uid != onlyUserID {
				continue
			}
			if prog.SettledCycleKey == cycleKey {
				continue
			}
			var eligible []*ServerMultiBookingTable
			for _, tbl := range car.Tables {
				sub, ok := tbl.SeatSubmissions[prog.SelectedSeat]
				if !ok || sub.PlayerID != uid || sub.SettledCycleKey != "" {
					continue
				}
				if sub.SubmittedAt < cutoffMs {
					eligible = append(eligible, tbl)
				} else if forceIncludeGraceUserID != "" && uid == forceIncludeGraceUserID {
					eligible = append(eligible, tbl)
				} else if sub.GraceFor0600Cycle == cycleKey && nowMillis-cutoffMs >= bookingGraceWindowMs {
					eligible = append(eligible, tbl)
				}
			}
			if len(eligible) > 0 {
				toSettle = append(toSettle, pendingUserCarriage{userID: uid, car: car, tables: eligible})
			}
		}
	}

	if len(toSettle) == 0 {
		return nil
	}

	var notifications []BookingSettledNotification
	_ = bm.storage.WithDBWrite(func(db *DatabaseSchema) bool {
		dirty := false
		for _, item := range toSettle {
			prog := item.car.PlayerProgress[item.userID]
			if prog == nil {
				continue
			}
			totalW := 0
			totalNet := 0
			var summaries []BookingTableSummaryItem
			for _, tbl := range item.tables {
				sub := tbl.SeatSubmissions[prog.SelectedSeat]
				det := settleSingleBookingTableForUser(item.car, tbl, item.userID, sub)
				sub.Settlement = &det
				sub.SettledCycleKey = cycleKey
				tbl.SeatSubmissions[prog.SelectedSeat] = sub

				spName := ""
				if sub.PlayerHand.SpecialHand != nil {
					spName = sub.PlayerHand.SpecialHand.Name
				}
				totalW += det.TotalWater
				totalNet += det.NetScore
				summaries = append(summaries, BookingTableSummaryItem{
					TableIndex:      tbl.TableIndex,
					PlayerSeat:      sub.PlayerSeat,
					TotalWater:      det.TotalWater,
					SpecialHandName: spName,
				})
			}
			prog.SettledCycleKey = cycleKey

			rec := BookingSettlementRecord{
				ID:                 "bk-" + itoa(int(nowMillis&0x7fffffff)) + "-" + randomHex(3),
				DateStr:            dateStr,
				SceneMode:          item.car.SceneMode,
				CarriageNumber:     item.car.CarriageNumber,
				SettledTablesCount: len(item.tables),
				TotalWater:         totalW,
				NetScore:           totalNet,
				SettledAt:          nowMillis,
				IsSimulation:       false,
				SettledCycleKey:    cycleKey,
				CoinsCredited:      false,
				TablesSummary:      summaries,
			}

			if prof := db.Profiles[item.userID]; prof != nil {
				prof.Coins += totalNet
				if prof.Coins < 0 {
					prof.Coins = 0
				}
				ratingDelta := int(math.Round(float64(totalW) * 1.2))
				prof.Rating += ratingDelta
				if prof.Rating < 1000 {
					prof.Rating = 1000
				}
				prof.Level = prof.Rating / 500
				if prof.Level < 1 {
					prof.Level = 1
				} else if prof.Level > 10 {
					prof.Level = 10
				}
				if prof.Stats == nil {
					prof.Stats = map[string]GameStat{}
				}
				stSSS := prof.Stats["shisanshui"]
				stSSS.Played += len(item.tables)
				if totalNet > 0 {
					wonTables := 0
					for _, tsItem := range summaries {
						if tsItem.TotalWater > 0 {
							wonTables++
						}
					}
					stSSS.Won += wonTables
					if totalNet > stSSS.HighestScore {
						stSSS.HighestScore = totalNet
					}
				}
				prof.Stats["shisanshui"] = stSSS
				prof.UpdatedAt = nowMillis
				rec.CoinsCredited = true
				gameLabel := "十三张·四人预约专列"
				if item.car.SceneMode == "eight-booking" {
					gameLabel = "十三张·八人预约专列"
				}
				resStr := "draw"
				if totalNet > 0 {
					resStr = "win"
				} else if totalNet < 0 {
					resStr = "lose"
				}
				wSign := ""
				if totalW >= 0 {
					wSign = "+"
				}
				db.Records = append([]MatchRecord{{
					ID:          rec.ID,
					PlayerID:    prof.ID,
					GameType:    "shisanshui",
					GameName:    gameLabel,
					Result:      resStr,
					CoinsDelta:  totalNet,
					RatingDelta: ratingDelta,
					Details:     "第" + itoa(item.car.CarriageNumber) + "节车厢 (" + itoa(len(item.tables)) + "桌06:00结算) · 净胜 " + wSign + itoa(totalW) + " 水",
					Timestamp:   now.Format("15:04"),
					UpdatedAt:   nowMillis,
				}}, db.Records...)
				if len(db.Records) > 500 {
					db.Records = db.Records[:500]
				}
				if totalNet != 0 {
					sign := "+"
					if totalNet < 0 {
						sign = ""
					}
					recordServerPointsTx(db, prof.ID, "booking_settle", "十三张预约专列 #"+itoa(item.car.CarriageNumber)+" 06:00结算 ("+sign+itoa(totalNet)+")", totalNet, prof.Coins)
				}
				dirty = true
				notifications = append(notifications, BookingSettledNotification{
					UserID: prof.ID,
					Coins:  prof.Coins,
					Delta:  totalNet,
					Rating: prof.Rating,
					Level:  prof.Level,
					Record: rec,
				})
			}

			hist := append([]BookingSettlementRecord{rec}, st.UserSettlementHistory[item.userID]...)
			if len(hist) > 60 {
				hist = hist[:60]
			}
			st.UserSettlementHistory[item.userID] = hist

			unread := append([]BookingSettlementRecord{rec}, st.UserUnreadSettlements[item.userID]...)
			if len(unread) > 20 {
				unread = unread[:20]
			}
			st.UserUnreadSettlements[item.userID] = unread
		}
		return dirty
	})

	bm.saveStoreLocked(st)
	return notifications
}

func (bm *BookingManager) Run0600Sweep(onlyUserID, forceGraceUserID string) []BookingSettledNotification {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	st := bm.loadStoreLocked()
	return run0600SweepLocked(bm, st, onlyUserID, forceGraceUserID)
}

func (bm *BookingManager) GetState(sceneMode, userID, playerName, avatar, reqCid string, reqNum int) map[string]interface{} {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	sceneMode = normalizeBookingSceneMode(sceneMode)
	st := bm.loadStoreLocked()
	notifs := run0600SweepLocked(bm, st, userID, "")
	car := getOrAssignCarriageForUser(st, sceneMode, userID, reqCid, reqNum, false)
	bm.saveStoreLocked(st)

	proj := projectCarriageForUser(car, userID, false)
	return map[string]interface{}{
		"carriage":             proj,
		"archivedCarriages":    buildArchivedCarriages(st, sceneMode, userID, car.CarriageID),
		"settlementHistory":    st.UserSettlementHistory[userID],
		"unreadSettlements":    st.UserUnreadSettlements[userID],
		"settledNotifications": notifs,
	}
}

func (bm *BookingManager) SelectSeat(sceneMode, userID, playerName, avatar, seat, reqCid string, reqNum int) map[string]interface{} {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	sceneMode = normalizeBookingSceneMode(sceneMode)
	st := bm.loadStoreLocked()
	car := getOrAssignCarriageForUser(st, sceneMode, userID, reqCid, reqNum, false)

	validSeat := false
	for _, s := range getBookingSeats(sceneMode) {
		if s == seat {
			validSeat = true
			break
		}
	}
	if !validSeat {
		return map[string]interface{}{"ok": false, "error": "无效的方位坐席"}
	}

	minCoins := getSceneRequiredCoins("shisanshui", sceneMode)
	var coinErr string
	bm.storage.WithDBRead(func(db *DatabaseSchema) {
		if prof := db.Profiles[userID]; prof != nil && prof.Coins < minCoins {
			coinErr = "您的可用积分不足（当前 " + itoa(prof.Coins) + " 分，需至少 " + itoa(minCoins) + " 分方可入座预约专列）"
		}
	})
	if coinErr != "" {
		return map[string]interface{}{"ok": false, "error": coinErr}
	}

	if occ, taken := car.SeatOccupants[seat]; taken && occ.PlayerID != userID {
		return map[string]interface{}{
			"ok":       false,
			"error":    "该方位已被 " + occ.PlayerName + " 抢先入座，请选择其他空余方位！",
			"carriage": projectCarriageForUser(car, userID, false),
		}
	}

	prog := car.PlayerProgress[userID]
	if prog != nil && prog.SelectedSeat != "" && prog.SelectedSeat != seat {
		// Release old seat only if no tables submitted yet
		submittedAny := false
		for _, tbl := range car.Tables {
			if sub, ok := tbl.SeatSubmissions[prog.SelectedSeat]; ok && sub.PlayerID == userID {
				submittedAny = true
				break
			}
		}
		if submittedAny {
			return map[string]interface{}{
				"ok":       false,
				"error":    "您已在该车厢完成理牌封牌，不可中途更换方位",
				"carriage": projectCarriageForUser(car, userID, false),
			}
		}
		delete(car.SeatOccupants, prog.SelectedSeat)
	}

	if prog == nil {
		unplayed := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		randIdx := rand.Intn(len(unplayed))
		firstTbl := unplayed[randIdx]
		prog = &ServerPlayerCarriageProgress{
			PlayerID:                userID,
			PlayerName:              playerName,
			Avatar:                  avatar,
			SelectedSeat:            seat,
			UnplayedTableIndices:    unplayed,
			CurrentActiveTableIndex: &firstTbl,
		}
		car.PlayerProgress[userID] = prog
	} else {
		prog.SelectedSeat = seat
		prog.PlayerName = playerName
		if len(prog.UnplayedTableIndices) > 0 && prog.CurrentActiveTableIndex == nil {
			firstTbl := prog.UnplayedTableIndices[rand.Intn(len(prog.UnplayedTableIndices))]
			prog.CurrentActiveTableIndex = &firstTbl
		}
	}

	car.SeatOccupants[seat] = &BookingSeatOccupant{
		Seat:           seat,
		PlayerID:       userID,
		PlayerName:     playerName,
		Avatar:         avatar,
		SelectedAt:     nowMs(),
		SubmittedCount: 10 - len(prog.UnplayedTableIndices),
	}

	bm.saveStoreLocked(st)
	return map[string]interface{}{
		"ok":                true,
		"carriage":          projectCarriageForUser(car, userID, false),
		"archivedCarriages": buildArchivedCarriages(st, sceneMode, userID, car.CarriageID),
		"carriageId":        car.CarriageID,
		"carriageNumber":    car.CarriageNumber,
		"rawSeatOccupants":  car.SeatOccupants,
	}
}

func (bm *BookingManager) PickTable(sceneMode, userID, reqCid string, reqNum, specificTable int) map[string]interface{} {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	sceneMode = normalizeBookingSceneMode(sceneMode)
	st := bm.loadStoreLocked()
	car := getOrAssignCarriageForUser(st, sceneMode, userID, reqCid, reqNum, false)
	prog := car.PlayerProgress[userID]
	if prog == nil || prog.SelectedSeat == "" {
		return map[string]interface{}{"ok": false, "error": "请先在本节车厢选定入座方位"}
	}
	if len(prog.UnplayedTableIndices) == 0 {
		prog.IsCompleted = true
		prog.CurrentActiveTableIndex = nil
		bm.saveStoreLocked(st)
		return map[string]interface{}{
			"ok":                true,
			"carriage":          projectCarriageForUser(car, userID, false),
			"archivedCarriages": buildArchivedCarriages(st, sceneMode, userID, car.CarriageID),
		}
	}
	chosen := prog.UnplayedTableIndices[rand.Intn(len(prog.UnplayedTableIndices))]
	if specificTable > 0 {
		for _, idx := range prog.UnplayedTableIndices {
			if idx == specificTable {
				chosen = specificTable
				break
			}
		}
	}
	prog.CurrentActiveTableIndex = &chosen
	bm.saveStoreLocked(st)
	return map[string]interface{}{
		"ok":                true,
		"carriage":          projectCarriageForUser(car, userID, false),
		"archivedCarriages": buildArchivedCarriages(st, sceneMode, userID, car.CarriageID),
	}
}

func (bm *BookingManager) SubmitHand(sceneMode, userID, playerName, avatar, reqCid string, reqNum, tableIndex int, playerSeat string, rawHand json.RawMessage, isGrace bool) map[string]interface{} {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	sceneMode = normalizeBookingSceneMode(sceneMode)
	st := bm.loadStoreLocked()
	car := getOrAssignCarriageForUser(st, sceneMode, userID, reqCid, reqNum, false)
	prog := car.PlayerProgress[userID]
	if prog == nil || prog.SelectedSeat == "" {
		return map[string]interface{}{"ok": false, "error": "请先选择车厢方位后再提交手牌"}
	}
	seat := prog.SelectedSeat
	if playerSeat != "" && playerSeat != seat {
		return map[string]interface{}{"ok": false, "error": "提交方位与已选方位不一致"}
	}
	if tableIndex <= 0 && prog.CurrentActiveTableIndex != nil {
		tableIndex = *prog.CurrentActiveTableIndex
	}
	if tableIndex < 1 || tableIndex > len(car.Tables) {
		return map[string]interface{}{"ok": false, "error": "无效的牌桌编号"}
	}
	tbl := car.Tables[tableIndex-1]
	if existing, ok := tbl.SeatSubmissions[seat]; ok && existing.PlayerID == userID {
		return map[string]interface{}{"ok": false, "error": "本桌已完成封牌，不可重复提交"}
	}

	var hand BookingSubmittedHand
	if err := json.Unmarshal(rawHand, &hand); err != nil || len(hand.Front) != 3 || len(hand.Mid) != 5 || len(hand.Back) != 5 {
		return map[string]interface{}{"ok": false, "error": "理牌格式不合法（须为 3 / 5 / 5）"}
	}
	// Verify cards match server dealt cards for this seat
	if dealt := tbl.SeatCards[seat]; len(dealt) == 13 {
		if !validateSSSPayloadCards(dealt, nil, &SSSRawTiers{Front: hand.Front, Mid: hand.Mid, Back: hand.Back}) {
			return map[string]interface{}{"ok": false, "error": "所提交手牌与系统预发牌面不一致"}
		}
	}
	all13 := append(append(append([]Card{}, hand.Front...), hand.Mid...), hand.Back...)
	serverSpecial := detectSSSSpecialHand(all13)
	if serverSpecial == nil && isFoulHand(hand.Front, hand.Mid, hand.Back) {
		return map[string]interface{}{"ok": false, "error": "当前摆牌倒水相公（尾道需 >= 中道 >= 头道），请调整后提交"}
	}
	hand.SpecialHand = serverSpecial

	nowTs := nowMs()
	graceCycle := ""
	if isGrace {
		ck, _, _ := getLatest0600CycleCutoff(time.Now())
		graceCycle = ck
	}
	tbl.SeatSubmissions[seat] = ServerTableSeatSubmission{
		PlayerID:          userID,
		PlayerName:        playerName,
		Avatar:            avatar,
		PlayerSeat:        seat,
		PlayerHand:        hand,
		SubmittedAt:       nowTs,
		GraceFor0600Cycle: graceCycle,
	}

	// Remove from unplayed
	var nextUnplayed []int
	for _, idx := range prog.UnplayedTableIndices {
		if idx != tableIndex {
			nextUnplayed = append(nextUnplayed, idx)
		}
	}
	prog.UnplayedTableIndices = nextUnplayed
	if len(nextUnplayed) == 0 {
		prog.IsCompleted = true
		prog.CompletedAt = nowTs
		prog.CurrentActiveTableIndex = nil
	} else {
		nextTbl := nextUnplayed[rand.Intn(len(nextUnplayed))]
		prog.CurrentActiveTableIndex = &nextTbl
	}

	if occ := car.SeatOccupants[seat]; occ != nil {
		occ.SubmittedCount = 10 - len(nextUnplayed)
	}

	notifs := run0600SweepLocked(bm, st, userID, "")
	bm.saveStoreLocked(st)

	proj := projectCarriageForUser(car, userID, false)
	var submittedTbl *ClientBookingTable
	for i := range proj.Tables {
		if proj.Tables[i].TableIndex == tableIndex {
			submittedTbl = &proj.Tables[i]
			break
		}
	}

	return map[string]interface{}{
		"ok":                   true,
		"carriage":             proj,
		"submittedTable":       submittedTbl,
		"remainingCount":       len(nextUnplayed),
		"isCarriageCompleted":  prog.IsCompleted,
		"archivedCarriages":    buildArchivedCarriages(st, sceneMode, userID, car.CarriageID),
		"settlementHistory":    st.UserSettlementHistory[userID],
		"settledNotifications": notifs,
		"carriageId":           car.CarriageID,
		"carriageNumber":       car.CarriageNumber,
		"tableIndex":           tableIndex,
		"seat":                 seat,
		"seatOccupants":        car.SeatOccupants,
	}
}

func (bm *BookingManager) AdvanceCarriage(sceneMode, userID, playerName, avatar string, createFresh bool) map[string]interface{} {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	sceneMode = normalizeBookingSceneMode(sceneMode)
	st := bm.loadStoreLocked()
	uKey := bookingUserModeKey(userID, sceneMode)
	curNum := st.UserCurrentCarriage[uKey]
	if curNum <= 0 {
		curNum = 1
	}

	var releasedInfo map[string]interface{}
	if prevCid := st.UserCurrentCarriageID[uKey]; prevCid != "" {
		if prevCar, ok := st.Carriages[prevCid]; ok && prevCar != nil {
			if prog, has := prevCar.PlayerProgress[userID]; has && prog.SelectedSeat != "" {
				submittedAny := false
				for _, tbl := range prevCar.Tables {
					if sub, ok := tbl.SeatSubmissions[prog.SelectedSeat]; ok && sub.PlayerID == userID {
						submittedAny = true
						break
					}
				}
				if !submittedAny {
					delete(prevCar.SeatOccupants, prog.SelectedSeat)
					delete(prevCar.PlayerProgress, userID)
					releasedInfo = map[string]interface{}{
						"carriageId":     prevCar.CarriageID,
						"carriageNumber": prevCar.CarriageNumber,
						"seatOccupants":  prevCar.SeatOccupants,
					}
				}
			}
		}
	}

	nextNum := curNum + 1
	car := getOrAssignCarriageForUser(st, sceneMode, userID, "", nextNum, createFresh)
	bm.saveStoreLocked(st)

	_ = playerName
	_ = avatar
	return map[string]interface{}{
		"ok":                true,
		"carriage":          projectCarriageForUser(car, userID, false),
		"archivedCarriages": buildArchivedCarriages(st, sceneMode, userID, car.CarriageID),
		"releasedSeatInfo":  releasedInfo,
	}
}

func (bm *BookingManager) SimulateSettlement(sceneMode, userID, reqCid string, reqNum int) map[string]interface{} {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	sceneMode = normalizeBookingSceneMode(sceneMode)
	st := bm.loadStoreLocked()
	car := getOrAssignCarriageForUser(st, sceneMode, userID, reqCid, reqNum, false)
	prog := car.PlayerProgress[userID]
	if prog == nil || prog.SelectedSeat == "" {
		return map[string]interface{}{"ok": false, "error": "请先在本节车厢选定方位并至少封牌 1 桌再进行模拟比水"}
	}
	if prog.SimulationCache == nil {
		prog.SimulationCache = map[int]*BookingTableSettlement{}
	}

	totalW := 0
	totalNet := 0
	var summaries []BookingTableSummaryItem
	for _, tbl := range car.Tables {
		sub, ok := tbl.SeatSubmissions[prog.SelectedSeat]
		if !ok || sub.PlayerID != userID {
			continue
		}
		det := settleSingleBookingTableForUser(car, tbl, userID, sub)
		prog.SimulationCache[tbl.TableIndex] = &det
		spName := ""
		if sub.PlayerHand.SpecialHand != nil {
			spName = sub.PlayerHand.SpecialHand.Name
		}
		totalW += det.TotalWater
		totalNet += det.NetScore
		summaries = append(summaries, BookingTableSummaryItem{
			TableIndex:      tbl.TableIndex,
			PlayerSeat:      sub.PlayerSeat,
			TotalWater:      det.TotalWater,
			SpecialHandName: spName,
		})
	}
	if len(summaries) == 0 {
		return map[string]interface{}{"ok": false, "error": "请先至少提交 1 桌理牌后再模拟比水测算"}
	}

	rec := BookingSettlementRecord{
		ID:                 "sim-" + itoa(int(nowMs()&0x7fffffff)),
		DateStr:            time.Now().Format("2006-01-02"),
		SceneMode:          car.SceneMode,
		CarriageNumber:     car.CarriageNumber,
		SettledTablesCount: len(summaries),
		TotalWater:         totalW,
		NetScore:           totalNet,
		SettledAt:          nowMs(),
		IsSimulation:       true,
		TablesSummary:      summaries,
	}
	bm.saveStoreLocked(st)

	return map[string]interface{}{
		"ok":                true,
		"record":            rec,
		"carriage":          projectCarriageForUser(car, userID, true),
		"archivedCarriages": buildArchivedCarriages(st, sceneMode, userID, car.CarriageID),
	}
}

func (bm *BookingManager) AckSettlements(userID string) {
	if userID == "" {
		return
	}
	bm.mu.Lock()
	defer bm.mu.Unlock()
	st := bm.loadStoreLocked()
	st.UserUnreadSettlements[userID] = []BookingSettlementRecord{}
	bm.saveStoreLocked(st)
}

func handleBookingWsMessage(ctx *HubContext, conn *WSConn, rawMap map[string]interface{}) bool {
	msgType, _ := rawMap["type"].(string)
	if !strings.HasPrefix(msgType, "booking_") {
		return false
	}

	rawUid, _ := rawMap["playerId"].(string)
	if rawUid == "" {
		rawUid, _ = rawMap["userId"].(string)
	}
	if rawUid == "" {
		rawUid = conn.authenticatedUserID
	}
	if rawUid == "" {
		rawUid = conn.userID
	}
	if rawUid == "" {
		rawUid = "guest"
	}
	token, _ := rawMap["sessionToken"].(string)

	// Verify session if user has password
	authErr := ""
	finalUid := rawUid
	ctx.Storage.WithDBRead(func(db *DatabaseSchema) {
		if token != "" && rawUid != "" {
			if prof := db.Profiles[rawUid]; prof != nil && prof.SessionToken != "" && safeTokenEqual(prof.SessionToken, token) && !prof.IsBanned {
				conn.authenticatedUserID = prof.ID
				finalUid = prof.ID
				return
			}
		}
		if prof := db.Profiles[rawUid]; prof != nil && prof.Password != "" && conn.authenticatedUserID != rawUid {
			authErr = "账号会话校验未通过，请重新登录或刷新重连"
		}
	})
	if authErr != "" {
		ctx.SendToConn(conn, map[string]interface{}{
			"type":     "booking_error",
			"action":   strings.TrimPrefix(msgType, "booking_"),
			"error":    authErr,
			"serverTs": nowMs(),
		})
		return true
	}

	sceneMode, _ := rawMap["sceneMode"].(string)
	sceneMode = normalizeBookingSceneMode(sceneMode)
	playerName, _ := rawMap["playerName"].(string)
	if playerName == "" {
		playerName = "茶客"
	}
	avatar, _ := rawMap["avatar"].(string)
	if avatar == "" {
		avatar = "👑"
	}
	carriageID, _ := rawMap["carriageId"].(string)
	carriageNumber := 0
	if n, ok := rawMap["carriageNumber"].(float64); ok {
		carriageNumber = int(n)
	}

	switch msgType {
	case "booking_get_carriage":
		st := ctx.Booking.GetState(sceneMode, finalUid, playerName, avatar, carriageID, carriageNumber)
		if notifs, ok := st["settledNotifications"].([]BookingSettledNotification); ok && len(notifs) > 0 {
			ctx.EmitBookingNotifications(notifs)
		}
		ctx.SendToConn(conn, map[string]interface{}{
			"type":              "booking_carriage_state",
			"sceneMode":         sceneMode,
			"carriage":          st["carriage"],
			"archivedCarriages": st["archivedCarriages"],
			"settlementHistory": st["settlementHistory"],
			"unreadSettlements": st["unreadSettlements"],
			"serverTs":          nowMs(),
		})
		return true

	case "booking_select_seat":
		seat, _ := rawMap["seat"].(string)
		if seat == "" {
			seat = "东"
		}
		res := ctx.Booking.SelectSeat(sceneMode, finalUid, playerName, avatar, seat, carriageID, carriageNumber)
		if ok, _ := res["ok"].(bool); !ok {
			ctx.SendToConn(conn, map[string]interface{}{
				"type":     "booking_error",
				"action":   "select_seat",
				"error":    res["error"],
				"carriage": res["carriage"],
				"serverTs": nowMs(),
			})
			return true
		}
		car := res["carriage"].(ClientBookingCarriage)
		ctx.SendToConn(conn, map[string]interface{}{
			"type":              "booking_seat_selected",
			"sceneMode":         sceneMode,
			"carriage":          car,
			"archivedCarriages": res["archivedCarriages"],
			"activeTableIndex":  car.CurrentActiveTableIndex,
			"serverTs":          nowMs(),
		})
		ctx.Broadcast(map[string]interface{}{
			"type":           "booking_carriage_occupants_updated",
			"sceneMode":      sceneMode,
			"carriageId":     res["carriageId"],
			"carriageNumber": res["carriageNumber"],
			"seatOccupants":  res["rawSeatOccupants"],
		})
		return true

	case "booking_pick_table":
		tableIdx := 0
		if n, ok := rawMap["tableIndex"].(float64); ok {
			tableIdx = int(n)
		}
		res := ctx.Booking.PickTable(sceneMode, finalUid, carriageID, carriageNumber, tableIdx)
		if ok, _ := res["ok"].(bool); !ok {
			ctx.SendToConn(conn, map[string]interface{}{
				"type":     "booking_error",
				"action":   "pick_table",
				"error":    res["error"],
				"serverTs": nowMs(),
			})
			return true
		}
		car := res["carriage"].(ClientBookingCarriage)
		ctx.SendToConn(conn, map[string]interface{}{
			"type":              "booking_table_picked",
			"sceneMode":         sceneMode,
			"carriage":          car,
			"archivedCarriages": res["archivedCarriages"],
			"activeTableIndex":  car.CurrentActiveTableIndex,
			"serverTs":          nowMs(),
		})
		return true

	case "booking_submit_hand", "booking_submit":
		tableIdx := 0
		if n, ok := rawMap["tableIndex"].(float64); ok {
			tableIdx = int(n)
		}
		pSeat, _ := rawMap["playerSeat"].(string)
		isGrace, _ := rawMap["isGraceSubmission"].(bool)
		rawHand, _ := json.Marshal(rawMap["playerHand"])
		res := ctx.Booking.SubmitHand(sceneMode, finalUid, playerName, avatar, carriageID, carriageNumber, tableIdx, pSeat, rawHand, isGrace)
		if ok, _ := res["ok"].(bool); !ok {
			ctx.SendToConn(conn, map[string]interface{}{
				"type":     "booking_error",
				"action":   "submit_hand",
				"error":    res["error"],
				"serverTs": nowMs(),
			})
			return true
		}
		if notifs, ok := res["settledNotifications"].([]BookingSettledNotification); ok && len(notifs) > 0 {
			ctx.EmitBookingNotifications(notifs)
		}
		ctx.SendToConn(conn, map[string]interface{}{
			"type":                "booking_submit_ack",
			"success":             true,
			"sceneMode":           sceneMode,
			"carriage":            res["carriage"],
			"submittedTable":      res["submittedTable"],
			"remainingCount":      res["remainingCount"],
			"isCarriageCompleted": res["isCarriageCompleted"],
			"archivedCarriages":   res["archivedCarriages"],
			"settlementHistory":   res["settlementHistory"],
			"serverTs":            nowMs(),
		})
		ctx.Broadcast(map[string]interface{}{
			"type":           "booking_carriage_occupants_updated",
			"sceneMode":      sceneMode,
			"carriageId":     res["carriageId"],
			"carriageNumber": res["carriageNumber"],
			"tableIndex":     res["tableIndex"],
			"seat":           res["seat"],
			"seatOccupants":  res["seatOccupants"],
		})
		return true

	case "booking_advance_carriage":
		createFresh, _ := rawMap["createFresh"].(bool)
		res := ctx.Booking.AdvanceCarriage(sceneMode, finalUid, playerName, avatar, createFresh)
		ctx.SendToConn(conn, map[string]interface{}{
			"type":              "booking_carriage_advanced",
			"sceneMode":         sceneMode,
			"carriage":          res["carriage"],
			"archivedCarriages": res["archivedCarriages"],
			"serverTs":          nowMs(),
		})
		if rel, ok := res["releasedSeatInfo"].(map[string]interface{}); ok && rel != nil {
			ctx.Broadcast(map[string]interface{}{
				"type":           "booking_carriage_occupants_updated",
				"sceneMode":      sceneMode,
				"carriageId":     rel["carriageId"],
				"carriageNumber": rel["carriageNumber"],
				"seatOccupants":  rel["seatOccupants"],
			})
		}
		return true

	case "booking_simulate_settle":
		res := ctx.Booking.SimulateSettlement(sceneMode, finalUid, carriageID, carriageNumber)
		if ok, _ := res["ok"].(bool); !ok {
			ctx.SendToConn(conn, map[string]interface{}{
				"type":     "booking_error",
				"action":   "simulate_settle",
				"error":    res["error"],
				"serverTs": nowMs(),
			})
			return true
		}
		ctx.SendToConn(conn, map[string]interface{}{
			"type":              "booking_simulation_result",
			"sceneMode":         sceneMode,
			"record":            res["record"],
			"carriage":          res["carriage"],
			"archivedCarriages": res["archivedCarriages"],
			"serverTs":          nowMs(),
		})
		return true

	case "booking_settle_0600":
		forceGrace, _ := rawMap["forceIncludeGrace"].(bool)
		var graceUID string
		if forceGrace {
			graceUID = finalUid
		}
		notifs := ctx.Booking.Run0600Sweep(finalUid, graceUID)
		if len(notifs) > 0 {
			ctx.EmitBookingNotifications(notifs)
		}
		if finalUid != "" && finalUid != "guest" {
			st := ctx.Booking.GetState(sceneMode, finalUid, playerName, avatar, carriageID, carriageNumber)
			ctx.SendToConn(conn, map[string]interface{}{
				"type":              "booking_carriage_state",
				"sceneMode":         sceneMode,
				"carriage":          st["carriage"],
				"archivedCarriages": st["archivedCarriages"],
				"settlementHistory": st["settlementHistory"],
				"unreadSettlements": st["unreadSettlements"],
				"serverTs":          nowMs(),
			})
		}
		return true

	case "booking_ack_settlement":
		ctx.Booking.AckSettlements(finalUid)
		return true
	}

	return false
}
