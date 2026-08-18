// Package handler 实现社区通知公告系统的 HTTP 处理器。
//
// 使用 Go 1.22 net/http.ServeMux 的方法路由与 {id} 路径通配：
//
//	mux.HandleFunc("GET /api/notices/{id}", h.GetNotice)
//	id := r.PathValue("id")
//
// 处理器只做：请求解码 + 校验入口 + 调用 service + 响应序列化，
// 不包含业务规则；业务规则全部在 service 层。
package handler

import (
	"io/fs"
	"net/http"

	"go01-community-notice/internal/auth"
	"go01-community-notice/internal/middleware"
	"go01-community-notice/internal/model"
	"go01-community-notice/internal/respond"
	"go01-community-notice/internal/service"
	"go01-community-notice/internal/store"
)

// Handler 聚合所有处理器与依赖服务。
type Handler struct {
	services *service.Services
	store    store.Store
	sessions *auth.SessionManager
	assets   fs.FS
}

// New 创建处理器聚合。
//
// services: 业务服务集合。
// store: 数据访问（同时作为 RequireAuth 中间件的 UserService 实现）。
// sessions: 会话管理器。
// assets: 前端静态资源文件系统（可为 nil，表示不提供页面）。
func New(svc *service.Services, st store.Store, sessions *auth.SessionManager, assets fs.FS) *Handler {
	return &Handler{
		services: svc,
		store:    st,
		sessions: sessions,
		assets:   assets,
	}
}

// Routes 注册所有路由到给定 mux。
func (h *Handler) Routes(mux *http.ServeMux) {
	// store.Store 满足 middleware.UserService（均有 GetUserByID）。
	auth := middleware.RequireAuth(h.sessions, h.store)
	admin := middleware.Chain(auth, middleware.RequireAdmin())
	resident := middleware.Chain(auth, middleware.RequireResident())

	// 公开接口。
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("POST /api/auth/login", h.Login)

	// 需登录。
	mux.Handle("POST /api/auth/logout", auth(http.HandlerFunc(h.Logout)))
	mux.Handle("GET /api/auth/me", auth(http.HandlerFunc(h.Me)))

	mux.Handle("GET /api/notices", auth(http.HandlerFunc(h.ListNotices)))
	mux.Handle("GET /api/notices/{id}", auth(http.HandlerFunc(h.GetNotice)))

	// 仅居民：已读/未读。
	mux.Handle("GET /api/notices/{id}/read-status", resident(http.HandlerFunc(h.ReadStatus)))
	mux.Handle("POST /api/notices/{id}/read", resident(http.HandlerFunc(h.MarkRead)))
	mux.Handle("GET /api/me/notices", resident(http.HandlerFunc(h.MyNotices)))
	mux.Handle("GET /api/me/unread-count", resident(http.HandlerFunc(h.UnreadCount)))

	// 仅管理员：通知写操作、用户管理、统计。
	mux.Handle("POST /api/notices", admin(http.HandlerFunc(h.CreateNotice)))
	mux.Handle("PUT /api/notices/{id}", admin(http.HandlerFunc(h.UpdateNotice)))
	mux.Handle("DELETE /api/notices/{id}", admin(http.HandlerFunc(h.DeleteNotice)))
	mux.Handle("POST /api/notices/{id}/publish", admin(http.HandlerFunc(h.PublishNotice)))
	mux.Handle("POST /api/notices/{id}/unpublish", admin(http.HandlerFunc(h.UnpublishNotice)))
	mux.Handle("POST /api/notices/{id}/pin", admin(http.HandlerFunc(h.TogglePin)))
	mux.Handle("GET /api/users", admin(http.HandlerFunc(h.ListUsers)))
	mux.Handle("POST /api/users", admin(http.HandlerFunc(h.CreateUser)))
	mux.Handle("DELETE /api/users/{id}", admin(http.HandlerFunc(h.DeleteUser)))
	mux.Handle("GET /api/stats", admin(http.HandlerFunc(h.GlobalStats)))
	mux.Handle("GET /api/stats/notices/{id}", admin(http.HandlerFunc(h.NoticeStats)))

	// 前端页面与静态资源。
	h.registerPageRoutes(mux)
}

// healthz 健康检查。
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	respond.OK(w, model.HealthResponse{Status: "ok"})
}

// ensure 上下文：避免某些包未被引用的编译期占位。
var _ = model.RoleAdmin
