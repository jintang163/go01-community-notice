package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go01-community-notice/internal/auth"
	"go01-community-notice/internal/model"
	"go01-community-notice/internal/store"
)

// ----- 测试辅助 -----

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{t: start} }
func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}
func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

type seqIDGen struct {
	mu sync.Mutex
	n  int
}

type failingReadStore struct {
	store.Store
	err error
}

func (s failingReadStore) UpsertReadRecord(context.Context, model.ReadRecord) (model.ReadRecord, error) {
	return model.ReadRecord{}, s.err
}

func (g *seqIDGen) next(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

// testEnv 聚合测试所需的全部依赖。
type testEnv struct {
	store    *store.MemoryStore
	hasher   *auth.PasswordHasher
	sessions *auth.SessionManager
	svc      *Services
	clock    *fakeClock
	admin    model.User
	resident model.User
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	fc := newFakeClock(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	gen := &seqIDGen{}
	st := store.NewMemoryStore(fc.Now, gen.next)
	hasher := auth.NewPasswordHasher()
	sessions := auth.NewSessionManager(time.Hour)
	svc := NewServices(st, hasher, sessions, fc)
	// 用 service.Clock 包装 fakeClock。
	env := &testEnv{store: st, hasher: hasher, sessions: sessions, clock: fc, svc: svc}
	// 创建 admin（直接走 store 以跳过登录）。
	salt, hash, iters, _ := hasher.Hash("admin123")
	admin, err := st.CreateUser(context.Background(), model.User{
		Username: "admin", Role: model.RoleAdmin, DisplayName: "管理员",
		PasswordHash: hash, PasswordSalt: salt, Iterations: iters,
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	env.admin = admin
	salt2, hash2, iters2, _ := hasher.Hash("res123")
	res, err := st.CreateUser(context.Background(), model.User{
		Username: "resident", Role: model.RoleResident, DisplayName: "居民",
		PasswordHash: hash2, PasswordSalt: salt2, Iterations: iters2,
	})
	if err != nil {
		t.Fatalf("create resident: %v", err)
	}
	env.resident = res
	return env
}

func (e *testEnv) createPublishedNotice(t *testing.T, title string) model.Notice {
	t.Helper()
	n, err := e.svc.Notice.Create(context.Background(), model.NoticeInput{
		Title: title, Content: title + " 内容", Priority: model.PriorityNormal,
		Status: model.StatusPublished,
	}, e.admin)
	if err != nil {
		t.Fatalf("create notice: %v", err)
	}
	return n
}

// ----- 核心业务规则测试 -----

// TestReadAfterMarkRead 标记已读后应判定为已读。
func TestReadAfterMarkRead(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "停水通知")
	// 初始未读。
	read, err := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID)
	if err != nil {
		t.Fatalf("IsRead initial: %v", err)
	}
	if read {
		t.Error("expected unread before marking")
	}
	// 标记已读。
	if err := env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	read, err = env.svc.Read.IsRead(ctx, env.resident.ID, n.ID)
	if err != nil {
		t.Fatalf("IsRead after: %v", err)
	}
	if !read {
		t.Error("expected read after marking")
	}
}

func TestMarkReadRejectsDeletedResident(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "账户删除期间的通知")
	residentID := env.resident.ID
	if err := env.svc.Auth.DeleteUser(ctx, residentID, env.admin); err != nil {
		t.Fatalf("delete resident: %v", err)
	}
	if err := env.svc.Read.MarkRead(ctx, residentID, n.ID); !model.IsNotFound(err) {
		t.Fatalf("expected not found for deleted resident, got %v", err)
	}
	if _, err := env.store.GetReadRecord(ctx, residentID, n.ID); !model.IsNotFound(err) {
		t.Fatalf("deleted resident must not leave a read record, got %v", err)
	}
}

// TestUpdateMeansUnread 核心：管理员更新已发布通知 -> 居民回到未读。
func TestUpdateMeansUnread(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "停水通知")
	env.clock.Advance(time.Minute) // 确保后续操作时间晚于发布时间
	// 居民标记已读。
	if err := env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if read, _ := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID); !read {
		t.Fatal("expected read before update")
	}
	// 管理员更新通知内容 -> 前移 UpdatedAt。
	env.clock.Advance(time.Minute)
	newContent := "停水通知（更新）：时间改为 14:00-18:00"
	_, err := env.svc.Notice.Update(ctx, n.ID, model.UpdateNoticeRequest{
		Content: &newContent,
	}, env.admin)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	// 居民应回到未读（核心规则）。
	read, err := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID)
	if err != nil {
		t.Fatalf("IsRead after update: %v", err)
	}
	if read {
		t.Error("EXPECTED UNREAD after notice update (更新即未读)")
	}
	// 再次标记已读 -> 又变为已读。
	if err := env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID); err != nil {
		t.Fatalf("MarkRead again: %v", err)
	}
	if read, _ := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID); !read {
		t.Error("expected read after re-marking")
	}
}

// TestViewDetailMarksRead 查看详情（居民）自动标记已读。
func TestViewDetailMarksRead(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "活动公告")
	if _, err := env.svc.Read.ViewDetail(ctx, n.ID, env.resident); err != nil {
		t.Fatalf("ViewDetail: %v", err)
	}
	if read, _ := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID); !read {
		t.Error("expected read after ViewDetail")
	}
}

// TestViewDetailAdminNoRead 管理员查看详情不应产生已读记录。
func TestViewDetailAdminNoRead(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "活动公告")
	if _, err := env.svc.Read.ViewDetail(ctx, n.ID, env.admin); err != nil {
		t.Fatalf("ViewDetail admin: %v", err)
	}
	if c, _ := env.store.CountReadRecordsByNotice(ctx, n.ID); c != 0 {
		t.Errorf("expected 0 read records after admin view, got %d", c)
	}
}

func TestViewDetailReportsReadPersistenceFailure(t *testing.T) {
	env := newTestEnv(t)
	n := env.createPublishedNotice(t, "持久化故障通知")
	failing := failingReadStore{Store: env.store, err: fmt.Errorf("read persistence unavailable")}
	svc := NewReadService(failing, env.clock)
	if _, err := svc.ViewDetail(context.Background(), n.ID, env.resident); err == nil {
		t.Fatal("expected detail view to report read persistence failure")
	}
}

// TestDraftInvisibleToResident 草稿对居民不可见（404）。
func TestDraftInvisibleToResident(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	// 创建草稿（不发布）。
	draft, err := env.svc.Notice.Create(ctx, model.NoticeInput{
		Title: "草稿通知", Content: "草稿内容", Priority: model.PriorityNormal,
	}, env.admin)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if draft.Status != model.StatusDraft {
		t.Fatalf("expected draft, got %s", draft.Status)
	}
	// 居民 Get 应 404。
	if _, err := env.svc.Notice.Get(ctx, draft.ID, env.resident); !model.IsNotFound(err) {
		t.Errorf("expected not found for resident on draft, got %v", err)
	}
	// 居民 ViewDetail 应 404 且不产生已读。
	if _, err := env.svc.Read.ViewDetail(ctx, draft.ID, env.resident); !model.IsNotFound(err) {
		t.Errorf("expected not found on ViewDetail draft, got %v", err)
	}
	if c, _ := env.store.CountReadRecordsByNotice(ctx, draft.ID); c != 0 {
		t.Errorf("expected 0 reads on draft, got %d", c)
	}
	// 管理员可见。
	if _, err := env.svc.Notice.Get(ctx, draft.ID, env.admin); err != nil {
		t.Errorf("admin should see draft: %v", err)
	}
}

// TestMarkReadOnDraftFails 对草稿标记已读应失败。
func TestMarkReadOnDraftFails(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	draft, _ := env.svc.Notice.Create(ctx, model.NoticeInput{Title: "d", Content: "c"}, env.admin)
	if err := env.svc.Read.MarkRead(ctx, env.resident.ID, draft.ID); !model.IsNotFound(err) {
		t.Errorf("expected not found marking read on draft, got %v", err)
	}
}

// TestPublishResetsReadBaseline 发布通知后，已读基准从发布时刻起算。
func TestPublishResetsReadBaseline(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	// 创建草稿，停一段时间再发布。
	draft, _ := env.svc.Notice.Create(ctx, model.NoticeInput{Title: "d", Content: "c"}, env.admin)
	env.clock.Advance(time.Hour)
	published, err := env.svc.Notice.Publish(ctx, draft.ID, env.admin)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !published.IsPublished() || published.PublishAt == nil {
		t.Fatal("expected published with publish time")
	}
	// 发布后标记已读，应已读。
	if err := env.svc.Read.MarkRead(ctx, env.resident.ID, published.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if read, _ := env.svc.Read.IsRead(ctx, env.resident.ID, published.ID); !read {
		t.Error("expected read after marking published notice")
	}
}

// TestUnreadCount 未读数量统计。
func TestUnreadCount(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n1 := env.createPublishedNotice(t, "通知一")
	env.clock.Advance(time.Minute)
	n2 := env.createPublishedNotice(t, "通知二")
	// 两篇未读。
	unread, total, err := env.svc.Read.UnreadCount(ctx, env.resident.ID)
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if unread != 2 || total != 2 {
		t.Fatalf("expected 2/2, got %d/%d", unread, total)
	}
	// 标记 n1 已读 -> 1 未读。
	env.svc.Read.MarkRead(ctx, env.resident.ID, n1.ID)
	unread, _, _ = env.svc.Read.UnreadCount(ctx, env.resident.ID)
	if unread != 1 {
		t.Errorf("expected 1 unread, got %d", unread)
	}
	// 更新 n2 -> 仍 1 未读（n2 原本未读），但更新后 n1 若被更新会变未读。
	env.clock.Advance(time.Minute)
	env.svc.Notice.Update(ctx, n1.ID, model.UpdateNoticeRequest{Content: strPtr("updated")}, env.admin)
	unread, _, _ = env.svc.Read.UnreadCount(ctx, env.resident.ID)
	if unread != 2 {
		t.Errorf("expected 2 unread after update (n1 reverted), got %d", unread)
	}
	_ = n2
}

// TestListForResidentReadStatus 居民列表带已读标识与排序。
func TestListForResidentReadStatus(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n1 := env.createPublishedNotice(t, "older")
	env.clock.Advance(time.Minute)
	n2 := env.createPublishedNotice(t, "newer")
	// 标记 n1 已读。
	env.svc.Read.MarkRead(ctx, env.resident.ID, n1.ID)
	list, err := env.svc.Read.ListForResident(ctx, env.resident.ID, model.NoticeFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	// newer 在前（发布时间倒序）。
	if list[0].Notice.ID != n2.ID {
		t.Errorf("expected newer first (%s), got %s", n2.ID, list[0].Notice.ID)
	}
	// n1 应已读，n2 未读。
	readMap := map[string]bool{}
	for _, it := range list {
		readMap[it.Notice.ID] = it.Read
	}
	if !readMap[n1.ID] {
		t.Error("n1 should be read")
	}
	if readMap[n2.ID] {
		t.Error("n2 should be unread")
	}
}

// TestUpdateNoChangeDoesNotInvalidateRead 无字段变更时不前移 UpdatedAt。
func TestUpdateNoChangeDoesNotInvalidateRead(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "通知")
	env.clock.Advance(time.Minute)
	env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID)
	before, _ := env.store.GetNotice(ctx, n.ID)
	// 空更新。
	env.clock.Advance(time.Minute)
	_, err := env.svc.Notice.Update(ctx, n.ID, model.UpdateNoticeRequest{}, env.admin)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := env.store.GetNotice(ctx, n.ID)
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Error("expected UpdatedAt unchanged when no fields changed")
	}
	if read, _ := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID); !read {
		t.Error("expected still read after no-op update")
	}
}

func strPtr(s string) *string { return &s }
