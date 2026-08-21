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

// ErrUserRevoked 用户已被撤销（如被管理员删除），不能再建立新会话。
//
// 删除用户后，DeleteUser 通过 RevokeUser 将用户标记为已撤销。此后即便某个
// 登录请求是在删除前发起、删除后才执行到 Create，也会因用户已撤销而被拒绝，
// 从而避免为已删除账号建立有效会话。
var ErrUserRevoked = errors.New("user revoked")

// SessionManager 内存会话管理器。
//
// Token 为 crypto/rand 生成的高熵随机串。会话存在 map 中，带过期时间。
// 提供 lazy 清理：每次 Get 命中过期会话时移除并返回无效。
// 定时清理可由调用方通过 CleanupExpired 触发。
//
// revoked 记录已被撤销的用户 ID（删除用户时写入）。Create 会拒绝为已撤销用户
// 建立会话，确保删除前未完成的登录请求不会再产生有效 token。
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]Session
	revoked  map[string]struct{}
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
		revoked:  make(map[string]struct{}),
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
//
// 若用户已被撤销（RevokeUser），返回 ErrUserRevoked 而不建立会话。这覆盖了
// 删除前已发起、删除后才执行到此处的前序登录请求——账号一旦删除，此前任何
// 尚未完成的登录都不能再建立有效会话。
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
	if _, revoked := sm.revoked[u.ID]; revoked {
		sm.mu.Unlock()
		return "", ErrUserRevoked
	}
	sm.sessions[token] = sess
	sm.mu.Unlock()
	return token, nil
}

// RevokeUser 撤销用户：标记其 ID 为已撤销，并使其所有现存会话立即失效。
//
// 删除用户时调用。已撤销标记使此后任何为该用户创建会话的尝试（包括删除前
// 发起、删除后才到达 Create 的登录请求）返回 ErrUserRevoked；同时清理删除
// 瞬间已存在的会话。返回失效的会话数。
//
// 与 InvalidateByUser 的区别：后者仅清理现存会话，不阻止后续 Create，因此
// 无法阻止"删除后才创建"的会话；RevokeUser 既清理又阻止。
func (sm *SessionManager) RevokeUser(userID string) int {
	if userID == "" {
		return 0
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.revoked[userID] = struct{}{}
	count := 0
	for k, s := range sm.sessions {
		if s.UserID == userID {
			delete(sm.sessions, k)
			count++
		}
	}
	return count
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
