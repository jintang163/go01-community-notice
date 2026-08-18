package store

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// defaultIDGenerator 基于 crypto/rand 的默认 ID 生成器。
// 返回 "<prefix><base32ish>" 形式，足够唯一用于演示场景。
func defaultIDGenerator(prefix string) string {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		// 极少数情况下 rand 失败，退化为时间戳保证不重复（演示用）。
		return fmt.Sprintf("%s%d%d", prefix, time.Now().UnixNano(), len(prefix))
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

// sanitizeDisplayName 过滤显示名：去首尾空白、控制字符替换为空格。
// 与 model 包中的同名函数行为一致，此处为 store 包内独立实现以避免循环依赖。
func sanitizeDisplayName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
