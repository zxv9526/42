package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dbFilePath       = "data/game_database.json"
	snapshotFilePath = "data/active_rooms_snapshot.json"
	maxRecordsKeep   = 1000
	maxAnnKeep       = 100
	maxLedgerPerUser = 100
)

type StorageManager struct {
	mu        sync.Mutex
	writeMu   sync.Mutex
	envMap    map[string]string
	cachedDB  *DatabaseSchema
	dirty     bool
	saveTimer *time.Timer
}

func newStorageManager() *StorageManager {
	sm := &StorageManager{envMap: map[string]string{}}
	sm.loadEnvFile()
	sm.cachedDB = sm.loadDBLocked()
	return sm
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func timeHHMM(ts int64) string {
	if ts <= 0 {
		return time.Now().Format("15:04")
	}
	return time.UnixMilli(ts).Format("15:04")
}

func (s *StorageManager) loadEnvFile() {
	candidates := []string{".env", "../.env"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".env"))
	}
	for _, p := range candidates {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			idx := strings.IndexByte(trimmed, '=')
			if idx <= 0 {
				continue
			}
			k := strings.TrimSpace(trimmed[:idx])
			v := strings.TrimSpace(trimmed[idx+1:])
			v = strings.Trim(v, `"'`)
			if v != "" {
				s.envMap[k] = v
			}
		}
		break
	}
}

func (s *StorageManager) getEnv(key string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return strings.Trim(v, `"'`)
	}
	return s.envMap[key]
}

func (s *StorageManager) saveEnvVariable(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.Setenv(key, value)
	s.envMap[key] = value
	candidates := []string{".env", "../.env"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append([]string{filepath.Join(home, ".env")}, candidates...)
	}
	target := ".env"
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			target = c
			break
		}
	}
	raw, _ := os.ReadFile(target)
	lines := []string{}
	if len(raw) > 0 {
		lines = strings.Split(string(raw), "\n")
	}
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+"=") || strings.HasPrefix(trimmed, key+" ") {
			lines[i] = fmt.Sprintf(`%s="%s"`, key, value)
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, fmt.Sprintf(`%s="%s"`, key, value))
	}
	_ = os.WriteFile(target, []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n")+"\n"), 0600)
}

func normalizePhone(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	p := b.String()
	if len(p) == 13 && strings.HasPrefix(p, "86") {
		p = p[2:]
	}
	return p
}

func phonesMatch(a, b string) bool {
	na, nb := normalizePhone(a), normalizePhone(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb || strings.HasSuffix(na, nb) || strings.HasSuffix(nb, na)
}

func randomHex(nBytes int) string {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func generateSecureSessionToken() string {
	return "sess_" + randomHex(24)
}

func hashPasswordGo(password string) string {
	salt := randomHex(16)
	sum := sha256.Sum256([]byte(salt + ":" + strings.TrimSpace(password)))
	return "sha256$" + salt + "$" + hex.EncodeToString(sum[:])
}

func verifyPasswordGo(password, stored string) bool {
	stored = strings.TrimSpace(stored)
	password = strings.TrimSpace(password)
	if stored == "" || password == "" {
		return false
	}
	if strings.HasPrefix(stored, "sha256$") {
		parts := strings.Split(stored, "$")
		if len(parts) != 3 {
			return false
		}
		sum := sha256.Sum256([]byte(parts[1] + ":" + password))
		expected := hex.EncodeToString(sum[:])
		return safeTokenEqual(expected, parts[2])
	}
	if !strings.Contains(stored, ":") {
		return safeTokenEqual(stored, password)
	}
	// Legacy Node scrypt format (<salt>:<hex>) cannot be verified without external x/crypto/scrypt;
	// fallback to rejecting mismatched plaintext and prompt password reset if migrated from Node.
	return false
}

func defaultStatsMap() map[string]GameStat {
	return map[string]GameStat{
		"yansan":     {Played: 0, Won: 0, HighestScore: 0},
		"shisanshui": {Played: 0, Won: 0, HighestScore: 0},
		"mahjong":    {Played: 0, Won: 0, HighestScore: 0},
	}
}

func defaultDatabase() *DatabaseSchema {
	return &DatabaseSchema{
		Profiles:         map[string]*UserProfile{},
		Records:          []MatchRecord{},
		AuthorizedPhones: []string{},
		AdminPhones:      []string{},
		RevokedPhones:    []string{},
		AdminLogs: []AdminLog{{
			ID:        "log-init-1",
			Action:    "SYSTEM_BOOT",
			Operator:  "Go Authoritative Engine",
			Target:    "全服",
			Details:   "Go 单体全栈服务端已启动，白名单与会话鉴权就绪",
			Timestamp: time.Now().Format("15:04"),
		}},
		SystemAnnouncements: []SystemAnnouncement{},
		PointsLedger:        map[string][]PointsTx{},
	}
}

func (s *StorageManager) loadDBLocked() *DatabaseSchema {
	for _, candidate := range []string{dbFilePath, dbFilePath + ".bak"} {
		raw, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		var db DatabaseSchema
		if err := json.Unmarshal(raw, &db); err == nil {
			ensureDBIntegrity(&db)
			return &db
		}
	}
	return defaultDatabase()
}

func ensureDBIntegrity(db *DatabaseSchema) {
	if db.Profiles == nil {
		db.Profiles = map[string]*UserProfile{}
	}
	if db.Records == nil {
		db.Records = []MatchRecord{}
	}
	if db.AuthorizedPhones == nil {
		db.AuthorizedPhones = []string{}
	}
	if db.AdminPhones == nil {
		db.AdminPhones = []string{}
	}
	if db.RevokedPhones == nil {
		db.RevokedPhones = []string{}
	}
	if db.AdminLogs == nil {
		db.AdminLogs = []AdminLog{}
	}
	if db.SystemAnnouncements == nil {
		db.SystemAnnouncements = []SystemAnnouncement{}
	}
	if db.PointsLedger == nil {
		db.PointsLedger = map[string][]PointsTx{}
	}
	for _, p := range db.Profiles {
		if p != nil && p.Stats == nil {
			p.Stats = defaultStatsMap()
		}
	}
}

func (s *StorageManager) getOrLoadDBLocked() *DatabaseSchema {
	if s.cachedDB == nil {
		s.cachedDB = s.loadDBLocked()
	}
	return s.cachedDB
}

func (s *StorageManager) scheduleSaveLocked() {
	s.dirty = true
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(600*time.Millisecond, func() {
		_ = s.FlushSync()
	})
}

func (s *StorageManager) FlushSync() error {
	s.mu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	if !s.dirty || s.cachedDB == nil {
		s.mu.Unlock()
		return nil
	}
	ensureDBIntegrity(s.cachedDB)
	if len(s.cachedDB.Records) > maxRecordsKeep {
		s.cachedDB.Records = s.cachedDB.Records[:maxRecordsKeep]
	}
	if len(s.cachedDB.SystemAnnouncements) > maxAnnKeep {
		s.cachedDB.SystemAnnouncements = s.cachedDB.SystemAnnouncements[:maxAnnKeep]
	}
	if len(s.cachedDB.AdminLogs) > 300 {
		s.cachedDB.AdminLogs = s.cachedDB.AdminLogs[:300]
	}
	data, err := json.Marshal(s.cachedDB)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.dirty = false
	s.mu.Unlock()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(dbFilePath), 0755); err != nil {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return err
	}
	tmp := dbFilePath + ".tmp." + randomHex(4)
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return err
	}
	if err := os.Rename(tmp, dbFilePath); err != nil {
		_ = os.Remove(tmp)
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *StorageManager) WithDBRead(fn func(db *DatabaseSchema)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db := s.getOrLoadDBLocked()
	fn(db)
}

func (s *StorageManager) WithDBWrite(fn func(db *DatabaseSchema) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	db := s.getOrLoadDBLocked()
	if fn(db) {
		s.scheduleSaveLocked()
	}
	return nil
}

func (s *StorageManager) getAllowedAndAdminPhones(db *DatabaseSchema) ([]string, []string) {
	revoked := map[string]bool{}
	for _, rp := range db.RevokedPhones {
		np := normalizePhone(rp)
		if np != "" {
			revoked[np] = true
		}
	}
	adminSet := map[string]bool{}
	for _, raw := range strings.FieldsFunc(s.getEnv("TELEGRAM_ADMIN_PHONE")+","+s.getEnv("ADMIN_PHONES"), func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	}) {
		np := normalizePhone(raw)
		if np != "" && !revoked[np] {
			adminSet[np] = true
		}
	}
	for _, p := range db.AdminPhones {
		np := normalizePhone(p)
		if np != "" && !revoked[np] {
			adminSet[np] = true
		}
	}
	allowedSet := map[string]bool{}
	for _, raw := range strings.FieldsFunc(s.getEnv("ALLOWED_PHONES"), func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	}) {
		np := normalizePhone(raw)
		if np != "" && !revoked[np] {
			allowedSet[np] = true
		}
	}
	for _, p := range db.AuthorizedPhones {
		np := normalizePhone(p)
		if np != "" && !revoked[np] {
			allowedSet[np] = true
		}
	}
	for p := range adminSet {
		allowedSet[p] = true
	}
	allowed := make([]string, 0, len(allowedSet))
	admins := make([]string, 0, len(adminSet))
	for p := range allowedSet {
		allowed = append(allowed, p)
	}
	for p := range adminSet {
		admins = append(admins, p)
	}
	return allowed, admins
}

func isPhoneInList(phone string, list []string) bool {
	np := normalizePhone(phone)
	if np == "" {
		return false
	}
	for _, item := range list {
		if phonesMatch(np, item) {
			return true
		}
	}
	return false
}

func findUserInDB(identifier string, db *DatabaseSchema) *UserProfile {
	raw := strings.TrimSpace(identifier)
	if raw == "" || db == nil {
		return nil
	}
	if u, ok := db.Profiles[raw]; ok && u != nil {
		return u
	}
	low := strings.ToLower(raw)
	np := normalizePhone(raw)
	for _, u := range db.Profiles {
		if u == nil {
			continue
		}
		if strings.ToLower(u.ID) == low || (u.Username != "" && strings.ToLower(u.Username) == low) {
			return u
		}
		if np != "" && u.Phone != "" && phonesMatch(u.Phone, np) {
			return u
		}
		if u.Name != "" && strings.ToLower(u.Name) == low {
			return u
		}
	}
	return nil
}

func (s *StorageManager) ensureUserProfile(phone string, db *DatabaseSchema, initialCoins int) *UserProfile {
	cleanPhone := normalizePhone(phone)
	if cleanPhone == "" {
		return nil
	}
	if db.Profiles == nil {
		db.Profiles = map[string]*UserProfile{}
	}
	for _, p := range db.Profiles {
		if p != nil && p.Phone != "" && phonesMatch(p.Phone, cleanPhone) {
			return p
		}
	}
	_, adminPhones := s.getAllowedAndAdminPhones(db)
	isAdmin := isPhoneInList(cleanPhone, adminPhones)
	suffix := cleanPhone
	if len(suffix) > 4 {
		suffix = suffix[len(suffix)-4:]
	}
	newID := "USER-" + randomHex(2)
	for {
		if _, exists := db.Profiles[newID]; !exists {
			break
		}
		newID = "USER-" + randomHex(2)
	}
	coins := initialCoins
	if coins < 0 {
		coins = 0
	}
	now := nowMs()
	prof := &UserProfile{
		ID:              newID,
		Name:            "茶客" + suffix,
		Username:        cleanPhone,
		Avatar:          "👑",
		Phone:           cleanPhone,
		IsAdmin:         isAdmin,
		IsBanned:        false,
		Coins:           coins,
		Rating:          1800,
		Level:           1,
		LastCheckinDate: "",
		RegisteredAt:    now,
		UpdatedAt:       now,
		Stats:           defaultStatsMap(),
	}
	db.Profiles[newID] = prof
	return prof
}

func recordServerPointsTx(db *DatabaseSchema, userID, txType, title string, amount, balanceAfter int) {
	if db.PointsLedger == nil {
		db.PointsLedger = map[string][]PointsTx{}
	}
	now := nowMs()
	tx := PointsTx{
		ID:           fmt.Sprintf("tx-%d-%s", now, randomHex(3)),
		Type:         txType,
		Title:        title,
		Amount:       amount,
		BalanceAfter: balanceAfter,
		Timestamp:    timeHHMM(now),
		CreatedAt:    now,
	}
	list := append([]PointsTx{tx}, db.PointsLedger[userID]...)
	if len(list) > maxLedgerPerUser {
		list = list[:maxLedgerPerUser]
	}
	db.PointsLedger[userID] = list
}

func appendAdminLog(db *DatabaseSchema, action, operator, target, details string, amount int) {
	logEntry := AdminLog{
		ID:        fmt.Sprintf("log-%d-%s", nowMs(), randomHex(3)),
		Action:    action,
		Operator:  operator,
		Target:    target,
		Amount:    amount,
		Details:   details,
		Timestamp: time.Now().Format("15:04"),
	}
	db.AdminLogs = append([]AdminLog{logEntry}, db.AdminLogs...)
	if len(db.AdminLogs) > 300 {
		db.AdminLogs = db.AdminLogs[:300]
	}
}

func (s *StorageManager) authenticateRequest(r *http.Request, db *DatabaseSchema) *UserProfile {
	token := strings.TrimSpace(r.Header.Get("X-Session-Token"))
	if token == "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			token = strings.TrimSpace(auth[7:])
		}
	}
	if token == "" {
		return nil
	}
	for _, prof := range db.Profiles {
		if prof != nil && prof.SessionToken != "" && safeTokenEqual(prof.SessionToken, token) {
			if prof.IsBanned {
				return nil
			}
			return prof
		}
	}
	return nil
}

func (s *StorageManager) authenticateAdmin(r *http.Request, db *DatabaseSchema) bool {
	secret := s.getEnv("ADMIN_API_SECRET")
	if secret != "" && safeTokenEqual(strings.TrimSpace(r.Header.Get("X-Admin-Secret")), secret) {
		return true
	}
	u := s.authenticateRequest(r, db)
	if u != nil && u.IsAdmin {
		return true
	}
	if u != nil && u.Phone != "" {
		_, admins := s.getAllowedAndAdminPhones(db)
		return isPhoneInList(u.Phone, admins)
	}
	return false
}

// ==============================================================================
// Active Room Crash-Recovery Snapshot
// ==============================================================================

type activeRoomSnapshotEntry struct {
	SceneRoom *SceneWaitingRoom `json:"sceneRoom,omitempty"`
	AuthRoom  *RoomState        `json:"authRoom,omitempty"`
	Deal      *RoomDeal         `json:"deal,omitempty"`
}

type activeRoomsSnapshotFile struct {
	Version   int                       `json:"version"`
	SavedAt   int64                     `json:"savedAt"`
	RoomPairs []activeRoomSnapshotEntry `json:"roomPairs"`
}

func buildActiveRoomsSnapshotBytes(rooms map[string]*RoomState, sceneRooms map[string]*SceneWaitingRoom) []byte {
	if os.Getenv("DISABLE_ACTIVE_ROOM_SNAPSHOT") == "1" {
		return nil
	}
	seen := map[string]bool{}
	entries := []activeRoomSnapshotEntry{}
	for id, sr := range sceneRooms {
		seen[id] = true
		ar := rooms[id]
		if sr.Status == "finished" && ar == nil {
			continue
		}
		deal := sr.Deal
		if deal == nil && ar != nil {
			deal = ar.Deal
		}
		srCopy := *sr
		srCopy.Deal = nil
		var arCopy *RoomState
		if ar != nil {
			tmp := *ar
			tmp.Deal = nil
			if len(tmp.Chat) > 0 {
				cleanChat := make([]RoomChatMessage, len(tmp.Chat))
				for i, c := range tmp.Chat {
					c.Audio = nil
					cleanChat[i] = c
				}
				tmp.Chat = cleanChat
			}
			arCopy = &tmp
		}
		entries = append(entries, activeRoomSnapshotEntry{
			SceneRoom: &srCopy,
			AuthRoom:  arCopy,
			Deal:      deal,
		})
	}
	for id, ar := range rooms {
		if seen[id] || ar == nil {
			continue
		}
		if ar.Status == "finished" && ar.SettlementExpiresAt == 0 {
			continue
		}
		tmp := *ar
		tmp.Deal = nil
		if len(tmp.Chat) > 0 {
			cleanChat := make([]RoomChatMessage, len(tmp.Chat))
			for i, c := range tmp.Chat {
				c.Audio = nil
				cleanChat[i] = c
			}
			tmp.Chat = cleanChat
		}
		entries = append(entries, activeRoomSnapshotEntry{
			AuthRoom: &tmp,
			Deal:     ar.Deal,
		})
	}
	payload := activeRoomsSnapshotFile{
		Version:   1,
		SavedAt:   nowMs(),
		RoomPairs: entries,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return raw
}

func writeActiveRoomsSnapshotBytes(raw []byte) {
	if len(raw) == 0 || os.Getenv("DISABLE_ACTIVE_ROOM_SNAPSHOT") == "1" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(snapshotFilePath), 0755)
	tmp := snapshotFilePath + ".tmp." + randomHex(3)
	if os.WriteFile(tmp, raw, 0644) == nil {
		if os.Rename(tmp, snapshotFilePath) != nil {
			_ = os.Remove(tmp)
		}
	}
}

func saveActiveRoomsSnapshot(rooms map[string]*RoomState, sceneRooms map[string]*SceneWaitingRoom) {
	raw := buildActiveRoomsSnapshotBytes(rooms, sceneRooms)
	writeActiveRoomsSnapshotBytes(raw)
}

func loadActiveRoomsSnapshot(rooms map[string]*RoomState, sceneRooms map[string]*SceneWaitingRoom) int {
	if os.Getenv("DISABLE_ACTIVE_ROOM_SNAPSHOT") == "1" {
		return 0
	}
	raw, err := os.ReadFile(snapshotFilePath)
	if err != nil {
		return 0
	}
	var snap activeRoomsSnapshotFile
	if err := json.Unmarshal(raw, &snap); err != nil {
		return 0
	}
	if nowMs()-snap.SavedAt > 2*60*60*1000 {
		return 0
	}
	restored := 0
	for _, entry := range snap.RoomPairs {
		if entry.SceneRoom != nil {
			sr := entry.SceneRoom
			sr.Deal = entry.Deal
			for i := range sr.Players {
				sr.Players[i].SocketID = ""
				if sr.Status == "playing" || sr.Status == "starting" {
					sr.Players[i].IsDisconnected = true
					sr.Players[i].IsAutoManaged = true
				}
			}
			sceneRooms[sr.ID] = sr
		}
		if entry.AuthRoom != nil {
			ar := entry.AuthRoom
			ar.Deal = entry.Deal
			for i := range ar.Players {
				ar.Players[i].SocketID = ""
				if ar.Status == "playing" || ar.Status == "starting" {
					ar.Players[i].IsDisconnected = true
					ar.Players[i].IsAutoManaged = true
				}
			}
			rooms[ar.ID] = ar
			restored++
		}
	}
	return restored
}
