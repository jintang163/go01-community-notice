package auth

import (
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
