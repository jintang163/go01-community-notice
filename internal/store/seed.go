package store

import (
	"context"
	"fmt"
	"log"

	"go01-community-notice/internal/auth"
	"go01-community-notice/internal/model"
)

// SeedAdmin 在 store 为空时创建种子管理员账号。
// 已存在管理员则跳过。
func SeedAdmin(ctx context.Context, s *MemoryStore, hasher *auth.PasswordHasher, username, password string) error {
	if username == "" {
		return fmt.Errorf("seed: empty admin username")
	}
	if password == "" {
		return fmt.Errorf("seed: empty admin password")
	}

	// 检查是否已有任意管理员。
	users, err := s.ListUsers(ctx, model.RoleAdmin)
	if err != nil {
		return fmt.Errorf("seed: list admins: %w", err)
	}
	if len(users) > 0 {
		return nil // 已有管理员，跳过
	}

	// 用户名若被普通居民占用，仍允许创建管理员（用户名唯一约束在 CreateUser 中保证）；
	// 种子场景下应不存在冲突。
	salt, hash, iterations, err := hasher.Hash(password)
	if err != nil {
		return fmt.Errorf("seed: hash password: %w", err)
	}

	u, err := s.CreateUser(ctx, model.User{
		Username:     username,
		PasswordHash: hash,
		PasswordSalt: salt,
		Iterations:   iterations,
		Role:         model.RoleAdmin,
		DisplayName:  "系统管理员",
	})
	if err != nil {
		return fmt.Errorf("seed: create admin: %w", err)
	}
	log.Printf("seed: created admin user %q (id=%s)", u.Username, u.ID)
	return nil
}
