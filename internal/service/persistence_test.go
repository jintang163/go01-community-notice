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
	salt, hash, iterations, err := hasher.Hash("admin123")
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	admin, err := fileStore.Store().CreateUser(ctx, model.User{
		Username:     "admin",
		PasswordHash: hash,
		PasswordSalt: salt,
		Iterations:   iterations,
		Role:         model.RoleAdmin,
		DisplayName:  "管理员",
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	if err := os.Remove(dataPath); err != nil {
		t.Fatalf("remove persisted snapshot: %v", err)
	}
	if err := os.Mkdir(dataPath, 0o755); err != nil {
		t.Fatalf("replace snapshot with directory: %v", err)
	}

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
