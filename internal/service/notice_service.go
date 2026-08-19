package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"go01-community-notice/internal/model"
	"go01-community-notice/internal/store"
)

// NoticeService 通知管理服务。
type NoticeService struct {
	store store.Store
	clock Clock
}

// NewNoticeService 创建通知服务。
func NewNoticeService(s store.Store, clock Clock) *NoticeService {
	return &NoticeService{store: s, clock: clock}
}

// Create 创建通知（仅管理员）。默认草稿，除非显式指定 published。
func (n *NoticeService) Create(ctx context.Context, in model.NoticeInput, author model.User) (model.Notice, error) {
	if !author.IsAdmin() {
		return model.Notice{}, model.ErrForbidden
	}
	if err := in.Validate(true); err != nil {
		return model.Notice{}, err
	}
	if in.Status == "" {
		in.Status = model.StatusDraft
	}
	now := n.now()
	draft := model.Notice{
		Title:    in.Title,
		Content:  in.Content,
		Status:   in.Status,
		Priority: in.Priority,
		Pinned:   in.Pinned,
		Category: in.Category,
		AuthorID: author.ID,
	}
	if draft.IsPublished() {
		pa := now
		draft.PublishAt = &pa
	}
	return n.store.CreateNotice(ctx, draft)
}

// Get 获取通知详情。居民访问草稿返回 ErrNotFound（不可见）。
// 不在此处标记已读（由 ReadService.ViewDetail 完成）。
func (n *NoticeService) Get(ctx context.Context, id string, viewer model.User) (model.Notice, error) {
	notice, err := n.store.GetNotice(ctx, id)
	if err != nil {
		return model.Notice{}, err
	}
	if !viewer.IsAdmin() && !notice.IsPublished() {
		// 草稿对居民不可见：返回 404 而非 403，避免泄露存在性。
		return model.Notice{}, model.ErrNotFound
	}
	return notice, nil
}

// List 列出通知。居民强制只看 published；管理员可任意过滤。
func (n *NoticeService) List(ctx context.Context, f model.NoticeFilter, viewer model.User) ([]model.Notice, error) {
	if !viewer.IsAdmin() {
		f.Status = model.StatusPublished // 居民仅可见已发布
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 500
	}
	return n.store.ListNotices(ctx, f)
}

// Update 更新通知（仅管理员）。前移 UpdatedAt 使历史已读失效（"更新即未读"）。
func (n *NoticeService) Update(ctx context.Context, id string, req model.UpdateNoticeRequest, editor model.User) (model.Notice, error) {
	if !editor.IsAdmin() {
		return model.Notice{}, model.ErrForbidden
	}
	notice, err := n.store.GetNotice(ctx, id)
	if err != nil {
		return model.Notice{}, err
	}
	// 应用变更。
	changed := false
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if utf8.RuneCountInString(t) < 1 || utf8.RuneCountInString(t) > 200 {
			return model.Notice{}, model.ErrInvalidTitle
		}
		if t != notice.Title {
			notice.Title = t
			changed = true
		}
	}
	if req.Content != nil {
		c := strings.TrimSpace(*req.Content)
		if utf8.RuneCountInString(c) < 1 || utf8.RuneCountInString(c) > 20000 {
			return model.Notice{}, model.ErrInvalidContent
		}
		if c != notice.Content {
			notice.Content = c
			changed = true
		}
	}
	if req.Priority != nil {
		if *req.Priority < 0 || *req.Priority > 999 {
			return model.Notice{}, model.ErrInvalidPriority
		}
		if *req.Priority != notice.Priority {
			notice.Priority = *req.Priority
			changed = true
		}
	}
	if req.Pinned != nil {
		if *req.Pinned != notice.Pinned {
			notice.Pinned = *req.Pinned
			changed = true
		}
	}
	if req.Category != nil {
		c := strings.TrimSpace(*req.Category)
		if utf8.RuneCountInString(c) > 32 {
			return model.Notice{}, model.ErrInvalidCategory
		}
		if c != notice.Category {
			notice.Category = c
			changed = true
		}
	}
	if !changed {
		// 无任何字段，返回当前（不前移 UpdatedAt，避免无意义使已读失效）。
		return notice, nil
	}
	// UpdateNotice 在 store 内会前移 UpdatedAt。
	return n.store.UpdateNotice(ctx, notice)
}

// Delete 删除通知（仅管理员）。store 内级联清理阅读记录。
func (n *NoticeService) Delete(ctx context.Context, id string, caller model.User) error {
	if !caller.IsAdmin() {
		return model.ErrForbidden
	}
	// 先校验存在性，给出明确错误。
	if _, err := n.store.GetNotice(ctx, id); err != nil {
		return err
	}
	return n.store.DeleteNotice(ctx, id)
}

// Publish 发布通知（草稿 -> 已发布）。
func (n *NoticeService) Publish(ctx context.Context, id string, caller model.User) (model.Notice, error) {
	if !caller.IsAdmin() {
		return model.Notice{}, model.ErrForbidden
	}
	notice, err := n.store.GetNotice(ctx, id)
	if err != nil {
		return model.Notice{}, err
	}
	if notice.IsPublished() {
		return model.Notice{}, model.ErrConflict
	}
	notice.Status = model.StatusPublished
	// 发布前移 UpdatedAt（同时设置 PublishAt）——保证已读基准从发布时刻起算。
	// store.UpdateNotice 会设置 PublishAt = UpdatedAt。
	return n.store.UpdateNotice(ctx, notice)
}

// Unpublish 下架通知（已发布 -> 草稿）。
func (n *NoticeService) Unpublish(ctx context.Context, id string, caller model.User) (model.Notice, error) {
	if !caller.IsAdmin() {
		return model.Notice{}, model.ErrForbidden
	}
	notice, err := n.store.GetNotice(ctx, id)
	if err != nil {
		return model.Notice{}, err
	}
	if notice.IsDraft() {
		return model.Notice{}, model.ErrConflict
	}
	notice.Status = model.StatusDraft
	notice.PublishAt = nil
	return n.store.UpdateNotice(ctx, notice)
}

// TogglePin 切换置顶状态。
func (n *NoticeService) TogglePin(ctx context.Context, id string, caller model.User) (model.Notice, error) {
	if !caller.IsAdmin() {
		return model.Notice{}, model.ErrForbidden
	}
	notice, err := n.store.GetNotice(ctx, id)
	if err != nil {
		return model.Notice{}, err
	}
	notice.Pinned = !notice.Pinned
	return n.store.UpdateNotice(ctx, notice)
}

// now 注入时钟。
func (n *NoticeService) now() time.Time {
	if n.clock == nil {
		return time.Now()
	}
	return n.clock.Now()
}
