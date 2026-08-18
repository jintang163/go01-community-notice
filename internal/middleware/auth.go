// Package middleware 提供 HTTP 中间件：鉴权、角色控制、日志、Recover、CORS。
package middleware

import (
	"context"
	"net/http"
	"strings"

	"go01-community-notice/internal/auth"
	"go01-community-notice/internal/model"
	"go01-community-notice/internal/respond"
	"go01-community-notice/internal/service"
)

// bearerPrefix Authorization 头前缀。
const bearerPrefix = "Bearer "

// extractToken 从 Authorization 头提取 Bearer token。
func extractToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	if !strings.HasPrefix(h, bearerPrefix) {
		return ""
	}
	return strings.TrimSpace(h[len(bearerPrefix):])
}

// RequireAuth 要求请求携带有效会话。验证通过后将用户注入 context。
func RequireAuth(sessions *auth.SessionManager, users UserService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			sess, err := sessions.Get(token)
			if err != nil {
				respond.Error(w, http.StatusUnauthorized, "unauthorized", "会话无效或已过期，请重新登录")
				return
			}
			user, err := users.GetUserByID(r.Context(), sess.UserID)
			if err != nil {
				// 用户可能已被删除：使会话失效并拒绝。
				sessions.Invalidate(token)
				respond.Error(w, http.StatusUnauthorized, "unauthorized", "用户不存在，请重新登录")
				return
			}
			ctx := service.WithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole 要求当前用户为指定角色之一。
func RequireRole(roles ...model.UserRole) func(http.Handler) http.Handler {
	allowed := make(map[model.UserRole]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := service.UserFromContext(r.Context())
			if !ok {
				respond.Error(w, http.StatusUnauthorized, "unauthorized", "未登录")
				return
			}
			if _, ok := allowed[user.Role]; !ok {
				respond.Error(w, http.StatusForbidden, "forbidden", "无权限执行此操作")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin 管理员快捷中间件。
func RequireAdmin() func(http.Handler) http.Handler {
	return RequireRole(model.RoleAdmin)
}

// RequireResident 居民快捷中间件。
func RequireResident() func(http.Handler) http.Handler {
	return RequireRole(model.RoleResident)
}

// UserService 用户查询接口（避免循环依赖 service 包）。
type UserService interface {
	GetUserByID(ctx context.Context, id string) (model.User, error)
}
