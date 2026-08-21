package service

import (
	"context"
	"sync"
	"testing"

	"go01-community-notice/internal/model"
	"go01-community-notice/internal/store"
)

type pausedLoginStore struct {
	store.Store
	username string
	loaded   chan struct{}
	resume   chan struct{}
	once     sync.Once
}

type concurrentPasswordStore struct {
	store.Store
	userID string
	loaded chan struct{}
	resume chan struct{}
	mu     sync.Mutex
	count  int
}

func (s *concurrentPasswordStore) GetUserByID(ctx context.Context, id string) (model.User, error) {
	u, err := s.Store.GetUserByID(ctx, id)
	if err != nil || id != s.userID {
		return u, err
	}
	s.mu.Lock()
	s.count++
	if s.count == 2 {
		close(s.loaded)
	}
	s.mu.Unlock()
	select {
	case <-s.resume:
		return u, nil
	case <-ctx.Done():
		return model.User{}, ctx.Err()
	}
}

func (s *pausedLoginStore) GetUserByUsername(ctx context.Context, username string) (model.User, error) {
	u, err := s.Store.GetUserByUsername(ctx, username)
	if err != nil || username != s.username {
		return u, err
	}
	s.once.Do(func() { close(s.loaded) })
	select {
	case <-s.resume:
		return u, nil
	case <-ctx.Done():
		return model.User{}, ctx.Err()
	}
}

func TestDeletedUserCannotFinishPendingLogin(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	gate := &pausedLoginStore{
		Store:    env.store,
		username: env.resident.Username,
		loaded:   make(chan struct{}),
		resume:   make(chan struct{}),
	}
	authService := NewAuthService(gate, env.hasher, env.sessions, env.clock)

	type loginResult struct {
		token string
		err   error
	}
	result := make(chan loginResult, 1)
	go func() {
		token, _, err := authService.Login(ctx, env.resident.Username, "res123")
		result <- loginResult{token: token, err: err}
	}()

	<-gate.loaded
	if err := authService.DeleteUser(ctx, env.resident.ID, env.admin); err != nil {
		close(gate.resume)
		t.Fatalf("delete resident while login is pending: %v", err)
	}
	close(gate.resume)

	login := <-result
	if login.err == nil {
		if _, err := authService.SessionByToken(login.token); err == nil {
			t.Fatal("a login that started before deletion created a valid session after the user was deleted")
		}
	}
}

func TestPasswordChangeRejectsPendingLoginWithOldPassword(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	gate := &pausedLoginStore{
		Store:    env.store,
		username: env.resident.Username,
		loaded:   make(chan struct{}),
		resume:   make(chan struct{}),
	}
	authService := NewAuthService(gate, env.hasher, env.sessions, env.clock)

	type loginResult struct {
		token string
		err   error
	}
	result := make(chan loginResult, 1)
	go func() {
		token, _, err := authService.Login(ctx, env.resident.Username, "res123")
		result <- loginResult{token: token, err: err}
	}()

	<-gate.loaded
	if err := authService.ChangePassword(ctx, env.resident.ID, "res123", "newpass123"); err != nil {
		close(gate.resume)
		t.Fatalf("change password while old-password login is pending: %v", err)
	}
	close(gate.resume)

	login := <-result
	if login.err == nil {
		if _, err := authService.SessionByToken(login.token); err == nil {
			t.Fatal("a login using the old password created a valid session after the password change completed")
		}
	}
}

func TestConcurrentPasswordChangesLeaveLatestPasswordUsable(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	gate := &concurrentPasswordStore{
		Store:  env.store,
		userID: env.resident.ID,
		loaded: make(chan struct{}),
		resume: make(chan struct{}),
	}
	authService := NewAuthService(gate, env.hasher, env.sessions, env.clock)

	passwords := []string{"newpass-one", "newpass-two"}
	results := make(chan error, len(passwords))
	for _, password := range passwords {
		password := password
		go func() {
			results <- authService.ChangePassword(ctx, env.resident.ID, "res123", password)
		}()
	}

	<-gate.loaded
	close(gate.resume)
	for range passwords {
		if err := <-results; err != nil {
			t.Fatalf("concurrent password change: %v", err)
		}
	}

	stored, err := env.store.GetUserByID(ctx, env.resident.ID)
	if err != nil {
		t.Fatalf("get updated resident: %v", err)
	}
	winningPassword := ""
	for _, password := range passwords {
		if env.hasher.Verify(password, stored.PasswordSalt, stored.PasswordHash, stored.Iterations) {
			winningPassword = password
			break
		}
	}
	if winningPassword == "" {
		t.Fatal("neither successful password change matches the persisted credentials")
	}
	if _, _, err := authService.Login(ctx, env.resident.Username, winningPassword); err != nil {
		t.Fatalf("the latest successfully persisted password must remain usable, got %v", err)
	}
}

func TestAuthLoginSuccess(t *testing.T) {
	env := newTestEnv(t)
	token, u, err := env.svc.Auth.Login(context.Background(), "admin", "admin123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token == "" {
		t.Error("expected token")
	}
	if u.ID != env.admin.ID {
		t.Errorf("user id mismatch: %s vs %s", u.ID, env.admin.ID)
	}
}

func TestAuthLoginWrongPassword(t *testing.T) {
	env := newTestEnv(t)
	_, _, err := env.svc.Auth.Login(context.Background(), "admin", "wrong")
	if !model.IsInvalidCredentials(err) {
		t.Errorf("expected invalid credentials, got %v", err)
	}
}

func TestAuthLoginUnknownUser(t *testing.T) {
	env := newTestEnv(t)
	_, _, err := env.svc.Auth.Login(context.Background(), "nobody", "x")
	if !model.IsInvalidCredentials(err) {
		t.Errorf("expected invalid credentials, got %v", err)
	}
}

func TestAuthLoginEmpty(t *testing.T) {
	env := newTestEnv(t)
	_, _, err := env.svc.Auth.Login(context.Background(), "", "")
	if !model.IsInvalidCredentials(err) {
		t.Errorf("expected invalid credentials, got %v", err)
	}
}

func TestAuthCreateUserByAdmin(t *testing.T) {
	env := newTestEnv(t)
	u, err := env.svc.Auth.CreateUser(context.Background(), model.UserInput{
		Username: "newresident", Password: "pw1234", Role: model.RoleResident, DisplayName: "新居民",
	}, env.admin)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if u.ID == "" {
		t.Error("expected id")
	}
	// 新用户可登录。
	if _, _, err := env.svc.Auth.Login(context.Background(), "newresident", "pw1234"); err != nil {
		t.Fatalf("login new user: %v", err)
	}
}

func TestAuthCreateUserForbidden(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.Auth.CreateUser(context.Background(), model.UserInput{
		Username: "x", Password: "pw1234", Role: model.RoleResident,
	}, env.resident)
	if !model.IsForbidden(err) {
		t.Errorf("expected forbidden, got %v", err)
	}
}

func TestAuthCreateUserValidation(t *testing.T) {
	env := newTestEnv(t)
	cases := []model.UserInput{
		{Username: "ab", Password: "pw1234", Role: model.RoleResident},    // 用户名过短
		{Username: "goodname", Password: "123", Role: model.RoleResident}, // 口令过短
		{Username: "goodname", Password: "pw1234", Role: "unknown"},       // 非法角色
	}
	for i, in := range cases {
		if _, err := env.svc.Auth.CreateUser(context.Background(), in, env.admin); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestAuthCreateUserDuplicate(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.Auth.CreateUser(context.Background(), model.UserInput{
		Username: "admin", Password: "pw1234", Role: model.RoleAdmin,
	}, env.admin)
	if !model.IsAlreadyExists(err) {
		t.Errorf("expected already exists, got %v", err)
	}
}

func TestAuthDeleteUser(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	u, _ := env.svc.Auth.CreateUser(ctx, model.UserInput{Username: "todelete", Password: "pw1234", Role: model.RoleResident}, env.admin)
	if err := env.svc.Auth.DeleteUser(ctx, u.ID, env.admin); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := env.store.GetUserByID(ctx, u.ID); !model.IsNotFound(err) {
		t.Errorf("expected not found after delete, got %v", err)
	}
}

func TestAuthDeleteUserCannotDeleteSelf(t *testing.T) {
	env := newTestEnv(t)
	if err := env.svc.Auth.DeleteUser(context.Background(), env.admin.ID, env.admin); !model.IsConflict(err) {
		t.Errorf("expected conflict deleting self, got %v", err)
	}
}

func TestAuthChangePassword(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	u, _ := env.svc.Auth.CreateUser(ctx, model.UserInput{Username: "changer", Password: "pw1234", Role: model.RoleResident}, env.admin)
	if err := env.svc.Auth.ChangePassword(ctx, u.ID, "pw1234", "newpass123"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	// 旧口令不应再能登录。
	if _, _, err := env.svc.Auth.Login(ctx, "changer", "pw1234"); err == nil {
		t.Error("old password should no longer work")
	}
	// 新口令可登录。
	if _, _, err := env.svc.Auth.Login(ctx, "changer", "newpass123"); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
}

func TestAuthChangePasswordWrongOld(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if err := env.svc.Auth.ChangePassword(ctx, env.admin.ID, "wrong", "newpass123"); !model.IsInvalidCredentials(err) {
		t.Errorf("expected invalid credentials, got %v", err)
	}
}

func TestAuthListUsers(t *testing.T) {
	env := newTestEnv(t)
	users, err := env.svc.Auth.ListUsers(context.Background(), env.admin)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) < 2 {
		t.Errorf("expected >=2 users, got %d", len(users))
	}
	// 居民不可列用户。
	if _, err := env.svc.Auth.ListUsers(context.Background(), env.resident); !model.IsForbidden(err) {
		t.Errorf("expected forbidden, got %v", err)
	}
}
