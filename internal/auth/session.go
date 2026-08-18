package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"go01-community-notice/internal/model"
)

// Session 一次登录会话。
type Session struct {
	Token     string
	UserID    string
	Username  string
	Role      model.UserRole
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Expired 会话是否过期。
func (s Session) Expired() bool {
	return time.Now().After(s.ExpiresAt)
}

// ErrInvalidToken Token 无效或过期。
var ErrInvalidToken = errors.New("invalid or expired token")

// SessionManager 内存会话管理器。
//
// Token 为 crypto/rand 生成的高熵随机串。会话存在 map 中，带过期时间。
// 提供 lazy 清理：每次 Get 命中过期会话时移除并返回无效。
// 定时清理可由调用方通过 CleanupExpired 触发。
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]Session
	now      func() time.Time
	tokenTTL time.Duration
}

// NewSessionManager 创建会话管理器。tokenTTL 为会话有效期。
func NewSessionManager(tokenTTL time.Duration) *SessionManager {
	if tokenTTL <= 0 {
		tokenTTL = 24 * time.Hour
	}
	return &SessionManager{
		sessions: make(map[string]Session),
		now:      time.Now,
		tokenTTL: tokenTTL,
	}
}

// generateToken 生成随机 Token。
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return model.TokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// Create 为给定用户创建会话，返回 Token。
func (sm *SessionManager) Create(u model.User) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	now := sm.now()
	sess := Session{
		Token:     token,
		UserID:    u.ID,
		Username:  u.Username,
		Role:      u.Role,
		CreatedAt: now,
		ExpiresAt: now.Add(sm.tokenTTL),
	}
	sm.mu.Lock()
	sm.sessions[token] = sess
	sm.mu.Unlock()
	return token, nil
}

// Get 查询会话，过期或不存在返回 ErrInvalidToken。
func (sm *SessionManager) Get(token string) (Session, error) {
	if token == "" {
		return Session{}, ErrInvalidToken
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sess, ok := sm.sessions[token]
	if !ok {
		return Session{}, ErrInvalidToken
	}
	if sess.Expired() {
		delete(sm.sessions, token)
		return Session{}, ErrInvalidToken
	}
	return sess, nil
}

// Invalidate 使会话失效（登出）。
func (sm *SessionManager) Invalidate(token string) {
	if token == "" {
		return
	}
	sm.mu.Lock()
	delete(sm.sessions, token)
	sm.mu.Unlock()
}

// CleanupExpired 清理所有过期会话，返回清理数量。
func (sm *SessionManager) CleanupExpired() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	count := 0
	for k, s := range sm.sessions {
		if s.Expired() {
			delete(sm.sessions, k)
			count++
		}
	}
	return count
}

// Count 当前活跃会话数（含可能过期但未清理的）。
func (sm *SessionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// InvalidateByUser 使用户的所有会话失效（修改口令 / 删除用户时调用）。
func (sm *SessionManager) InvalidateByUser(userID string) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	count := 0
	for k, s := range sm.sessions {
		if s.UserID == userID {
			delete(sm.sessions, k)
			count++
		}
	}
	return count
}
