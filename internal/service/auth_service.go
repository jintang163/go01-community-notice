package service

import (
	"context"
	"strings"
	"time"

	"go01-community-notice/internal/auth"
	"go01-community-notice/internal/model"
	"go01-community-notice/internal/store"
)

// AuthService 鉴权与用户管理服务。
type AuthService struct {
	store    store.Store
	hasher   *auth.PasswordHasher
	sessions *auth.SessionManager
	clock    Clock
}

// NewAuthService 创建鉴权服务。
func NewAuthService(s store.Store, hasher *auth.PasswordHasher, sessions *auth.SessionManager, clock Clock) *AuthService {
	return &AuthService{store: s, hasher: hasher, sessions: sessions, clock: clock}
}

// Login 校验凭据并创建会话。
func (a *AuthService) Login(ctx context.Context, username, password string) (string, model.User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return "", model.User{}, model.ErrInvalidCredentials
	}
	u, err := a.store.GetUserByUsername(ctx, username)
	if err != nil {
		if model.IsNotFound(err) {
			return "", model.User{}, model.ErrInvalidCredentials
		}
		return "", model.User{}, err
	}
	if !a.hasher.Verify(password, u.PasswordSalt, u.PasswordHash, u.Iterations) {
		return "", model.User{}, model.ErrInvalidCredentials
	}
	current, err := a.store.GetUserByID(ctx, u.ID)
	if err != nil {
		if model.IsNotFound(err) {
			return "", model.User{}, model.ErrInvalidCredentials
		}
		return "", model.User{}, err
	}
	if current.Username != u.Username || current.PasswordSalt != u.PasswordSalt || current.PasswordHash != u.PasswordHash || current.Iterations != u.Iterations {
		return "", model.User{}, model.ErrInvalidCredentials
	}
	token, err := a.sessions.Create(current)
	if err != nil {
		return "", model.User{}, err
	}
	return token, current, nil
}

// Logout 使会话失效。
func (a *AuthService) Logout(token string) {
	a.sessions.Invalidate(token)
}

// SessionByToken 根据 token 取会话。
func (a *AuthService) SessionByToken(token string) (auth.Session, error) {
	return a.sessions.Get(token)
}

// CreateUser 创建用户（由管理员调用）。
func (a *AuthService) CreateUser(ctx context.Context, in model.UserInput, creator model.User) (model.User, error) {
	if !creator.IsAdmin() {
		return model.User{}, model.ErrForbidden
	}
	if err := in.Validate(); err != nil {
		return model.User{}, err
	}
	salt, hash, iterations, err := a.hasher.Hash(in.Password)
	if err != nil {
		return model.User{}, err
	}
	u, err := a.store.CreateUser(ctx, model.User{
		Username:     in.Username,
		PasswordHash: hash,
		PasswordSalt: salt,
		Iterations:   iterations,
		Role:         in.Role,
		DisplayName:  in.DisplayName,
	})
	if err != nil {
		return model.User{}, err
	}
	return u, nil
}

// DeleteUser 删除用户，并使其会话失效、清理阅读记录。
func (a *AuthService) DeleteUser(ctx context.Context, id string, caller model.User) error {
	if !caller.IsAdmin() {
		return model.ErrForbidden
	}
	if id == caller.ID {
		return model.ErrConflict
	}
	target, err := a.store.GetUserByID(ctx, id)
	if err != nil {
		return err
	}
	// 删除用户（store 内级联清理该用户阅读记录）。
	if err := a.store.DeleteUser(ctx, id); err != nil {
		return err
	}
	// 使其所有会话失效。
	a.sessions.InvalidateByUser(target.ID)
	return nil
}

// ListUsers 列出用户（仅管理员）。
func (a *AuthService) ListUsers(ctx context.Context, caller model.User) ([]model.User, error) {
	if !caller.IsAdmin() {
		return nil, model.ErrForbidden
	}
	return a.store.ListUsers(ctx, "")
}

// ChangePassword 修改自己的口令。
func (a *AuthService) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	u, err := a.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !a.hasher.Verify(oldPassword, u.PasswordSalt, u.PasswordHash, u.Iterations) {
		return model.ErrInvalidCredentials
	}
	if len(newPassword) < 6 || len(newPassword) > 64 {
		return model.ErrInvalidPassword
	}
	salt, hash, iterations, err := a.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	u.PasswordSalt = salt
	u.PasswordHash = hash
	u.Iterations = iterations
	if _, err := a.store.UpdateUser(ctx, u); err != nil {
		return err
	}
	a.sessions.InvalidateByUser(userID)
	return nil
}

// now 当前时间（注入时钟）。
func (a *AuthService) now() time.Time {
	if a.clock == nil {
		return time.Now()
	}
	return a.clock.Now()
}
