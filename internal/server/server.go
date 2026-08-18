// Package server 组装 HTTP 服务器与优雅关闭逻辑。
package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"go01-community-notice/internal/config"
	"go01-community-notice/internal/middleware"
)

// Server 封装 *http.Server 与依赖，提供 Start / Shutdown。
type Server struct {
	httpServer *http.Server
	cfg        config.Config
	handler    http.Handler
}

// New 用给定处理器与配置创建服务器。
func New(cfg config.Config, handler http.Handler) *Server {
	// 全局中间件：Recover -> CORS -> Logger -> 业务路由。
	wrapped := middleware.Chain(
		middleware.Recover(),
		middleware.CORS(cfg.CORSOrigins),
		middleware.Logger(log.Printf),
	)(handler)

	return &Server{
		cfg:     cfg,
		handler: wrapped,
		httpServer: &http.Server{
			Addr:         cfg.Addr,
			Handler:      wrapped,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
			IdleTimeout:  60 * time.Second,
		},
	}
}

// Start 启动 HTTP 服务。阻塞直到 Shutdown 或出错。
// 使用 errGroup 语义：ListenAndServe 返回 http.ErrServerClosed 视为正常。
func (s *Server) Start() error {
	log.Printf("server: listening on %s", s.cfg.Addr)
	err := s.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown 优雅关闭：停止接收新连接，等待在途请求完成（带超时）。
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("server: shutting down...")
	return s.httpServer.Shutdown(ctx)
}

// HTTPServer 暴露底层 *http.Server（供测试启动用）。
func (s *Server) HTTPServer() *http.Server { return s.httpServer }

// Addr 监听地址。
func (s *Server) Addr() string { return s.cfg.Addr }

// runOnce 防止重复关闭。
var shutdownMu sync.Mutex
