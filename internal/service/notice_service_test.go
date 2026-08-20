package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"go01-community-notice/internal/model"
)

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

// TestPinningViaUpdateKeepsItRead 置顶同样可经 PUT {pinned} 完成：
// 该路径也不应让未改动内容的已读居民变未读，且不应前移 UpdatedAt。
func TestPinningViaUpdateKeepsItRead(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "停电提醒")
	if err := env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	before := n.UpdatedAt
	env.clock.Advance(time.Minute)

	pin := true
	updated, err := env.svc.Notice.Update(ctx, n.ID, model.UpdateNoticeRequest{Pinned: &pin}, env.admin)
	if err != nil {
		t.Fatalf("pin via update: %v", err)
	}
	if !updated.Pinned {
		t.Fatal("expected pinned")
	}
	if !updated.UpdatedAt.Equal(before) {
		t.Errorf("pinning via update advanced UpdatedAt: before=%s after=%s", before, updated.UpdatedAt)
	}
	if read, err := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID); err != nil {
		t.Fatalf("read status after pin: %v", err)
	} else if !read {
		t.Fatal("pinning via update should not make an unchanged notice unread")
	}

	// 取消置顶同样只改展示属性，不应使已读失效。
	env.clock.Advance(time.Minute)
	unpin := false
	updated, err = env.svc.Notice.Update(ctx, n.ID, model.UpdateNoticeRequest{Pinned: &unpin}, env.admin)
	if err != nil {
		t.Fatalf("unpin via update: %v", err)
	}
	if updated.Pinned {
		t.Fatal("expected unpinned")
	}
	if !updated.UpdatedAt.Equal(before) {
		t.Errorf("unpinning via update advanced UpdatedAt: before=%s after=%s", before, updated.UpdatedAt)
	}
	if read, err := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID); err != nil {
		t.Fatalf("read status after unpin: %v", err)
	} else if !read {
		t.Fatal("unpinning via update should not make an unchanged notice unread")
	}
}

// TestUpdatePinnedWithContentAdvancesUpdatedAt 内容变更与置顶同时提交时，
// 以内容更新为主：前移 UpdatedAt 使已读失效（保留正常"更新即未读"语义）。
func TestUpdatePinnedWithContentAdvancesUpdatedAt(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "停气提醒")
	if err := env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	before := n.UpdatedAt
	env.clock.Advance(time.Minute)

	pin, newTitle := true, "停气紧急提醒"
	updated, err := env.svc.Notice.Update(ctx, n.ID, model.UpdateNoticeRequest{Pinned: &pin, Title: &newTitle}, env.admin)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !updated.Pinned || updated.Title != "停气紧急提醒" {
		t.Fatalf("expected pinned and retitled, got pinned=%v title=%q", updated.Pinned, updated.Title)
	}
	if !updated.UpdatedAt.After(before) {
		t.Fatal("content change should advance UpdatedAt")
	}
	if read, err := env.svc.Read.IsRead(ctx, env.resident.ID, n.ID); err != nil {
		t.Fatalf("read status: %v", err)
	} else if read {
		t.Fatal("content change should invalidate prior read")
	}
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
