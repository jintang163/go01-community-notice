package service

import (
	"context"
	"sort"
	"time"

	"go01-community-notice/internal/model"
	"go01-community-notice/internal/store"
)

// ReadService 阅读记录与已读/未读服务。
//
// 核心业务规则：
//   已读 = 存在阅读记录 rr 且 rr.ReadAt >= notice.UpdatedAt
//   管理员更新通知前移 UpdatedAt，使历史阅读失效 -> 居民回到未读。
type ReadService struct {
	store store.Store
	clock Clock
}

// NewReadService 创建阅读服务。
func NewReadService(s store.Store, clock Clock) *ReadService {
	return &ReadService{store: s, clock: clock}
}

// IsRead 判定某通知对某居民是否已读。
func (r *ReadService) IsRead(ctx context.Context, userID, noticeID string) (bool, error) {
	notice, err := r.store.GetNotice(ctx, noticeID)
	if err != nil {
		return false, err
	}
	rr, err := r.store.GetReadRecord(ctx, userID, noticeID)
	if model.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// ReadAt >= UpdatedAt 视为已读。
	return !rr.ReadAt.Before(notice.UpdatedAt), nil
}

// MarkRead 标记已读（幂等）。仅对已发布通知有效；草稿不可标记。
func (r *ReadService) MarkRead(ctx context.Context, userID, noticeID string) error {
	notice, err := r.store.GetNotice(ctx, noticeID)
	if err != nil {
		return err
	}
	if !notice.HasResidentReadState() {
		return model.ErrNotFound
	}
	return r.recordRead(ctx, userID, noticeID)
}

// ViewDetail 居民查看通知详情：校验可见性 + 标记已读 + 返回通知。
func (r *ReadService) ViewDetail(ctx context.Context, noticeID string, viewer model.User) (model.Notice, error) {
	notice, err := r.store.GetNotice(ctx, noticeID)
	if err != nil {
		return model.Notice{}, err
	}
	if !viewer.IsAdmin() && !notice.IsPublished() {
		return model.Notice{}, model.ErrNotFound
	}
	// 仅居民查看已发布通知才标记已读；管理员查看不产生阅读记录。
	if viewer.IsResident() && notice.HasResidentReadState() {
		if err := r.recordRead(ctx, viewer.ID, noticeID); err != nil {
			return model.Notice{}, err
		}
	}
	return notice, nil
}

func (r *ReadService) recordRead(ctx context.Context, userID, noticeID string) error {
	_, err := r.store.UpsertReadRecord(ctx, model.ReadRecord{
		UserID: userID, NoticeID: noticeID, ReadAt: r.now(),
	})
	return err
}

// ListForResident 列出面向某居民的通知（仅已发布），并附带已读状态。
// 排序遵循 ResidentListOrder：置顶 -> 优先级 -> 发布时间倒序。
func (r *ReadService) ListForResident(ctx context.Context, userID string, f model.NoticeFilter) ([]model.NoticeWithReadStatus, error) {
	f.Status = model.StatusPublished
	notices, err := r.store.ListNotices(ctx, f)
	if err != nil {
		return nil, err
	}
	reads, err := r.store.ListReadRecordsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	readAt := make(map[string]time.Time, len(reads))
	for _, rr := range reads {
		readAt[rr.NoticeID] = rr.ReadAt
	}
	out := make([]model.NoticeWithReadStatus, 0, len(notices))
	for _, n := range notices {
		read := false
		if at, ok := readAt[n.ID]; ok {
			read = !at.Before(n.UpdatedAt)
		}
		out = append(out, model.NoticeWithReadStatus{Notice: n, Read: read})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return model.ResidentListOrder(out[i].Notice, out[j].Notice)
	})
	return out, nil
}

// UnreadCount 居民未读通知数量。
// 遍历所有已发布通知，按已读规则统计未读数。
func (r *ReadService) UnreadCount(ctx context.Context, userID string) (unread, total int, err error) {
	notices, err := r.store.ListNotices(ctx, model.NoticeFilter{Status: model.StatusPublished})
	if err != nil {
		return 0, 0, err
	}
	reads, err := r.store.ListReadRecordsByUser(ctx, userID)
	if err != nil {
		return 0, 0, err
	}
	readAt := make(map[string]time.Time, len(reads))
	for _, rr := range reads {
		readAt[rr.NoticeID] = rr.ReadAt
	}
	total = len(notices)
	for _, n := range notices {
		at, ok := readAt[n.ID]
		if !ok || at.Before(n.UpdatedAt) {
			unread++
		}
	}
	return unread, total, nil
}

// now 注入时钟。
func (r *ReadService) now() time.Time {
	if r.clock == nil {
		return time.Now()
	}
	return r.clock.Now()
}
