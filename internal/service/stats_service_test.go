package service

import (
	"context"
	"testing"
	"time"

	"go01-community-notice/internal/model"
)

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
