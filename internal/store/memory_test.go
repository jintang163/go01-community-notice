package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go01-community-notice/internal/model"
)

// fakeClock 可控时钟，用于确定性测试。
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

// seqIDGen 顺序 ID 生成器，保证唯一且可预测。
type seqIDGen struct {
	mu sync.Mutex
	n  int
}

func (g *seqIDGen) next(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

func newTestStore() (*MemoryStore, *fakeClock, *seqIDGen) {
	fc := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	gen := &seqIDGen{}
	s := NewMemoryStore(fc.Now, gen.next)
	return s, fc, gen
}

func mustCreateUser(t *testing.T, s *MemoryStore, username string, role model.UserRole) model.User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), model.User{
		Username: username, Role: role, DisplayName: username,
		PasswordHash: "x", PasswordSalt: "y", Iterations: 1,
	})
	if err != nil {
		t.Fatalf("CreateUser %s: %v", username, err)
	}
	return u
}

func TestCreateGetUser(t *testing.T) {
	s, _, _ := newTestStore()
	ctx := context.Background()
	u, err := s.CreateUser(ctx, model.User{Username: "alice", Role: model.RoleResident, DisplayName: "Alice"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if u.CreatedAt.IsZero() || u.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps set")
	}
	got, err := s.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("get by username: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("id mismatch: %s vs %s", got.ID, u.ID)
	}
	if _, err := s.GetUserByID(ctx, u.ID); err != nil {
		t.Errorf("get by id: %v", err)
	}
	if _, err := s.GetUserByUsername(ctx, "nobody"); !model.IsNotFound(err) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestCreateUserDuplicate(t *testing.T) {
	s, _, _ := newTestStore()
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, model.User{Username: "bob", Role: model.RoleResident}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := s.CreateUser(ctx, model.User{Username: "bob", Role: model.RoleResident})
	if !model.IsAlreadyExists(err) {
		t.Errorf("expected already exists, got %v", err)
	}
}

func TestDeleteUserCascadesReads(t *testing.T) {
	s, _, _ := newTestStore()
	ctx := context.Background()
	admin := mustCreateUser(t, s, "admin", model.RoleAdmin)
	res := mustCreateUser(t, s, "res1", model.RoleResident)
	n, _ := s.CreateNotice(ctx, model.Notice{Title: "t", Content: "c", Status: model.StatusPublished, AuthorID: admin.ID})
	if _, err := s.UpsertReadRecord(ctx, model.ReadRecord{UserID: res.ID, NoticeID: n.ID}); err != nil {
		t.Fatalf("upsert read: %v", err)
	}
	if c, _ := s.CountReadRecordsByNotice(ctx, n.ID); c != 1 {
		t.Fatalf("expected 1 read, got %d", c)
	}
	if err := s.DeleteUser(ctx, res.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if c, _ := s.CountReadRecordsByNotice(ctx, n.ID); c != 0 {
		t.Fatalf("expected 0 reads after user delete, got %d", c)
	}
}

func TestNoticeCRUD(t *testing.T) {
	s, _, _ := newTestStore()
	ctx := context.Background()
	admin := mustCreateUser(t, s, "admin", model.RoleAdmin)
	n, err := s.CreateNotice(ctx, model.Notice{Title: "title", Content: "body", Status: model.StatusDraft, AuthorID: admin.ID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if n.Status != model.StatusDraft {
		t.Errorf("expected draft, got %s", n.Status)
	}
	got, err := s.GetNotice(ctx, n.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "title" {
		t.Errorf("title mismatch: %s", got.Title)
	}
	list, err := s.ListNotices(ctx, model.NoticeFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
}

func TestDeleteNoticeCascadesReads(t *testing.T) {
	s, _, _ := newTestStore()
	ctx := context.Background()
	admin := mustCreateUser(t, s, "admin", model.RoleAdmin)
	res := mustCreateUser(t, s, "res1", model.RoleResident)
	n, _ := s.CreateNotice(ctx, model.Notice{Title: "t", Content: "c", Status: model.StatusPublished, AuthorID: admin.ID})
	_, _ = s.UpsertReadRecord(ctx, model.ReadRecord{UserID: res.ID, NoticeID: n.ID})
	if err := s.DeleteNotice(ctx, n.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if c, _ := s.CountReadRecordsByNotice(ctx, n.ID); c != 0 {
		t.Fatalf("expected 0 reads after notice delete, got %d", c)
	}
	if _, err := s.GetNotice(ctx, n.ID); !model.IsNotFound(err) {
		t.Errorf("expected not found after delete, got %v", err)
	}
}

func TestUpsertReadRecordIdempotent(t *testing.T) {
	s, fc, _ := newTestStore()
	ctx := context.Background()
	admin := mustCreateUser(t, s, "admin", model.RoleAdmin)
	res := mustCreateUser(t, s, "res1", model.RoleResident)
	n, _ := s.CreateNotice(ctx, model.Notice{Title: "t", Content: "c", Status: model.StatusPublished, AuthorID: admin.ID})

	rr1, err := s.UpsertReadRecord(ctx, model.ReadRecord{UserID: res.ID, NoticeID: n.ID})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	fc.Advance(time.Second)
	rr2, err := s.UpsertReadRecord(ctx, model.ReadRecord{UserID: res.ID, NoticeID: n.ID})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if rr1.ID != rr2.ID {
		t.Errorf("expected same record id, %s vs %s", rr1.ID, rr2.ID)
	}
	if !rr2.ReadAt.After(rr1.ReadAt) {
		t.Error("expected ReadAt to advance on re-read")
	}
	if c, _ := s.CountReadRecordsByNotice(ctx, n.ID); c != 1 {
		t.Errorf("expected 1 record, got %d", c)
	}
}

func TestListNoticeOrdering(t *testing.T) {
	s, fc, _ := newTestStore()
	ctx := context.Background()
	admin := mustCreateUser(t, s, "admin", model.RoleAdmin)
	// n1: 普通, 早发布
	n1, _ := s.CreateNotice(ctx, model.Notice{Title: "n1", Content: "c", Status: model.StatusPublished, Priority: 10, AuthorID: admin.ID})
	fc.Advance(time.Minute)
	// n2: 置顶
	n2, _ := s.CreateNotice(ctx, model.Notice{Title: "n2", Content: "c", Status: model.StatusPublished, Priority: 5, Pinned: true, AuthorID: admin.ID})
	fc.Advance(time.Minute)
	// n3: 高优先级
	n3, _ := s.CreateNotice(ctx, model.Notice{Title: "n3", Content: "c", Status: model.StatusPublished, Priority: 90, AuthorID: admin.ID})

	list, err := s.ListNotices(ctx, model.NoticeFilter{Status: model.StatusPublished})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	// 期望顺序：n2(置顶) -> n3(优先级90) -> n1(优先级10, 早发布)
	want := []string{n2.ID, n3.ID, n1.ID}
	for i, w := range want {
		if list[i].ID != w {
			t.Errorf("pos %d: want %s, got %s", i, w, list[i].ID)
		}
	}
}

func TestListNoticeFilter(t *testing.T) {
	s, fc, _ := newTestStore()
	ctx := context.Background()
	admin := mustCreateUser(t, s, "admin", model.RoleAdmin)
	s.CreateNotice(ctx, model.Notice{Title: "draft one", Content: "c", Status: model.StatusDraft, Category: "A", AuthorID: admin.ID})
	fc.Advance(time.Minute)
	s.CreateNotice(ctx, model.Notice{Title: "published one", Content: "c", Status: model.StatusPublished, Category: "B", AuthorID: admin.ID})
	fc.Advance(time.Minute)
	s.CreateNotice(ctx, model.Notice{Title: "published two", Content: "c", Status: model.StatusPublished, Category: "A", AuthorID: admin.ID})

	pub, _ := s.ListNotices(ctx, model.NoticeFilter{Status: model.StatusPublished})
	if len(pub) != 2 {
		t.Errorf("expected 2 published, got %d", len(pub))
	}
	catA, _ := s.ListNotices(ctx, model.NoticeFilter{Category: "A"})
	if len(catA) != 2 {
		t.Errorf("expected 2 category A, got %d", len(catA))
	}
	byKw, _ := s.ListNotices(ctx, model.NoticeFilter{Keyword: "published"})
	if len(byKw) != 2 {
		t.Errorf("expected 2 keyword match, got %d", len(byKw))
	}
}
