package store

import (
	"context"
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
// 持久化由调用方（FileStore）通过 persistHook 回调驱动：每个写方法成功后
// 在释放写锁后调用 persistHook（若已设置），保证不会在持锁状态下重入死锁。
type MemoryStore struct {
	mu sync.RWMutex

	users    map[string]model.User    // id -> user
	username map[string]string        // username -> id（唯一索引）
	notices  map[string]model.Notice  // id -> notice
	reads    map[string]model.ReadRecord // id -> read record
	readIdx  map[model.ReadKey]model.ReadRecord // (userID,noticeID) -> read record

	now  func() time.Time
	genID IDGenerator
	persistHook func() // 写后持久化回调（由 FileStore 注入）
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
func (s *MemoryStore) SetPersistHook(hook func()) {
	s.mu.Lock()
	s.persistHook = hook
	s.mu.Unlock()
}

// afterWrite 在写锁释放后安全地触发持久化。
func (s *MemoryStore) afterWrite() {
	if s.persistHook != nil {
		s.persistHook()
	}
}

// ---- 用户 ----

func (s *MemoryStore) CreateUser(ctx context.Context, u model.User) (model.User, error) {
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	u.Username = strings.TrimSpace(u.Username)
	u.DisplayName = sanitizeDisplayName(u.DisplayName)
	s.mu.Lock()
	if _, exists := s.username[u.Username]; exists {
		s.mu.Unlock()
		return model.User{}, model.ErrAlreadyExists
	}
	now := s.now()
	u.ID = s.genID(model.UserIDPrefix)
	u.CreatedAt = now
	u.UpdatedAt = now
	s.users[u.ID] = u
	s.username[u.Username] = u.ID
	s.mu.Unlock()
	s.afterWrite()
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
	cur, ok := s.users[u.ID]
	if !ok {
		s.mu.Unlock()
		return model.User{}, model.ErrNotFound
	}
	// 若用户名变更，检查新用户名是否已被占用。
	if u.Username != cur.Username {
		if otherID, exists := s.username[u.Username]; exists && otherID != u.ID {
			s.mu.Unlock()
			return model.User{}, model.ErrAlreadyExists
		}
		delete(s.username, cur.Username)
		s.username[u.Username] = u.ID
	}
	u.CreatedAt = cur.CreatedAt
	u.UpdatedAt = s.now()
	s.users[u.ID] = u
	s.mu.Unlock()
	s.afterWrite()
	return u, nil
}

func (s *MemoryStore) DeleteUser(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	u, ok := s.users[id]
	if !ok {
		s.mu.Unlock()
		return model.ErrNotFound
	}
	delete(s.users, id)
	delete(s.username, u.Username)
	// 级联删除该用户的阅读记录。
	for _, rr := range s.reads {
		if rr.UserID == id {
			delete(s.reads, rr.ID)
			delete(s.readIdx, model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID})
		}
	}
	s.mu.Unlock()
	s.afterWrite()
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
	now := s.now()
	n.ID = s.genID(model.NoticeIDPrefix)
	n.CreatedAt = now
	n.UpdatedAt = now
	if n.IsPublished() && n.PublishAt == nil {
		pa := now
		n.PublishAt = &pa
	}
	s.notices[n.ID] = n
	s.mu.Unlock()
	s.afterWrite()
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
	cur, ok := s.notices[n.ID]
	if !ok {
		s.mu.Unlock()
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
	s.mu.Unlock()
	s.afterWrite()
	return n, nil
}

func (s *MemoryStore) DeleteNotice(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if _, ok := s.notices[id]; !ok {
		s.mu.Unlock()
		return model.ErrNotFound
	}
	delete(s.notices, id)
	// 级联删除该通知的阅读记录。
	for _, rr := range s.reads {
		if rr.NoticeID == id {
			delete(s.reads, rr.ID)
			delete(s.readIdx, model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID})
		}
	}
	s.mu.Unlock()
	s.afterWrite()
	return nil
}

// UpdateNoticeMetadata updates non-content metadata (置顶) without invalidating reads.
//
// 只把调用方传入的置顶变更（Pinned）应用到当前存储的通知，其余字段
// （标题/正文/优先级/分类/作者/状态/CreatedAt/UpdatedAt/PublishAt）一律保留
// 当前值。置顶是展示属性，不应前移 UpdatedAt、不应使已读失效。
//
// 关键：在写锁内重新读取当前通知再合并，而非直接写入调用方（可能在并发更新
// 之前）读到的整条通知。否则当另一位管理员并发更新正文时，置顶操作会用陈旧
// 正文覆盖对方刚保存的结果（"更新丢失"）。这里只取 Pinned，正文等内容字段
// 始终取自当前存储值，从而与并发的正文更新互不覆盖。
func (s *MemoryStore) UpdateNoticeMetadata(ctx context.Context, n model.Notice) (model.Notice, error) {
	if err := ctx.Err(); err != nil { return model.Notice{}, err }
	s.mu.Lock()
	cur, ok := s.notices[n.ID]
	if !ok { s.mu.Unlock(); return model.Notice{}, model.ErrNotFound }
	cur.Pinned = n.Pinned // 仅应用置顶变更，保留当前正文等内容字段
	s.notices[n.ID] = cur
	s.mu.Unlock()
	s.afterWrite()
	return cur, nil
}

// ---- 阅读记录 ----

func (s *MemoryStore) UpsertReadRecord(ctx context.Context, rr model.ReadRecord) (model.ReadRecord, error) {
	if err := ctx.Err(); err != nil {
		return model.ReadRecord{}, err
	}
	key := model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID}
	s.mu.Lock()
	if _, ok := s.users[rr.UserID]; !ok {
		s.mu.Unlock()
		return model.ReadRecord{}, model.ErrNotFound
	}
	if _, ok := s.notices[rr.NoticeID]; !ok {
		s.mu.Unlock()
		return model.ReadRecord{}, model.ErrNotFound
	}
	now := s.now()
	if existing, ok := s.readIdx[key]; ok {
		rr.ID = existing.ID
		rr.CreatedAt = existing.CreatedAt
		rr.ReadAt = now
		s.reads[rr.ID] = rr
		s.readIdx[key] = rr
		s.mu.Unlock()
		s.afterWrite()
		return rr, nil
	}
	rr.ID = s.genID(model.ReadIDPrefix)
	rr.CreatedAt = now
	rr.ReadAt = now
	s.reads[rr.ID] = rr
	s.readIdx[key] = rr
	s.mu.Unlock()
	s.afterWrite()
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
	for _, rr := range s.reads {
		if rr.NoticeID == noticeID {
			delete(s.reads, rr.ID)
			delete(s.readIdx, model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID})
		}
	}
	s.mu.Unlock()
	s.afterWrite()
	return nil
}

func (s *MemoryStore) DeleteReadRecordsByUser(ctx context.Context, userID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	for _, rr := range s.reads {
		if rr.UserID == userID {
			delete(s.reads, rr.ID)
			delete(s.readIdx, model.ReadKey{UserID: rr.UserID, NoticeID: rr.NoticeID})
		}
	}
	s.mu.Unlock()
	s.afterWrite()
	return nil
}

// AllUsers 返回全部用户副本（仅供 FileStore 快照用）。
func (s *MemoryStore) AllUsers() []model.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	return out
}

// AllNotices 返回全部通知副本。
func (s *MemoryStore) AllNotices() []model.Notice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Notice, 0, len(s.notices))
	for _, n := range s.notices {
		out = append(out, n)
	}
	return out
}

// AllReads 返回全部阅读记录副本。
func (s *MemoryStore) AllReads() []model.ReadRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.ReadRecord, 0, len(s.reads))
	for _, rr := range s.reads {
		out = append(out, rr)
	}
	return out
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
