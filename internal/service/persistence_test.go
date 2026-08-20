package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go01-community-notice/internal/auth"
	"go01-community-notice/internal/model"
	"go01-community-notice/internal/store"
)

// mustCreatePersistedAdmin 在可写的文件存储上创建一个管理员并落盘，供持久化失败测试复用。
func mustCreatePersistedAdmin(t *testing.T, ctx context.Context, fileStore *store.FileStore, hasher *auth.PasswordHasher) model.User {
	t.Helper()
	salt, hash, iterations, err := hasher.Hash("admin123")
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	admin, err := fileStore.Store().CreateUser(ctx, model.User{
		Username:     "admin",
		PasswordHash:  hash,
		PasswordSalt: salt,
		Iterations:   iterations,
		Role:         model.RoleAdmin,
		DisplayName:  "管理员",
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	return admin
}

// breakDataPath 把数据文件路径占用为目录，使 FileStore 的原子写（rename）失败，
// 模拟"系统运行期间数据路径临时不可写"。
func breakDataPath(t *testing.T, dataPath string) {
	t.Helper()
	if err := os.Remove(dataPath); err != nil {
		t.Fatalf("remove persisted snapshot: %v", err)
	}
	if err := os.Mkdir(dataPath, 0o755); err != nil {
		t.Fatalf("replace snapshot with directory: %v", err)
	}
}

func TestCreateUserResultMatchesPersistentState(t *testing.T) {
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "store.json")
	fileStore, err := store.NewFileStore(dataPath)
	if err != nil {
		t.Fatalf("open file store: %v", err)
	}

	hasher := auth.NewPasswordHasher()
	sessions := auth.NewSessionManager(time.Hour)
	services := NewServices(fileStore.Store(), hasher, sessions, nil)
	admin := mustCreatePersistedAdmin(t, ctx, fileStore, hasher)

	breakDataPath(t, dataPath)

	created, createErr := services.Auth.CreateUser(ctx, model.UserInput{
		Username:    "newresident",
		Password:    "resident123",
		Role:        model.RoleResident,
		DisplayName: "新居民",
	}, admin)
	flushErr := fileStore.Flush()
	if createErr == nil {
		t.Fatalf("create user reported success for id %q although durable storage failed: %v", created.ID, flushErr)
	}
}

// TestCreateUserPersistFailureLeavesNoPhantom 验证持久化失败时 CreateUser 不仅
// 返回错误，还回滚了内存中的创建：失败的用户不在列表、不能按用户名查到，
// 且同一用户名在恢复数据路径后可重新创建成功——即不留下"内存中有、磁盘上无"
// 的幻影账号（登录/列表看似存在，重启后丢失，造成账号已创建的假象）。
func TestCreateUserPersistFailureLeavesNoPhantom(t *testing.T) {
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "store.json")
	fileStore, err := store.NewFileStore(dataPath)
	if err != nil {
		t.Fatalf("open file store: %v", err)
	}

	hasher := auth.NewPasswordHasher()
	sessions := auth.NewSessionManager(time.Hour)
	services := NewServices(fileStore.Store(), hasher, sessions, nil)
	admin := mustCreatePersistedAdmin(t, ctx, fileStore, hasher)

	breakDataPath(t, dataPath)

	if _, err := services.Auth.CreateUser(ctx, model.UserInput{
		Username:    "newresident",
		Password:    "resident123",
		Role:        model.RoleResident,
		DisplayName: "新居民",
	}, admin); err == nil {
		t.Fatal("expected create to fail when persistence is unavailable")
	}

	// 回滚验证一：失败用户不应残留在内存中（按用户名查不到）。
	if _, err := fileStore.Store().GetUserByUsername(ctx, "newresident"); !model.IsNotFound(err) {
		t.Errorf("expected rolled-back user to be absent from memory, got %v", err)
	}
	// 回滚验证二：失败用户不应出现在用户列表中。
	users, _ := fileStore.Store().ListUsers(ctx, "")
	for _, u := range users {
		if u.Username == "newresident" {
			t.Error("rolled-back user still appears in user list")
		}
	}
	// 回滚验证三：该用户不应能凭据登录（账号未真正创建）。
	if _, _, err := services.Auth.Login(ctx, "newresident", "resident123"); !model.IsInvalidCredentials(err) {
		t.Errorf("expected login to fail for rolled-back user, got %v", err)
	}

	// 恢复数据路径后，同一用户名应可重新创建成功（证明无幻影占用用户名）。
	if err := os.RemoveAll(dataPath); err != nil {
		t.Fatalf("clear broken path: %v", err)
	}
	if _, err := services.Auth.CreateUser(ctx, model.UserInput{
		Username:    "newresident",
		Password:    "resident123",
		Role:        model.RoleResident,
		DisplayName: "新居民",
	}, admin); err != nil {
		t.Fatalf("expected recreate after rollback to succeed, got %v", err)
	}
}
