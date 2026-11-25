package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxRequestBodyBytes = 2 << 20

var (
	privateNet192Re = regexp.MustCompile(`^192\.168\.\d+\.\d+$`)
	privateNet10Re  = regexp.MustCompile(`^10\.\d+\.\d+\.\d+$`)
	privateNet172Re = regexp.MustCompile(`^172\.(1[6-9]|2\d|3[01])\.\d+\.\d+$`)
	ttsStripRe      = regexp.MustCompile(`[^\p{Han}a-zA-Z0-9，。！、？]`)
	ttsDanLeadRe    = regexp.MustCompile(`^单([0-9一二三四五六七八九十勾圈老K尖A-Z])`)
	gitRepoRe       = regexp.MustCompile(`github\.com[:/]([^\s/]+/[^\s/.]+)`)
	voiceDataURLRe  = regexp.MustCompile(`^data:(audio/[a-zA-Z0-9.+-]+)(?:;[a-zA-Z0-9.=_-]+)*;base64,([A-Za-z0-9+/=]+)$`)
)

const (
	voiceClipTTL      = 15 * time.Minute
	voiceClipMaxItems = 120
	voiceClipMaxBytes = 650000
)

type voiceClipEntry struct {
	mime      string
	data      []byte
	createdAt time.Time
}

type voiceClipStore struct {
	mu    sync.RWMutex
	order []string
	items map[string]voiceClipEntry
}

func newVoiceClipStore() *voiceClipStore {
	return &voiceClipStore{
		items: make(map[string]voiceClipEntry),
	}
}

func (vc *voiceClipStore) set(id, mime string, data []byte) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	now := time.Now()
	kept := vc.order[:0]
	for _, k := range vc.order {
		if entry, ok := vc.items[k]; ok && now.Sub(entry.createdAt) <= voiceClipTTL {
			kept = append(kept, k)
		} else {
			delete(vc.items, k)
		}
	}
	vc.order = kept
	for len(vc.order) >= voiceClipMaxItems {
		oldest := vc.order[0]
		vc.order = vc.order[1:]
		delete(vc.items, oldest)
	}
	vc.order = append(vc.order, id)
	vc.items[id] = voiceClipEntry{mime: mime, data: data, createdAt: now}
}

func (vc *voiceClipStore) get(id string) (voiceClipEntry, bool) {
	vc.mu.RLock()
	entry, ok := vc.items[id]
	vc.mu.RUnlock()
	if !ok {
		return voiceClipEntry{}, false
	}
	if time.Since(entry.createdAt) > voiceClipTTL {
		vc.mu.Lock()
		delete(vc.items, id)
		vc.mu.Unlock()
		return voiceClipEntry{}, false
	}
	return entry, true
}

type ttsCacheStore struct {
	mu    sync.Mutex
	order []string
	data  map[string][]byte
}

func newTTSCacheStore() *ttsCacheStore {
	return &ttsCacheStore{
		data: make(map[string][]byte),
	}
}

func (tc *ttsCacheStore) get(key string) ([]byte, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	b, ok := tc.data[key]
	return b, ok
}

func (tc *ttsCacheStore) set(key string, val []byte) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if _, exists := tc.data[key]; !exists {
		if len(tc.order) >= 500 {
			oldest := tc.order[0]
			tc.order = tc.order[1:]
			delete(tc.data, oldest)
		}
		tc.order = append(tc.order, key)
	}
	tc.data[key] = val
}

func sendJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func parseRequestJSON(r *http.Request) (map[string]interface{}, error) {
	if r.Body == nil {
		return map[string]interface{}{}, nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return map[string]interface{}{}, nil
	}
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		vals, err := url.ParseQuery(trimmed)
		if err == nil {
			out := map[string]interface{}{}
			for k, v := range vals {
				if len(v) > 0 {
					out[k] = v[0]
				}
			}
			return out, nil
		}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		if strings.Contains(trimmed, "=") {
			if vals, qErr := url.ParseQuery(trimmed); qErr == nil {
				out = map[string]interface{}{}
				for k, v := range vals {
					if len(v) > 0 {
						out[k] = v[0]
					}
				}
				return out, nil
			}
		}
		return nil, err
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}

func getGitHubRepoName() string {
	for _, p := range []string{".git/config", "../.git/config"} {
		if raw, err := os.ReadFile(p); err == nil {
			if m := gitRepoRe.FindStringSubmatch(string(raw)); len(m) > 1 {
				return strings.TrimSuffix(m[1], ".git")
			}
		}
	}
	return "wen775866/yy"
}

func isTrustedOrigin(origin, reqHost string, sm *StorageManager) bool {
	trimmed := strings.TrimSpace(origin)
	if trimmed == "" {
		return false
	}
	low := strings.ToLower(trimmed)
	if strings.HasPrefix(low, "capacitor://") ||
		strings.HasPrefix(low, "ionic://") ||
		low == "file://" ||
		low == "null" {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	if reqHost != "" {
		bareReqHost := strings.ToLower(strings.Split(reqHost, ":")[0])
		if bareReqHost != "" && host == bareReqHost {
			return true
		}
	}
	if host == "localhost" ||
		host == "127.0.0.1" ||
		host == "::1" ||
		host == "m.my66.cc.cd" ||
		strings.HasSuffix(host, ".trycloudflare.com") ||
		strings.HasSuffix(host, ".ngrok-free.app") ||
		strings.HasSuffix(host, ".ngrok.io") ||
		strings.HasSuffix(host, ".loca.lt") ||
		privateNet192Re.MatchString(host) ||
		privateNet10Re.MatchString(host) ||
		privateNet172Re.MatchString(host) {
		return true
	}
	for _, k := range []string{"URL", "APP_URL", "WEBHOOK_URL", "SERVER_URL", "ALLOWED_ORIGIN"} {
		if eu := sm.getEnv(k); eu != "" {
			if u, uErr := url.Parse(normalizePublicURL(eu)); uErr == nil && strings.ToLower(u.Hostname()) == host {
				return true
			}
		}
	}
	return false
}

func applyCORSHeaders(w http.ResponseWriter, r *http.Request, sm *StorageManager) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" && isTrustedOrigin(origin, r.Host, sm) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
	} else if origin == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Session-Token, X-Admin-Secret, X-Requested-With, Range, Accept, Origin, x-telegram-bot-api-secret-token")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

func isUserAuthorizedAndNotRevoked(h *Hub, db *DatabaseSchema, phone string) (allowed bool, revoked bool) {
	pPhone := normalizePhone(phone)
	if pPhone == "" {
		return true, false
	}
	for _, rp := range db.RevokedPhones {
		if phonesMatch(rp, pPhone) {
			return false, true
		}
	}
	allowedPhones, adminPhones := h.storage.getAllowedAndAdminPhones(db)
	return isPhoneInList(pPhone, allowedPhones) || isPhoneInList(pPhone, adminPhones), false
}

func bookingUserFromRequest(r *http.Request, h *Hub, fallbackID string) (userID, playerName, avatar string, errStatus int, errMsg string) {
	var authUser *UserProfile
	var hasPwd bool
	var resolvedName, resolvedAvatar string
	h.storage.WithDBRead(func(db *DatabaseSchema) {
		authUser = h.storage.authenticateRequest(r, db)
		if authUser != nil {
			resolvedName = authUser.Name
			resolvedAvatar = authUser.Avatar
		} else if fallbackID != "" {
			if prof := db.Profiles[fallbackID]; prof != nil && prof.Password != "" {
				hasPwd = true
			}
		}
	})
	if authUser != nil {
		return authUser.ID, resolvedName, resolvedAvatar, 0, ""
	}
	if hasPwd {
		return "", "", "", http.StatusUnauthorized, "会话失效，请重新登录"
	}
	uid := strings.TrimSpace(fallbackID)
	if uid == "" {
		uid = "guest"
	}
	return uid, "", "", 0, ""
}

func serveAPKFileOrRedirect(w http.ResponseWriter, r *http.Request, fileName string) {
	candidates := []string{
		filepath.Join("dist", fileName),
		filepath.Join("dist", "app-latest.apk"),
		filepath.Join("apk", fileName),
		filepath.Join("apk", "app-latest.apk"),
		filepath.Join("..", "apk", fileName),
		filepath.Join("..", "apk", "app-latest.apk"),
		filepath.Join("public", fileName),
		filepath.Join("public", "apk", fileName),
	}
	for _, lc := range candidates {
		st, err := os.Stat(lc)
		if err == nil && !st.IsDir() && st.Size() >= 4200000 {
			w.Header().Set("Content-Type", "application/vnd.android.package-archive")
			w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Disposition")
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			f, err := os.Open(lc)
			if err == nil {
				defer f.Close()
				w.WriteHeader(http.StatusOK)
				_, _ = io.Copy(w, f)
				return
			}
		}
	}
	repo := getGitHubRepoName()
	mirror := fmt.Sprintf("https://ghproxy.net/https://github.com/%s/releases/download/latest-apk/%s", repo, fileName)
	w.Header().Set("Location", mirror)
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusFound)
}

func setStaticContentTypeAndCache(w http.ResponseWriter, cleanPath string) {
	ext := strings.ToLower(filepath.Ext(cleanPath))
	switch ext {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".js", ".mjs":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case ".mp3":
		w.Header().Set("Content-Type", "audio/mpeg")
	case ".wav":
		w.Header().Set("Content-Type", "audio/wav")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".woff2":
		w.Header().Set("Content-Type", "font/woff2")
	case ".apk":
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	}

	if strings.HasPrefix(cleanPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else if cleanPath == "index.html" || cleanPath == "app-config.json" || ext == ".apk" {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
}

func setupRoutes(h *Hub, tm *TelegramManager) http.Handler {
	mux := http.NewServeMux()
	ttsCache := newTTSCacheStore()

	// =========================================================================
	// 1. WebSocket Upgrade Endpoint (/ws)
	// =========================================================================
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "Expected websocket upgrade", http.StatusBadRequest)
			return
		}
		key := r.Header.Get("Sec-WebSocket-Key")
		if key == "" {
			http.Error(w, "Missing Sec-WebSocket-Key", http.StatusBadRequest)
			return
		}

		hSha := sha1.New()
		_, _ = hSha.Write([]byte(key + wsGUID))
		accept := base64.StdEncoding.EncodeToString(hSha.Sum(nil))

		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "Hijack failed", http.StatusInternalServerError)
			return
		}
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetNoDelay(true)
		}

		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := bufrw.WriteString(resp); err != nil {
			_ = conn.Close()
			return
		}
		if err := bufrw.Flush(); err != nil {
			_ = conn.Close()
			return
		}

		socketID := fmt.Sprintf("go-ws-%d-%d", time.Now().UnixNano(), rand.Intn(10000))
		wsConn := newWSConn(conn, bufrw.Reader, bufrw.Writer, socketID)
		h.registerClient(wsConn)

		go func() {
			defer h.unregisterClient(wsConn)
			for {
				op, payload, err := wsConn.ReadMessage()
				if err != nil {
					break
				}
				if op == opText {
					h.handleMessage(wsConn, payload)
				}
			}
		}()
	})

	// =========================================================================
	// 2. Health & APK Endpoints
	// =========================================================================
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.clientsMu.RLock()
		online := len(h.clients)
		h.clientsMu.RUnlock()
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"status":      "ok",
			"engine":      "Go-Authoritative",
			"onlineCount": online,
			"uptimeSecs":  int64(time.Since(h.bootTime).Seconds()),
			"timestamp":   nowMs(),
		})
	})

	mux.HandleFunc("/api/apk/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		isDebug := r.URL.Query().Get("variant") == "debug"
		fileName := "chess-game-release.apk"
		if isDebug {
			fileName = "chess-game-debug.apk"
		}
		repo := getGitHubRepoName()

		var diskInfo map[string]interface{}
		for _, p := range []string{"apk/apk-build-info.json", "../apk/apk-build-info.json"} {
			if raw, err := os.ReadFile(p); err == nil {
				if json.Unmarshal(raw, &diskInfo) == nil {
					break
				}
			}
		}

		var fileSize int64 = 6394293
		if diskInfo != nil {
			if sz, ok := diskInfo["fileSize"].(float64); ok && int64(sz) >= 4200000 {
				fileSize = int64(sz)
			}
		}
		isLocal := false
		for _, lc := range []string{
			filepath.Join("apk", fileName),
			filepath.Join("apk", "app-latest.apk"),
			filepath.Join("..", "apk", fileName),
			filepath.Join("..", "apk", "app-latest.apk"),
		} {
			if st, err := os.Stat(lc); err == nil && !st.IsDir() && st.Size() >= 4200000 {
				fileSize = st.Size()
				isLocal = true
				break
			}
		}
		if !isLocal {
			headURL := fmt.Sprintf("https://ghproxy.net/https://github.com/%s/releases/download/latest-apk/%s", repo, fileName)
			if resp, err := (&http.Client{Timeout: 2500 * time.Millisecond}).Head(headURL); err == nil {
				if cl := resp.Header.Get("Content-Length"); cl != "" {
					if sz, pErr := strconv.ParseInt(cl, 10, 64); pErr == nil && sz >= 4200000 {
						fileSize = sz
					}
				}
				_ = resp.Body.Close()
			}
		}
		fileSizeMb := fmt.Sprintf("%.2f MB", float64(fileSize)/(1024.0*1024.0))
		if diskInfo == nil {
			diskInfo = map[string]interface{}{
				"version":     "1.0.13",
				"buildTime":   "GitHub Actions 官方全量编译 (正式签名版)",
				"fileSizeMb":  fileSizeMb,
				"fileSize":    fileSize,
				"description": "三大经典国粹棋牌 - Android 官方最新全量发布版",
			}
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"repo":       repo,
			"apkExists":  true,
			"fileSize":   fileSize,
			"fileSizeMb": fileSizeMb,
			"isLocal":    isLocal,
			"mtime":      time.Now().UTC().Format(time.RFC3339),
			"buildInfo":  diskInfo,
		})
	})

	handleRebuildNotice := func(w http.ResponseWriter, r *http.Request) {
		allowed := false
		h.storage.WithDBRead(func(db *DatabaseSchema) { allowed = h.storage.authenticateAdmin(r, db) })
		if !allowed {
			sendJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "需要管理员权限方可执行构建"})
			return
		}
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "当前为纯 Go 单体全栈服务端模式，请使用 go-server/build_single_binary.sh 构建最新资源与二进制",
		})
	}
	mux.HandleFunc("/api/apk/rebuild", handleRebuildNotice)
	mux.HandleFunc("/api/download/apk/rebuild", handleRebuildNotice)

	handleAPKDownload := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		isDebug := r.URL.Path == "/api/apk/debug" || r.URL.Query().Get("variant") == "debug"
		fileName := "chess-game-release.apk"
		if isDebug {
			fileName = "chess-game-debug.apk"
		}
		serveAPKFileOrRedirect(w, r, fileName)
	}
	mux.HandleFunc("/api/download/apk", handleAPKDownload)
	mux.HandleFunc("/api/apk/release", handleAPKDownload)
	mux.HandleFunc("/api/apk/debug", handleAPKDownload)

	// =========================================================================
	// 3. TTS Speech Proxy with Server Memory Caching
	// =========================================================================
	mux.HandleFunc("/api/tts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		text := r.URL.Query().Get("text")
		isFemale := r.URL.Query().Get("female") == "true"
		if r.Method == http.MethodPost {
			body, err := parseRequestJSON(r)
			if err == nil {
				if t := jsonString(body, "text"); t != "" {
					text = t
				}
				if _, ok := body["female"]; ok {
					isFemale = jsonBool(body, "female", isFemale)
				}
			}
		}
		cleanText := strings.TrimSpace(ttsStripRe.ReplaceAllString(text, ""))
		if cleanText == "" {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Text parameter required"})
			return
		}
		speechText := strings.ReplaceAll(cleanText, "单张", "丹张")
		speechText = ttsDanLeadRe.ReplaceAllString(speechText, "丹$1")

		genderTag := "m"
		per := "1"
		if isFemale {
			genderTag = "f"
			per = "0"
		}
		cacheKey := cleanText + "_" + genderTag
		if cached, ok := ttsCache.get(cacheKey); ok {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Header().Set("Content-Length", strconv.Itoa(len(cached)))
			w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cached)
			return
		}

		client := &http.Client{Timeout: 2500 * time.Millisecond}
		endpoints := []string{
			fmt.Sprintf("https://fanyi.baidu.com/gettts?lan=zh&per=%s&text=%s&spd=6&source=web", per, url.QueryEscape(speechText)),
			fmt.Sprintf("https://tts.baidu.com/text2audio?cuid=baike&lan=ZH&ctp=1&per=%s&padd=&tp=1&tex=%s", per, url.QueryEscape(speechText)),
		}
		for _, ep := range endpoints {
			req, err := http.NewRequest(http.MethodGet, ep, nil)
			if err != nil {
				continue
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0.0.0 Safari/537.36")
			req.Header.Set("Referer", "https://fanyi.baidu.com/")
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			status := resp.StatusCode
			_ = resp.Body.Close()
			if readErr == nil && status >= 200 && status < 300 && len(data) > 300 {
				ttsCache.set(cacheKey, data)
				w.Header().Set("Content-Type", "audio/mpeg")
				w.Header().Set("Content-Length", strconv.Itoa(len(data)))
				w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(data)
				return
			}
		}

		voiceName := "zh-CN-YunxiNeural"
		if isFemale {
			voiceName = "zh-CN-XiaoxiaoNeural"
		}
		ssml := fmt.Sprintf("<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='zh-CN'><voice name='%s'><prosody pitch='+0Hz' rate='+12%%'>%s</prosody></voice></speak>", voiceName, cleanText)
		edgeBody, _ := json.Marshal(map[string]interface{}{
			"context": map[string]interface{}{
				"synthesis": map[string]interface{}{
					"audio": map[string]interface{}{
						"metadataversion": "1.0",
						"name":            "speech",
						"outputFormat":    "audio-24khz-48kbitrate-mono-mp3",
						"type":            "audio",
					},
				},
			},
			"script": ssml,
		})
		edgeReq, _ := http.NewRequest(http.MethodPost, "https://speech.platform.bing.com/consumer/speech/synthesize/readaloud/single/v1?trustedclienttoken=6A5AA1D4EA2D4738320C0F880E4CB756", bytes.NewReader(edgeBody))
		edgeReq.Header.Set("Content-Type", "application/json")
		edgeReq.Header.Set("User-Agent", "Mozilla/5.0 Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0")
		if resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(edgeReq); err == nil {
			data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			status := resp.StatusCode
			_ = resp.Body.Close()
			if readErr == nil && status >= 200 && status < 300 && len(data) > 300 {
				ttsCache.set(cacheKey, data)
				w.Header().Set("Content-Type", "audio/mpeg")
				w.Header().Set("Content-Length", strconv.Itoa(len(data)))
				w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(data)
				return
			}
		}

		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "TTS synthesis temporary failure"})
	})

	// =========================================================================
	// 4. Shisanshui Multi-Table Booking Train HTTP APIs
	// =========================================================================
	mux.HandleFunc("/api/booking/carriage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		fbID := q.Get("playerId")
		if fbID == "" {
			fbID = q.Get("userId")
		}
		userID, authName, authAvatar, errSt, errMsg := bookingUserFromRequest(r, h, fbID)
		if errSt != 0 {
			sendJSON(w, errSt, map[string]interface{}{"success": false, "error": errMsg})
			return
		}
		pName := authName
		if pName == "" {
			pName = q.Get("playerName")
		}
		if pName == "" {
			pName = "茶客"
		}
		av := authAvatar
		if av == "" {
			av = q.Get("avatar")
		}
		if av == "" {
			av = "👑"
		}
		reqNum, _ := strconv.Atoi(q.Get("carriageNumber"))
		state := h.booking.GetState(q.Get("sceneMode"), userID, pName, av, q.Get("carriageId"), reqNum)
		if notifs, ok := state["settledNotifications"].([]BookingSettledNotification); ok && len(notifs) > 0 {
			h.ctx.EmitBookingNotifications(notifs)
		}
		state["success"] = true
		sendJSON(w, http.StatusOK, state)
	})

	mux.HandleFunc("/api/booking/select-seat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		fbID := jsonString(payload, "playerId")
		if fbID == "" {
			fbID = jsonString(payload, "userId")
		}
		userID, authName, authAvatar, errSt, errMsg := bookingUserFromRequest(r, h, fbID)
		if errSt != 0 {
			sendJSON(w, errSt, map[string]interface{}{"success": false, "error": errMsg})
			return
		}
		pName := authName
		if pName == "" {
			pName = jsonString(payload, "playerName")
		}
		if pName == "" {
			pName = "茶客"
		}
		av := authAvatar
		if av == "" {
			av = jsonString(payload, "avatar")
		}
		if av == "" {
			av = "👑"
		}
		seat := jsonString(payload, "seat")
		if seat == "" {
			seat = "东"
		}
		resData := h.booking.SelectSeat(jsonString(payload, "sceneMode"), userID, pName, av, seat, jsonString(payload, "carriageId"), jsonInt(payload, "carriageNumber", 0))
		ok, _ := resData["ok"].(bool)
		resData["success"] = ok
		st := http.StatusBadRequest
		if ok {
			st = http.StatusOK
		}
		sendJSON(w, st, resData)
	})

	mux.HandleFunc("/api/booking/pick-table", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		fbID := jsonString(payload, "playerId")
		if fbID == "" {
			fbID = jsonString(payload, "userId")
		}
		userID, _, _, errSt, errMsg := bookingUserFromRequest(r, h, fbID)
		if errSt != 0 {
			sendJSON(w, errSt, map[string]interface{}{"success": false, "error": errMsg})
			return
		}
		resData := h.booking.PickTable(jsonString(payload, "sceneMode"), userID, jsonString(payload, "carriageId"), jsonInt(payload, "carriageNumber", 0), jsonInt(payload, "tableIndex", 0))
		ok, _ := resData["ok"].(bool)
		resData["success"] = ok
		st := http.StatusBadRequest
		if ok {
			st = http.StatusOK
		}
		sendJSON(w, st, resData)
	})

	mux.HandleFunc("/api/booking/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		fbID := jsonString(payload, "playerId")
		if fbID == "" {
			fbID = jsonString(payload, "userId")
		}
		userID, authName, authAvatar, errSt, errMsg := bookingUserFromRequest(r, h, fbID)
		if errSt != 0 {
			sendJSON(w, errSt, map[string]interface{}{"success": false, "error": errMsg})
			return
		}
		pName := authName
		if pName == "" {
			pName = jsonString(payload, "playerName")
		}
		if pName == "" {
			pName = "茶客"
		}
		av := authAvatar
		if av == "" {
			av = jsonString(payload, "avatar")
		}
		if av == "" {
			av = "👑"
		}
		rawHand, _ := json.Marshal(payload["playerHand"])
		resData := h.booking.SubmitHand(
			jsonString(payload, "sceneMode"),
			userID,
			pName,
			av,
			jsonString(payload, "carriageId"),
			jsonInt(payload, "carriageNumber", 0),
			jsonInt(payload, "tableIndex", 0),
			jsonString(payload, "playerSeat"),
			rawHand,
			jsonBool(payload, "isGraceSubmission", false),
		)
		ok, _ := resData["ok"].(bool)
		resData["success"] = ok
		st := http.StatusBadRequest
		if ok {
			st = http.StatusOK
		}
		sendJSON(w, st, resData)
	})

	mux.HandleFunc("/api/booking/advance-carriage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		fbID := jsonString(payload, "playerId")
		if fbID == "" {
			fbID = jsonString(payload, "userId")
		}
		userID, authName, authAvatar, errSt, errMsg := bookingUserFromRequest(r, h, fbID)
		if errSt != 0 {
			sendJSON(w, errSt, map[string]interface{}{"success": false, "error": errMsg})
			return
		}
		pName := authName
		if pName == "" {
			pName = jsonString(payload, "playerName")
		}
		if pName == "" {
			pName = "茶客"
		}
		av := authAvatar
		if av == "" {
			av = jsonString(payload, "avatar")
		}
		if av == "" {
			av = "👑"
		}
		resData := h.booking.AdvanceCarriage(jsonString(payload, "sceneMode"), userID, pName, av, jsonBool(payload, "createFresh", false))
		resData["success"] = true
		sendJSON(w, http.StatusOK, resData)
	})

	mux.HandleFunc("/api/booking/simulate-settle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		fbID := jsonString(payload, "playerId")
		if fbID == "" {
			fbID = jsonString(payload, "userId")
		}
		userID, _, _, errSt, errMsg := bookingUserFromRequest(r, h, fbID)
		if errSt != 0 {
			sendJSON(w, errSt, map[string]interface{}{"success": false, "error": errMsg})
			return
		}
		resData := h.booking.SimulateSettlement(jsonString(payload, "sceneMode"), userID, jsonString(payload, "carriageId"), jsonInt(payload, "carriageNumber", 0))
		ok, _ := resData["ok"].(bool)
		resData["success"] = ok
		st := http.StatusBadRequest
		if ok {
			st = http.StatusOK
		}
		sendJSON(w, st, resData)
	})

	mux.HandleFunc("/api/booking/ack-settlement", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, _ := parseRequestJSON(r)
		fbID := jsonString(payload, "playerId")
		if fbID == "" {
			fbID = jsonString(payload, "userId")
		}
		userID, _, _, errSt, errMsg := bookingUserFromRequest(r, h, fbID)
		if errSt != 0 {
			sendJSON(w, errSt, map[string]interface{}{"success": false, "error": errMsg})
			return
		}
		if userID != "" && userID != "guest" {
			h.booking.AckSettlements(userID)
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"success": true})
	})

	// =========================================================================
	// 5. Database Export, User Sync, Profile & Points Ledger
	// =========================================================================
	mux.HandleFunc("/api/database/export", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var allowed bool
		var out map[string]interface{}
		h.storage.WithDBRead(func(db *DatabaseSchema) {
			allowed = h.storage.authenticateAdmin(r, db)
			if !allowed {
				return
			}
			sanitized := make(map[string]interface{}, len(db.Profiles))
			for k, p := range db.Profiles {
				sanitized[k] = sanitizeProfileForClient(p)
			}
			out = map[string]interface{}{
				"profiles":            sanitized,
				"records":             db.Records,
				"authorizedPhones":    db.AuthorizedPhones,
				"adminPhones":         db.AdminPhones,
				"revokedPhones":       db.RevokedPhones,
				"adminLogs":           db.AdminLogs,
				"systemAnnouncements": db.SystemAnnouncements,
				"pointsLedger":        db.PointsLedger,
			}
		})
		if !allowed {
			sendJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "需要管理员权限方可导出数据"})
			return
		}
		sendJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("/api/user/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		profMap, _ := payload["profile"].(map[string]interface{})

		var status = http.StatusOK
		var respBody map[string]interface{}

		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			authUser := h.storage.authenticateRequest(r, db)
			if authUser == nil {
				status = http.StatusUnauthorized
				respBody = map[string]interface{}{"success": false, "error": "会话无效或未登录，请重新登录"}
				return false
			}
			if reqID := jsonString(profMap, "id"); reqID != "" && reqID != authUser.ID {
				status = http.StatusForbidden
				respBody = map[string]interface{}{"success": false, "error": "无权同步他人账号数据"}
				return false
			}
			isAllowed, isRevoked := isUserAuthorizedAndNotRevoked(h, db, authUser.Phone)
			if isRevoked || !isAllowed {
				delete(db.Profiles, authUser.ID)
				status = http.StatusOK
				respBody = map[string]interface{}{
					"success":  false,
					"notFound": true,
					"revoked":  true,
					"error":    "该账号手机号授权已被管理员移除，已注销",
				}
				return true
			}
			if authUser.IsBanned {
				status = http.StatusForbidden
				respBody = map[string]interface{}{"success": false, "error": "该账号已被封禁，禁止同步操作"}
				return false
			}

			if name := strings.TrimSpace(jsonString(profMap, "name")); name != "" && len([]rune(name)) <= 3 {
				authUser.Name = name
			}
			if av := jsonString(profMap, "avatar"); av != "" {
				runes := []rune(av)
				if len(runes) > 12 {
					av = string(runes[:12])
				}
				authUser.Avatar = av
			}
			if rtF, ok := profMap["rating"].(float64); ok {
				curRating := authUser.Rating
				if curRating <= 0 {
					curRating = 1800
				}
				delta := int(rtF+0.5) - curRating
				if delta < -200 {
					delta = -200
				} else if delta > 200 {
					delta = 200
				}
				nextRt := curRating + delta
				if nextRt < 1000 {
					nextRt = 1000
				} else if nextRt > 9999 {
					nextRt = 9999
				}
				authUser.Rating = nextRt
			}
			if lvF, ok := profMap["level"].(float64); ok {
				curLv := authUser.Level
				if curLv <= 0 {
					curLv = 1
				}
				nextLv := int(lvF + 0.5)
				if nextLv < 1 {
					nextLv = 1
				} else if nextLv > 20 {
					nextLv = 20
				}
				diff := nextLv - curLv
				if diff >= -2 && diff <= 2 {
					authUser.Level = nextLv
				}
			}
			if stMap, ok := profMap["stats"].(map[string]interface{}); ok {
				if authUser.Stats == nil {
					authUser.Stats = defaultStatsMap()
				}
				for _, gk := range []string{"yansan", "shisanshui", "mahjong"} {
					if incoming, ok := stMap[gk].(map[string]interface{}); ok {
						played := jsonInt(incoming, "played", 0)
						won := jsonInt(incoming, "won", 0)
						hs := jsonInt(incoming, "highestScore", 0)
						if played < 0 {
							played = 0
						} else if played > 999999 {
							played = 999999
						}
						if won < 0 {
							won = 0
						} else if won > 999999 {
							won = 999999
						}
						if hs < 0 {
							hs = 0
						} else if hs > 9999999 {
							hs = 9999999
						}
						authUser.Stats[gk] = GameStat{Played: played, Won: won, HighestScore: hs}
					}
				}
			}
			authUser.UpdatedAt = nowMs()

			if rawRecs, ok := payload["records"].([]interface{}); ok && len(rawRecs) > 0 {
				existingIDs := map[string]bool{}
				for _, rec := range db.Records {
					existingIDs[rec.ID] = true
				}
				var additions []MatchRecord
				for _, item := range rawRecs {
					if len(additions) >= 20 {
						break
					}
					rm, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					rid := jsonString(rm, "id")
					if rid == "" || existingIDs[rid] {
						continue
					}
					existingIDs[rid] = true
					additions = append(additions, MatchRecord{
						ID:          rid,
						PlayerID:    authUser.ID,
						GameType:    jsonString(rm, "gameType"),
						GameName:    jsonString(rm, "gameName"),
						Result:      jsonString(rm, "result"),
						CoinsDelta:  jsonInt(rm, "coinsDelta", 0),
						RatingDelta: jsonInt(rm, "ratingDelta", 0),
						Details:     jsonString(rm, "details"),
						Timestamp:   jsonString(rm, "timestamp"),
						UpdatedAt:   nowMs(),
					})
				}
				if len(additions) > 0 {
					merged := append(additions, db.Records...)
					if len(merged) > 500 {
						merged = merged[:500]
					}
					db.Records = merged
				}
			}

			respBody = map[string]interface{}{
				"success": true,
				"coins":   authUser.Coins,
				"rating":  authUser.Rating,
				"level":   authUser.Level,
				"profile": sanitizeProfileForClient(authUser),
			}
			return true
		})
		sendJSON(w, status, respBody)
	})

	mux.HandleFunc("/api/user/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		cleanName := strings.TrimSpace(jsonString(payload, "name"))
		if len([]rune(cleanName)) > 3 {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "昵称长度不能超过3个字"})
			return
		}
		var status = http.StatusOK
		var respBody map[string]interface{}
		changed := false
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			authUser := h.storage.authenticateRequest(r, db)
			if authUser == nil {
				status = http.StatusUnauthorized
				respBody = map[string]interface{}{"success": false, "error": "请先登录后再修改资料"}
				return false
			}
			if cleanName != "" {
				authUser.Name = cleanName
			}
			if av := strings.TrimSpace(jsonString(payload, "avatar")); av != "" {
				runes := []rune(av)
				if len(runes) > 12 {
					av = string(runes[:12])
				}
				authUser.Avatar = av
			}
			authUser.UpdatedAt = nowMs()
			changed = true
			respBody = map[string]interface{}{"success": true, "profile": sanitizeProfileForClient(authUser)}
			return true
		})
		if changed {
			h.broadcastTeahousePlayers()
		}
		sendJSON(w, status, respBody)
	})

	mux.HandleFunc("/api/points/ledger", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var status = http.StatusOK
		var respBody map[string]interface{}
		h.storage.WithDBRead(func(db *DatabaseSchema) {
			authUser := h.storage.authenticateRequest(r, db)
			if authUser == nil {
				status = http.StatusUnauthorized
				respBody = map[string]interface{}{"success": false, "error": "请先登录查看账单流水"}
				return
			}
			txs := db.PointsLedger[authUser.ID]
			if txs == nil {
				txs = []PointsTx{}
			}
			respBody = map[string]interface{}{
				"success":      true,
				"ledger":       txs,
				"transactions": txs,
			}
		})
		sendJSON(w, status, respBody)
	})

	// =========================================================================
	// 6. Authentication (/api/auth/register, /login, /check-session)
	// =========================================================================
	mux.HandleFunc("/api/auth/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		cleanPhone := normalizePhone(jsonString(payload, "phone"))
		password := strings.TrimSpace(jsonString(payload, "password"))
		nickname := strings.TrimSpace(jsonString(payload, "nickname"))
		avatar := strings.TrimSpace(jsonString(payload, "avatar"))
		if cleanPhone == "" || password == "" {
			sendJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "请填写完整的手机号和密码"})
			return
		}
		if len([]rune(password)) != 6 {
			sendJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "密码必须为六位数"})
			return
		}
		if len(cleanPhone) < 5 {
			sendJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "手机号格式不正确"})
			return
		}
		if len([]rune(nickname)) > 3 {
			sendJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "昵称长度不能超过3个字"})
			return
		}

		var respBody map[string]interface{}
		var registeredName string
		didRegister := false
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			allowedPhones, adminPhones := h.storage.getAllowedAndAdminPhones(db)
			isAuth := isPhoneInList(cleanPhone, allowedPhones) || isPhoneInList(cleanPhone, adminPhones)
			if !isAuth {
				respBody = map[string]interface{}{
					"success": false,
					"error":   "该手机号未获得 Telegram Bot 授权白名单！必须先由管理员在 Bot 中发送 /authphone 授权后方可注册。",
				}
				return false
			}
			isAdmin := isPhoneInList(cleanPhone, adminPhones)
			var existing *UserProfile
			for _, p := range db.Profiles {
				if p != nil && p.Phone != "" && phonesMatch(p.Phone, cleanPhone) {
					if p.Password != "" {
						respBody = map[string]interface{}{"success": false, "error": "该授权手机号已注册过账号，请直接使用手机号登录"}
						return false
					}
					existing = p
					break
				}
			}

			sessToken := generateSecureSessionToken()
			now := nowMs()
			if existing != nil {
				finalName := nickname
				if finalName == "" {
					finalName = existing.Name
				}
				if finalName == "" {
					suf := cleanPhone
					if len(suf) > 4 {
						suf = suf[len(suf)-4:]
					}
					finalName = "茶客" + suf
				}
				existing.Password = hashPasswordGo(password)
				existing.Name = finalName
				if avatar != "" {
					existing.Avatar = avatar
				} else if existing.Avatar == "" {
					existing.Avatar = "👑"
				}
				existing.IsAdmin = isAdmin
				existing.UpdatedAt = now
				existing.SessionToken = sessToken
				appendAdminLog(db, "USER_REGISTER", "Telegram Auth", cleanPhone,
					fmt.Sprintf("白名单预建用户激活注册成功: %s (📱 %s, UID: %s)", existing.Name, cleanPhone, existing.ID), 0)
				registeredName = existing.Name
				didRegister = true
				respBody = map[string]interface{}{
					"success":      true,
					"profile":      sanitizeProfileForClient(existing),
					"sessionToken": sessToken,
				}
				return true
			}

			suf := cleanPhone
			if len(suf) > 4 {
				suf = suf[len(suf)-4:]
			}
			finalName := nickname
			if finalName == "" {
				finalName = "茶客" + suf
			}
			if avatar == "" {
				avatar = "👑"
			}
			newID := fmt.Sprintf("USER-%d", 1000+rand.Intn(9000))
			for {
				if _, exists := db.Profiles[newID]; !exists {
					break
				}
				newID = fmt.Sprintf("USER-%d", 1000+rand.Intn(9000))
			}
			newProf := &UserProfile{
				ID:           newID,
				Username:     cleanPhone,
				Password:     hashPasswordGo(password),
				Name:         finalName,
				Avatar:       avatar,
				Phone:        cleanPhone,
				IsAdmin:      isAdmin,
				Coins:        0,
				Rating:       1800,
				Level:        1,
				RegisteredAt: now,
				UpdatedAt:    now,
				Stats:        defaultStatsMap(),
				SessionToken: sessToken,
			}
			db.Profiles[newID] = newProf
			appendAdminLog(db, "USER_REGISTER", "Telegram Auth", cleanPhone,
				fmt.Sprintf("新用户注册: %s (📱 %s, UID: %s)，初始筹码: 0", newProf.Name, cleanPhone, newID), 0)
			registeredName = newProf.Name
			didRegister = true
			respBody = map[string]interface{}{
				"success":      true,
				"profile":      sanitizeProfileForClient(newProf),
				"sessionToken": sessToken,
			}
			return true
		})

		if didRegister {
			tm.triggerDebouncedTxtBackup(h.storage, fmt.Sprintf("新茶客注册 (%s)", registeredName), "")
			h.broadcastTeahousePlayers()
		}
		sendJSON(w, http.StatusOK, respBody)
	})

	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		rawIdent := jsonString(payload, "phone")
		if rawIdent == "" {
			rawIdent = jsonString(payload, "username")
		}
		cleanPhone := normalizePhone(rawIdent)
		password := strings.TrimSpace(jsonString(payload, "password"))
		if cleanPhone == "" || password == "" {
			sendJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "请输入手机号和密码"})
			return
		}

		var respBody map[string]interface{}
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			var found *UserProfile
			for _, p := range db.Profiles {
				if p != nil && p.Phone != "" && phonesMatch(p.Phone, cleanPhone) {
					found = p
					break
				}
			}
			allowedPhones, adminPhones := h.storage.getAllowedAndAdminPhones(db)
			if found == nil {
				if isPhoneInList(cleanPhone, allowedPhones) || isPhoneInList(cleanPhone, adminPhones) {
					respBody = map[string]interface{}{
						"success":      false,
						"needRegister": true,
						"error":        "该手机号已在白名单授权，但尚未激活注册。请先点击【注册账号】设置您的6位密码与牌桌昵称！",
					}
					return false
				}
				respBody = map[string]interface{}{"success": false, "error": "该手机号尚未注册或密码错误，请先注册"}
				return false
			}
			if found.IsBanned {
				reason := found.BannedReason
				if reason == "" {
					reason = "违规操作"
				}
				respBody = map[string]interface{}{"success": false, "error": "该账号已被管理员封禁: " + reason}
				return false
			}
			isStillAuth, isRevoked := isUserAuthorizedAndNotRevoked(h, db, found.Phone)
			if isRevoked || !isStillAuth {
				delete(db.Profiles, found.ID)
				respBody = map[string]interface{}{
					"success": false,
					"error":   "该账号绑定的手机号授权已被管理员移除，账号已注销。如需使用请联系管理员重新授权。",
				}
				return true
			}
			if strings.TrimSpace(found.Password) == "" {
				respBody = map[string]interface{}{
					"success":      false,
					"needRegister": true,
					"error":        "该手机号已在白名单授权，但尚未激活注册。请先点击【注册账号】设置您的6位密码与牌桌昵称！",
				}
				return false
			}
			if !verifyPasswordGo(password, found.Password) {
				respBody = map[string]interface{}{"success": false, "error": "手机号或密码不正确"}
				return false
			}
			if !strings.HasPrefix(found.Password, "sha256$") {
				found.Password = hashPasswordGo(password)
			}
			sessToken := generateSecureSessionToken()
			found.SessionToken = sessToken
			respBody = map[string]interface{}{
				"success":      true,
				"profile":      sanitizeProfileForClient(found),
				"sessionToken": sessToken,
			}
			return true
		})
		sendJSON(w, http.StatusOK, respBody)
	})

	mux.HandleFunc("/api/auth/check-session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		userID := jsonString(payload, "userId")
		sessToken := jsonString(payload, "sessionToken")
		var respBody map[string]interface{}
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			user := db.Profiles[userID]
			if user == nil {
				respBody = map[string]interface{}{"success": true, "valid": false, "sessionExpired": true, "error": "用户不存在或已被清理"}
				return false
			}
			isStillAuth, isRevoked := isUserAuthorizedAndNotRevoked(h, db, user.Phone)
			if isRevoked || !isStillAuth {
				delete(db.Profiles, user.ID)
				respBody = map[string]interface{}{"success": true, "valid": false, "sessionExpired": true, "error": "该账号手机号授权已被管理员移出白名单，账号已注销"}
				return true
			}
			if user.SessionToken != "" && !safeTokenEqual(user.SessionToken, sessToken) {
				respBody = map[string]interface{}{"success": true, "valid": false, "sessionExpired": true, "error": "您的账号已在另一台设备登录，当前会话已失效"}
				return false
			}
			respBody = map[string]interface{}{
				"success": true,
				"valid":   true,
				"coins":   user.Coins,
				"rating":  user.Rating,
				"level":   user.Level,
				"name":    user.Name,
				"profile": sanitizeProfileForClient(user),
			}
			return false
		})
		sendJSON(w, http.StatusOK, respBody)
	})

	// =========================================================================
	// 7. User Search, Points Transfer, Teahouse Roster, Presence & Rooms
	// =========================================================================
	mux.HandleFunc("/api/users/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rawPhone := strings.TrimSpace(r.URL.Query().Get("phone"))
		cleanPhone := normalizePhone(rawPhone)
		var status = http.StatusOK
		var respBody map[string]interface{}
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			if h.storage.authenticateRequest(r, db) == nil {
				status = http.StatusUnauthorized
				respBody = map[string]interface{}{"success": false, "error": "请先登录后再查找茶客"}
				return false
			}
			if cleanPhone == "" {
				status = http.StatusBadRequest
				respBody = map[string]interface{}{"success": false, "error": "请输入要搜索的手机号"}
				return false
			}
			found := findUserInDB(cleanPhone, db)
			dirty := false
			if found == nil && len(cleanPhone) >= 7 {
				allowedPhones, adminPhones := h.storage.getAllowedAndAdminPhones(db)
				if isPhoneInList(cleanPhone, allowedPhones) || isPhoneInList(cleanPhone, adminPhones) {
					found = h.storage.ensureUserProfile(cleanPhone, db, 0)
					dirty = true
				}
			}
			if found == nil {
				respBody = map[string]interface{}{"success": false, "error": "未找到该手机号对应的茶客，请确认手机号是否正确或已获白名单授权"}
				return dirty
			}
			respBody = map[string]interface{}{
				"success": true,
				"user": map[string]interface{}{
					"id":       found.ID,
					"name":     found.Name,
					"username": found.Username,
					"phone":    found.Phone,
					"coins":    found.Coins,
					"rating":   found.Rating,
					"level":    found.Level,
					"avatar":   found.Avatar,
					"isAdmin":  found.IsAdmin,
					"isBanned": found.IsBanned,
				},
			}
			return dirty
		})
		sendJSON(w, status, respBody)
	})

	mux.HandleFunc("/api/points/transfer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		senderID := jsonString(payload, "senderId")
		recipientID := jsonString(payload, "recipientId")
		recipientPhone := jsonString(payload, "recipientPhone")
		amt := jsonInt(payload, "amount", 0)

		var status = http.StatusOK
		var respBody map[string]interface{}
		var senderSnap, recSnap *UserProfile
		transferred := false

		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			authUser := h.storage.authenticateRequest(r, db)
			if authUser == nil {
				status = http.StatusUnauthorized
				respBody = map[string]interface{}{"success": false, "error": "请先登录后再赠送筹码"}
				return false
			}
			if senderID != "" && senderID != authUser.ID {
				status = http.StatusForbidden
				respBody = map[string]interface{}{"success": false, "error": "身份校验失败，禁止操作他人账户"}
				return false
			}
			if amt <= 0 || amt > 10000000 {
				status = http.StatusBadRequest
				respBody = map[string]interface{}{"success": false, "error": "赠送筹码数量无效 (需为 1 - 10,000,000 之间的整数)"}
				return false
			}
			if authUser.IsBanned {
				status = http.StatusForbidden
				respBody = map[string]interface{}{"success": false, "error": "当前账号已被封禁，禁止划转筹码"}
				return false
			}
			if authUser.Coins < amt {
				respBody = map[string]interface{}{"success": false, "error": fmt.Sprintf("持有筹码不足，当前仅有 %d 分，无法赠送", authUser.Coins)}
				return false
			}

			var recipient *UserProfile
			if recipientID != "" {
				recipient = db.Profiles[recipientID]
			}
			if recipient == nil && recipientPhone != "" {
				recipient = findUserInDB(recipientPhone, db)
			}
			if recipient == nil && recipientPhone != "" {
				cleanRec := normalizePhone(recipientPhone)
				if len(cleanRec) >= 7 {
					allowedPhones, adminPhones := h.storage.getAllowedAndAdminPhones(db)
					if isPhoneInList(cleanRec, allowedPhones) || isPhoneInList(cleanRec, adminPhones) {
						recipient = h.storage.ensureUserProfile(cleanRec, db, 0)
					}
				}
			}
			if recipient == nil {
				respBody = map[string]interface{}{"success": false, "error": "未找到该接收手机号对应的已授权茶客"}
				return false
			}
			if recipient.IsBanned {
				respBody = map[string]interface{}{"success": false, "error": "对方账号当前处于封禁状态，无法接收筹码"}
				return false
			}
			sPhone := normalizePhone(authUser.Phone)
			rPhone := normalizePhone(recipient.Phone)
			if recipient.ID == authUser.ID || (sPhone != "" && rPhone != "" && sPhone == rPhone) {
				respBody = map[string]interface{}{"success": false, "error": "不能向自己的手机号赠送筹码"}
				return false
			}

			now := nowMs()
			authUser.Coins -= amt
			recipient.Coins += amt
			authUser.UpdatedAt = now
			recipient.UpdatedAt = now

			recDesc := recipient.Phone
			if recDesc == "" {
				recDesc = recipient.ID
			}
			sndDesc := authUser.Phone
			if sndDesc == "" {
				sndDesc = authUser.ID
			}
			recordServerPointsTx(db, authUser.ID, "transfer_out", fmt.Sprintf("向 %s (%s) 赠送筹码", recipient.Name, recDesc), -amt, authUser.Coins)
			recordServerPointsTx(db, recipient.ID, "transfer_in", fmt.Sprintf("收到 %s (%s) 赠送筹码", authUser.Name, sndDesc), amt, recipient.Coins)
			appendAdminLog(db, "POINTS_TRANSFER", authUser.Name, recipient.Name,
				fmt.Sprintf("互赠筹码 %d 分 (发起人: %s -> 接收人: %s)", amt, sndDesc, recDesc), amt)

			sCopy := *authUser
			rCopy := *recipient
			senderSnap = &sCopy
			recSnap = &rCopy
			transferred = true

			respBody = map[string]interface{}{
				"success":          true,
				"transferAmount":   amt,
				"senderCoins":      authUser.Coins,
				"recipientCoins":   recipient.Coins,
				"senderProfile":    sanitizeProfileForClient(authUser),
				"recipientProfile": sanitizeProfileForClient(recipient),
			}
			return true
		})

		if transferred && senderSnap != nil && recSnap != nil {
			tm.triggerDebouncedTxtBackup(h.storage, fmt.Sprintf("互赠筹码 (%s -> %s)", senderSnap.Name, recSnap.Name), "")
			h.BroadcastJSON(map[string]interface{}{
				"type":     "user_coins_updated",
				"userId":   senderSnap.ID,
				"phone":    senderSnap.Phone,
				"username": senderSnap.Username,
				"name":     senderSnap.Name,
				"coins":    senderSnap.Coins,
				"rating":   senderSnap.Rating,
				"level":    senderSnap.Level,
				"delta":    -amt,
			})
			h.BroadcastJSON(map[string]interface{}{
				"type":     "user_coins_updated",
				"userId":   recSnap.ID,
				"phone":    recSnap.Phone,
				"username": recSnap.Username,
				"name":     recSnap.Name,
				"coins":    recSnap.Coins,
				"rating":   recSnap.Rating,
				"level":    recSnap.Level,
				"delta":    amt,
			})
			h.broadcastTeahousePlayers()
			h.BroadcastJSON(map[string]interface{}{
				"type": "chat",
				"message": map[string]interface{}{
					"id":      fmt.Sprintf("gift_%d", nowMs()),
					"sender":  "钱庄管家",
					"avatar":  "🪙",
					"text":    fmt.Sprintf("🎉 茶客【%s】向【%s】赠送了 %s 分筹码！", senderSnap.Name, recSnap.Name, formatIntComma(amt)),
					"time":    time.Now().In(shanghaiZone).Format("15:04"),
					"channel": "world",
					"action":  "gift",
				},
			})
		}
		sendJSON(w, status, respBody)
	})

	voiceStore := newVoiceClipStore()

	mux.HandleFunc("/api/voice/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Invalid JSON"})
			return
		}
		rawAudio := strings.TrimSpace(jsonString(body, "audio"))
		m := voiceDataURLRe.FindStringSubmatch(rawAudio)
		if len(m) != 3 {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "无效的语音音频格式"})
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(m[2])
		if err != nil || len(decoded) == 0 || len(decoded) > voiceClipMaxBytes {
			sendJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{"success": false, "error": "语音文件过大或为空"})
			return
		}
		voiceID := fmt.Sprintf("v_%d_%s", nowMs(), randomHex(4))
		voiceStore.set(voiceID, m[1], decoded)
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"voiceId":  voiceID,
			"voiceUrl": "/api/voice/" + voiceID,
		})
	})

	mux.HandleFunc("/api/voice/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rawID := strings.TrimPrefix(r.URL.Path, "/api/voice/")
		var cleanB strings.Builder
		for _, ch := range rawID {
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
				cleanB.WriteRune(ch)
			}
		}
		voiceID := cleanB.String()
		if voiceID == "" {
			sendJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "语音已过期或不存在"})
			return
		}
		entry, ok := voiceStore.get(voiceID)
		if !ok {
			sendJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "语音已过期或不存在"})
			return
		}
		w.Header().Set("Content-Type", entry.mime)
		w.Header().Set("Cache-Control", "public, max-age=900")
		w.Header().Set("Content-Length", strconv.Itoa(len(entry.data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(entry.data)
	})

	mux.HandleFunc("/api/teahouse/players", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var players []TeahousePlayerItem
		h.storage.WithDBRead(func(db *DatabaseSchema) {
			players = h.getAllPlayers(db.Profiles)
		})
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"count":       len(players),
			"onlineCount": countOnlineTeahouse(players),
			"players":     players,
		})
	})

	mux.HandleFunc("/api/presence", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		counts := h.getRealGamePresence()
		if r.URL.Query().Get("includePlayers") == "1" {
			var players []TeahousePlayerItem
			h.storage.WithDBRead(func(db *DatabaseSchema) {
				players = h.getAllPlayers(db.Profiles)
			})
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"success":           true,
				"yansan":            counts.Yansan,
				"shisanshui":        counts.Shisanshui,
				"mahjong":           counts.Mahjong,
				"totalOnline":       counts.TotalOnline,
				"sceneWaitingStats": counts.SceneWaitingStats,
				"playersCount":      len(players),
				"players":           players,
			})
			return
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":           true,
			"yansan":            counts.Yansan,
			"shisanshui":        counts.Shisanshui,
			"mahjong":           counts.Mahjong,
			"totalOnline":       counts.TotalOnline,
			"sceneWaitingStats": counts.SceneWaitingStats,
		})
	})

	mux.HandleFunc("/api/rooms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.roomMu.Lock()
		rooms := h.buildSanitizedRoomsListLocked()
		h.roomMu.Unlock()
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"rooms":   rooms,
		})
	})

	// =========================================================================
	// 8. Admin Control APIs (/api/admin/*)
	// =========================================================================
	withAdmin := func(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			allowed := false
			h.storage.WithDBRead(func(db *DatabaseSchema) {
				allowed = h.storage.authenticateAdmin(r, db)
			})
			if !allowed {
				sendJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "管理员权限校验失败"})
				return
			}
			fn(w, r)
		}
	}

	mux.HandleFunc("/api/admin/users", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var users []map[string]interface{}
		h.storage.WithDBRead(func(db *DatabaseSchema) {
			users = make([]map[string]interface{}, 0, len(db.Profiles))
			for _, p := range db.Profiles {
				if p == nil {
					continue
				}
				ph := p.Phone
				if ph == "" {
					ph = "未绑定"
				}
				users = append(users, map[string]interface{}{
					"id":           p.ID,
					"name":         p.Name,
					"username":     p.Username,
					"avatar":       p.Avatar,
					"phone":        ph,
					"isAdmin":      p.IsAdmin,
					"isBanned":     p.IsBanned,
					"bannedReason": p.BannedReason,
					"coins":        p.Coins,
					"rating":       p.Rating,
					"level":        p.Level,
					"updatedAt":    p.UpdatedAt,
					"stats":        p.Stats,
				})
			}
		})
		sendJSON(w, http.StatusOK, map[string]interface{}{"success": true, "users": users})
	}))

	mux.HandleFunc("/api/admin/points/adjust", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		targetID := jsonString(payload, "targetId")
		phone := jsonString(payload, "phone")
		adjType := jsonString(payload, "type")
		if adjType == "" {
			adjType = "add"
		}
		reason := jsonString(payload, "reason")
		if reason == "" {
			reason = "管理员调整"
		}
		operator := jsonString(payload, "operator")
		if operator == "" {
			operator = "Telegram Admin"
		}
		num := jsonInt(payload, "delta", 0)

		var status = http.StatusOK
		var respBody map[string]interface{}
		var adjusted *UserProfile
		var netDelta int

		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			var user *UserProfile
			if targetID != "" {
				user = db.Profiles[targetID]
			}
			if user == nil && phone != "" {
				user = findUserInDB(phone, db)
			}
			if user == nil {
				status = http.StatusNotFound
				respBody = map[string]interface{}{"success": false, "error": "未找到目标用户"}
				return false
			}
			oldCoins := user.Coins
			absNum := num
			if absNum < 0 {
				absNum = -absNum
			}
			switch adjType {
			case "set":
				if num < 0 {
					user.Coins = 0
				} else {
					user.Coins = num
				}
			case "subtract":
				user.Coins = oldCoins - absNum
				if user.Coins < 0 {
					user.Coins = 0
				}
			default:
				user.Coins = oldCoins + absNum
			}
			user.UpdatedAt = nowMs()
			netDelta = user.Coins - oldCoins
			if netDelta != 0 {
				txType := "admin_grant"
				if netDelta < 0 {
					txType = "admin_deduct"
				}
				recordServerPointsTx(db, user.ID, txType, fmt.Sprintf("%s (%+d)", reason, netDelta), netDelta, user.Coins)
			}
			appendAdminLog(db, "POINTS_"+strings.ToUpper(adjType), operator, user.Name,
				fmt.Sprintf("%s: %d -> %d (变动: %+d)", reason, oldCoins, user.Coins, netDelta), netDelta)
			cp := *user
			adjusted = &cp
			respBody = map[string]interface{}{
				"success":  true,
				"user":     sanitizeProfileForClient(user),
				"oldCoins": oldCoins,
				"newCoins": user.Coins,
				"delta":    netDelta,
			}
			return true
		})

		if adjusted != nil {
			tm.triggerDebouncedTxtBackup(h.storage, fmt.Sprintf("Web后台调整筹码 (%s)", adjusted.Name), "")
			h.BroadcastJSON(map[string]interface{}{
				"type":      "user_coins_updated",
				"userId":    adjusted.ID,
				"phone":     adjusted.Phone,
				"username":  adjusted.Username,
				"name":      adjusted.Name,
				"coins":     adjusted.Coins,
				"rating":    adjusted.Rating,
				"level":     adjusted.Level,
				"delta":     netDelta,
				"operator":  operator,
				"timestamp": nowMs(),
			})
			h.broadcastTeahousePlayers()
		}
		sendJSON(w, status, respBody)
	}))

	handleExportPointsTxt := withAdmin(func(w http.ResponseWriter, r *http.Request) {
		var snap *DatabaseSchema
		h.storage.WithDBRead(func(db *DatabaseSchema) { snap = db })
		exp := exportPlayersPointsTxt(snap)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, exp.SafeFileName, url.QueryEscape(exp.FileName)))
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(exp.TxtContent))
	})
	mux.HandleFunc("/api/admin/export-points-txt", handleExportPointsTxt)
	mux.HandleFunc("/api/admin/points/export-txt", handleExportPointsTxt)

	mux.HandleFunc("/api/admin/points/batch", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		num := jsonInt(payload, "amount", 0)
		if num == 0 {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "批量调整数量不能为0"})
			return
		}
		reason := jsonString(payload, "reason")
		if reason == "" {
			reason = "全服福利金发放"
		}
		operator := jsonString(payload, "operator")
		if operator == "" {
			operator = "Telegram Admin"
		}
		affected := 0
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			now := nowMs()
			for _, u := range db.Profiles {
				if u == nil {
					continue
				}
				oldCoins := u.Coins
				u.Coins += num
				if u.Coins < 0 {
					u.Coins = 0
				}
				u.UpdatedAt = now
				delta := u.Coins - oldCoins
				if delta != 0 {
					txType := "admin_grant"
					if delta < 0 {
						txType = "admin_deduct"
					}
					recordServerPointsTx(db, u.ID, txType, fmt.Sprintf("%s (%+d)", reason, delta), delta, u.Coins)
				}
				affected++
			}
			appendAdminLog(db, "POINTS_BATCH", operator, fmt.Sprintf("全服玩家 (%d人)", affected),
				fmt.Sprintf("%s: 每人发放 %+d 分", reason, num), num)
			return true
		})
		h.BroadcastJSON(map[string]interface{}{
			"type":   "chat_broadcast",
			"roomId": "ALL",
			"message": map[string]interface{}{
				"id":        fmt.Sprintf("sys-%d", nowMs()),
				"sender":    "茶楼通告",
				"avatar":    "📢",
				"text":      fmt.Sprintf("【全服通告】%s：所有茶客每人到账 %d 分筹码！", reason, num),
				"timestamp": time.Now().In(shanghaiZone).Format("15:04"),
			},
		})
		h.broadcastTeahousePlayers()
		sendJSON(w, http.StatusOK, map[string]interface{}{"success": true, "affectedCount": affected, "amount": num})
	}))

	mux.HandleFunc("/api/admin/whitelist", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var allowedPhones, adminPhones []string
		h.storage.WithDBRead(func(db *DatabaseSchema) {
			allowedPhones, adminPhones = h.storage.getAllowedAndAdminPhones(db)
		})
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":          true,
			"authorizedPhones": allowedPhones,
			"adminPhones":      adminPhones,
		})
	}))

	mux.HandleFunc("/api/admin/whitelist/add", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		operator := jsonString(payload, "operator")
		if operator == "" {
			operator = "Telegram Admin"
		}
		var rawList []string
		if arr, ok := payload["phones"].([]interface{}); ok {
			for _, it := range arr {
				if p := normalizePhone(fmt.Sprintf("%v", it)); p != "" {
					rawList = append(rawList, p)
				}
			}
		} else {
			for _, tok := range strings.FieldsFunc(jsonString(payload, "phones"), func(r rune) bool {
				return r == ' ' || r == ',' || r == ';'
			}) {
				if p := normalizePhone(tok); p != "" {
					rawList = append(rawList, p)
				}
			}
		}
		if len(rawList) == 0 {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请输入有效的手机号"})
			return
		}

		added := []string{}
		var totalAuth []string
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			for _, p := range rawList {
				if !isPhoneInList(p, db.AuthorizedPhones) {
					db.AuthorizedPhones = append(db.AuthorizedPhones, p)
					added = append(added, p)
				}
				kept := db.RevokedPhones[:0]
				for _, rp := range db.RevokedPhones {
					if !phonesMatch(rp, p) {
						kept = append(kept, rp)
					}
				}
				db.RevokedPhones = kept
			}
			appendAdminLog(db, "WHITELIST_ADD", operator, strings.Join(added, ", "),
				fmt.Sprintf("将 %d 个手机号加入 Telegram 授权白名单: %s", len(added), strings.Join(added, ", ")), 0)
			totalAuth = db.AuthorizedPhones
			return true
		})
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":         true,
			"added":           added,
			"totalAuthorized": totalAuth,
		})
	}))

	mux.HandleFunc("/api/admin/whitelist/remove", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		cleanPhone := normalizePhone(jsonString(payload, "phone"))
		if cleanPhone == "" {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请提供要移出的手机号"})
			return
		}
		operator := jsonString(payload, "operator")
		if operator == "" {
			operator = "Telegram Admin"
		}
		deleted := []string{}
		var totalAuth []string
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			authKept := db.AuthorizedPhones[:0]
			for _, p := range db.AuthorizedPhones {
				if !phonesMatch(p, cleanPhone) {
					authKept = append(authKept, p)
				}
			}
			db.AuthorizedPhones = authKept
			admKept := db.AdminPhones[:0]
			for _, p := range db.AdminPhones {
				if !phonesMatch(p, cleanPhone) {
					admKept = append(admKept, p)
				}
			}
			db.AdminPhones = admKept
			if !isPhoneInList(cleanPhone, db.RevokedPhones) {
				db.RevokedPhones = append(db.RevokedPhones, cleanPhone)
			}
			for pid, prof := range db.Profiles {
				if prof != nil && prof.Phone != "" && phonesMatch(prof.Phone, cleanPhone) {
					deleted = append(deleted, fmt.Sprintf("%s (%s)", prof.Name, pid))
					delete(db.Profiles, pid)
				}
			}
			delDesc := "无注册用户"
			if len(deleted) > 0 {
				delDesc = strings.Join(deleted, ", ")
			}
			appendAdminLog(db, "WHITELIST_REMOVE", operator, cleanPhone,
				fmt.Sprintf("将手机号 %s 移出授权白名单，彻底清理注销账号: %s", cleanPhone, delDesc), 0)
			totalAuth = db.AuthorizedPhones
			return true
		})
		h.broadcastTeahousePlayers()
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":         true,
			"removed":         cleanPhone,
			"deletedUsers":    deleted,
			"totalAuthorized": totalAuth,
		})
	}))

	mux.HandleFunc("/api/admin/user/delete", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		userID := jsonString(payload, "userId")
		phone := jsonString(payload, "phone")
		operator := jsonString(payload, "operator")
		if operator == "" {
			operator = "Telegram Admin"
		}
		var status = http.StatusOK
		var respBody map[string]interface{}
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			var u *UserProfile
			if userID != "" {
				u = db.Profiles[userID]
			}
			if u == nil && phone != "" {
				u = findUserInDB(phone, db)
			}
			if u == nil {
				status = http.StatusNotFound
				respBody = map[string]interface{}{"success": false, "error": "未找到指定用户"}
				return false
			}
			pid := u.ID
			uName := u.Name
			uPhone := normalizePhone(u.Phone)
			delete(db.Profiles, pid)
			if uPhone != "" {
				authKept := db.AuthorizedPhones[:0]
				for _, p := range db.AuthorizedPhones {
					if !phonesMatch(p, uPhone) {
						authKept = append(authKept, p)
					}
				}
				db.AuthorizedPhones = authKept
				admKept := db.AdminPhones[:0]
				for _, p := range db.AdminPhones {
					if !phonesMatch(p, uPhone) {
						admKept = append(admKept, p)
					}
				}
				db.AdminPhones = admKept
				if !isPhoneInList(uPhone, db.RevokedPhones) {
					db.RevokedPhones = append(db.RevokedPhones, uPhone)
				}
			}
			pShow := uPhone
			if pShow == "" {
				pShow = "未绑"
			}
			appendAdminLog(db, "USER_DELETE", operator, uName, fmt.Sprintf("管理员彻底删除并注销用户: %s (%s, 📱 %s)", uName, pid, pShow), 0)
			respBody = map[string]interface{}{"success": true, "deletedUser": uName, "userId": pid}
			return true
		})
		h.broadcastTeahousePlayers()
		sendJSON(w, status, respBody)
	}))

	mux.HandleFunc("/api/admin/user/ban", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		userID := jsonString(payload, "userId")
		phone := jsonString(payload, "phone")
		isBanned := jsonBool(payload, "isBanned", false)
		reason := jsonString(payload, "reason")
		if reason == "" {
			reason = "违反茶楼守则"
		}
		operator := jsonString(payload, "operator")
		if operator == "" {
			operator = "Telegram Admin"
		}
		var status = http.StatusOK
		var respBody map[string]interface{}
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			var u *UserProfile
			if userID != "" {
				u = db.Profiles[userID]
			}
			if u == nil && phone != "" {
				u = findUserInDB(phone, db)
			}
			if u == nil {
				status = http.StatusNotFound
				respBody = map[string]interface{}{"success": false, "error": "未找到指定用户"}
				return false
			}
			u.IsBanned = isBanned
			if isBanned {
				u.BannedReason = reason
			} else {
				u.BannedReason = ""
			}
			u.UpdatedAt = nowMs()
			act := "USER_UNBAN"
			det := fmt.Sprintf("解封账号 (%s)", u.Phone)
			if isBanned {
				act = "USER_BAN"
				det = fmt.Sprintf("封禁账号 (%s): %s", u.Phone, reason)
			}
			appendAdminLog(db, act, operator, u.Name, det, 0)
			respBody = map[string]interface{}{"success": true, "user": sanitizeProfileForClient(u)}
			return true
		})
		sendJSON(w, status, respBody)
	}))

	mux.HandleFunc("/api/admin/broadcast", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := parseRequestJSON(r)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		cleanText := strings.TrimSpace(jsonString(payload, "text"))
		if cleanText == "" {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "公告内容不能为空"})
			return
		}
		author := jsonString(payload, "author")
		if author == "" {
			author = "Telegram 管理员"
		}
		now := nowMs()
		ann := SystemAnnouncement{
			ID:        fmt.Sprintf("ann-%d", now),
			Text:      cleanText,
			Author:    author,
			Timestamp: time.Now().In(shanghaiZone).Format("15:04"),
		}
		_ = h.storage.WithDBWrite(func(db *DatabaseSchema) bool {
			list := append([]SystemAnnouncement{ann}, db.SystemAnnouncements...)
			if len(list) > 20 {
				list = list[:20]
			}
			db.SystemAnnouncements = list
			appendAdminLog(db, "ANNOUNCEMENT_BROADCAST", author, "全服房间", cleanText, 0)
			return true
		})
		h.BroadcastJSON(map[string]interface{}{
			"type":   "chat_broadcast",
			"roomId": "ALL",
			"message": map[string]interface{}{
				"id":        fmt.Sprintf("broadcast-%d", now),
				"sender":    "📢 " + author,
				"avatar":    "🏮",
				"text":      cleanText,
				"timestamp": ann.Timestamp,
			},
		})
		sendJSON(w, http.StatusOK, map[string]interface{}{"success": true, "announcement": ann})
	}))

	mux.HandleFunc("/api/admin/logs", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var logs []AdminLog
		h.storage.WithDBRead(func(db *DatabaseSchema) {
			logs = db.AdminLogs
			if logs == nil {
				logs = []AdminLog{}
			}
		})
		sendJSON(w, http.StatusOK, map[string]interface{}{"success": true, "logs": logs})
	}))

	// =========================================================================
	// 9. Telegram Bot Admin & Webhook Endpoints
	// =========================================================================
	registerTelegramRoutes(mux, h, tm)

	// =========================================================================
	// 10. Static Files & Embedded SPA Fallback (./dist -> ../dist -> embed)
	// =========================================================================
	var rootFS fs.FS
	if info, err := os.Stat("dist"); err == nil && info.IsDir() {
		rootFS = os.DirFS("dist")
	} else if info, err := os.Stat("../dist"); err == nil && info.IsDir() {
		rootFS = os.DirFS("../dist")
	} else {
		if sub, err := fs.Sub(embeddedDistFS, "dist"); err == nil {
			rootFS = sub
		} else {
			rootFS = embeddedDistFS
		}
	}

	fileServer := http.FileServer(http.FS(rootFS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/ws" {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(strings.ToLower(r.URL.Path), ".apk") {
			serveAPKFileOrRedirect(w, r, filepath.Base(r.URL.Path))
			return
		}

		cleanPath := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(r.URL.Path)), "/")
		if cleanPath == "" || cleanPath == "." {
			cleanPath = "index.html"
		}

		if f, err := rootFS.Open(cleanPath); err == nil {
			info, stErr := f.Stat()
			_ = f.Close()
			if stErr == nil && !info.IsDir() {
				setStaticContentTypeAndCache(w, cleanPath)
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA Fallback -> index.html (never cached)
		if idxFile, err := rootFS.Open("index.html"); err == nil {
			data, readErr := io.ReadAll(idxFile)
			_ = idxFile.Close()
			if readErr == nil {
				setStaticContentTypeAndCache(w, "index.html")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(data)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})

	// Universal CORS & Preflight wrapper
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyCORSHeaders(w, r, h.storage)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
