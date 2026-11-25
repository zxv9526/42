package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

//go:embed all:dist
var embeddedDistFS embed.FS

const defaultPort = "3000"

func cleanupCorruptLocalAPKs() {
	candidates := []string{
		"dist/chess-game-release.apk",
		"dist/chess-game-debug.apk",
		"dist/app-latest.apk",
		"public/chess-game-release.apk",
		"public/chess-game-debug.apk",
		"apk/chess-game-release.apk",
		"apk/chess-game-debug.apk",
		"apk/app-latest.apk",
	}
	for _, p := range candidates {
		info, err := os.Stat(p)
		if err == nil && !info.IsDir() && info.Size() < 4200000 {
			if rmErr := os.Remove(p); rmErr == nil {
				log.Printf("[APK Cleanup] Removed incomplete APK (< 4.2MB): %s (%d bytes)", filepath.Clean(p), info.Size())
			}
		}
	}
}

func main() {
	sm := newStorageManager()
	port := sm.getEnv("PORT")
	if port == "" {
		port = defaultPort
	}

	cleanupCorruptLocalAPKs()

	bm := newBookingManager(sm)
	predealt4, predealt8 := bm.InitInventory()
	log.Printf("[Booking Engine] Initialized multi-table train inventory (four-booking=%d, eight-booking=%d)", predealt4, predealt8)

	hub := newHub(sm, bm)
	hub.startBackgroundLoops()

	tm := newTelegramManager()
	tm.initBotService(hub)

	handler := setupRoutes(hub, tm)
	addr := fmt.Sprintf("0.0.0.0:%s", port)
	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println("==================================================================")
		fmt.Println("  🀄 国粹棋牌 Go 单体全栈权威服务端已就绪 (Go-Authoritative Engine)")
		fmt.Printf("  📱 监听地址: http://%s\n", addr)
		fmt.Printf("  ⚡ WebSocket: ws://%s/ws\n", addr)
		fmt.Println("  🎮 权威引擎: 经典烟三 · 水镜十三水(含预约专列) · 国标麻将")
		fmt.Println("  🚀 架构特性: 零外部依赖、//go:embed 静态资源内嵌、多端毫秒同步")
		fmt.Println("==================================================================")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Go Server Error] %v", err)
		}
	}()

	<-stopChan
	fmt.Println("\n[Go Server] 正在保存状态并安全停止服务...")
	tm.stopPolling()
	hub.stop()
	if err := sm.FlushSync(); err != nil {
		log.Printf("[Go Server] FlushSync warning: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	fmt.Println("[Go Server] 服务已安全退出。")
}
