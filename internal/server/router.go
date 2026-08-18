package server

import (
	"net/http"

	"go01-community-notice/internal/handler"
)

// NewMux 创建 Go 1.22 ServeMux 并注册所有路由。
func NewMux(h *handler.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux
}

// Handle wraps a handler with the standard method-routing pattern.
// 仅在需要时暴露的辅助构造，保留以兼容外部用法。
func Handle(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	mux.HandleFunc(pattern, h)
}
