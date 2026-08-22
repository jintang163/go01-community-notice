package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go01-community-notice/internal/model"
)

// snapshotData 落盘的 JSON 快照结构。
// 版本号字段便于后续升级时做数据迁移。
type snapshotData struct {
	Version int                `json:"version"`
	Users   []model.User       `json:"users"`
	Notices []model.Notice     `json:"notices"`
	Reads   []model.ReadRecord `json:"reads"`
}

const snapshotVersion = 1

// FileStore 在 MemoryStore 之上叠加 JSON 文件持久化。
//
// 设计：
//   - 启动时 Load() 读取数据文件，整体替换内存状态。
//   - 每个写操作成功后，MemoryStore 调用 persistHook，触发 Save() 落盘。
//   - Save() 采用"写临时文件 + os.Rename"的原子写策略，避免写中途崩溃损坏数据。
//   - Save() 在 RLock 下取快照（深拷贝切片），释放锁后再写文件，不阻塞写操作。
type FileStore struct {
	mem  *MemoryStore
	path string

	saveMu sync.Mutex // 串行化磁盘写，避免并发写互相覆盖
}

// NewFileStore 创建文件持久化存储。若数据文件存在则自动加载。
func NewFileStore(path string) (*FileStore, error) {
	mem := NewMemoryStore(time.Now, defaultIDGenerator)
	fs := &FileStore{mem: mem, path: path}
	mem.SetPersistHook(fs.save)
	if err := fs.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load store %q: %w", path, err)
	}
	return fs, nil
}

// Store 返回底层 MemoryStore（实现 Store 接口），供 service 层使用。
func (fs *FileStore) Store() *MemoryStore { return fs.mem }

// Path 数据文件路径。
func (fs *FileStore) Path() string { return fs.path }

// load 从磁盘加载快照到内存。
func (fs *FileStore) load() error {
	data, err := os.ReadFile(fs.path)
	if err != nil {
		return err
	}
	var snap snapshotData
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("unmarshal snapshot: %w", err)
	}
	fs.mem.ReplaceAll(snap.Users, snap.Notices, snap.Reads)
	return nil
}

// save 将当前内存状态原子写入磁盘。由 MemoryStore 写后钩子调用。
func (fs *FileStore) save() error {
	fs.saveMu.Lock()
	defer fs.saveMu.Unlock()

	snap := snapshotData{
		Version: snapshotVersion,
		Users:   fs.mem.AllUsers(),
		Notices: fs.mem.AllNotices(),
		Reads:   fs.mem.AllReads(),
	}

	return fs.writeAtomic(snap)
}

// writeAtomic 原子写：先写临时文件，再 rename 覆盖目标文件。
func (fs *FileStore) writeAtomic(snap snapshotData) error {
	if dir := filepath.Dir(fs.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(fs.path), ".store-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	// 任何失败路径都清理临时文件。
	cleanup := func() { _ = os.Remove(tmpName) }

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "")
	if err := enc.Encode(snap); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("encode: %w", err)
	}
	if err := tmp.Sync(); err != nil && !errors.Is(err, io.ErrNoProgress) {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, fs.path); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Flush 强制立即写一次快照（用于优雅关闭前落盘）。
func (fs *FileStore) Flush() error {
	fs.saveMu.Lock()
	defer fs.saveMu.Unlock()
	snap := snapshotData{
		Version: snapshotVersion,
		Users:   fs.mem.AllUsers(),
		Notices: fs.mem.AllNotices(),
		Reads:   fs.mem.AllReads(),
	}
	return fs.writeAtomic(snap)
}

// 确保编译期接口实现检查（FileStore 提供 Store()，不直接实现 Store，
// 此处保留以提示用法）。
var _ = context.Background
