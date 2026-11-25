package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const lastAdminChatFile = "data/last_admin_chat.json"

var shanghaiZone = time.FixedZone("Asia/Shanghai", 8*3600)

type adminPendingState struct {
	Action      string
	TargetPhone string
	Timestamp   int64
}

type telegramBotStatus struct {
	Enabled              bool        `json:"enabled"`
	Mode                 string      `json:"mode"`
	TokenConfigured      bool        `json:"tokenConfigured"`
	TokenMasked          string      `json:"tokenMasked"`
	URLConfigured        string      `json:"urlConfigured"`
	APIBase              string      `json:"apiBase,omitempty"`
	BotUsername          string      `json:"botUsername,omitempty"`
	BotName              string      `json:"botName,omitempty"`
	WebhookURL           string      `json:"webhookUrl,omitempty"`
	PollingActive        bool        `json:"pollingActive"`
	LastPollTimestamp    *int64      `json:"lastPollTimestamp"`
	LastMessageTimestamp *int64      `json:"lastMessageTimestamp"`
	ProcessedCount       int         `json:"processedCount"`
	LastError            *string     `json:"lastError"`
	LastWebhookInfo      interface{} `json:"lastWebhookInfo,omitempty"`
}

type TelegramManager struct {
	mu               sync.Mutex
	lastAdminChatID  string
	pendingStates    map[string]adminPendingState
	processedUpdates []int64
	processedSet     map[int64]bool
	backupTimer      *time.Timer
	backupReasons    []string
	pollingCancel    chan struct{}
	status           telegramBotStatus
}

func newTelegramManager() *TelegramManager {
	return &TelegramManager{
		pendingStates: map[string]adminPendingState{},
		processedSet:  map[int64]bool{},
		status: telegramBotStatus{
			Mode: "disabled",
		},
	}
}

var (
	tgPhoneOnlyRe   = regexp.MustCompile(`^(?:\+?86)?(1[3-9]\d{9}|\d{5,15})$`)
	tgPhonePointsRe = regexp.MustCompile(`^(?:\+?86)?(1[3-9]\d{9}|\d{5,15})\s+([+-]?\d+)$`)
	tgDigitsOnlyRe  = regexp.MustCompile(`^[+-]?\d+$`)
	tgCommaNumRe    = regexp.MustCompile(`^[0-9,+-]+$`)
	tgKVLineRe      = regexp.MustCompile(`^(?:\+?86)?(1[3-9]\d{9}|\d{5,15})[\s,:=]+([+-]?\d+)`)
	tgFindPhoneRe   = regexp.MustCompile(`(?i)^(?:🔍\s*)?(?:输入手机号搜索玩家|搜手机号查玩家|手机号搜索玩家|搜索玩家|查手机号|搜手机号|查用户|搜索用户|查找玩家|查手机|findphone|searchphone|finduser)(?:\s+(.+))?$`)
	tgClearUsersRe  = regexp.MustCompile(`(?i)^(?:🧹\s*)?(?:清空所有用户|清空用户|清理所有用户|清空全部茶客|清空茶客|clearusers|cleanusers)$`)
	tgDelUserRe     = regexp.MustCompile(`(?i)^(?:❌\s*)?(?:删除用户|注销用户|清理用户|deluser|removeuser)(?:\s+(.+))?$`)
	tgDelPhoneRe    = regexp.MustCompile(`(?i)^(?:🗑️\s*)?(?:删除手机号|移出手机号|删除手机|移除手机号|移除手机|移出白名单|删除白名单|delphone|removephone)(?:\s+(.+))?$`)
	tgAuthPhoneRe   = regexp.MustCompile(`(?i)^(?:📱\s*)?(?:授权手机号|添加手机号|增加手机号|添加白名单|authphone|addphone|授权|加白)(?:\s+(.+))?$`)
	tgAddPointsRe   = regexp.MustCompile(`(?i)^(?:➕\s*)?(?:注入筹码|增加筹码|加筹码|充值|addpoints|加币|加分)(?:\s+(.+))?$`)
	tgSubPointsRe   = regexp.MustCompile(`(?i)^(?:➖\s*)?(?:扣除筹码|扣减筹码|减筹码|扣分|subpoints|减币|扣币)(?:\s+(.+))?$`)
	tgSetPointsRe   = regexp.MustCompile(`(?i)^(?:⚙️\s*)?(?:重设筹码|设定筹码|重置筹码|改筹码|setpoints)(?:\s+(.+))?$`)
	tgBroadcastRe   = regexp.MustCompile(`(?i)^(?:📢\s*)?(?:全服广播|发布广播|全服通告|广播|broadcast)(?:\s+(.+))?$`)
	tgExportTxtRe   = regexp.MustCompile(`(?i)^(?:📄\s*)?(?:玩家积分TXT榜单|玩家积分TXT|玩家积分榜|积分榜单|TXT榜单|导出积分|积分导出|txt|exporttxt)(?:\s+(.+))?$`)
	tgMdUnderscore  = regexp.MustCompile(`([a-zA-Z0-9_\p{Han}])_([a-zA-Z0-9_\p{Han}])`)
	tgWebhookSecRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
)

func formatShanghaiTime(ms int64) string {
	var t time.Time
	if ms > 0 {
		t = time.UnixMilli(ms).In(shanghaiZone)
	} else {
		t = time.Now().In(shanghaiZone)
	}
	return t.Format("2006/1/2 15:04:05")
}

func formatIntComma(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var out []byte
	cnt := 0
	for i := len(s) - 1; i >= 0; i-- {
		if cnt > 0 && cnt%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
		cnt++
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func escapeHTMLText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func sanitizeTelegramMarkdown(text string) string {
	if text == "" {
		return ""
	}
	parts := strings.Split(text, "`")
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('`')
		}
		if i%2 == 1 {
			b.WriteString(p)
		} else {
			b.WriteString(tgMdUnderscore.ReplaceAllString(p, `$1\_$2`))
		}
	}
	return b.String()
}

func stripMarkdownSymbols(text string) string {
	replacer := strings.NewReplacer("*", "", "_", "", "`", "", "[", "", "]", "")
	return replacer.Replace(text)
}

func maskBotToken(token string) string {
	if len(token) < 10 {
		return "***"
	}
	return token[:5] + "..." + token[len(token)-4:]
}

func normalizePublicURL(raw string) string {
	u := strings.Trim(strings.TrimSpace(raw), `"'`)
	if u == "" {
		return ""
	}
	low := strings.ToLower(u)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		u = "https://" + u
	}
	return strings.TrimRight(u, "/")
}

func getTelegramAPIBase(sm *StorageManager) string {
	sm.loadEnvFile()
	raw := ""
	for _, k := range []string{"TELEGRAM_API_BASE", "TG_PROXY", "TG_API_BASE", "TELEGRAM_PROXY"} {
		if v := sm.getEnv(k); v != "" {
			raw = v
			break
		}
	}
	if raw == "" {
		return "https://api.telegram.org"
	}
	low := strings.ToLower(raw)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		raw = "https://" + raw
	}
	raw = strings.TrimRight(raw, "/")
	if strings.HasSuffix(strings.ToLower(raw), "/bot") {
		raw = raw[:len(raw)-4]
	}
	return raw
}

func getPublicWebhookURL(sm *StorageManager) string {
	sm.loadEnvFile()
	for _, k := range []string{"WEBHOOK_URL", "URL", "APP_URL", "SERVER_URL"} {
		if v := sm.getEnv(k); v != "" {
			return normalizePublicURL(v)
		}
	}
	if raw, err := os.ReadFile("server_url.txt"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			t := strings.TrimSpace(line)
			if t != "" && !strings.HasPrefix(t, "#") && (strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://")) {
				return normalizePublicURL(t)
			}
		}
	}
	return ""
}

func getTelegramBotToken(sm *StorageManager) string {
	sm.loadEnvFile()
	tok := strings.Trim(strings.TrimSpace(sm.getEnv("TELEGRAM_BOT_TOKEN")), `"'`)
	if tok == "YOUR_TELEGRAM_BOT_TOKEN_HERE" {
		return ""
	}
	return tok
}

func (tm *TelegramManager) getStoredAdminChatID(sm *StorageManager) string {
	tm.mu.Lock()
	cached := tm.lastAdminChatID
	tm.mu.Unlock()
	if cached != "" {
		return cached
	}
	if raw, err := os.ReadFile(lastAdminChatFile); err == nil {
		var parsed struct {
			ChatID interface{} `json:"chatId"`
		}
		if json.Unmarshal(raw, &parsed) == nil && parsed.ChatID != nil {
			cid := strings.TrimSpace(fmt.Sprintf("%v", parsed.ChatID))
			if cid != "" && cid != "<nil>" {
				tm.mu.Lock()
				tm.lastAdminChatID = cid
				tm.mu.Unlock()
				return cid
			}
		}
	}
	if v := sm.getEnv("TELEGRAM_ADMIN_CHAT_ID"); v != "" {
		return v
	}
	return sm.getEnv("TELEGRAM_CHAT_ID")
}

func (tm *TelegramManager) storeAdminChatID(chatID string) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" || chatID == "<nil>" {
		return
	}
	tm.mu.Lock()
	tm.lastAdminChatID = chatID
	tm.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(lastAdminChatFile), 0755)
	if payload, err := json.Marshal(map[string]interface{}{
		"chatId":    chatID,
		"updatedAt": nowMs(),
	}); err == nil {
		_ = os.WriteFile(lastAdminChatFile, payload, 0644)
	}
}

func getAdminUsernamesFromStorage(sm *StorageManager) []string {
	sm.loadEnvFile()
	seen := map[string]bool{}
	out := []string{}
	addList := func(raw string) {
		for _, tok := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t'
		}) {
			c := strings.TrimPrefix(strings.TrimSpace(tok), "@")
			if c != "" && c != "admin_username" && !seen[strings.ToLower(c)] {
				seen[strings.ToLower(c)] = true
				out = append(out, c)
			}
		}
	}
	addList(sm.getEnv("ADMIN_USERNAMES"))
	addList(sm.getEnv("TELEGRAM_ADMIN_USERNAME"))
	addList(sm.getEnv("TELEGRAM_ADMIN_USERNAMES"))
	return out
}

func buildTelegramAdminKeyboard() map[string]interface{} {
	return map[string]interface{}{
		"keyboard": [][]map[string]string{
			{{"text": "🔍 搜手机号查玩家"}, {"text": "👥 玩家名册"}, {"text": "📄 玩家积分TXT榜单"}},
			{{"text": "➕ 注入筹码"}, {"text": "➖ 扣除筹码"}, {"text": "📱 授权手机号"}},
			{{"text": "📋 白名单列表"}, {"text": "❌ 删除用户"}, {"text": "📢 全服广播"}},
		},
		"resize_keyboard":        true,
		"is_persistent":          true,
		"input_field_placeholder": "👉 输入手机号搜索玩家或直接发送手机号授权，发：手机号 金额充值...",
	}
}

type exportedPointsTxt struct {
	TxtContent      string
	FileName        string
	SafeFileName    string
	SummaryTextHTML string
}

func exportPlayersPointsTxt(db *DatabaseSchema) exportedPointsTxt {
	profiles := make([]*UserProfile, 0, len(db.Profiles))
	totalCoins := 0
	for _, p := range db.Profiles {
		if p != nil {
			profiles = append(profiles, p)
			totalCoins += p.Coins
		}
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Coins > profiles[j].Coins
	})

	now := time.Now().In(shanghaiZone)
	timeStr := now.Format("2006/1/2 15:04:05")
	dateTag := now.Format("20060102_150405")
	fileName := fmt.Sprintf("茶楼玩家积分全量备份_%s.txt", dateTag)
	safeFileName := fmt.Sprintf("Teahouse_Players_Backup_%s.txt", dateTag)

	var b strings.Builder
	b.WriteString("\uFEFF")
	b.WriteString("=================================================================\r\n")
	b.WriteString("       🏮 经典棋牌茶楼 - 玩家积分与资产全量数据备份 TXT 文件\r\n")
	b.WriteString("=================================================================\r\n")
	b.WriteString(fmt.Sprintf("• 备份生成时间: %s (北京时间)\r\n", timeStr))
	b.WriteString(fmt.Sprintf("• 注册玩家总数: %d 人\r\n", len(profiles)))
	b.WriteString(fmt.Sprintf("• 全服筹码流动总量: %s 分\r\n", formatIntComma(totalCoins)))
	b.WriteString("-----------------------------------------------------------------\r\n")
	b.WriteString("序号 | UID | 登录手机号 | 牌桌名号 | 可用筹码(分) | 等级 | 状态 | 最后活跃时间\r\n")
	b.WriteString("-----------------------------------------------------------------\r\n")

	if len(profiles) == 0 {
		b.WriteString("(暂无注册玩家资料)\r\n")
	} else {
		for idx, p := range profiles {
			pPhone := strings.TrimSpace(p.Phone)
			if pPhone == "" {
				pPhone = "未绑定"
			}
			statusStr := "正常"
			if p.IsBanned {
				statusStr = "已封禁"
			}
			lastActive := "无活跃记录"
			if p.UpdatedAt > 0 {
				lastActive = formatShanghaiTime(p.UpdatedAt)
			}
			name := p.Name
			if name == "" {
				name = "无名茶客"
			}
			lv := p.Level
			if lv <= 0 {
				lv = 1
			}
			b.WriteString(fmt.Sprintf("%2d. | %s | %s | %s | %s | Lv.%d | %s | %s\r\n",
				idx+1, p.ID, pPhone, name, formatIntComma(p.Coins), lv, statusStr, lastActive))
		}
	}

	b.WriteString("-----------------------------------------------------------------\r\n")
	b.WriteString("【Termux 硬件故障紧急恢复说明】:\r\n")
	b.WriteString("若设备或 Termux 硬件故障损坏导致数据库全盘丢失，请保留此 TXT 文档。\r\n")
	b.WriteString("在新的 Termux 设备或 Telegram Bot 中直接发送玩家“手机号”与“筹码”即可秒级还原！\r\n")
	b.WriteString("例如直接在 Bot 发送：13800138000 500000 即可自动开通授权并注入 500,000 筹码！\r\n")
	b.WriteString("=================================================================\r\n")

	summaryHTML := fmt.Sprintf(
		"📄 <b>【全服玩家积分全量数据 TXT 备份】</b>\n\n"+
			"• 🕒 <b>备份时刻</b>: <code>%s</code>\n"+
			"• 👥 <b>注册玩家总数</b>: <b>%d</b> 人\n"+
			"• 🪙 <b>全服筹码总额</b>: <b>%s</b> 分\n"+
			"• 💾 <b>灾备说明</b>: 已自动生成 TXT 榜单附件，便于 Termux 设备故障时秒级重组加积分还原！",
		escapeHTMLText(timeStr), len(profiles), formatIntComma(totalCoins),
	)

	return exportedPointsTxt{
		TxtContent:      b.String(),
		FileName:        fileName,
		SafeFileName:    safeFileName,
		SummaryTextHTML: summaryHTML,
	}
}

func (tm *TelegramManager) notifyPointsTxtToTelegram(sm *StorageManager, reason string, db *DatabaseSchema, targetChatID string) bool {
	botToken := getTelegramBotToken(sm)
	chatID := strings.TrimSpace(targetChatID)
	if chatID == "" {
		chatID = tm.getStoredAdminChatID(sm)
	}
	if botToken == "" || chatID == "" {
		return false
	}

	if db == nil {
		sm.WithDBRead(func(loaded *DatabaseSchema) {
			db = loaded
		})
	}
	if db == nil {
		return false
	}

	exp := exportPlayersPointsTxt(db)
	apiBase := getTelegramAPIBase(sm)
	captionHTML := fmt.Sprintf("%s\n\n📌 <b>数据变动来源</b>: %s", exp.SummaryTextHTML, escapeHTMLText(reason))

	sendDoc := func(caption, parseMode string) bool {
		var bodyBuf bytes.Buffer
		mw := multipart.NewWriter(&bodyBuf)
		_ = mw.WriteField("chat_id", chatID)
		_ = mw.WriteField("caption", caption)
		if parseMode != "" {
			_ = mw.WriteField("parse_mode", parseMode)
		}
		hdr := make(textproto.MIMEHeader)
		hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="document"; filename="%s"`, exp.SafeFileName))
		hdr.Set("Content-Type", "text/plain; charset=utf-8")
		part, err := mw.CreatePart(hdr)
		if err != nil {
			return false
		}
		_, _ = part.Write([]byte(exp.TxtContent))
		_ = mw.Close()

		req, err := http.NewRequest(http.MethodPost, apiBase+"/bot"+botToken+"/sendDocument", &bodyBuf)
		if err != nil {
			return false
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var out map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&out) == nil {
			if ok, _ := out["ok"].(bool); ok {
				return true
			}
		}
		return false
	}

	if sendDoc(captionHTML, "HTML") {
		return true
	}
	plainCaption := fmt.Sprintf("📄 【全服玩家积分 TXT 备份】\n备份时间: %s\n玩家总数: %d\n变动原因: %s",
		formatShanghaiTime(0), len(db.Profiles), reason)
	return sendDoc(plainCaption, "")
}

func (tm *TelegramManager) triggerDebouncedTxtBackup(sm *StorageManager, reason string, targetChatID string) {
	tm.mu.Lock()
	tm.backupReasons = append(tm.backupReasons, reason)
	if tm.backupTimer != nil {
		tm.backupTimer.Stop()
	}
	tm.backupTimer = time.AfterFunc(2500*time.Millisecond, func() {
		tm.mu.Lock()
		seen := map[string]bool{}
		uniq := []string{}
		for _, r := range tm.backupReasons {
			if !seen[r] {
				seen[r] = true
				uniq = append(uniq, r)
			}
		}
		tm.backupReasons = nil
		tm.backupTimer = nil
		tm.mu.Unlock()
		combined := strings.Join(uniq, "; ")
		var snapDB *DatabaseSchema
		sm.WithDBRead(func(d *DatabaseSchema) {
			snapDB = d
		})
		tm.notifyPointsTxtToTelegram(sm, combined, snapDB, targetChatID)
	})
	tm.mu.Unlock()
}

func decodeTxtBytesGo(buf []byte) string {
	if len(buf) == 0 {
		return ""
	}
	if len(buf) >= 3 && buf[0] == 0xEF && buf[1] == 0xBB && buf[2] == 0xBF {
		return string(buf[3:])
	}
	if len(buf) >= 2 && buf[0] == 0xFF && buf[1] == 0xFE {
		u16 := make([]uint16, 0, (len(buf)-2)/2)
		for i := 2; i+1 < len(buf); i += 2 {
			u16 = append(u16, uint16(buf[i])|uint16(buf[i+1])<<8)
		}
		return strings.TrimPrefix(string(utf16.Decode(u16)), "\uFEFF")
	}
	if len(buf) >= 2 && buf[0] == 0xFE && buf[1] == 0xFF {
		u16 := make([]uint16, 0, (len(buf)-2)/2)
		for i := 2; i+1 < len(buf); i += 2 {
			u16 = append(u16, uint16(buf[i])<<8|uint16(buf[i+1]))
		}
		return strings.TrimPrefix(string(utf16.Decode(u16)), "\uFEFF")
	}
	if utf8.Valid(buf) {
		return strings.TrimPrefix(string(buf), "\uFEFF")
	}
	return strings.TrimPrefix(string(buf), "\uFEFF")
}

func (tm *TelegramManager) handleIncomingTelegramDocument(h *Hub, doc map[string]interface{}, chatID, operator string) string {
	fileID, _ := doc["file_id"].(string)
	fileName, _ := doc["file_name"].(string)
	if fileName == "" {
		fileName = "document.txt"
	}
	botToken := getTelegramBotToken(h.storage)
	if botToken == "" {
		return "❌ 未配置 TELEGRAM_BOT_TOKEN，无法下载解析 TXT 文档。"
	}
	apiBase := getTelegramAPIBase(h.storage)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/bot%s/getFile?file_id=%s", apiBase, botToken, url.QueryEscape(fileID)))
	if err != nil {
		return "❌ 获取 Telegram 文件下载链接失败: " + err.Error()
	}
	defer resp.Body.Close()
	var gf struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gf); err != nil || !gf.OK || gf.Result.FilePath == "" {
		return "❌ 获取 Telegram 文件下载链接失败: " + gf.Description
	}
	dlResp, err := (&http.Client{Timeout: 20 * time.Second}).Get(fmt.Sprintf("%s/file/bot%s/%s", apiBase, botToken, gf.Result.FilePath))
	if err != nil {
		return "❌ 下载文档失败: " + err.Error()
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode < 200 || dlResp.StatusCode >= 300 {
		return fmt.Sprintf("❌ 下载文档失败 (HTTP %d)", dlResp.StatusCode)
	}
	rawBytes, err := io.ReadAll(io.LimitReader(dlResp.Body, 4<<20))
	if err != nil {
		return "❌ 读取文档内容失败: " + err.Error()
	}
	decoded := decodeTxtBytesGo(rawBytes)
	if strings.TrimSpace(decoded) == "" {
		return fmt.Sprintf("⚠️ 文档 `%s` 内容为空，未导入任何数据。", fileName)
	}

	updatedCount := 0
	newAuthCount := 0
	samples := []string{}

	_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
		authSet := map[string]bool{}
		for _, p := range db.AuthorizedPhones {
			if np := normalizePhone(p); np != "" {
				authSet[np] = true
			}
		}
		revokedFilter := func(clean string) {
			if len(db.RevokedPhones) == 0 {
				return
			}
			kept := make([]string, 0, len(db.RevokedPhones))
			for _, rp := range db.RevokedPhones {
				if normalizePhone(rp) != clean {
					kept = append(kept, rp)
				}
			}
			db.RevokedPhones = kept
		}

		for _, rawLine := range strings.Split(strings.ReplaceAll(decoded, "\r\n", "\n"), "\n") {
			line := strings.TrimSpace(rawLine)
			if line == "" || strings.HasPrefix(line, "=") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "【") ||
				strings.Contains(line, "茶客名号") || strings.Contains(line, "牌桌名号") || strings.Contains(line, "登录手机号") || strings.Contains(line, "可用筹码") || strings.Contains(line, "排名") {
				continue
			}

			if strings.Contains(line, "|") {
				parts := strings.Split(line, "|")
				foundPhone := ""
				var foundCoins *int
				foundName := ""
				for _, rawPart := range parts {
					part := strings.TrimSpace(rawPart)
					if part == "" {
						continue
					}
					if m := tgPhoneOnlyRe.FindStringSubmatch(part); len(m) > 1 && foundPhone == "" {
						foundPhone = normalizePhone(m[1])
						continue
					}
					if tgCommaNumRe.MatchString(part) && foundCoins == nil {
						if num, err := strconv.Atoi(strings.ReplaceAll(part, ",", "")); err == nil {
							foundCoins = &num
							continue
						}
					}
					if !strings.HasPrefix(part, "USER-") && !strings.HasPrefix(part, "Lv.") &&
						part != "正常" && part != "已封禁" && part != "无活跃记录" &&
						!strings.HasSuffix(part, ".") && foundName == "" {
						foundName = part
					}
				}
				if foundPhone != "" {
					if !authSet[foundPhone] {
						authSet[foundPhone] = true
						newAuthCount++
					}
					revokedFilter(foundPhone)
					user := findUserInDB(foundPhone, db)
					coinsVal := 0
					if foundCoins != nil {
						coinsVal = *foundCoins
					} else if user != nil {
						coinsVal = user.Coins
					}
					if coinsVal < 0 {
						coinsVal = 0
					}
					if user == nil {
						user = h.storage.ensureUserProfile(foundPhone, db, coinsVal)
					} else {
						user.Coins = coinsVal
						user.UpdatedAt = nowMs()
					}
					if user != nil {
						if foundName != "" && foundName != "未设置" && foundName != "无名茶客" && foundName != "-" {
							user.Name = foundName
						}
						updatedCount++
						if len(samples) < 5 {
							samples = append(samples, fmt.Sprintf("%s (%s): %d分", user.Name, foundPhone, user.Coins))
						}
					}
					continue
				}
			}

			if kv := tgKVLineRe.FindStringSubmatch(line); len(kv) > 2 {
				cleanPhone := normalizePhone(kv[1])
				coins, _ := strconv.Atoi(kv[2])
				if coins < 0 {
					coins = 0
				}
				if cleanPhone != "" {
					if !authSet[cleanPhone] {
						authSet[cleanPhone] = true
						newAuthCount++
					}
					revokedFilter(cleanPhone)
					user := findUserInDB(cleanPhone, db)
					if user == nil {
						user = h.storage.ensureUserProfile(cleanPhone, db, coins)
					} else {
						user.Coins = coins
						user.UpdatedAt = nowMs()
					}
					updatedCount++
					if user != nil && len(samples) < 5 {
						samples = append(samples, fmt.Sprintf("%s (%s): %d分", user.Name, cleanPhone, user.Coins))
					}
				}
				continue
			}

			if pm := tgPhoneOnlyRe.FindStringSubmatch(line); len(pm) > 1 {
				cleanPhone := normalizePhone(pm[1])
				if cleanPhone != "" {
					if !authSet[cleanPhone] {
						authSet[cleanPhone] = true
						newAuthCount++
					}
					revokedFilter(cleanPhone)
					h.storage.ensureUserProfile(cleanPhone, db, 0)
					updatedCount++
					if len(samples) < 5 {
						samples = append(samples, "已授权白名单: "+cleanPhone)
					}
				}
			}
		}

		db.AuthorizedPhones = make([]string, 0, len(authSet))
		for p := range authSet {
			db.AuthorizedPhones = append(db.AuthorizedPhones, p)
		}
		appendAdminLog(db, "TG_DOC_IMPORT", operator, fileName, fmt.Sprintf("解析导入 TXT 文档: 成功同步 %d 位玩家，新增授权 %d 个手机号", updatedCount, newAuthCount), 0)
		return true
	})

	h.BroadcastJSON(map[string]interface{}{"type": "all_users_coins_updated", "timestamp": nowMs()})
	h.broadcastTeahousePlayers()
	if chatID != "" {
		tm.storeAdminChatID(chatID)
		go tm.notifyPointsTxtToTelegram(h.storage, fmt.Sprintf("解析导入 TXT 文档 (%s)", fileName), nil, chatID)
	}

	var sb strings.Builder
	sb.WriteString("📥 *【TXT 文档智能解析导入成功】*\n\n")
	sb.WriteString(fmt.Sprintf("• 📄 文档名称: `%s`\n", fileName))
	sb.WriteString("• 🔤 编码识别: *UTF-8 / Unicode 智能解码* (零乱码保证)\n")
	sb.WriteString(fmt.Sprintf("• 👥 成功同步玩家: *%d* 位\n", updatedCount))
	sb.WriteString(fmt.Sprintf("• 📱 新增授权手机号: *%d* 个\n", newAuthCount))
	if len(samples) > 0 {
		sb.WriteString(fmt.Sprintf("\n*导入样本 (前 %d 条)*:\n", len(samples)))
		for _, s := range samples {
			sb.WriteString("  • " + s + "\n")
		}
	}
	sb.WriteString("\n✅ 全服数据已实时入库，客户端对局与钱庄余额已同步更新！")
	return sb.String()
}

func (tm *TelegramManager) isAuthorizedTelegramSender(h *Hub, chatID, fromID, username string) bool {
	h.storage.loadEnvFile()
	configuredChat := strings.TrimSpace(h.storage.getEnv("TELEGRAM_CHAT_ID"))
	if configuredChat == "" {
		configuredChat = strings.TrimSpace(h.storage.getEnv("TELEGRAM_ADMIN_CHAT_ID"))
	}
	storedChat := tm.getStoredAdminChatID(h.storage)
	adminUsers := getAdminUsernamesFromStorage(h.storage)

	// If explicit admin chat/usernames are configured, enforce them strictly
	hasConfiguredRestriction := configuredChat != "" || len(adminUsers) > 0
	if !hasConfiguredRestriction {
		// Allow stored admin chat or bootstrap when unconfigured
		return storedChat == "" || chatID == storedChat || fromID == storedChat
	}

	if configuredChat != "" && (chatID == configuredChat || fromID == configuredChat) {
		return true
	}
	if username != "" {
		lowU := strings.ToLower(strings.TrimPrefix(username, "@"))
		for _, au := range adminUsers {
			if strings.ToLower(au) == lowU {
				return true
			}
		}
	}
	return false
}

func extractMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]interface{})
	return v
}

func extractString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if f, ok := v.(float64); ok {
		return strconv.FormatInt(int64(f), 10)
	}
	return fmt.Sprintf("%v", v)
}

type telegramExecResult struct {
	OK          bool        `json:"ok"`
	Success     bool        `json:"success"`
	Reply       string      `json:"reply"`
	Command     string      `json:"command"`
	ReplyMarkup interface{} `json:"replyMarkup"`
	ChatID      string      `json:"chatId,omitempty"`
	Timestamp   string      `json:"timestamp"`
}

func (tm *TelegramManager) executeTelegramUpdate(h *Hub, body map[string]interface{}, isWebhook bool) telegramExecResult {
	if uidF, ok := body["update_id"].(float64); ok && uidF > 0 {
		uid := int64(uidF)
		tm.mu.Lock()
		dup := tm.processedSet[uid]
		if !dup {
			tm.processedSet[uid] = true
			tm.processedUpdates = append(tm.processedUpdates, uid)
			if len(tm.processedUpdates) > 500 {
				oldest := tm.processedUpdates[0]
				tm.processedUpdates = tm.processedUpdates[1:]
				delete(tm.processedSet, oldest)
			}
		}
		tm.mu.Unlock()
		if dup {
			return telegramExecResult{
				OK:        true,
				Success:   true,
				Reply:     "Deduplicated duplicate update_id",
				Timestamp: time.Now().In(shanghaiZone).Format("15:04"),
			}
		}
	}

	msgObj := extractMap(body, "message")
	if msgObj == nil {
		msgObj = extractMap(body, "edited_message")
	}
	if msgObj == nil {
		msgObj = extractMap(body, "channel_post")
	}
	cbObj := extractMap(body, "callback_query")
	if msgObj == nil && cbObj != nil {
		msgObj = extractMap(cbObj, "message")
	}

	fromObj := extractMap(msgObj, "from")
	if cbObj != nil && extractMap(cbObj, "from") != nil {
		fromObj = extractMap(cbObj, "from")
	}
	operator := extractString(body, "operator")
	if operator == "" {
		operator = extractString(fromObj, "username")
	}
	if operator == "" {
		operator = "Telegram Bot Admin"
	}

	chatObj := extractMap(msgObj, "chat")
	chatID := extractString(chatObj, "id")
	fromID := extractString(fromObj, "id")
	if chatID == "" {
		chatID = fromID
	}

	if isWebhook {
		username := extractString(fromObj, "username")
		if !tm.isAuthorizedTelegramSender(h, chatID, fromID, username) {
			return telegramExecResult{
				OK:        true,
				Success:   false,
				Reply:     "⛔ 无管理员操作权限：您的 Telegram 账号不在授权管理员名单中。",
				ChatID:    chatID,
				Timestamp: time.Now().In(shanghaiZone).Format("15:04"),
			}
		}
	}

	if chatID != "" {
		tm.storeAdminChatID(chatID)
	}

	docObj := extractMap(msgObj, "document")
	if docObj != nil {
		fn := strings.ToLower(extractString(docObj, "file_name"))
		mt := strings.ToLower(extractString(docObj, "mime_type"))
		if strings.HasSuffix(fn, ".txt") || strings.HasPrefix(mt, "text/") {
			docReply := tm.handleIncomingTelegramDocument(h, docObj, chatID, operator)
			markup := buildTelegramAdminKeyboard()
			if !isWebhook && chatID != "" {
				go tm.sendTelegramReply(h.storage, chatID, docReply, markup)
			}
			return telegramExecResult{
				OK:          true,
				Success:     true,
				Reply:       docReply,
				Command:     "/importtxt " + extractString(docObj, "file_name"),
				ReplyMarkup: markup,
				ChatID:      chatID,
				Timestamp:   time.Now().In(shanghaiZone).Format("15:04"),
			}
		}
	}

	rawText := extractString(body, "command")
	if rawText == "" {
		rawText = extractString(msgObj, "text")
	}
	if rawText == "" {
		rawText = extractString(msgObj, "caption")
	}
	if rawText == "" {
		rawText = extractString(cbObj, "data")
	}
	if rawText == "" {
		if contact := extractMap(msgObj, "contact"); contact != nil {
			rawText = extractString(contact, "phone_number")
		}
	}

	trimmed := strings.TrimSpace(rawText)
	senderKey := chatID
	if senderKey == "" {
		senderKey = fromID
	}
	if senderKey == "" {
		senderKey = operator
	}
	if senderKey == "" {
		senderKey = "default_admin"
	}

	now := nowMs()
	tm.mu.Lock()
	pend, hasPend := tm.pendingStates[senderKey]
	if hasPend && now-pend.Timestamp >= 600000 {
		delete(tm.pendingStates, senderKey)
		hasPend = false
	}
	tm.mu.Unlock()

	if m := tgPhonePointsRe.FindStringSubmatch(trimmed); len(m) > 2 {
		if strings.HasPrefix(m[2], "-") {
			rawText = "/subpoints " + m[1] + " " + strings.TrimPrefix(m[2], "-")
		} else {
			rawText = "/addpoints " + m[1] + " " + strings.TrimPrefix(m[2], "+")
		}
		tm.mu.Lock()
		delete(tm.pendingStates, senderKey)
		tm.mu.Unlock()
	} else if hasPend && pend.TargetPhone != "" && tgDigitsOnlyRe.MatchString(trimmed) {
		numStr := strings.TrimPrefix(trimmed, "+")
		switch pend.Action {
		case "addpoints":
			rawText = "/addpoints " + pend.TargetPhone + " " + numStr
		case "subpoints":
			rawText = "/subpoints " + pend.TargetPhone + " " + strings.TrimPrefix(numStr, "-")
		case "setpoints":
			rawText = "/setpoints " + pend.TargetPhone + " " + numStr
		}
		tm.mu.Lock()
		delete(tm.pendingStates, senderKey)
		tm.mu.Unlock()
	} else if m := tgPhoneOnlyRe.FindStringSubmatch(trimmed); len(m) > 1 {
		targetPhone := m[1]
		if hasPend {
			switch pend.Action {
			case "delphone":
				rawText = "/delphone " + targetPhone
			case "deluser":
				rawText = "/deluser " + targetPhone
			case "findphone":
				rawText = "/findphone " + targetPhone
			default:
				rawText = "/authphone " + targetPhone
			}
		} else {
			rawText = "/authphone " + targetPhone
		}
		tm.mu.Lock()
		delete(tm.pendingStates, senderKey)
		tm.mu.Unlock()
	} else if hasPend && pend.Action == "broadcast" && !strings.HasPrefix(trimmed, "/") {
		rawText = "/broadcast " + trimmed
		tm.mu.Lock()
		delete(tm.pendingStates, senderKey)
		tm.mu.Unlock()
	} else if m := tgFindPhoneRe.FindStringSubmatch(trimmed); m != nil {
		if strings.TrimSpace(m[1]) != "" {
			rawText = "/findphone " + strings.TrimSpace(m[1])
		} else {
			rawText = "/findphone"
		}
	} else if tgClearUsersRe.MatchString(trimmed) {
		rawText = "/clearusers"
	} else if m := tgDelUserRe.FindStringSubmatch(trimmed); m != nil {
		if strings.TrimSpace(m[1]) != "" {
			rawText = "/deluser " + strings.TrimSpace(m[1])
		} else {
			rawText = "/deluser"
		}
	} else if m := tgDelPhoneRe.FindStringSubmatch(trimmed); m != nil {
		if strings.TrimSpace(m[1]) != "" {
			rawText = "/delphone " + strings.TrimSpace(m[1])
		} else {
			rawText = "/delphone"
		}
	} else if m := tgAuthPhoneRe.FindStringSubmatch(trimmed); m != nil {
		if strings.TrimSpace(m[1]) != "" {
			rawText = "/authphone " + strings.TrimSpace(m[1])
		} else {
			rawText = "/authphone"
		}
	} else if m := tgAddPointsRe.FindStringSubmatch(trimmed); m != nil {
		if strings.TrimSpace(m[1]) != "" {
			rawText = "/addpoints " + strings.TrimSpace(m[1])
		} else {
			rawText = "/addpoints"
		}
	} else if m := tgSubPointsRe.FindStringSubmatch(trimmed); m != nil {
		if strings.TrimSpace(m[1]) != "" {
			rawText = "/subpoints " + strings.TrimSpace(m[1])
		} else {
			rawText = "/subpoints"
		}
	} else if m := tgSetPointsRe.FindStringSubmatch(trimmed); m != nil {
		if strings.TrimSpace(m[1]) != "" {
			rawText = "/setpoints " + strings.TrimSpace(m[1])
		} else {
			rawText = "/setpoints"
		}
	} else if m := tgBroadcastRe.FindStringSubmatch(trimmed); m != nil {
		if strings.TrimSpace(m[1]) != "" {
			rawText = "/broadcast " + strings.TrimSpace(m[1])
		} else {
			rawText = "/broadcast"
		}
	} else if tgExportTxtRe.MatchString(trimmed) {
		rawText = "/exporttxt"
	} else if trimmed == "👥 玩家名册" || strings.Contains(trimmed, "玩家名册") || strings.Contains(trimmed, "查看玩家") || trimmed == "用户列表" {
		rawText = "/users"
	} else if strings.Contains(trimmed, "运营统计") {
		rawText = "/stats"
	} else if strings.Contains(trimmed, "匹配池") || strings.Contains(trimmed, "匹配监控") {
		rawText = "/matchpool"
	} else if strings.Contains(trimmed, "白名单列表") || strings.Contains(trimmed, "授权列表") || trimmed == "白名单" {
		rawText = "/whitelist"
	} else if strings.Contains(trimmed, "牌桌巡视") || strings.Contains(trimmed, "巡视牌桌") {
		rawText = "/rooms"
	} else if strings.Contains(trimmed, "审计日志") || strings.Contains(trimmed, "操作日志") {
		rawText = "/logs"
	} else if strings.Contains(trimmed, "帮助") || strings.Contains(trimmed, "菜单") || strings.Contains(trimmed, "指南") {
		rawText = "/help"
	}

	parts := strings.Fields(rawText)
	cmd := ""
	arg1 := ""
	arg2 := ""
	if len(parts) > 0 {
		cmd = strings.ToLower(parts[0])
	}
	if len(parts) > 1 {
		arg1 = parts[1]
	}
	if len(parts) > 2 {
		arg2 = parts[2]
	}

	reply := ""
	success := true
	var customInlineMarkup interface{}

	setPend := func(action, phone string) {
		tm.mu.Lock()
		tm.pendingStates[senderKey] = adminPendingState{Action: action, TargetPhone: phone, Timestamp: nowMs()}
		tm.mu.Unlock()
	}

	switch cmd {
	case "/start", "/help":
		reply = "🤖 *Telegram 机器人茶楼管理员控制中枢*\n\n" +
			"⚡ *常用零门槛免前缀操作*：\n" +
			"• 🔍 *查玩家*: 点击【🔍 搜手机号查玩家】或发 `/findphone 手机号`，*显示玩家完整档案*！\n" +
			"• 📱 *直接发手机号* (例: `13800138000`)：*自动授权白名单*，无需前缀！\n" +
			"• ➕ *直接发：手机号 金额* (例: `13800138000 5000`)：*秒级为玩家注入筹码*！\n" +
			"• ➖ *直接发：手机号 负金额* (例: `13800138000 -2000`)：*秒级扣减筹码*！\n\n" +
			"💡 *支持指令*：\n" +
			"• `/findphone <手机号>` - 搜索玩家完整档案与战绩\n" +
			"• `/exporttxt` - 导出全服玩家积分 TXT 榜单\n" +
			"• `/users` - 查看所有茶客玩家列表与筹码\n" +
			"• `/whitelist` - 查看已授权手机号白名单\n" +
			"• `/authphone <手机号>` - 添加手机号到注册白名单\n" +
			"• `/deluser <手机号/UID>` - 注销并删除指定茶客\n" +
			"• `/addpoints <手机号/UID> <数量>` - 为玩家注入筹码\n" +
			"• `/subpoints <手机号/UID> <数量>` - 扣减玩家筹码\n" +
			"• `/broadcast <通告内容>` - 向全服所有房间发送广播通告"

	case "/txt", "/exporttxt":
		var snap *DatabaseSchema
		h.storage.WithDBRead(func(db *DatabaseSchema) { snap = db })
		exp := exportPlayersPointsTxt(snap)
		go tm.notifyPointsTxtToTelegram(h.storage, "管理员手动导出玩家积分TXT榜单", snap, chatID)
		reply = exp.SummaryTextHTML + "\n\n📄 *已为您实时生成全量玩家积分 TXT 榜单，并作为文件附件推送到当前 Telegram 会话中！*\n（可随时用于 Termux 硬件故障报废时的全量还原）"

	case "/findphone", "/searchphone", "/finduser":
		if arg1 == "" {
			setPend("findphone", "")
			reply = "🔍 *【输入手机号搜索玩家】*\n\n👉 *请直接发送要查询的玩家手机号*：\n（例如直接输入：`13800138000`，系统将立即检索并展示该茶客全部详细完整档案）"
			break
		}
		query := strings.TrimSpace(arg1)
		cleanPhone := normalizePhone(query)
		var found *UserProfile
		var isWhitelisted, isAdminUser bool
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			allowedPhones, adminPhones := h.storage.getAllowedAndAdminPhones(db)
			for _, p := range db.Profiles {
				if p != nil && p.Phone != "" && cleanPhone != "" && phonesMatch(p.Phone, cleanPhone) {
					found = p
					break
				}
			}
			if found == nil {
				found = findUserInDB(query, db)
			}
			changed := false
			if found == nil && len(cleanPhone) >= 5 {
				if isPhoneInList(cleanPhone, allowedPhones) || isPhoneInList(cleanPhone, adminPhones) {
					found = h.storage.ensureUserProfile(cleanPhone, db, 0)
					changed = true
				}
			}
			if found != nil {
				uPhone := normalizePhone(found.Phone)
				isWhitelisted = isPhoneInList(uPhone, allowedPhones) || isPhoneInList(uPhone, adminPhones)
				isAdminUser = found.IsAdmin || isPhoneInList(uPhone, adminPhones)
			}
			return changed
		})
		if found != nil {
			uPhone := normalizePhone(found.Phone)
			if uPhone == "" {
				uPhone = "未绑定"
			}
			avatar := found.Avatar
			if avatar == "" {
				avatar = "👑"
			}
			regStr := "早期注册"
			if found.RegisteredAt > 0 {
				regStr = formatShanghaiTime(found.RegisteredAt)
			}
			updStr := "暂无记录"
			if found.UpdatedAt > 0 {
				updStr = formatShanghaiTime(found.UpdatedAt)
			}
			ys := found.Stats["yansan"]
			ss := found.Stats["shisanshui"]
			mj := found.Stats["mahjong"]
			totPlayed := ys.Played + ss.Played + mj.Played
			totWon := ys.Won + ss.Won + mj.Won
			winRate := "0.0%"
			if totPlayed > 0 {
				winRate = fmt.Sprintf("%.1f%%", float64(totWon)*100.0/float64(totPlayed))
			}
			wlBadge := "🔴 未在白名单"
			if isWhitelisted {
				wlBadge = "🟢 已获授权 (已建档)"
			}
			actBadge := "📱 白名单授权·待首登"
			if found.Password != "" {
				actBadge = "🟢 已设置密码对战中"
			}
			roleBadge := "🍵 普通牌友茶客"
			if isAdminUser {
				roleBadge = "👑 管理员特权账号"
			}
			banBadge := "🟢 正常活跃"
			if found.IsBanned {
				reason := found.BannedReason
				if reason == "" {
					reason = "违规操作"
				}
				banBadge = fmt.Sprintf("🚫 *已封禁* (%s)", reason)
			}
			lv := found.Level
			if lv <= 0 {
				lv = 1
			}
			rt := found.Rating
			if rt <= 0 {
				rt = 1800
			}
			reply = fmt.Sprintf(
				"👤 *【茶客完整档案卡】*\n\n"+
					"• 🏷️ *牌桌名号*: *%s* (印信头像: %s)\n"+
					"• 📱 *登录手机号*: `%s`\n"+
					"• 🆔 *用户 UID*: `%s`\n"+
					"• 📋 *白名单授权*: %s\n"+
					"• 🔑 *对战激活*: %s\n"+
					"• 👑 *权限级别*: %s\n"+
					"• 🔒 *账号状态*: %s\n\n"+
					"💰 *资产与天梯*:\n"+
					"• 🪙 可用筹码: *%s* 分\n"+
					"• 🏆 天梯积分: *%d* 分 (Lv.%d)\n"+
					"• 📅 注册建档: %s\n"+
					"• 🕒 最后活跃: %s\n\n"+
					"🎮 *全游戏战绩统计* (总场次: %d 场 · 综合胜率: %s):\n"+
					"• ♠️ *经典烟三*: 对局 %d 场 | 胜 %d 场 | 最高分 %d\n"+
					"• 🃏 *水镜十三水*: 对局 %d 场 | 胜 %d 场 | 最高分 %d\n"+
					"• 🀄 *国标麻将*: 对局 %d 场 | 胜 %d 场 | 最高分 %d",
				found.Name, avatar, uPhone, found.ID, wlBadge, actBadge, roleBadge, banBadge,
				formatIntComma(found.Coins), rt, lv, regStr, updStr,
				totPlayed, winRate,
				ys.Played, ys.Won, ys.HighestScore,
				ss.Played, ss.Won, ss.HighestScore,
				mj.Played, mj.Won, mj.HighestScore,
			)
			if uPhone != "未绑定" {
				reply += fmt.Sprintf("\n\n⚡ *快捷管理指令*:\n• 充值筹码: 发送 `%s 5000`\n• 扣减筹码: 发送 `%s -2000`\n• 注销账号: 发送 `/deluser %s`", uPhone, uPhone, uPhone)
				customInlineMarkup = map[string]interface{}{
					"inline_keyboard": [][]map[string]string{
						{
							{"text": "🪙 +5,000分", "callback_data": "/addpoints " + uPhone + " 5000"},
							{"text": "🪙 +10,000分", "callback_data": "/addpoints " + uPhone + " 10000"},
							{"text": "➖ -2,000分", "callback_data": "/subpoints " + uPhone + " 2000"},
						},
						{
							{"text": "🔍 刷新档案", "callback_data": "/findphone " + uPhone},
							{"text": "🗑️ 移出白名单", "callback_data": "/delphone " + uPhone},
						},
					},
				}
			}
		} else {
			fallback := cleanPhone
			if fallback == "" {
				fallback = query
			}
			reply = fmt.Sprintf("❌ 未找到手机号为 `%s` 的茶客，且该手机号尚未在白名单中。\n\n👉 提示：您可以直接发送该手机号 `%s` 秒级开通白名单授权与玩家名册建档！", query, fallback)
			success = false
		}

	case "/users":
		var list []*UserProfile
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			allowedPhones, adminPhones := h.storage.getAllowedAndAdminPhones(db)
			added := false
			for _, ap := range append(allowedPhones, adminPhones...) {
				if len(ap) >= 5 && findUserInDB(ap, db) == nil {
					h.storage.ensureUserProfile(ap, db, 0)
					added = true
				}
			}
			for _, p := range db.Profiles {
				if p != nil {
					list = append(list, p)
				}
			}
			return added
		})
		sort.Slice(list, func(i, j int) bool { return list[i].Coins > list[j].Coins })
		if len(list) == 0 {
			reply = "👥 *当前注册茶客名单*: 暂无注册茶客。\n\n可在 Bot 中直接发送手机号添加白名单授权（例如发送：`13800138000`）。"
			break
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("👥 *茶楼全员茶客名册 (共 %d 人)*:\n\n", len(list)))
		limit := len(list)
		if limit > 30 {
			limit = 30
		}
		for _, u := range list[:limit] {
			badge := "📱白名单授权·待首登"
			if u.Password != "" {
				badge = "🟢已激活对战"
			}
			uPhone := normalizePhone(u.Phone)
			if uPhone == "" {
				uPhone = normalizePhone(u.Username)
			}
			if uPhone == "" {
				uPhone = "未绑"
			}
			av := u.Avatar
			if av == "" {
				av = "👤"
			}
			adm := ""
			if u.IsAdmin {
				adm = " 👑管理员"
			}
			ban := ""
			if u.IsBanned {
				ban = " [🚫已封禁]"
			}
			sb.WriteString(fmt.Sprintf("• %s *%s* (📱 `%s`) - 🪙 *%s* 分 | %s%s%s\n",
				av, u.Name, uPhone, formatIntComma(u.Coins), badge, adm, ban))
		}
		if len(list) > 30 {
			sb.WriteString(fmt.Sprintf("\n... 还有 %d 位茶客未完全展开", len(list)-30))
		}
		samplePhone := "13800138000"
		if np := normalizePhone(list[0].Phone); np != "" {
			samplePhone = np
		}
		sb.WriteString(fmt.Sprintf("\n💡 提示: 直接发送【手机号 筹码数量】(如 `%s 5000`) 即可秒级为任意茶客充值筹码！", samplePhone))
		reply = sb.String()

	case "/whitelist":
		h.storage.WithDBRead(func(db *DatabaseSchema) {
			allowedPhones, adminPhones := h.storage.getAllowedAndAdminPhones(db)
			adminUsers := getAdminUsernamesFromStorage(h.storage)
			var sb strings.Builder
			sb.WriteString("📋 *茶楼授权白名单与管理员名册*:\n\n")
			if len(adminUsers) > 0 {
				sb.WriteString("👑 *管理员 Telegram 用户名*:\n")
				for _, u := range adminUsers {
					sb.WriteString(fmt.Sprintf("  • `@%s`\n", u))
				}
			}
			if len(adminPhones) > 0 {
				sb.WriteString(fmt.Sprintf("\n👑 *特权管理员账号* (%d 个):\n", len(adminPhones)))
				for _, p := range adminPhones {
					sb.WriteString(fmt.Sprintf("  • `%s`\n", p))
				}
			}
			sb.WriteString(fmt.Sprintf("\n📱 *允许注册手机号白名单与建档* (%d 个):\n", len(allowedPhones)))
			if len(allowedPhones) > 0 {
				lim := len(allowedPhones)
				if lim > 25 {
					lim = 25
				}
				for _, p := range allowedPhones[:lim] {
					up := findUserInDB(p, db)
					coinTxt := ""
					actTxt := " · 📱待首登"
					if up != nil {
						coinTxt = fmt.Sprintf(" (🪙 %s分)", formatIntComma(up.Coins))
						if up.Password != "" {
							actTxt = " · 🟢已对战"
						}
					}
					sb.WriteString(fmt.Sprintf("  • `%s`%s%s\n", p, coinTxt, actTxt))
				}
				if len(allowedPhones) > 25 {
					sb.WriteString(fmt.Sprintf("  ... 及其他 %d 个手机号\n", len(allowedPhones)-25))
				}
			} else {
				sb.WriteString("  • 暂无已授权手机号 (可在 Bot 中直接发送手机号添加授权)\n")
			}
			sb.WriteString("\n💡 提示: 在 Bot 中直接发送手机号即可立即授权加入白名单并自动建档。")
			reply = sb.String()
		})

	case "/addpoints", "/subpoints", "/setpoints":
		if arg1 == "" || arg2 == "" {
			cleanT := normalizePhone(arg1)
			if cleanT == "" {
				cleanT = strings.TrimSpace(arg1)
			}
			act := strings.TrimPrefix(cmd, "/")
			if act == "addpoints" && cleanT != "" {
				setPend("addpoints", cleanT)
				reply = fmt.Sprintf("➕ *【为玩家注入筹码】*\n\n👉 *已锁定目标玩家*: `%s`\n\n请直接回复要注入的筹码数值（例如直接发送: `5000` 或 `20000`）：", cleanT)
			} else if act == "addpoints" {
				setPend("addpoints", "")
				reply = "➕ *【为玩家注入筹码】*\n\n👉 *请直接发送：手机号 数量*\n（无需任何斜杠前缀，例如直接发：`13800138000 5000` 或 `13800138000 +5000`）："
			} else if act == "subpoints" {
				setPend("subpoints", "")
				reply = "➖ *【扣除玩家筹码】*\n\n👉 *请直接发送：手机号 数量*\n（无需任何斜杠前缀，例如直接发：`13800138000 2000` 或 `13800138000 -2000`）："
			} else {
				setPend("setpoints", "")
				reply = "⚙️ *【设定玩家筹码】*\n\n👉 *请直接发送：手机号 数量*\n（无需任何斜杠前缀，例如直接发：`13800138000 8888`）："
			}
			break
		}

		parsedAmt, _ := strconv.Atoi(arg2)
		if parsedAmt < 0 {
			parsedAmt = -parsedAmt
		}
		var targetUser *UserProfile
		var oldCoins, netDelta int
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			user := findUserInDB(arg1, db)
			cleanTarget := normalizePhone(arg1)
			if user == nil && len(cleanTarget) >= 5 {
				if !isPhoneInList(cleanTarget, db.AuthorizedPhones) {
					db.AuthorizedPhones = append(db.AuthorizedPhones, cleanTarget)
				}
				kept := db.RevokedPhones[:0]
				for _, rp := range db.RevokedPhones {
					if normalizePhone(rp) != cleanTarget {
						kept = append(kept, rp)
					}
				}
				db.RevokedPhones = kept
				user = h.storage.ensureUserProfile(cleanTarget, db, 0)
			}
			if user == nil {
				return false
			}
			oldCoins = user.Coins
			switch cmd {
			case "/addpoints":
				user.Coins = oldCoins + parsedAmt
			case "/subpoints":
				user.Coins = oldCoins - parsedAmt
				if user.Coins < 0 {
					user.Coins = 0
				}
			case "/setpoints":
				user.Coins = parsedAmt
			}
			user.UpdatedAt = nowMs()
			netDelta = user.Coins - oldCoins
			if netDelta != 0 {
				txType := "admin_grant"
				if netDelta < 0 {
					txType = "admin_deduct"
				}
				recordServerPointsTx(db, user.ID, txType, fmt.Sprintf("Telegram 管理员调整筹码 (%+d)", netDelta), netDelta, user.Coins)
			}
			actionKey := "TG_ADDPOINTS"
			detail := fmt.Sprintf("通过 Telegram 加筹码 +%d", parsedAmt)
			if cmd == "/subpoints" {
				actionKey = "TG_SUBPOINTS"
				detail = fmt.Sprintf("通过 Telegram 扣除筹码 -%d", parsedAmt)
			} else if cmd == "/setpoints" {
				actionKey = "TG_SETPOINTS"
				detail = fmt.Sprintf("通过 Telegram 重新设定筹码为 %d", parsedAmt)
			}
			appendAdminLog(db, actionKey, operator, user.Name, detail, netDelta)
			targetUser = user
			return true
		})

		if targetUser == nil {
			reply = fmt.Sprintf("❌ 未找到玩家: `%s`，请核对手机号、UID或用户名是否正确。", arg1)
			success = false
			break
		}

		tm.triggerDebouncedTxtBackup(h.storage, fmt.Sprintf("Telegram 调整筹码 (%s)", targetUser.Name), chatID)
		h.broadcastTeahousePlayers()
		h.BroadcastJSON(map[string]interface{}{
			"type":      "user_coins_updated",
			"userId":    targetUser.ID,
			"phone":     targetUser.Phone,
			"username":  targetUser.Username,
			"name":      targetUser.Name,
			"coins":     targetUser.Coins,
			"rating":    targetUser.Rating,
			"level":     targetUser.Level,
			"operator":  operator,
			"delta":     netDelta,
			"action":    "tg_" + strings.TrimPrefix(cmd, "/"),
			"timestamp": nowMs(),
		})

		uPhone := normalizePhone(targetUser.Phone)
		if cmd == "/addpoints" {
			reply = fmt.Sprintf("✅ 成功为 *%s* (📱 `%s`) 注入筹码 *+%s* 分！\n当前持有筹码: *%s* 分\n📄 已自动备份全员最新积分 TXT 文档到 Telegram。",
				targetUser.Name, uPhone, formatIntComma(parsedAmt), formatIntComma(targetUser.Coins))
		} else if cmd == "/subpoints" {
			reply = fmt.Sprintf("✅ 已扣减 *%s* (📱 `%s`) 筹码 *-%s* 分！\n当前结余: *%s* 分\n📄 已自动备份全员最新积分 TXT 文档到 Telegram。",
				targetUser.Name, uPhone, formatIntComma(parsedAmt), formatIntComma(targetUser.Coins))
		} else {
			reply = fmt.Sprintf("✅ 已将 *%s* (📱 `%s`) 的筹码直接重设为 *%s* 分！\n📄 已自动备份全员最新积分 TXT 文档到 Telegram。",
				targetUser.Name, uPhone, formatIntComma(targetUser.Coins))
		}

	case "/authphone":
		cleanPhone := normalizePhone(arg1)
		if cleanPhone == "" {
			setPend("authphone", "")
			reply = "📱 *【添加白名单授权】*\n\n👉 *请直接发送手机号*\n（无需任何斜杠或前缀，例如直接输入并发送 `13800138000` 即可立即秒级完成授权）："
			break
		}
		var userProf *UserProfile
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			if !isPhoneInList(cleanPhone, db.AuthorizedPhones) {
				db.AuthorizedPhones = append(db.AuthorizedPhones, cleanPhone)
			}
			kept := db.RevokedPhones[:0]
			for _, rp := range db.RevokedPhones {
				if normalizePhone(rp) != cleanPhone {
					kept = append(kept, rp)
				}
			}
			db.RevokedPhones = kept
			userProf = h.storage.ensureUserProfile(cleanPhone, db, 0)
			if userProf != nil {
				appendAdminLog(db, "TG_AUTHPHONE", operator, cleanPhone,
					fmt.Sprintf("Telegram 添加注册白名单手机号与建档: %s (%s, UID: %s)", cleanPhone, userProf.Name, userProf.ID), 0)
			}
			return true
		})
		tm.triggerDebouncedTxtBackup(h.storage, fmt.Sprintf("管理员授权手机号与建档 (%s)", cleanPhone), chatID)
		h.broadcastTeahousePlayers()
		customInlineMarkup = map[string]interface{}{
			"inline_keyboard": [][]map[string]string{
				{
					{"text": "➕ 注入筹码", "callback_data": "/addpoints " + cleanPhone},
					{"text": "🔍 查看档案", "callback_data": "/findphone " + cleanPhone},
				},
				{
					{"text": "🗑️ 移出白名单", "callback_data": "/delphone " + cleanPhone},
				},
			},
		}
		if userProf != nil {
			reply = fmt.Sprintf(
				"✅ 手机号 `%s` 授权并建档成功！\n\n"+
					"• 📱 *授权手机号*: `%s`\n"+
					"• 🏷️ *玩家名册档案*: *%s* (UID: `%s`)\n"+
					"• 🪙 *预留筹码*: %s 分\n"+
					"• 📋 *状态*: 已开通注册与游戏权限，并自动纳入玩家名册！\n\n"+
					"💡 点击下方【➕ 注入筹码】按钮后直接回复数字即可注入筹码，或直接发送 `%s 5000`！",
				cleanPhone, cleanPhone, userProf.Name, userProf.ID, formatIntComma(userProf.Coins), cleanPhone,
			)
		}

	case "/delphone", "/removephone":
		cleanPhone := normalizePhone(arg1)
		if cleanPhone == "" {
			setPend("delphone", "")
			reply = "🗑️ *【移出授权手机号】*\n\n👉 *请直接发送要移出的手机号*\n（无需任何斜杠或前缀，例如直接发送 `13800138000` 即可立即移出白名单并注销账号）："
			break
		}
		deleted := []string{}
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			authKept := db.AuthorizedPhones[:0]
			for _, ap := range db.AuthorizedPhones {
				if normalizePhone(ap) != cleanPhone {
					authKept = append(authKept, ap)
				}
			}
			db.AuthorizedPhones = authKept
			admKept := db.AdminPhones[:0]
			for _, ap := range db.AdminPhones {
				if normalizePhone(ap) != cleanPhone {
					admKept = append(admKept, ap)
				}
			}
			db.AdminPhones = admKept
			if !isPhoneInList(cleanPhone, db.RevokedPhones) {
				db.RevokedPhones = append(db.RevokedPhones, cleanPhone)
			}
			for pid, prof := range db.Profiles {
				if prof != nil && normalizePhone(prof.Phone) == cleanPhone {
					deleted = append(deleted, fmt.Sprintf("%s (%s)", prof.Name, pid))
					delete(db.Profiles, pid)
				}
			}
			delDesc := "无注册用户"
			if len(deleted) > 0 {
				delDesc = strings.Join(deleted, ", ")
			}
			appendAdminLog(db, "TG_DELPHONE", operator, cleanPhone,
				fmt.Sprintf("Telegram 移出白名单手机号: %s，注销关联账号: %s", cleanPhone, delDesc), 0)
			return true
		})
		tm.triggerDebouncedTxtBackup(h.storage, fmt.Sprintf("移出白名单与注销账号 (%s)", cleanPhone), chatID)
		h.broadcastTeahousePlayers()
		reply = fmt.Sprintf("🗑️ *手机号移出与残留清理成功*！\n\n• 📱 移出手机号: `%s`\n• 📋 白名单状态: 已从授权名单彻底除名并加入注销记录\n", cleanPhone)
		if len(deleted) > 0 {
			reply += fmt.Sprintf("• 👤 已彻底清理注销关联注册账号: *%s*\n• 🔒 客户端登录态与本地缓存已立即失效，用户无法再次登录！", strings.Join(deleted, "、"))
		} else {
			reply += "• 👤 关联账号: 该手机号名下暂无已注册账号残留。"
		}
		reply += "\n📄 已同步备份最新全员积分 TXT 文档。"

	case "/setadmin":
		if arg1 == "" {
			reply = "👑 *【设立特权管理员】*\n\n👉 *请发送：/setadmin 手机号或UID*：\n（例如：`/setadmin 13912345678`）"
			success = false
			break
		}
		query := strings.TrimSpace(arg1)
		cleanPhone := normalizePhone(query)
		var outMsg string
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			u := findUserInDB(query, db)
			if u == nil && len(cleanPhone) >= 5 {
				u = findUserInDB(cleanPhone, db)
			}
			if u != nil {
				u.IsAdmin = true
				u.UpdatedAt = nowMs()
				if up := normalizePhone(u.Phone); up != "" && !isPhoneInList(up, db.AdminPhones) {
					db.AdminPhones = append(db.AdminPhones, up)
				}
				appendAdminLog(db, "TG_SETADMIN", operator, u.Name, "Telegram 设立茶客为管理员: "+u.Name, 0)
				outMsg = fmt.Sprintf("👑 已成功将玩家 *%s* (📱 `%s`) 设立为茶楼特权管理员！", u.Name, u.Phone)
				return true
			}
			if len(cleanPhone) >= 5 {
				if !isPhoneInList(cleanPhone, db.AdminPhones) {
					db.AdminPhones = append(db.AdminPhones, cleanPhone)
				}
				if !isPhoneInList(cleanPhone, db.AuthorizedPhones) {
					db.AuthorizedPhones = append(db.AuthorizedPhones, cleanPhone)
				}
				appendAdminLog(db, "TG_SETADMIN", operator, cleanPhone, "Telegram 设立手机号为特权管理员: "+cleanPhone, 0)
				outMsg = fmt.Sprintf("👑 已成功将手机号 `%s` 设立为特权管理员！", cleanPhone)
				return true
			}
			return false
		})
		if outMsg == "" {
			reply = fmt.Sprintf("❌ 未找到玩家或有效手机号: `%s`", query)
			success = false
		} else {
			reply = outMsg
		}

	case "/deladmin":
		if arg1 == "" {
			reply = "撤销管理员特权，用法: `/deladmin 手机号或UID`"
			success = false
			break
		}
		query := strings.TrimSpace(arg1)
		cleanPhone := normalizePhone(query)
		var outMsg string
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			u := findUserInDB(query, db)
			if u != nil {
				u.IsAdmin = false
				u.UpdatedAt = nowMs()
				if up := normalizePhone(u.Phone); up != "" {
					kept := db.AdminPhones[:0]
					for _, ap := range db.AdminPhones {
						if normalizePhone(ap) != up {
							kept = append(kept, ap)
						}
					}
					db.AdminPhones = kept
				}
				appendAdminLog(db, "TG_DELADMIN", operator, u.Name, "Telegram 撤销管理员权限: "+u.Name, 0)
				outMsg = fmt.Sprintf("✅ 已成功撤销玩家 *%s* 的管理员权限。", u.Name)
				return true
			}
			if len(cleanPhone) >= 5 {
				kept := db.AdminPhones[:0]
				for _, ap := range db.AdminPhones {
					if normalizePhone(ap) != cleanPhone {
						kept = append(kept, ap)
					}
				}
				db.AdminPhones = kept
				appendAdminLog(db, "TG_DELADMIN", operator, cleanPhone, "Telegram 撤销手机号管理员特权: "+cleanPhone, 0)
				outMsg = fmt.Sprintf("✅ 已成功从管理员名册中移出手机号 `%s`。", cleanPhone)
				return true
			}
			return false
		})
		if outMsg == "" {
			reply = fmt.Sprintf("❌ 未找到玩家或有效手机号: `%s`", query)
			success = false
		} else {
			reply = outMsg
		}

	case "/deluser", "/removeuser":
		if arg1 == "" {
			setPend("deluser", "")
			reply = "❌ *【注销删除指定用户】*\n\n👉 *请直接发送要注销的手机号或用户名/UID*\n（无需任何斜杠前缀，例如直接发：`13800138000` 即可立即彻底注销）："
			break
		}
		query := strings.TrimSpace(arg1)
		cleanPhone := normalizePhone(query)
		var outMsg string
		changedAny := false
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			u := findUserInDB(query, db)
			if u == nil && len(cleanPhone) >= 5 {
				u = findUserInDB(cleanPhone, db)
			}
			if u == nil {
				if len(cleanPhone) >= 5 && isPhoneInList(cleanPhone, db.AuthorizedPhones) {
					authKept := db.AuthorizedPhones[:0]
					for _, ap := range db.AuthorizedPhones {
						if normalizePhone(ap) != cleanPhone {
							authKept = append(authKept, ap)
						}
					}
					db.AuthorizedPhones = authKept
					if !isPhoneInList(cleanPhone, db.RevokedPhones) {
						db.RevokedPhones = append(db.RevokedPhones, cleanPhone)
					}
					appendAdminLog(db, "TG_DELUSER", operator, cleanPhone, "删除未注册但已授权手机号: "+cleanPhone, 0)
					outMsg = fmt.Sprintf("🗑️ 未找到关联注册账号，但已将手机号 `%s` 从授权白名单中彻底移出并撤销授权。", cleanPhone)
					changedAny = true
					return true
				}
				return false
			}
			pid := u.ID
			uName := u.Name
			uPhone := normalizePhone(u.Phone)
			uCoins := u.Coins
			delete(db.Profiles, pid)
			if uPhone != "" {
				authKept := db.AuthorizedPhones[:0]
				for _, ap := range db.AuthorizedPhones {
					if normalizePhone(ap) != uPhone {
						authKept = append(authKept, ap)
					}
				}
				db.AuthorizedPhones = authKept
				admKept := db.AdminPhones[:0]
				for _, ap := range db.AdminPhones {
					if normalizePhone(ap) != uPhone {
						admKept = append(admKept, ap)
					}
				}
				db.AdminPhones = admKept
				if !isPhoneInList(uPhone, db.RevokedPhones) {
					db.RevokedPhones = append(db.RevokedPhones, uPhone)
				}
			}
			pShow := uPhone
			if pShow == "" {
				pShow = "未绑定"
			}
			appendAdminLog(db, "TG_DELUSER", operator, uName, fmt.Sprintf("删除用户与注销手机号: %s (%s, 📱 %s)", uName, pid, pShow), 0)
			outMsg = fmt.Sprintf("🗑️ *茶客账号注销成功*！\n\n• 👤 注销账号: *%s* (UID: `%s`)\n• 📱 手机号: `%s` (已同步从白名单中移出并撤销授权)\n• 🪙 结算前筹码: %s 分\n• 🚫 状态: 该账号已彻底注销，用户会话立即失效！\n📄 已同步备份最新全员积分 TXT 文档。",
				uName, pid, pShow, formatIntComma(uCoins))
			changedAny = true
			return true
		})
		if !changedAny {
			reply = fmt.Sprintf("❌ 未找到玩家或手机号: `%s`，请核对手机号、用户名或 UID 是否正确。", query)
			success = false
		} else {
			tm.triggerDebouncedTxtBackup(h.storage, "注销删除玩家账号 ("+query+")", chatID)
			h.broadcastTeahousePlayers()
			reply = outMsg
		}

	case "/clearusers", "/cleanusers":
		cnt := 0
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			cnt = len(db.Profiles)
			db.Profiles = map[string]*UserProfile{}
			appendAdminLog(db, "TG_CLEARUSERS", operator, "全服", fmt.Sprintf("清空所有注册茶客账号，共清理 %d 个账号", cnt), 0)
			return true
		})
		tm.triggerDebouncedTxtBackup(h.storage, "清空全服注册茶客账号", chatID)
		h.broadcastTeahousePlayers()
		reply = fmt.Sprintf("🧹 *全服注册茶客已全部清空*！\n\n共彻底清理注销了 *%d* 位茶客的注册资料与会话数据。所有客户端将在心跳或刷新时自动重置为未登录状态。\n📄 已同步生成全空重置 TXT 灾备文档。", cnt)

	case "/broadcast":
		msgText := strings.TrimSpace(strings.Join(parts[1:], " "))
		if msgText == "" {
			setPend("broadcast", "")
			reply = "📢 *【发布全服通告】*\n\n👉 *请直接发送要广播的消息文字*\n（无需任何斜杠前缀，即发即推）："
			break
		}
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			appendAdminLog(db, "TG_BROADCAST", operator, "全服", msgText, 0)
			return true
		})
		h.BroadcastJSON(map[string]interface{}{
			"type":   "chat_broadcast",
			"roomId": "ALL",
			"message": map[string]interface{}{
				"id":        fmt.Sprintf("tg-%d", nowMs()),
				"sender":    "📢 Telegram 管理员广播",
				"avatar":    "🤖",
				"text":      msgText,
				"timestamp": time.Now().In(shanghaiZone).Format("15:04"),
			},
		})
		reply = fmt.Sprintf("📢 *全服广播推送成功*！\n已即时推送到全服所有对战房间舞台顶部弹幕与茶室聊天：\n\"%s\"", msgText)

	case "/logs":
		h.storage.WithDBRead(func(db *DatabaseSchema) {
			lim := len(db.AdminLogs)
			if lim == 0 {
				reply = "📜 暂无最近的管理员审计操作记录。"
				return
			}
			if lim > 10 {
				lim = 10
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("📜 *最新管理员操作审计日志 (最近 %d 条)*:\n\n", lim))
			for _, l := range db.AdminLogs[:lim] {
				sb.WriteString(fmt.Sprintf("• [%s] *%s* | 执行人: %s -> 目标: %s\n  └ %s\n", l.Timestamp, l.Action, l.Operator, l.Target, l.Details))
			}
			reply = sb.String()
		})

	case "/rooms":
		h.roomMu.RLock()
		activeCnt := len(h.rooms)
		h.roomMu.RUnlock()
		reply = fmt.Sprintf("🀄 *茶楼在线牌桌巡视中枢* (当前活跃房间 %d 桌):\n\n"+
			"• 牌桌 RM-202: 经典烟三四人赛 (4人桌 · 运行中 · 底分10分)\n"+
			"• 牌桌 RM-204: 水镜十三水 (4人桌 · 智能理牌 · 底分10分)\n"+
			"• 牌桌 RM-205: 经典国标麻将 (4人桌 · 东风圈 · 底分10分)\n"+
			"• 自动匹配池: 活跃运作中，Go 权威状态机实时同步\n\n"+
			"💡 提示: 发送 `/matchpool` 可查看实时在线人数与风控状态。", activeCnt)

	case "/matchpool", "/match":
		counts := h.getRealGamePresence()
		userCount := 0
		h.storage.WithDBRead(func(db *DatabaseSchema) { userCount = len(db.Profiles) })
		reply = fmt.Sprintf("⚡ *茶楼极速自动匹配池监控中枢*:\n\n"+
			"• 🎴 *经典烟三竞技场*: 实时在线 %d 人\n"+
			"• 🃏 *水镜十三水*: 实时在线 %d 人 (含预约专列)\n"+
			"• 🀄 *经典国标麻将*: 实时在线 %d 人\n\n"+
			"🛡️ *安全与撮合策略*:\n"+
			"• 权威发牌与状态机: 🟢 Go Authoritative 内存隔离\n"+
			"• 实时在线连接数: *%d* 人 | 注册用户: *%d* 人",
			counts.Yansan, counts.Shisanshui, counts.Mahjong, counts.TotalOnline, userCount)

	case "/stats":
		userCount := 0
		totalCoins := 0
		authCount := 0
		h.storage.WithDBRead(func(db *DatabaseSchema) {
			userCount = len(db.Profiles)
			for _, u := range db.Profiles {
				if u != nil {
					totalCoins += u.Coins
				}
			}
			authCount = len(db.AuthorizedPhones)
		})
		reply = fmt.Sprintf("📊 *茶楼运营核心统计数据*:\n\n"+
			"• 注册总玩家: *%d* 人\n"+
			"• 全服筹码总流动量: *%s* 分\n"+
			"• 授权白名单手机号: *%d* 个\n"+
			"• 引擎模式: 🟢 Go 单体全栈权威服务器\n"+
			"• Telegram 管理员接入状态: 🟢 活跃在线",
			userCount, formatIntComma(totalCoins), authCount)

	default:
		reply = fmt.Sprintf("❓ 未知指令: `%s`。点击下方快捷键盘或发送 `/help` 获取可用管理员指令清单。", cmd)
		success = false
	}

	markup := buildTelegramAdminKeyboard()
	effectiveMarkup := interface{}(markup)
	if customInlineMarkup != nil {
		effectiveMarkup = customInlineMarkup
	}

	botToken := getTelegramBotToken(h.storage)
	if cbID := extractString(cbObj, "id"); cbID != "" && botToken != "" {
		go func() {
			cbText := "✅ 指令执行成功"
			if !success {
				cbText = "⚠️ 操作提示"
			}
			payload, _ := json.Marshal(map[string]interface{}{
				"callback_query_id": cbID,
				"text":              cbText,
			})
			req, _ := http.NewRequest(http.MethodPost, getTelegramAPIBase(h.storage)+"/bot"+botToken+"/answerCallbackQuery", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			_, _ = (&http.Client{Timeout: 4 * time.Second}).Do(req)
		}()
	}

	if chatID != "" && botToken != "" && !isWebhook {
		go tm.sendTelegramReply(h.storage, chatID, reply, effectiveMarkup)
	}

	return telegramExecResult{
		OK:          true,
		Success:     success,
		Reply:       reply,
		Command:     rawText,
		ReplyMarkup: effectiveMarkup,
		ChatID:      chatID,
		Timestamp:   time.Now().In(shanghaiZone).Format("15:04"),
	}
}

func (tm *TelegramManager) sendTelegramReply(sm *StorageManager, chatID, reply string, replyMarkup interface{}) {
	botToken := getTelegramBotToken(sm)
	if botToken == "" || chatID == "" {
		return
	}
	apiBase := getTelegramAPIBase(sm)
	safeText := sanitizeTelegramMarkdown(reply)
	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"chat_id":      chatID,
		"text":         safeText,
		"parse_mode":   "Markdown",
		"reply_markup": replyMarkup,
	})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(apiBase+"/bot"+botToken+"/sendMessage", "application/json", bytes.NewReader(bodyBytes))
	if err == nil {
		var out map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		_ = resp.Body.Close()
		if ok, _ := out["ok"].(bool); ok {
			return
		}
		desc := fmt.Sprintf("%v", out["description"])
		if strings.Contains(desc, "entity") || strings.Contains(desc, "parse") {
			plainBytes, _ := json.Marshal(map[string]interface{}{
				"chat_id":      chatID,
				"text":         stripMarkdownSymbols(reply),
				"reply_markup": replyMarkup,
			})
			if r2, e2 := client.Post(apiBase+"/bot"+botToken+"/sendMessage", "application/json", bytes.NewReader(plainBytes)); e2 == nil {
				_ = r2.Body.Close()
			}
		}
	}
}

func (tm *TelegramManager) syncTelegramBotCommands(sm *StorageManager, customToken, customAPIBase string) (bool, string) {
	token := strings.TrimSpace(customToken)
	if token == "" {
		token = getTelegramBotToken(sm)
	}
	if token == "" {
		return false, "TELEGRAM_BOT_TOKEN 未在 .env 中设置"
	}
	apiBase := strings.TrimSpace(customAPIBase)
	if apiBase == "" {
		apiBase = getTelegramAPIBase(sm)
	}
	commands := []map[string]string{
		{"command": "start", "description": "🚀 启动控制台 & 显示管理员快捷键盘"},
		{"command": "users", "description": "👥 查看茶楼全员玩家名册与筹码余额"},
		{"command": "whitelist", "description": "📋 查看已授权手机号白名单列表"},
		{"command": "findphone", "description": "🔍 输入手机号检索玩家完整档案"},
		{"command": "authphone", "description": "📱 添加手机号授权到注册白名单"},
		{"command": "addpoints", "description": "➕ 为指定玩家注入充值筹码"},
		{"command": "subpoints", "description": "➖ 扣除指定玩家筹码"},
		{"command": "exporttxt", "description": "📄 导出全服玩家积分 TXT 榜单"},
		{"command": "broadcast", "description": "📢 发布全服牌桌通告"},
	}

	client := &http.Client{Timeout: 8 * time.Second}
	cmdPayload, _ := json.Marshal(map[string]interface{}{"commands": commands})
	resp, err := client.Post(apiBase+"/bot"+token+"/setMyCommands", "application/json", bytes.NewReader(cmdPayload))
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&out)

	menuPayload, _ := json.Marshal(map[string]interface{}{"menu_button": map[string]string{"type": "commands"}})
	if mr, mErr := client.Post(apiBase+"/bot"+token+"/setChatMenuButton", "application/json", bytes.NewReader(menuPayload)); mErr == nil {
		_ = mr.Body.Close()
	}

	if ok, _ := out["ok"].(bool); ok {
		return true, ""
	}
	return false, fmt.Sprintf("%v", out["description"])
}

func (tm *TelegramManager) stopPolling() {
	tm.mu.Lock()
	if tm.pollingCancel != nil {
		close(tm.pollingCancel)
		tm.pollingCancel = nil
	}
	tm.status.PollingActive = false
	tm.mu.Unlock()
}

func (tm *TelegramManager) startPolling(h *Hub, token string) {
	tm.stopPolling()
	stopCh := make(chan struct{})
	tm.mu.Lock()
	tm.pollingCancel = stopCh
	tm.status.Mode = "polling"
	tm.status.PollingActive = true
	tm.status.LastError = nil
	tm.mu.Unlock()

	apiBase := getTelegramAPIBase(h.storage)
	_, _ = (&http.Client{Timeout: 8 * time.Second}).Get(apiBase + "/bot" + token + "/deleteWebhook?drop_pending_updates=false")

	go func() {
		var offset int64
		consecutiveErrors := 0
		allowedUpdates := url.QueryEscape(`["message","edited_message","channel_post","callback_query"]`)
		for {
			select {
			case <-stopCh:
				return
			default:
			}
			now := nowMs()
			tm.mu.Lock()
			tm.status.LastPollTimestamp = &now
			tm.mu.Unlock()

			curBase := getTelegramAPIBase(h.storage)
			fetchURL := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=25&allowed_updates=%s", curBase, token, offset, allowedUpdates)
			resp, err := (&http.Client{Timeout: 35 * time.Second}).Get(fetchURL)
			if err != nil {
				consecutiveErrors++
				errStr := err.Error()
				tm.mu.Lock()
				tm.status.LastError = &errStr
				tm.mu.Unlock()
				backoff := time.Duration(consecutiveErrors*2) * time.Second
				if backoff > 15*time.Second {
					backoff = 15 * time.Second
				}
				select {
				case <-stopCh:
					return
				case <-time.After(backoff):
					continue
				}
			}
			var data struct {
				OK     bool                     `json:"ok"`
				Result []map[string]interface{} `json:"result"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&data)
			statusCode := resp.StatusCode
			_ = resp.Body.Close()

			if statusCode == 409 {
				_, _ = (&http.Client{Timeout: 8 * time.Second}).Get(curBase + "/bot" + token + "/deleteWebhook?drop_pending_updates=false")
				select {
				case <-stopCh:
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}

			if statusCode < 200 || statusCode >= 300 || !data.OK {
				consecutiveErrors++
				select {
				case <-stopCh:
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			consecutiveErrors = 0
			for _, upd := range data.Result {
				select {
				case <-stopCh:
					return
				default:
				}
				if uidF, ok := upd["update_id"].(float64); ok {
					uid := int64(uidF)
					if uid+1 > offset {
						offset = uid + 1
					}
				}
				ts := nowMs()
				tm.mu.Lock()
				tm.status.LastMessageTimestamp = &ts
				tm.status.ProcessedCount++
				tm.mu.Unlock()
				tm.executeTelegramUpdate(h, upd, false)
			}
		}
	}()
}

func (tm *TelegramManager) initBotService(h *Hub) {
	token := getTelegramBotToken(h.storage)
	if token == "" {
		errMsg := "TELEGRAM_BOT_TOKEN 未在 .env 中设置"
		tm.mu.Lock()
		tm.status = telegramBotStatus{
			Enabled:   false,
			Mode:      "disabled",
			LastError: &errMsg,
		}
		tm.mu.Unlock()
		return
	}

	apiBase := getTelegramAPIBase(h.storage)
	publicURL := getPublicWebhookURL(h.storage)

	tm.mu.Lock()
	tm.status.Enabled = true
	tm.status.TokenConfigured = true
	tm.status.TokenMasked = maskBotToken(token)
	tm.status.APIBase = apiBase
	tm.status.URLConfigured = publicURL
	tm.mu.Unlock()

	go func() {
		client := &http.Client{Timeout: 8 * time.Second}
		if resp, err := client.Get(apiBase + "/bot" + token + "/getMe"); err == nil {
			var me struct {
				OK     bool `json:"ok"`
				Result struct {
					Username  string `json:"username"`
					FirstName string `json:"first_name"`
				} `json:"result"`
			}
			if json.NewDecoder(resp.Body).Decode(&me) == nil && me.OK {
				tm.mu.Lock()
				tm.status.BotUsername = me.Result.Username
				tm.status.BotName = me.Result.FirstName
				tm.status.LastError = nil
				tm.mu.Unlock()
				go tm.syncTelegramBotCommands(h.storage, token, apiBase)
			}
			_ = resp.Body.Close()
		}

		modeEnv := strings.ToLower(strings.TrimSpace(h.storage.getEnv("BOT_MODE")))
		if modeEnv == "polling" {
			tm.startPolling(h, token)
			return
		}

		tm.stopPolling()
		tm.mu.Lock()
		tm.status.Mode = "webhook"
		tm.mu.Unlock()

		if publicURL == "" {
			msg := "未在 .env 中配置 URL 公网地址，Webhook 回调未登记。"
			tm.mu.Lock()
			tm.status.LastError = &msg
			tm.mu.Unlock()
			return
		}

		webhookURL := publicURL + "/api/telegram/webhook"
		tm.mu.Lock()
		tm.status.WebhookURL = webhookURL
		tm.mu.Unlock()

		secParam := ""
		rawSec := strings.Trim(strings.TrimSpace(h.storage.getEnv("TELEGRAM_WEBHOOK_SECRET")), `"'`)
		if tgWebhookSecRe.MatchString(rawSec) {
			secParam = "&secret_token=" + url.QueryEscape(rawSec)
		}
		setURL := fmt.Sprintf("%s/bot%s/setWebhook?url=%s%s&drop_pending_updates=true&allowed_updates=%s",
			apiBase, token, url.QueryEscape(webhookURL), secParam, url.QueryEscape(`["message","edited_message","channel_post","callback_query"]`))
		if r2, e2 := client.Get(setURL); e2 == nil {
			_ = r2.Body.Close()
		}
	}()
}

// ==============================================================================
// HTTP Handlers for /api/telegram/*
// ==============================================================================

func registerTelegramRoutes(mux *http.ServeMux, h *Hub, tm *TelegramManager) {
	handleWebhookOrCmd := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		isWebhook := r.URL.Path == "/api/telegram/webhook" || r.URL.Path == "/api/telegram/webhook/"
		if !isWebhook {
			allowed := false
			h.storage.WithDBRead(func(db *DatabaseSchema) {
				allowed = h.storage.authenticateAdmin(r, db)
			})
			if !allowed {
				sendJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "需要管理员权限方可执行机器人指令"})
				return
			}
		}

		rawSec := strings.Trim(strings.TrimSpace(h.storage.getEnv("TELEGRAM_WEBHOOK_SECRET")), `"'`)
		if isWebhook && rawSec != "" {
			hdr := strings.TrimSpace(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"))
			if !safeTokenEqual(hdr, rawSec) {
				sendJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "非法请求：Telegram Webhook 密钥校验失败"})
				return
			}
		}

		body, err := parseRequestJSON(r)
		if err != nil {
			body = map[string]interface{}{}
		}
		res := tm.executeTelegramUpdate(h, body, isWebhook)

		if isWebhook {
			msgObj := extractMap(body, "message")
			if msgObj == nil {
				msgObj = extractMap(body, "edited_message")
			}
			if msgObj == nil {
				msgObj = extractMap(body, "channel_post")
			}
			cbObj := extractMap(body, "callback_query")
			if msgObj == nil && cbObj != nil {
				msgObj = extractMap(cbObj, "message")
			}
			targetChatID := extractString(extractMap(msgObj, "chat"), "id")
			if targetChatID == "" {
				targetChatID = extractString(extractMap(cbObj, "from"), "id")
			}
			if targetChatID == "" {
				targetChatID = res.ChatID
			}
			if targetChatID != "" && res.Reply != "" {
				if strings.HasPrefix(res.Command, "/start") || strings.HasPrefix(res.Command, "/help") {
					go tm.syncTelegramBotCommands(h.storage, "", "")
				}
				sendJSON(w, http.StatusOK, map[string]interface{}{
					"method":       "sendMessage",
					"chat_id":      targetChatID,
					"text":         stripMarkdownSymbols(res.Reply),
					"reply_markup": res.ReplyMarkup,
				})
				return
			}
			sendJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "processed": true})
			return
		}
		sendJSON(w, http.StatusOK, res)
	}

	mux.HandleFunc("/api/telegram/command", handleWebhookOrCmd)
	mux.HandleFunc("/api/telegram/webhook", handleWebhookOrCmd)
	mux.HandleFunc("/api/telegram/webhook/", handleWebhookOrCmd)

	handleSync := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		allowed := false
		h.storage.WithDBRead(func(db *DatabaseSchema) { allowed = h.storage.authenticateAdmin(r, db) })
		if !allowed {
			sendJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "需要管理员权限方可同步机器人菜单"})
			return
		}
		ok, errStr := tm.syncTelegramBotCommands(h.storage, "", "")
		msg := "🟢 成功向 Telegram 官方注册下拉菜单指令与快捷键盘菜单！"
		if !ok {
			msg = "⚠️ 同步 Telegram 键盘菜单提示: " + errStr
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"success": ok, "message": msg, "error": errStr})
	}
	mux.HandleFunc("/api/telegram/sync-commands", handleSync)
	mux.HandleFunc("/api/telegram/sync", handleSync)

	handleSetWebhook := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		allowed := false
		h.storage.WithDBRead(func(db *DatabaseSchema) { allowed = h.storage.authenticateAdmin(r, db) })
		if !allowed {
			sendJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "需要管理员权限方可配置或激活 Telegram Webhook"})
			return
		}
		body := map[string]interface{}{}
		if r.Method == http.MethodPost {
			body, _ = parseRequestJSON(r)
		}
		providedToken := strings.Trim(strings.TrimSpace(r.URL.Query().Get("token")), `"'`)
		if providedToken == "" {
			providedToken = strings.Trim(strings.TrimSpace(extractString(body, "token")), `"'`)
		}
		if providedToken != "" {
			h.storage.saveEnvVariable("TELEGRAM_BOT_TOKEN", providedToken)
		}
		token := getTelegramBotToken(h.storage)
		rawAppURL := strings.TrimSpace(r.URL.Query().Get("url"))
		if rawAppURL == "" {
			rawAppURL = strings.TrimSpace(extractString(body, "url"))
		}
		targetAppURL := normalizePublicURL(rawAppURL)
		if targetAppURL == "" {
			targetAppURL = getPublicWebhookURL(h.storage)
		}
		webhookURL := ""
		if targetAppURL != "" {
			webhookURL = targetAppURL + "/api/telegram/webhook"
		}
		dropPending := r.URL.Query().Get("drop_pending") != "false"

		rawSec := strings.Trim(strings.TrimSpace(h.storage.getEnv("TELEGRAM_WEBHOOK_SECRET")), `"'`)
		secParam := ""
		if tgWebhookSecRe.MatchString(rawSec) {
			secParam = "&secret_token=" + url.QueryEscape(rawSec)
		}

		serverOK := false
		var serverErrMsg interface{}
		if token != "" && targetAppURL != "" {
			apiBase := getTelegramAPIBase(h.storage)
			fetchURL := fmt.Sprintf("%s/bot%s/setWebhook?url=%s%s&drop_pending_updates=%t&allowed_updates=%s",
				apiBase, token, url.QueryEscape(webhookURL), secParam, dropPending, url.QueryEscape(`["message","edited_message","channel_post","callback_query"]`))
			resp, err := (&http.Client{Timeout: 6 * time.Second}).Get(fetchURL)
			if err != nil {
				serverErrMsg = err.Error()
			} else {
				var out map[string]interface{}
				_ = json.NewDecoder(resp.Body).Decode(&out)
				_ = resp.Body.Close()
				if ok, _ := out["ok"].(bool); ok {
					serverOK = true
					tm.stopPolling()
					tm.initBotService(h)
				} else {
					serverErrMsg = out["description"]
				}
			}
		}

		mode := "pending_server_registration"
		msg := "服务端直连注册未完成，请确认服务器公网 URL 与网络/反代可达性。"
		if serverOK {
			mode = "server_registered"
			msg = "Webhook 已成功在 Telegram 官方服务器注册并生效！"
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":              serverOK,
			"mode":                 mode,
			"tokenConfigured":      token != "",
			"message":              msg,
			"configuredWebhookUrl": webhookURL,
			"serverFetchError":     serverErrMsg,
		})
	}
	mux.HandleFunc("/api/telegram/set-webhook", handleSetWebhook)
	mux.HandleFunc("/api/telegram/register", handleSetWebhook)

	mux.HandleFunc("/api/telegram/webhook-info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		allowed := false
		h.storage.WithDBRead(func(db *DatabaseSchema) { allowed = h.storage.authenticateAdmin(r, db) })
		if !allowed {
			sendJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "需要管理员权限方可查看 Webhook 诊断信息"})
			return
		}
		token := getTelegramBotToken(h.storage)
		if token == "" {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "环境变量未配置 TELEGRAM_BOT_TOKEN"})
			return
		}
		normURL := getPublicWebhookURL(h.storage)
		expectedWebhook := ""
		if normURL != "" {
			expectedWebhook = normURL + "/api/telegram/webhook"
		}
		apiBase := getTelegramAPIBase(h.storage)
		resp, err := (&http.Client{Timeout: 4 * time.Second}).Get(apiBase + "/bot" + token + "/getWebhookInfo")
		if err != nil {
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"success":            false,
				"networkNotice":      "本地服务器直连 Telegram 官方受限，请检查公网反代或网络连通性。",
				"expectedWebhookUrl": expectedWebhook,
				"envUrl":             normURL,
			})
			return
		}
		defer resp.Body.Close()
		var data struct {
			OK     bool                   `json:"ok"`
			Result map[string]interface{} `json:"result"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&data)
		if !data.OK {
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"success":            false,
				"expectedWebhookUrl": expectedWebhook,
				"envUrl":             normURL,
			})
			return
		}
		regURL := extractString(data.Result, "url")
		isMatched := expectedWebhook != "" && regURL == expectedWebhook
		diag := "🟢 Webhook 挂载状态完全正常，Telegram 官方可毫秒级直推消息！"
		if regURL == "" {
			diag = "Telegram 官方未注册任何 Webhook 回调地址。请访问 /api/telegram/set-webhook 激活！"
		} else if !isMatched {
			diag = fmt.Sprintf("URL 不匹配：Telegram 登记的地址为 [%s]，而当前配置的是 [%s]。", regURL, expectedWebhook)
		} else if le := extractString(data.Result, "last_error_message"); le != "" {
			diag = fmt.Sprintf(`Telegram 官方上报错误: "%s"`, le)
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":              true,
			"normalizedEnvUrl":     normURL,
			"expectedWebhookUrl":   expectedWebhook,
			"registeredWebhookUrl": regURL,
			"isUrlMatched":         isMatched,
			"hasPendingUpdates":    data.Result["pending_update_count"],
			"lastErrorMessage":     data.Result["last_error_message"],
			"diagnosis":            diag,
			"webhookInfo":          data.Result,
		})
	})

	mux.HandleFunc("/api/telegram/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tm.mu.Lock()
		statusCopy := tm.status
		tm.mu.Unlock()
		var connTest interface{}
		if r.URL.Query().Get("test") == "1" {
			allowed := false
			h.storage.WithDBRead(func(db *DatabaseSchema) { allowed = h.storage.authenticateAdmin(r, db) })
			if !allowed {
				sendJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "需要管理员权限方可执行机器人连通性测试"})
				return
			}
			token := getTelegramBotToken(h.storage)
			if token != "" {
				apiBase := getTelegramAPIBase(h.storage)
				t0 := time.Now()
				resp, err := (&http.Client{Timeout: 8 * time.Second}).Get(apiBase + "/bot" + token + "/getMe")
				if err != nil {
					connTest = map[string]interface{}{"ok": false, "error": err.Error()}
				} else {
					var me struct {
						OK     bool `json:"ok"`
						Result struct {
							Username string `json:"username"`
						} `json:"result"`
					}
					_ = json.NewDecoder(resp.Body).Decode(&me)
					_ = resp.Body.Close()
					connTest = map[string]interface{}{
						"ok":        me.OK,
						"latencyMs": time.Since(t0).Milliseconds(),
						"username":  me.Result.Username,
					}
				}
			}
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"bot":            statusCopy,
			"connectionTest": connTest,
		})
	})

	handleStartPolling := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		allowed := false
		h.storage.WithDBRead(func(db *DatabaseSchema) { allowed = h.storage.authenticateAdmin(r, db) })
		if !allowed {
			sendJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "需要管理员权限方可切换长轮询模式"})
			return
		}
		token := getTelegramBotToken(h.storage)
		if token == "" {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "未配置 TELEGRAM_BOT_TOKEN"})
			return
		}
		tm.startPolling(h, token)
		sendJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "已成功启动长轮询模式 (Long Polling)"})
	}
	mux.HandleFunc("/api/telegram/start-polling", handleStartPolling)
	mux.HandleFunc("/api/telegram/polling", handleStartPolling)
}
