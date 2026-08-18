// Command go01-community-notice 启动社区通知公告系统服务。
//
// 启动流程：
//  1. 加载配置（环境变量）。
//  2. 打开文件持久化存储（加载已有数据）。
//  3. 初始化口令哈希器、会话管理器、业务服务。
//  4. 注册路由、装配中间件、创建 HTTP 服务器。
//  5. 种子管理员（若启用且不存在）。
//  6. 监听信号，优雅关闭：停止服务 -> 落盘 -> 退出。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go01-community-notice/internal/auth"
	"go01-community-notice/internal/config"
	"go01-community-notice/internal/handler"
	"go01-community-notice/internal/server"
	"go01-community-notice/internal/service"
	"go01-community-notice/internal/store"
	"go01-community-notice/internal/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error {
	// 1. 配置。
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log.Printf("config: addr=%s data=%s sessionTTL=%s", cfg.Addr, cfg.DataPath, cfg.SessionTTL)

	// 2. 存储层：文件持久化 + 内存。
	fs, err := store.NewFileStore(cfg.DataPath)
	if err != nil {
		return err
	}
	st := fs.Store()
	defer func() {
		// 兜底落盘（优雅关闭流程也会落盘，此处为 panic 兜底）。
		_ = fs.Flush()
	}()

	// 3. 鉴权与服务层。
	hasher := auth.NewPasswordHasher()
	sessions := auth.NewSessionManager(cfg.SessionTTL)
	services := service.NewServices(st, hasher, sessions, nil)

	// 4. 路由与服务器。
	h := handler.New(services, st, sessions, web.Files())
	mux := server.NewMux(h)
	srv := server.New(cfg, mux)

	// 5. 种子管理员。
	if cfg.SeedAdmin {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := store.SeedAdmin(ctx, st, hasher, cfg.AdminUsername, cfg.AdminPassword); err != nil {
			cancel()
			return err
		}
		cancel()
	}

	// 启动会话清理协程（后台 lazy 清理的补充）。
	stopCleaner := startSessionCleaner(sessions)
	defer stopCleaner()

	// 6. 信号监听与优雅关闭。
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		log.Printf("main: received signal %s", sig)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("main: graceful shutdown error: %v", err)
	}
	// 最终落盘。
	if err := fs.Flush(); err != nil {
		log.Printf("main: flush error: %v", err)
	}
	log.Println("main: bye")
	return nil
}

// startSessionCleaner 定时清理过期会话，返回停止函数。
func startSessionCleaner(sm *auth.SessionManager) func() {
	ticker := time.NewTicker(10 * time.Minute)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				if n := sm.CleanupExpired(); n > 0 {
					log.Printf("session: cleaned %d expired sessions", n)
				}
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()
	return func() { close(done) }
}
