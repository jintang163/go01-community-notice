package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go01-community-notice/internal/model"
)

// IDGenerator 生成唯一 ID 的函数类型。返回带前缀的字符串。
type IDGenerator func(prefix string) string

// MemoryStore 基于 map + sync.RWMutex 的内存实现。
//
// 持久化由调用方（FileStore）通过 persistHook 回调驱动：每个写方法在写锁内
// 完成内存变更后调用 persistHook（若已设置）落盘。持久化是创建/更新/删除流程
// 的一部分——若落盘失败，写方法回滚内存变更并返回错误，保证"磁盘与内存一致"：
// 向调用方确认成功当且仅当变更已可靠落盘，避免出现"内存中有、磁盘上无"的幻影
// 数据（重启后丢失，造成账号已创建的假象）。因此 persistHook 在写锁内调用，
// 回调不得再次获取读锁或写锁（会重入死锁）。
type MemoryStore struct {
	mu sync.RWMutex

	users    map[string]model.User    // id -> user
	username map[string]string        // username -> id（唯一索引）
	notices  map[string]model.Notice  // id -> notice
	reads    map[string]model.ReadRecord // id -> read record
	readIdx  map[model.ReadKey]model.ReadRecord // (userID,noticeID) -> read record

	now  func() time.Time
	genID IDGenerator
	persistHook func() error // 写后持久化回调（由 FileStore 注入）；在写锁内调用，返回错误以便回滚
}

// NewMemoryStore 创建内存存储。
//
// now 用于注入可控时钟（测试），为 nil 时使用 time.Now。
// genID 用于注入可控 ID 生成器，为 nil 时使用基于 crypto/rand 的默认实现。
func NewMemoryStore(now func() time.Time, genID IDGenerator) *MemoryStore {
	if now == nil {
		now = time.Now
	}
	if genID == nil {
		genID = defaultIDGenerator
	}
	return &MemoryStore{
		users:    make(map[string]model.User),
		username: make(map[string]string),
		notices:  make(map[string]model.Notice),
		reads:    make(map[string]model.ReadRecord),
		readIdx:  make(map[model.ReadKey]model.ReadRecord),
		now:      now,
		genID:    genID,
	}
}

// SetPersistHook 设置写后持久化回调。FileStore 用它把内存快照落盘。
// 回调在写锁内被调用；返回错误时调用方将回滚本次内存变更并返回失败，
// 确保持久化失败不会留下"内存中有、磁盘上无"的幻影数据。
func (s *MemoryStore) SetPersistHook(hook func() error) {
	s.mu.Lock()
	s.persistHook = hook
	s.mu.Unlock()
}

// persistLocked 触发持久化钩子；须在已持有写锁（mu.Lock）时调用。
// 钩子不得再获取读锁或写锁（会重入死锁），应在已持锁前提下直接读取内存快照落盘。
// 返回持久化错误，供调用方在写锁内回滚内存变更。
func (s *MemoryStore) persistLocked() error {
	if s.persistHook != nil {
		return s.persistHook()
	}
	return nil
}

// PersistNow 强制立即持久化一次当前内存状态（用于优雅关闭前落盘）。
// 在写锁内调用持久化钩子，与并发写操作互斥。
func (s *MemoryStore) PersistNow() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

// ---- 用户 ----

func (s *MemoryStore) CreateUser(ctx context.Context, u model.User) (model.User, error) {
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	u.Username = strings.TrimSpace(u.Username)
	u.DisplayName = sanitizeDisplayName(u.DisplayName)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.username[u.Username]; exists {
		return model.User{}, model.ErrAlreadyExists
	}
	now := s.now()
	u.ID = s.genID(model.UserIDPrefix)
	u.CreatedAt = now
	u.UpdatedAt = now
	s.users[u.ID] = u
	s.username[u.Username] = u.ID
	if err := s.persistLocked(); err != nil {
		// 持久化失败：回滚内存创建，确保磁盘与内存一致（均无此用户），
		// 避免向管理员确认成功却无法恢复的"幻影账号"。
		delete(s.users, u.ID)
		delete(s.username, u.Username)
		return model.User{}, fmt.Errorf("persist user: %w", err)
	}
	return u, nil
}

func (s *MemoryStore) GetUserByUsername(ctx context.Context, username string) (model.User, error) {
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.username[username]
	if !ok {
		return model.User{}, model.ErrNotFound
	}
	return s.users[id], nil
}

func (s *MemoryStore) GetUserByID(ctx context.Context, id string) (model.User, error) {
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return model.User{}, model.ErrNotFound
	}
	return u, nil
}

func (s *MemoryStore) ListUsers(ctx context.Context, role model.UserRole) ([]model.User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.User, 0, len(s.users))
	for _, u := range s.users {
		if role != "" && u.Role != role {
			continue
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (s *MemoryStore) UpdateUser(ctx context.Context, u model.User) (model.User, error) {
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	u.Username = strings.TrimSpace(u.Username)
	u.DisplayName = sanitizeDisplayName(u.DisplayName)
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.users[u.ID]
	if !ok {
		return model.User{}, model.ErrNotFound
	}
	// 若用户名变更，检查新用户名是否已被占用。
	usernameChanged := u.Username != cur.Username
	if usernameChanged {
		if otherID, exists := s.username[u.Username]; exists && otherID != u.ID {
			return model.User{}, model.ErrAlreadyExists
		}
		delete(s.username, cur.Username)
		s.username[u.Username] = u.ID
	}
	u.CreatedAt = cur.CreatedAt
	u.UpdatedAt = s.now()
	s.users[u.ID] = u
	if err := s.persistLocked(); err != nil {
		// 回滚到更新前状态。
		s.users[u.ID] = cur
		if usernameChanged {
			delete(s.username, u.Username)
			s.username[cur.Username] = cur.ID
		}
		return model.User{}, fmt.Errorf("persist user: %w", err)
	}
	return u, nil
}

func (s *MemoryStore) DeleteUser(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return model.ErrNotFound
	}
	// 捕获级联删除的阅读记录，便于持久化失败时回滚。
	var deletedReads []model.ReadRecord
	delete(s.users, id)
	delete(s.username, u.Username)
	for _, rr := range s.reads {
		if rr.UserID == id {
			delete(s.reads, rr.ID)
			delete(s.readIdx, model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID})
			deletedReads = append(deletedReads, rr)
		}
	}
	if err := s.persistLocked(); err != nil {
		// 回滚：恢复用户与其级联删除的阅读记录。
		s.users[u.ID] = u
		s.username[u.Username] = u.ID
		for _, rr := range deletedReads {
			s.reads[rr.ID] = rr
			s.readIdx[model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID}] = rr
		}
		return fmt.Errorf("persist delete user: %w", err)
	}
	return nil
}

// ---- 通知 ----

func (s *MemoryStore) CreateNotice(ctx context.Context, n model.Notice) (model.Notice, error) {
	if err := ctx.Err(); err != nil {
		return model.Notice{}, err
	}
	n.Title = strings.TrimSpace(n.Title)
	n.Content = strings.TrimSpace(n.Content)
	n.Category = strings.TrimSpace(n.Category)
	if n.Status == "" {
		n.Status = model.StatusDraft
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	n.ID = s.genID(model.NoticeIDPrefix)
	n.CreatedAt = now
	n.UpdatedAt = now
	if n.IsPublished() && n.PublishAt == nil {
		pa := now
		n.PublishAt = &pa
	}
	s.notices[n.ID] = n
	if err := s.persistLocked(); err != nil {
		delete(s.notices, n.ID)
		return model.Notice{}, fmt.Errorf("persist notice: %w", err)
	}
	return n, nil
}

func (s *MemoryStore) GetNotice(ctx context.Context, id string) (model.Notice, error) {
	if err := ctx.Err(); err != nil {
		return model.Notice{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.notices[id]
	if !ok {
		return model.Notice{}, model.ErrNotFound
	}
	return n, nil
}

func (s *MemoryStore) ListNotices(ctx context.Context, f model.NoticeFilter) ([]model.Notice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keyword := strings.ToLower(strings.TrimSpace(f.Keyword))
	s.mu.RLock()
	out := make([]model.Notice, 0, len(s.notices))
	for _, n := range s.notices {
		if f.Status != "" && n.Status != f.Status {
			continue
		}
		if f.Category != "" && n.Category != f.Category {
			continue
		}
		if f.AuthorID != "" && n.AuthorID != f.AuthorID {
			continue
		}
		if f.PinnedOnly && !n.Pinned {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(n.Title), keyword) {
			continue
		}
		out = append(out, n)
	}
	s.mu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool {
		return model.ResidentListOrder(out[i], out[j])
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *MemoryStore) UpdateNotice(ctx context.Context, n model.Notice) (model.Notice, error) {
	if err := ctx.Err(); err != nil {
		return model.Notice{}, err
	}
	n.Title = strings.TrimSpace(n.Title)
	n.Content = strings.TrimSpace(n.Content)
	n.Category = strings.TrimSpace(n.Category)
	if n.Status == "" {
		n.Status = model.StatusDraft
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.notices[n.ID]
	if !ok {
		return model.Notice{}, model.ErrNotFound
	}
	n.CreatedAt = cur.CreatedAt
	n.UpdatedAt = s.now() // 前移 UpdatedAt —— "更新即未读"的基准
	if n.IsPublished() && n.PublishAt == nil {
		pa := n.UpdatedAt
		n.PublishAt = &pa
	}
	if n.IsDraft() {
		n.PublishAt = nil
	}
	s.notices[n.ID] = n
	if err := s.persistLocked(); err != nil {
		s.notices[n.ID] = cur
		return model.Notice{}, fmt.Errorf("persist notice: %w", err)
	}
	return n, nil
}

func (s *MemoryStore) DeleteNotice(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notices[id]
	if !ok {
		return model.ErrNotFound
	}
	// 捕获级联删除的阅读记录，便于持久化失败时回滚。
	var deletedReads []model.ReadRecord
	delete(s.notices, id)
	for _, rr := range s.reads {
		if rr.NoticeID == id {
			delete(s.reads, rr.ID)
			delete(s.readIdx, model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID})
			deletedReads = append(deletedReads, rr)
		}
	}
	if err := s.persistLocked(); err != nil {
		// 回滚：恢复通知与其级联删除的阅读记录。
		s.notices[n.ID] = n
		for _, rr := range deletedReads {
			s.reads[rr.ID] = rr
			s.readIdx[model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID}] = rr
		}
		return fmt.Errorf("persist delete notice: %w", err)
	}
	return nil
}

// UpdateNoticeMetadata updates non-content metadata without invalidating reads.
func (s *MemoryStore) UpdateNoticeMetadata(ctx context.Context, n model.Notice) (model.Notice, error) {
	if err := ctx.Err(); err != nil { return model.Notice{}, err }
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.notices[n.ID]
	if !ok { return model.Notice{}, model.ErrNotFound }
	n.CreatedAt = cur.CreatedAt
	n.UpdatedAt = cur.UpdatedAt
	n.PublishAt = cur.PublishAt
	s.notices[n.ID] = n
	if err := s.persistLocked(); err != nil {
		s.notices[n.ID] = cur
		return model.Notice{}, fmt.Errorf("persist notice metadata: %w", err)
	}
	return n, nil
}

// ---- 阅读记录 ----

func (s *MemoryStore) UpsertReadRecord(ctx context.Context, rr model.ReadRecord) (model.ReadRecord, error) {
	if err := ctx.Err(); err != nil {
		return model.ReadRecord{}, err
	}
	key := model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[rr.UserID]; !ok {
		return model.ReadRecord{}, model.ErrNotFound
	}
	if _, ok := s.notices[rr.NoticeID]; !ok {
		return model.ReadRecord{}, model.ErrNotFound
	}
	now := s.now()
	if existing, ok := s.readIdx[key]; ok {
		rr.ID = existing.ID
		rr.CreatedAt = existing.CreatedAt
		rr.ReadAt = now
		s.reads[rr.ID] = rr
		s.readIdx[key] = rr
		if err := s.persistLocked(); err != nil {
			// 回滚到更新前的阅读记录。
			s.reads[existing.ID] = existing
			s.readIdx[key] = existing
			return model.ReadRecord{}, fmt.Errorf("persist read record: %w", err)
		}
		return rr, nil
	}
	rr.ID = s.genID(model.ReadIDPrefix)
	rr.CreatedAt = now
	rr.ReadAt = now
	s.reads[rr.ID] = rr
	s.readIdx[key] = rr
	if err := s.persistLocked(); err != nil {
		delete(s.reads, rr.ID)
		delete(s.readIdx, key)
		return model.ReadRecord{}, fmt.Errorf("persist read record: %w", err)
	}
	return rr, nil
}

func (s *MemoryStore) GetReadRecord(ctx context.Context, userID, noticeID string) (model.ReadRecord, error) {
	if err := ctx.Err(); err != nil {
		return model.ReadRecord{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rr, ok := s.readIdx[model.ReadKey{UserID: userID, NoticeID: noticeID}]
	if !ok {
		return model.ReadRecord{}, model.ErrNotFound
	}
	return rr, nil
}

func (s *MemoryStore) ListReadRecordsByUser(ctx context.Context, userID string) ([]model.ReadRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.ReadRecord, 0)
	for _, rr := range s.reads {
		if rr.UserID == userID {
			out = append(out, rr)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReadAt.After(out[j].ReadAt)
	})
	return out, nil
}

func (s *MemoryStore) CountReadRecordsByNotice(ctx context.Context, noticeID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, rr := range s.reads {
		if rr.NoticeID == noticeID {
			count++
		}
	}
	return count, nil
}

func (s *MemoryStore) DeleteReadRecordsByNotice(ctx context.Context, noticeID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 捕获被删除的阅读记录，便于持久化失败时回滚。
	var deletedReads []model.ReadRecord
	for _, rr := range s.reads {
		if rr.NoticeID == noticeID {
			delete(s.reads, rr.ID)
			delete(s.readIdx, model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID})
			deletedReads = append(deletedReads, rr)
		}
	}
	if err := s.persistLocked(); err != nil {
		for _, rr := range deletedReads {
			s.reads[rr.ID] = rr
			s.readIdx[model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID}] = rr
		}
		return fmt.Errorf("persist delete reads by notice: %w", err)
	}
	return nil
}

func (s *MemoryStore) DeleteReadRecordsByUser(ctx context.Context, userID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 捕获被删除的阅读记录，便于持久化失败时回滚。
	var deletedReads []model.ReadRecord
	for _, rr := range s.reads {
		if rr.UserID == userID {
			delete(s.reads, rr.ID)
			delete(s.readIdx, model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID})
			deletedReads = append(deletedReads, rr)
		}
	}
	if err := s.persistLocked(); err != nil {
		for _, rr := range deletedReads {
			s.reads[rr.ID] = rr
			s.readIdx[model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID}] = rr
		}
		return fmt.Errorf("persist delete reads by user: %w", err)
	}
	return nil
}

// snapshotLocked 返回当前全部数据的快照（深拷贝切片）。
// 调用方必须已持有写锁（mu.Lock）；本方法不再加锁，避免在写锁内重入读锁而死锁。
// 供 FileStore.save 在写锁内落盘使用。
func (s *MemoryStore) snapshotLocked() (users []model.User, notices []model.Notice, reads []model.ReadRecord) {
	users = make([]model.User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u)
	}
	notices = make([]model.Notice, 0, len(s.notices))
	for _, n := range s.notices {
		notices = append(notices, n)
	}
	reads = make([]model.ReadRecord, 0, len(s.reads))
	for _, rr := range s.reads {
		reads = append(reads, rr)
	}
	return users, notices, reads
}

// ReplaceAll 用给定数据整体替换内存状态（FileStore 加载文件后调用）。
func (s *MemoryStore) ReplaceAll(users []model.User, notices []model.Notice, reads []model.ReadRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = make(map[string]model.User, len(users))
	s.username = make(map[string]string, len(users))
	for _, u := range users {
		s.users[u.ID] = u
		s.username[u.Username] = u.ID
	}
	s.notices = make(map[string]model.Notice, len(notices))
	for _, n := range notices {
		s.notices[n.ID] = n
	}
	s.reads = make(map[string]model.ReadRecord, len(reads))
	s.readIdx = make(map[model.ReadKey]model.ReadRecord, len(reads))
	for _, rr := range reads {
		s.reads[rr.ID] = rr
		s.readIdx[model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID}] = rr
	}
}
