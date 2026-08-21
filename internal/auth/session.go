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

// ErrCredentialsRotated 口令已变更：发起登录时使用的凭据版本与建会话时
// 的当前版本不符，拒绝建立会话。
//
// 修改口令后，ChangePassword 通过 RotateCredentials 前移用户的会话颁发版本
// 并清理现存会话。此后即便某个登录请求是在改密前发起（携旧凭据版本）、改密后
// 才执行到 Create，也会因版本不符而被拒绝，从而避免旧口令登录在口令变更完成
// 后再建立有效会话。与 ErrUserRevoked 的区别：改密后用户仍可凭新口令登录，
// 版本只是前移而非永久撤销，故 ErrCredentialsRotated 对外映射为凭据无效
// （401），与旧口令失效的语义一致。
var ErrCredentialsRotated = errors.New("credentials rotated")

// SessionManager 内存会话管理器。
//
// Token 为 crypto/rand 生成的高熵随机串。会话存在 map 中，带过期时间。
// 提供 lazy 清理：每次 Get 命中过期会话时移除并返回无效。
// 定时清理可由调用方通过 CleanupExpired 触发。
//
// revoked 记录已被撤销的用户 ID（删除用户时写入）。Create 会拒绝为已撤销用户
// 建立会话，确保删除前未完成的登录请求不会再产生有效 token。
//
// versions 记录每个用户的会话颁发版本（修改口令时由 RotateCredentials
// 前移）。Create 在持锁阶段把请求所携带的 CredentialVersion 与
// versions[userID] 比较：相符才建会话，不符则返回 ErrCredentialsRotated。
// 这拦截了"改密前发起、改密后才执行到 Create"的旧口令登录——它在改密前读取
// 用户快照、携旧版本，改密后 Create 时版本已前移、不符而被拒；而改密后凭新
// 口令发起的登录读到新版本，相符可正常建会话。
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]Session
	revoked  map[string]struct{}
	versions map[string]uint64
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
		versions: make(map[string]uint64),
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
//
// 若请求携带的凭据版本 u.CredentialVersion 与当前会话颁发版本不符（见
// RotateCredentials），返回 ErrCredentialsRotated 而不建立会话。这覆盖了
// 改密前已发起、改密后才执行到此处的前序旧口令登录——它在改密前读取用户快照、
// 携旧版本，改密后建会话时版本已前移、不符而被拒；而改密后凭新口令发起的登录
// 读到的是新版本，相符可正常建会话。
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
	if cur, ok := sm.versions[u.ID]; ok && cur != u.CredentialVersion {
		sm.mu.Unlock()
		return "", ErrCredentialsRotated
	}
	sm.sessions[token] = sess
	sm.mu.Unlock()
	return token, nil
}

// RotateCredentials 前移用户的会话颁发版本，并使其所有现存会话立即失效。
//
// 修改口令时调用。前移后的版本使此后任何携带旧版本的 Create（即改密前发起、
// 改密后才到达 Create 的旧口令登录）返回 ErrCredentialsRotated；同时清理改密
// 瞬间已存在的会话。返回失效的会话数。
//
// 与 RevokeUser 的区别：后者永久撤销（用户已删除、不能再登录）；RotateCredentials
// 仅前移版本——改密后用户仍可凭新口令登录（其登录读到的是更新后用户实体的
// 新 CredentialVersion，与版本表相符），故用于口令变更场景。
func (sm *SessionManager) RotateCredentials(userID string) int {
	if userID == "" {
		return 0
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.versions[userID]++
	count := 0
	for k, s := range sm.sessions {
		if s.UserID == userID {
			delete(sm.sessions, k)
			count++
		}
	}
	return count
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
