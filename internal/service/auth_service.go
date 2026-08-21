package service

import (
	"context"
	"errors"
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
	token, err := a.sessions.Create(u)
	if err != nil {
		// Create 在两种竞态下拒绝建会话，均对外映射为凭据无效（401），
		// 不泄露账号/口令的变更时序：
		//   - ErrUserRevoked：登录在删除前发起、删除后才执行到建会话，
		//     账号已不存在，不能再建立会话。
		//   - ErrCredentialsRotated：旧口令登录在改密前发起、改密后才执行
		//     到建会话，其携带的凭据版本已与当前版本不符，旧口令不能再建会话。
		if errors.Is(err, auth.ErrUserRevoked) || errors.Is(err, auth.ErrCredentialsRotated) {
			return "", model.User{}, model.ErrInvalidCredentials
		}
		return "", model.User{}, err
	}
	return token, u, nil
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
	// 撤销用户：使其现存会话失效，并阻止删除后才创建的会话。
	// 用 RevokeUser 而非 InvalidateByUser——后者只清理现存会话，无法阻止
	// 删除前发起、删除后才执行到 Create 的登录请求建立新会话。
	a.sessions.RevokeUser(target.ID)
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
	// 凭据版本的"落盘自增"与"会话颁发版本的前移"必须同源且各自从当前值自增，
	// 否则并发改密会让二者各从陈旧基线自增、最终不一致——落盘的新口令读到的
	// 凭据版本与会话颁发版本不符，被 Create 以 ErrCredentialsRotated 拒绝，
	// 账号被永久锁在登录流程外。
	//
	// 这里用 SetUserPassword 而非 UpdateUser：UpdateUser 整条覆盖，会把调用方在
	// 并发更新前读到的（可能陈旧的）CredentialVersion 自增后写回，两位居民并发
	// 改密时各自从同一陈旧基线自增、都写入同一版本（"更新丢失"）。SetUserPassword
	// 在写锁内重新读取当前用户再自增，从当前存储值推进，与会话颁发版本在
	// RotateCredentials 内从当前 map 值自增保持同源。两者各自在自身锁下从当前值
	// 自增，故并发改密时均单调推进、始终一致。
	if _, err := a.store.SetUserPassword(ctx, userID, salt, hash, iterations); err != nil {
		return err
	}
	// 旋转凭据：前移会话颁发版本并清理现存会话。用 RotateCredentials 而非
	// InvalidateByUser——后者只清理改密瞬间已存在的会话，无法阻止改密前发起、
	// 改密后才执行到 Create 的旧口令登录建立新会话（其携带改密前的凭据版本，
	// 与此处前移后的当前版本不符才被 Create 拒绝）。
	a.sessions.RotateCredentials(userID)
	return nil
}

// now 当前时间（注入时钟）。
func (a *AuthService) now() time.Time {
	if a.clock == nil {
		return time.Now()
	}
	return a.clock.Now()
}
