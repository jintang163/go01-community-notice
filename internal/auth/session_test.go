package auth

import (
	"errors"
	"testing"
	"time"

	"go01-community-notice/internal/model"
)

func TestSessionCreateAndGet(t *testing.T) {
	sm := NewSessionManager(time.Hour)
	u := model.User{ID: "u1", Username: "alice", Role: model.RoleResident}
	token, err := sm.Create(u)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	sess, err := sm.Get(token)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sess.UserID != u.ID {
		t.Errorf("user id: %s vs %s", sess.UserID, u.ID)
	}
	if sess.Role != u.Role {
		t.Errorf("role: %s", sess.Role)
	}
	if sess.Expired() {
		t.Error("new session should not be expired")
	}
}

func TestSessionInvalidToken(t *testing.T) {
	sm := NewSessionManager(time.Hour)
	if _, err := sm.Get(""); err == nil {
		t.Error("expected error for empty token")
	}
	if _, err := sm.Get("t_nonexistent"); err == nil {
		t.Error("expected error for unknown token")
	}
}

func TestSessionInvalidate(t *testing.T) {
	sm := NewSessionManager(time.Hour)
	token, _ := sm.Create(model.User{ID: "u1", Username: "a", Role: model.RoleResident})
	sm.Invalidate(token)
	if _, err := sm.Get(token); err == nil {
		t.Error("expected error after logout")
	}
}

func TestSessionExpiry(t *testing.T) {
	sm := NewSessionManager(50 * time.Millisecond)
	token, _ := sm.Create(model.User{ID: "u1", Username: "a", Role: model.RoleResident})
	time.Sleep(80 * time.Millisecond)
	if _, err := sm.Get(token); err == nil {
		t.Error("expected expired session to be invalid")
	}
}

func TestSessionInvalidateByUser(t *testing.T) {
	sm := NewSessionManager(time.Hour)
	u := model.User{ID: "u1", Username: "a", Role: model.RoleResident}
	t1, _ := sm.Create(u)
	t2, _ := sm.Create(u)
	if n := sm.InvalidateByUser(u.ID); n != 2 {
		t.Errorf("expected 2 invalidated, got %d", n)
	}
	if _, err := sm.Get(t1); err == nil {
		t.Error("t1 should be invalid")
	}
	if _, err := sm.Get(t2); err == nil {
		t.Error("t2 should be invalid")
	}
}

func TestSessionCleanupExpired(t *testing.T) {
	sm := NewSessionManager(30 * time.Millisecond)
	sm.Create(model.User{ID: "u1", Username: "a", Role: model.RoleResident})
	sm.Create(model.User{ID: "u2", Username: "b", Role: model.RoleResident})
	time.Sleep(60 * time.Millisecond)
	n := sm.CleanupExpired()
	if n != 2 {
		t.Errorf("expected 2 cleaned, got %d", n)
	}
	if sm.Count() != 0 {
		t.Errorf("expected 0 remaining, got %d", sm.Count())
	}
}

func TestSessionDefaultTTL(t *testing.T) {
	sm := NewSessionManager(0) // 应回退到 24h
	token, _ := sm.Create(model.User{ID: "u1", Username: "a", Role: model.RoleResident})
	sess, _ := sm.Get(token)
	if !sess.ExpiresAt.After(time.Now().Add(23 * time.Hour)) {
		t.Error("expected ~24h TTL")
	}
}

func TestSessionUniqueTokens(t *testing.T) {
	sm := NewSessionManager(time.Hour)
	u := model.User{ID: "u1", Username: "a", Role: model.RoleResident}
	t1, _ := sm.Create(u)
	t2, _ := sm.Create(u)
	if t1 == t2 {
		t.Error("expected unique tokens")
	}
}

// TestSessionRevokeUser 覆盖删除用户的会话语义：
//   - 撤销前已建立的会话立即失效（被清理）；
//   - 撤销后为该用户 Create 必须失败（ErrUserRevoked），不产生 token。
//
// 这覆盖了"登录在删除前发起、删除后才执行到建会话"的竞态：账号一旦删除，
// 前序登录不能再建立有效会话。
func TestSessionRevokeUser(t *testing.T) {
	sm := NewSessionManager(time.Hour)
	u := model.User{ID: "u1", Username: "a", Role: model.RoleResident}

	// 删除前已有会话：撤销时一并清理。
	existing, err := sm.Create(u)
	if err != nil {
		t.Fatalf("create existing: %v", err)
	}
	if n := sm.RevokeUser(u.ID); n != 1 {
		t.Errorf("expected 1 invalidated on revoke, got %d", n)
	}
	if _, err := sm.Get(existing); err == nil {
		t.Error("pre-revoke session should be invalidated")
	}

	// 撤销后再建会话：必须被拒绝，不返回 token。
	token, err := sm.Create(u)
	if !errors.Is(err, ErrUserRevoked) {
		t.Fatalf("expected ErrUserRevoked after revoke, got %v", err)
	}
	if token != "" {
		t.Errorf("expected no token for revoked user, got %q", token)
	}
}

// TestSessionRevokeUserEmpty 空用户 ID 不应崩溃也不计数。
func TestSessionRevokeUserEmpty(t *testing.T) {
	sm := NewSessionManager(time.Hour)
	if n := sm.RevokeUser(""); n != 0 {
		t.Errorf("expected 0 for empty id, got %d", n)
	}
}

// TestSessionRotateCredentials 覆盖修改口令的会话语义：
//   - 改密前已建立的会话立即失效（被清理）；
//   - 改密后为携带旧凭据版本的 Create 必须失败（ErrCredentialsRotated），不产生 token；
//   - 改密后为携带新凭据版本的 Create 仍可正常建会话（改密非永久撤销）。
//
// 这覆盖了"旧口令登录在改密前发起、改密后才执行到建会话"的竞态：口令一旦变更，
// 前序旧口令登录携带改密前的版本，与已前移的当前版本不符而被拒；而改密后凭新
// 口令发起的登录读到新版本，相符可正常建会话。
func TestSessionRotateCredentials(t *testing.T) {
	sm := NewSessionManager(time.Hour)
	u := model.User{ID: "u1", Username: "a", Role: model.RoleResident} // CredentialVersion=0

	// 改密前已有会话：旋转时一并清理。
	existing, err := sm.Create(u)
	if err != nil {
		t.Fatalf("create existing: %v", err)
	}
	if n := sm.RotateCredentials(u.ID); n != 1 {
		t.Errorf("expected 1 invalidated on rotate, got %d", n)
	}
	if _, err := sm.Get(existing); err == nil {
		t.Error("pre-rotate session should be invalidated")
	}

	// 旋转后用旧版本建会话：必须被拒绝，不返回 token。
	token, err := sm.Create(u) // u.CredentialVersion 仍为 0，而 versions[u1]=1
	if !errors.Is(err, ErrCredentialsRotated) {
		t.Fatalf("expected ErrCredentialsRotated after rotate with old version, got %v", err)
	}
	if token != "" {
		t.Errorf("expected no token for rotated credentials, got %q", token)
	}

	// 改密后新口令登录读到新版本（此处模拟为 1），应能正常建会话。
	fresh := u
	fresh.CredentialVersion = 1
	token2, err := sm.Create(fresh)
	if err != nil {
		t.Fatalf("expected new-version login to succeed after rotate, got %v", err)
	}
	if token2 == "" {
		t.Error("expected token for new credentials")
	}
}

// TestSessionRotateCredentialsEmpty 空用户 ID 不应崩溃也不计数。
func TestSessionRotateCredentialsEmpty(t *testing.T) {
	sm := NewSessionManager(time.Hour)
	if n := sm.RotateCredentials(""); n != 0 {
		t.Errorf("expected 0 for empty id, got %d", n)
	}
}

// TestSessionRotateCredentialsMultiple 前移版本可连续多次自增，
// 任意一次前移后旧版本即不可建会话。
func TestSessionRotateCredentialsMultiple(t *testing.T) {
	sm := NewSessionManager(time.Hour)
	u := model.User{ID: "u1", Username: "a", Role: model.RoleResident}
	sm.RotateCredentials(u.ID) // versions[u1]=1
	sm.RotateCredentials(u.ID) // versions[u1]=2

	// 携 v1 的登录也应被拒（只认最新 v2）。
	v1 := u
	v1.CredentialVersion = 1
	if _, err := sm.Create(v1); !errors.Is(err, ErrCredentialsRotated) {
		t.Fatalf("expected ErrCredentialsRotated for v1 after two rotates, got %v", err)
	}
	// 携 v2 的登录可通过。
	v2 := u
	v2.CredentialVersion = 2
	if _, err := sm.Create(v2); err != nil {
		t.Fatalf("expected v2 login to succeed, got %v", err)
	}
}
