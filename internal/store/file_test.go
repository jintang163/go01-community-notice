package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go01-community-notice/internal/model"
)

func TestFileStorePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")

	fc := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	gen := &seqIDGen{}
	mem := NewMemoryStore(fc.Now, gen.next)
	mem.SetPersistHook(func() error { return nil })

	// 用内存实现直接构造数据。
	ctx := context.Background()
	admin, _ := mem.CreateUser(ctx, model.User{Username: "admin", Role: model.RoleAdmin, DisplayName: "Admin", PasswordHash: "h", PasswordSalt: "s", Iterations: 1})
	res, _ := mem.CreateUser(ctx, model.User{Username: "res1", Role: model.RoleResident, DisplayName: "R", PasswordHash: "h", PasswordSalt: "s", Iterations: 1})
	n, _ := mem.CreateNotice(ctx, model.Notice{Title: "hello", Content: "world", Status: model.StatusPublished, AuthorID: admin.ID})
	_, _ = mem.UpsertReadRecord(ctx, model.ReadRecord{UserID: res.ID, NoticeID: n.ID})

	// 用 FileStore 包装并写入磁盘。
	fs := &FileStore{mem: mem, path: path}
	mem.SetPersistHook(fs.save)
	if err := fs.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// 重新加载到新内存实例，验证数据恢复。
	fc2 := newFakeClock(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	gen2 := &seqIDGen{}
	mem2 := NewMemoryStore(fc2.Now, gen2.next)
	fs2 := &FileStore{mem: mem2, path: path}
	mem2.SetPersistHook(fs2.save)
	if err := fs2.load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	// 用户恢复。
	u, err := mem2.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("user not restored: %v", err)
	}
	if u.ID != admin.ID {
		t.Errorf("admin id mismatch: %s vs %s", u.ID, admin.ID)
	}
	// 通知恢复。
	nn, err := mem2.GetNotice(ctx, n.ID)
	if err != nil {
		t.Fatalf("notice not restored: %v", err)
	}
	if nn.Title != "hello" {
		t.Errorf("title: %s", nn.Title)
	}
	// 阅读记录恢复。
	rr, err := mem2.GetReadRecord(ctx, res.ID, n.ID)
	if err != nil {
		t.Fatalf("read not restored: %v", err)
	}
	if rr.UserID != res.ID || rr.NoticeID != n.ID {
		t.Errorf("read mismatch: %+v", rr)
	}
}

func TestFileStoreNewLoadsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")

	// 第一次：用 NewFileStore 创建并写入数据。
	fs1, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new file store 1: %v", err)
	}
	ctx := context.Background()
	admin, _ := fs1.Store().CreateUser(ctx, model.User{Username: "admin", Role: model.RoleAdmin, DisplayName: "A", PasswordHash: "h", PasswordSalt: "s", Iterations: 1})
	fs1.Store().CreateNotice(ctx, model.Notice{Title: "t", Content: "c", Status: model.StatusPublished, AuthorID: admin.ID})
	if err := fs1.Flush(); err != nil {
		t.Fatalf("flush1: %v", err)
	}

	// 第二次：重新 NewFileStore 同一路径，应自动加载已有数据。
	fs2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new file store 2: %v", err)
	}
	if _, err := fs2.Store().GetUserByUsername(ctx, "admin"); err != nil {
		t.Fatalf("expected admin loaded on reopen, got %v", err)
	}
}

func TestFileStoreMissingFileOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if fs == nil {
		t.Fatal("expected non-nil file store")
	}
}

func TestFileStoreAtomicWriteCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "store.json")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	fs.Store().CreateUser(ctx, model.User{Username: "u", Role: model.RoleResident, DisplayName: "U", PasswordHash: "h", PasswordSalt: "s", Iterations: 1})
	if err := fs.Flush(); err != nil {
		t.Fatalf("flush into nested dir: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created in nested dir: %v", err)
	}
}
