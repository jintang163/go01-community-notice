package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"go01-community-notice/internal/model"
	"go01-community-notice/internal/store"
)

type failingStatsStore struct { store.Store }
func (f failingStatsStore) ListReadRecordsByUser(context.Context, string) ([]model.ReadRecord, error) {
	return nil, errors.New("read statistics unavailable")
}

func TestStatsGlobal(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	// admin + 1 resident 已存在；再创建若干通知。
	env.svc.Notice.Create(ctx, model.NoticeInput{Title: "d1", Content: "c"}, env.admin)                  // draft
	n2 := env.createPublishedNotice(t, "p1")                                                              // published
	env.svc.Read.MarkRead(ctx, env.resident.ID, n2.ID)                                                     // 1 read

	stats, err := env.svc.Stats.Global(ctx)
	if err != nil {
		t.Fatalf("global stats: %v", err)
	}
	if stats.NoticeTotal != 2 {
		t.Errorf("notice_total: %d", stats.NoticeTotal)
	}
	if stats.NoticeDraft != 1 {
		t.Errorf("notice_draft: %d", stats.NoticeDraft)
	}
	if stats.NoticePublished != 1 {
		t.Errorf("notice_published: %d", stats.NoticePublished)
	}
	if stats.UserResident != 1 {
		t.Errorf("user_resident: %d", stats.UserResident)
	}
	if stats.UserAdmin != 1 {
		t.Errorf("user_admin: %d", stats.UserAdmin)
	}
	if stats.ReadTotal != 1 {
		t.Errorf("read_total: %d", stats.ReadTotal)
	}
}

func TestStatsNoticeByID(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "统计测试")
	// 一个居民已读。
	env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID)
	stats, err := env.svc.Stats.NoticeByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("notice stats: %v", err)
	}
	if stats.ResidentTotal != 1 {
		t.Errorf("resident_total: %d", stats.ResidentTotal)
	}
	if stats.ReadCount != 1 {
		t.Errorf("read_count: %d", stats.ReadCount)
	}
	if stats.UnreadCount != 0 {
		t.Errorf("unread_count: %d", stats.UnreadCount)
	}
	if stats.ReadRate != 1.0 {
		t.Errorf("read_rate: %f", stats.ReadRate)
	}
}

func TestStatsNoticeAfterUpdate(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "更新统计")
	env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID)
	// 推进时间后更新通知 -> 居民回到未读 -> 未读数应为 1。
	env.clock.Advance(time.Minute)
	newContent := "更新后的内容"
	env.svc.Notice.Update(ctx, n.ID, model.UpdateNoticeRequest{Content: &newContent}, env.admin)
	stats, _ := env.svc.Stats.NoticeByID(ctx, n.ID)
	if stats.ReadCount != 0 {
		t.Errorf("expected 0 read after update, got %d", stats.ReadCount)
	}
	if stats.UnreadCount != 1 {
		t.Errorf("expected 1 unread after update, got %d", stats.UnreadCount)
	}
}

// TestStatsNoticeByIDDraftUnavailable 草稿未对居民发布，不存在居民阅读统计。
func TestStatsNoticeByIDDraftUnavailable(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	// 草稿（默认 status 为 draft）。
	d, err := env.svc.Notice.Create(ctx, model.NoticeInput{
		Title: "待确认的停水安排", Content: "尚未对居民发布",
	}, env.admin)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := env.svc.Stats.NoticeByID(ctx, d.ID); !model.IsNotFound(err) {
		t.Fatalf("expected not found for draft stats, got %v", err)
	}
}

// TestStatsNoticeByIDAfterPublishUnpublish 同一通知发布后可用、下架回草稿后不可用。
func TestStatsNoticeByIDAfterPublishUnpublish(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	d, err := env.svc.Notice.Create(ctx, model.NoticeInput{
		Title: "待发布通知", Content: "内容",
	}, env.admin)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := env.svc.Stats.NoticeByID(ctx, d.ID); !model.IsNotFound(err) {
		t.Fatalf("draft stats should be unavailable, got %v", err)
	}
	// 发布后统计可用。
	published, err := env.svc.Notice.Publish(ctx, d.ID, env.admin)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	stats, err := env.svc.Stats.NoticeByID(ctx, published.ID)
	if err != nil {
		t.Fatalf("published stats: %v", err)
	}
	if stats.ResidentTotal != 1 || stats.ReadCount != 0 || stats.UnreadCount != 1 {
		t.Errorf("published stats unexpected: %+v", stats)
	}
	// 下架回草稿后再次不可用。
	if _, err := env.svc.Notice.Unpublish(ctx, published.ID, env.admin); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	if _, err := env.svc.Stats.NoticeByID(ctx, published.ID); !model.IsNotFound(err) {
		t.Fatalf("unpublished stats should be unavailable, got %v", err)
	}
}

func TestStatsGlobalReportsReadAggregationFailure(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	n := env.createPublishedNotice(t, "统计故障")
	if err := env.svc.Read.MarkRead(ctx, env.resident.ID, n.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	svc := NewStatsService(failingStatsStore{Store: env.store}, env.clock)
	if _, err := svc.Global(ctx); err == nil {
		t.Fatal("expected global statistics to report read aggregation failure")
	}
}
