// Package auth 提供口令哈希与会话管理。
//
// 为保证零第三方依赖与 Go 1.22 兼容，口令哈希采用盐值 + 多轮迭代 SHA-256
// （类 PBKDF1 演示实现）。生产环境应替换为 bcrypt/argon2。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// 默认参数（可调）。
const (
	defaultIterations = 10000
	defaultSaltLen    = 16
)

// PasswordHasher 口令哈希器。
type PasswordHasher struct {
	iterations int
	saltLen    int
	now        func() time.Time
}

// NewPasswordHasher 创建哈希器。
func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{
		iterations: defaultIterations,
		saltLen:    defaultSaltLen,
		now:        time.Now,
	}
}

// Hash 计算口令的盐值 + 迭代哈希。
// 返回 (salt base64, hash base64, iterations, error)。
func (h *PasswordHasher) Hash(password string) (salt, hash string, iterations int, err error) {
	if password == "" {
		return "", "", 0, errors.New("password must not be empty")
	}
	saltBytes := make([]byte, h.saltLen)
	if _, err = rand.Read(saltBytes); err != nil {
		return "", "", 0, fmt.Errorf("generate salt: %w", err)
	}
	dk := deriveKey([]byte(password), saltBytes, h.iterations)
	return base64.StdEncoding.EncodeToString(saltBytes),
		base64.StdEncoding.EncodeToString(dk),
		h.iterations,
		nil
}

// Verify 校验口令是否匹配给定的盐与哈希。
func (h *PasswordHasher) Verify(password, saltB64, hashB64 string, iterations int) bool {
	saltBytes, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return false
	}
	expected, err := base64.StdEncoding.DecodeString(hashB64)
	if err != nil {
		return false
	}
	if iterations <= 0 {
		iterations = defaultIterations
	}
	dk := deriveKey([]byte(password), saltBytes, iterations)
	return hmac.Equal(dk, expected)
}

// deriveKey 迭代 SHA-256 派生密钥（PBKDF1 风格）。
//   dk0 = sha256(salt || password)
//   dki = sha256(dk(i-1) || password)
func deriveKey(password, salt []byte, iterations int) []byte {
	h := sha256.New()
	h.Write(salt)
	h.Write(password)
	dk := h.Sum(nil)
	for i := 1; i < iterations; i++ {
		h.Reset()
		h.Write(dk)
		h.Write(password)
		dk = h.Sum(nil)
	}
	return dk
}

// MustHash 便捷方法：哈希失败则 panic（仅用于种子等不可恢复场景）。
func (h *PasswordHasher) MustHash(password string) (salt, hash string, iterations int) {
	s, hs, it, err := h.Hash(password)
	if err != nil {
		panic(err)
	}
	return s, hs, it
}
