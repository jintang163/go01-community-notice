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

// SetUserPassword 更新用户口令并自增凭据版本。
//
// 在写锁内重新读取当前用户，仅写入新的盐/哈希/迭代次数并从当前存储值自增
// CredentialVersion，保留 ID/用户名/角色/显示名/CreatedAt。凭据版本的自增
// 必须在写锁内从当前值进行，而非用调用方在并发更新前读到的可能陈旧值自增——
// 否则两位居民并发改密时各自从同一陈旧基线自增、最终都写入同一版本（"更新
// 丢失"），落盘的凭据版本与会话颁发版本（RotateCredentials 在各自锁内从当前
// 值自增）不一致，最终落盘的新口令在 Login 时读到旧版本被 Create 拒绝。
func (s *MemoryStore) SetUserPassword(ctx context.Context, userID, salt, hash string, iterations int) (model.User, error) {
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	s.mu.Lock()
	cur, ok := s.users[userID]
	if !ok {
		s.mu.Unlock()
		return model.User{}, model.ErrNotFound
	}
	cur.PasswordSalt = salt
	cur.PasswordHash = hash
	cur.Iterations = iterations
	cur.CredentialVersion++ // 从当前存储值自增，不取调用方传入的可能陈旧版本
	cur.UpdatedAt = s.now()
	s.users[userID] = cur
	s.mu.Unlock()
	s.afterWrite()
	return cur, nil
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

// SetNoticeStatus 转换通知状态（发布/下架），保留并发的内容编辑。
//
// 与 UpdateNoticeMetadata 同属"只改特定字段"的读-改-写：在写锁内重新读取当前
// 通知，仅切换 Status 并按状态调整 PublishAt 与 UpdatedAt，正文/标题/优先级/
// 分类/作者/置顶/CreatedAt 一律保留当前存储值。这样当一位管理员在另一位管理员
// 并发编辑正文期间发布/下架通知时，不会用发布前读到的旧正文覆盖刚保存的新正文
// （"更新丢失"）。
//
// 发布时前移 UpdatedAt 并设置 PublishAt = UpdatedAt，使"更新即未读"基准从发布
// 时刻起算，与 UpdateNotice 处理发布状态的语义一致；下架时清空 PublishAt。
func (s *MemoryStore) SetNoticeStatus(ctx context.Context, id string, status model.NoticeStatus) (model.Notice, error) {
	if err := ctx.Err(); err != nil {
		return model.Notice{}, err
	}
	if status == "" {
		status = model.StatusDraft
	}
	s.mu.Lock()
	cur, ok := s.notices[id]
	if !ok {
		s.mu.Unlock()
		return model.Notice{}, model.ErrNotFound
	}
	cur.Status = status
	cur.UpdatedAt = s.now() // 前移 UpdatedAt —— "更新即未读"的基准
	if cur.IsPublished() {
		// 发布：确保有发布时刻，且以本次转换为基准。
		pa := cur.UpdatedAt
		cur.PublishAt = &pa
	} else {
		// 下架：清空发布时刻。
		cur.PublishAt = nil
	}
	s.notices[id] = cur
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
	notice, ok := s.notices[rr.NoticeID]
	if !ok {
		s.mu.Unlock()
		return model.ReadRecord{}, model.ErrNotFound
	}
	// 下架（转草稿）与本次阅读记录写入并发时，不能让尚未落库的阅读记录写到
	// 草稿上：居民打开详情时通知尚为已发布，但服务层判定之后、本写入之前，管理员
	// 可能已完成下架。此处与 SetNoticeStatus 共用同一把写锁，故在写锁内重新读取
	// 当前通知状态——若已下架为草稿则拒绝写入并返回 ErrNotFound（与草稿不可见、
	// 草稿不可标记已读的语义一致），写锁使该检查与随后的写入原子，消除
	// "服务层判定已发布 -> 写入前被并发下架"的窗口。已删除通知在上面的存在性
	// 检查中同样返回 ErrNotFound。
	if !notice.IsPublished() {
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
