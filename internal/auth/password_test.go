package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashAndVerify(t *testing.T) {
	h := NewPasswordHasher()
	password := "s3cret-pw"
	salt, hash, iters, err := h.Hash(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if salt == "" || hash == "" || iters <= 0 {
		t.Fatalf("bad hash output: salt=%q hash=%q iters=%d", salt, hash, iters)
	}
	if !h.Verify(password, salt, hash, iters) {
		t.Error("expected verify to succeed for correct password")
	}
	if h.Verify("wrong", salt, hash, iters) {
		t.Error("expected verify to fail for wrong password")
	}
}

func TestPasswordHashUniqueSalts(t *testing.T) {
	h := NewPasswordHasher()
	s1, _, _, _ := h.Hash("same")
	s2, _, _, _ := h.Hash("same")
	if s1 == s2 {
		t.Error("expected different salts for same password")
	}
}

func TestPasswordVerifyBadEncoding(t *testing.T) {
	h := NewPasswordHasher()
	if h.Verify("pw", "not-base64!!!", "also-bad", 10) {
		t.Error("expected false for invalid base64")
	}
}

func TestPasswordHashEmpty(t *testing.T) {
	h := NewPasswordHasher()
	if _, _, _, err := h.Hash(""); err == nil {
		t.Error("expected error for empty password")
	}
}

func TestPasswordHashLength(t *testing.T) {
	h := NewPasswordHasher()
	salt, hash, _, _ := h.Hash("abcdef")
	// SHA-256 输出 32 字节 -> base64 编码长度应 > 32。
	if len(hash) < 32 {
		t.Errorf("hash too short: %d", len(hash))
	}
	if len(salt) < 16 {
		t.Errorf("salt too short: %d", len(salt))
	}
}

func TestMustHashPanics(t *testing.T) {
	h := NewPasswordHasher()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty password")
		}
	}()
	_, _, _ = h.MustHash("")
}

func TestPasswordVerifyIterationsFallback(t *testing.T) {
	h := NewPasswordHasher()
	salt, hash, _, _ := h.Hash("pw123")
	// iterations=0 应回退到默认值仍能验证。
	if !h.Verify("pw123", salt, hash, 0) {
		t.Error("expected verify with iters=0 fallback to succeed")
	}
}

func TestPasswordHashDoesNotContainPassword(t *testing.T) {
	h := NewPasswordHasher()
	pw := "supersecret-long-password-value-xyz"
	salt, hash, _, _ := h.Hash(pw)
	if strings.Contains(salt, pw) || strings.Contains(hash, pw) {
		t.Error("password must not appear in salt or hash")
	}
}
