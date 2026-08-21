package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"go01-community-notice/internal/model"
	"go01-community-notice/internal/store"
)

type delayedMetadataStore struct {
	store.Store
	updateStarted chan struct{}
	allowUpdate   chan struct{}
}

type delayedPublishStore struct {
	store.Store
	publishStarted chan struct{}
	allowPublish   chan struct{}
}

func (s *delayedPublishStore) UpdateNotice(ctx context.Context, notice model.Notice) (model.Notice, error) {
	if notice.IsPublished() {
		close(s.publishStarted)
		select {
		case <-s.allowPublish:
			return s.Store.UpdateNotice(ctx, notice)
		case <-ctx.Done():
			return model.Notice{}, ctx.Err()
		}
	}
	return s.Store.UpdateNotice(ctx, notice)
}

func (s *delayedMetadataStore) UpdateNoticeMetadata(ctx context.Context, notice model.Notice) (model.Notice, error) {
	close(s.updateStarted)
	select {
	case <-s.allowUpdate:
		return s.Store.UpdateNoticeMetadata(ctx, notice)
	case <-ctx.Done():
		return model.Notice{}, ctx.Err()
	}
}

func TestNoticeCreateAcceptsBoundaryLengthLocalizedText(t *testing.T) {
	env := newTestEnv(t)
	n, err := env.svc.Notice.Create(context.Background(), model.NoticeInput{
		Title: strings.Repeat("公", 200), Content: "面向居民的通知内容",
		Category: strings.Repeat("类", 32), Priority: model.PriorityNormal,
	}, env.admin)
	if err != nil {
		t.Fatalf("create localized notice at documented limits: %v", err)
	}
	if n.ID == "" {
		t.Fatal("expected created notice id")
	}
}

func TestNoticeCreateDefaultsDraft(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n, err := env.svc.Notice.Create(ctx, model.NoticeInput{
		Title: "t", Content: "c", Priority: 10,
	}, env.admin)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if n.Status != model.StatusDraft {
		t.Errorf("expected draft by default, got %s", n.Status)
	}
	if n.PublishAt != nil {
		t.Error("draft should not have publish time")
	}
	if n.ID == "" {
		t.Error("expected id set")
	}
}

func TestNoticeCreateResidentForbidden(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.svc.Notice.Create(context.Background(), model.NoticeInput{Title: "t", Content: "c"}, env.resident)
	if !model.IsForbidden(err) {
		t.Errorf("expected forbidden, got %v", err)
	}
}

func TestNoticeCreateValidation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	cases := []model.NoticeInput{
		{Title: "", Content: "c"},            // empty title
		{Title: "t", Content: ""},            // empty content
		{Title: "t", Content: "c", Priority: -1},   // bad priority
		{Title: "t", Content: "c", Priority: 1000}, // bad priority
	}
	for i, in := range cases {
		if _, err := env.svc.Notice.Create(ctx, in, env.admin); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestNoticePublishAndUnpublish(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	draft, _ := env.svc.Notice.Create(ctx, model.NoticeInput{Title: "t", Content: "c"}, env.admin)
	env.clock.Advance(time.Minute)
	published, err := env.svc.Notice.Publish(ctx, draft.ID, env.admin)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !published.IsPublished() || published.PublishAt == nil {
		t.Error("expected published with publish time")
	}
	// 重复发布应冲突。
	if _, err := env.svc.Notice.Publish(ctx, draft.ID, env.admin); !model.IsConflict(err) {
		t.Errorf("expected conflict on re-publish, got %v", err)
	}
	env.clock.Advance(time.Minute)
	draft2, err := env.svc.Notice.Unpublish(ctx, draft.ID, env.admin)
	if err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	if !draft2.IsDraft() || draft2.PublishAt != nil {
		t.Error("expected draft after unpublish")
	}
	// 重复下架应冲突。
	if _, err := env.svc.Notice.Unpublish(ctx, draft.ID, env.admin); !model.IsConflict(err) {
		t.Errorf("expected conflict on re-unpublish, got %v", err)
	}
}

func TestPinningReadNoticeKeepsItRead(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n, err := env.svc.Notice.Create(ctx, model.NoticeInput{Title: "公告", Content: "内容", Status: model.StatusPublished}, env.admin)
	if err != nil { t.Fatalf("create: %v", err) }
	if err := env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID); err != nil { t.Fatalf("mark read: %v", err) }
	env.clock.Advance(time.Minute)
	if _, err := env.svc.Notice.TogglePin(ctx, n.ID, env.admin); err != nil { t.Fatalf("pin: %v", err) }
	read, err := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID)
	if err != nil { t.Fatalf("read status: %v", err) }
	if !read { t.Fatal("pinning should not make an unchanged notice unread") }
}

func TestNoticeTogglePin(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n, _ := env.svc.Notice.Create(ctx, model.NoticeInput{Title: "t", Content: "c"}, env.admin)
	n, _ = env.svc.Notice.TogglePin(ctx, n.ID, env.admin)
	if !n.Pinned {
		t.Error("expected pinned after toggle")
	}
	n, _ = env.svc.Notice.TogglePin(ctx, n.ID, env.admin)
	if n.Pinned {
		t.Error("expected unpinned after second toggle")
	}
}

func TestNoticeUpdateAdvancesUpdatedAt(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n, _ := env.svc.Notice.Create(ctx, model.NoticeInput{Title: "t", Content: "c"}, env.admin)
	before := n.UpdatedAt
	env.clock.Advance(time.Second)
	newTitle := "new title"
	updated, err := env.svc.Notice.Update(ctx, n.ID, model.UpdateNoticeRequest{Title: &newTitle}, env.admin)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !updated.UpdatedAt.After(before) {
		t.Error("expected UpdatedAt to advance after update")
	}
	if updated.Title != "new title" {
		t.Errorf("title not updated: %s", updated.Title)
	}
}

func TestNoticeUpdateSameValuesKeepsResidentReadState(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "停水提醒")
	env.clock.Advance(time.Minute)
	if err := env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	current, err := env.store.GetNotice(ctx, n.ID)
	if err != nil {
		t.Fatalf("get notice: %v", err)
	}
	env.clock.Advance(time.Minute)
	updated, err := env.svc.Notice.Update(ctx, n.ID, model.UpdateNoticeRequest{
		Title:    &current.Title,
		Content:  &current.Content,
		Priority: &current.Priority,
		Pinned:   &current.Pinned,
		Category: &current.Category,
	}, env.admin)
	if err != nil {
		t.Fatalf("update same values: %v", err)
	}
	if !updated.UpdatedAt.Equal(current.UpdatedAt) {
		t.Errorf("same-value update changed timestamp: before=%s after=%s", current.UpdatedAt, updated.UpdatedAt)
	}
	read, err := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID)
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if !read {
		t.Fatal("resident became unread after a same-value update")
	}
}

func TestConcurrentNoticeChangesPreserveBothResults(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	notice := env.createPublishedNotice(t, "社区活动安排")

	controlledStore := &delayedMetadataStore{
		Store:         env.store,
		updateStarted: make(chan struct{}),
		allowUpdate:   make(chan struct{}),
	}
	controlledService := NewNoticeService(controlledStore, env.clock)

	toggleResult := make(chan error, 1)
	go func() {
		_, err := controlledService.TogglePin(ctx, notice.ID, env.admin)
		toggleResult <- err
	}()

	<-controlledStore.updateStarted
	updatedContent := "活动时间调整为本周六下午三点"
	if _, err := env.svc.Notice.Update(ctx, notice.ID, model.UpdateNoticeRequest{Content: &updatedContent}, env.admin); err != nil {
		t.Fatalf("update content: %v", err)
	}
	close(controlledStore.allowUpdate)
	if err := <-toggleResult; err != nil {
		t.Fatalf("toggle pin: %v", err)
	}

	stored, err := env.store.GetNotice(ctx, notice.ID)
	if err != nil {
		t.Fatalf("get notice: %v", err)
	}
	if !stored.Pinned {
		t.Fatal("expected notice to remain pinned")
	}
	if stored.Content != updatedContent {
		t.Fatalf("concurrent content update was lost: got %q, want %q", stored.Content, updatedContent)
	}
}

func TestConcurrentPublishAndEditPreserveBothResults(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	draft, err := env.svc.Notice.Create(ctx, model.NoticeInput{
		Title: "社区活动安排", Content: "活动时间待定",
	}, env.admin)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	controlledStore := &delayedPublishStore{
		Store:          env.store,
		publishStarted: make(chan struct{}),
		allowPublish:   make(chan struct{}),
	}
	publishService := NewNoticeService(controlledStore, env.clock)
	publishResult := make(chan error, 1)
	go func() {
		_, err := publishService.Publish(ctx, draft.ID, env.admin)
		publishResult <- err
	}()

	<-controlledStore.publishStarted
	updatedContent := "活动时间调整为本周六下午三点"
	if _, err := env.svc.Notice.Update(ctx, draft.ID, model.UpdateNoticeRequest{
		Content: &updatedContent,
	}, env.admin); err != nil {
		t.Fatalf("update content: %v", err)
	}
	close(controlledStore.allowPublish)
	if err := <-publishResult; err != nil {
		t.Fatalf("publish: %v", err)
	}

	stored, err := env.store.GetNotice(ctx, draft.ID)
	if err != nil {
		t.Fatalf("get notice: %v", err)
	}
	if !stored.IsPublished() {
		t.Fatal("expected notice to remain published")
	}
	if stored.Content != updatedContent {
		t.Fatalf("concurrent content update was lost: got %q, want %q", stored.Content, updatedContent)
	}
}

func TestNoticeDeleteForbidden(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n, _ := env.svc.Notice.Create(ctx, model.NoticeInput{Title: "t", Content: "c"}, env.admin)
	if err := env.svc.Notice.Delete(ctx, n.ID, env.resident); !model.IsForbidden(err) {
		t.Errorf("expected forbidden, got %v", err)
	}
}

func TestNoticeDeleteNotFound(t *testing.T) {
	env := newTestEnv(t)
	if err := env.svc.Notice.Delete(context.Background(), "n_nope", env.admin); !model.IsNotFound(err) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestNoticeListResidentOnlyPublished(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.svc.Notice.Create(ctx, model.NoticeInput{Title: "draft", Content: "c"}, env.admin)
	env.createPublishedNotice(t, "pub1")
	// 居民列表应只含已发布。
	list, err := env.svc.Notice.List(ctx, model.NoticeFilter{}, env.resident)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 published for resident, got %d", len(list))
	}
	if list[0].Status != model.StatusPublished {
		t.Errorf("expected published, got %s", list[0].Status)
	}
	// 管理员列表应含草稿。
	allList, _ := env.svc.Notice.List(ctx, model.NoticeFilter{}, env.admin)
	if len(allList) != 2 {
		t.Errorf("expected 2 for admin, got %d", len(allList))
	}
}

func TestNoticeUpdateValidation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n, _ := env.svc.Notice.Create(ctx, model.NoticeInput{Title: "t", Content: "c"}, env.admin)
	empty := ""
	if _, err := env.svc.Notice.Update(ctx, n.ID, model.UpdateNoticeRequest{Title: &empty}, env.admin); err == nil {
		t.Error("expected validation error for empty title")
	}
}
