// Package service 实现社区通知公告系统的业务逻辑。
//
// 三个核心服务：
//   - AuthService：登录、登出、当前用户、用户管理。
//   - NoticeService：通知的创建、更新、发布、下架、删除、查询（含可见性控制）。
//   - ReadService：已读/未读判定与标记（核心业务规则"更新即未读"）。
//   - StatsService：全局与单通知阅读统计。
//
// 服务层只依赖 store.Store 接口与 auth.*，不依赖 HTTP，便于复用与测试。
package service

import (
	"context"
	"time"

	"go01-community-notice/internal/auth"
	"go01-community-notice/internal/model"
	"go01-community-notice/internal/store"
)

// Clock 时间提供者，便于测试注入可控时钟。
type Clock interface {
	Now() time.Time
}

// systemClock 総 systemClock 包级默认时钟。
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// DefaultClock 默认时钟。
var DefaultClock Clock = systemClock{}

// Services 聚合所有服务，便于 main 一次性装配。
type Services struct {
	Auth   *AuthService
	Notice *NoticeService
	Read   *ReadService
	Stats  *StatsService
}

// NewServices 创建服务聚合。
func NewServices(s store.Store, hasher *auth.PasswordHasher, sessions *auth.SessionManager, clock Clock) *Services {
	if clock == nil {
		clock = DefaultClock
	}
	authSvc := NewAuthService(s, hasher, sessions, clock)
	noticeSvc := NewNoticeService(s, clock)
	readSvc := NewReadService(s, clock)
	statsSvc := NewStatsService(s, clock)
	return &Services{
		Auth:   authSvc,
		Notice: noticeSvc,
		Read:   readSvc,
		Stats:  statsSvc,
	}
}

// 上下文键类型，避免冲突。
type ctxKey string

const (
	// ctxUserKey 当前登录用户。
	ctxUserKey ctxKey = "user"
)

// WithUser 把当前用户放入 context。
func WithUser(ctx context.Context, u model.User) context.Context {
	return context.WithValue(ctx, ctxUserKey, u)
}

// UserFromContext 从 context 取出当前用户。第二个返回值表示是否存在。
func UserFromContext(ctx context.Context) (model.User, bool) {
	u, ok := ctx.Value(ctxUserKey).(model.User)
	return u, ok
}

// MustUserFromContext 从 context 取出当前用户，不存在则 panic（仅用于已过鉴权的处理器）。
func MustUserFromContext(ctx context.Context) model.User {
	u, ok := UserFromContext(ctx)
	if !ok {
		panic("service: user not found in context")
	}
	return u
}
