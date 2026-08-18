package model

import (
	"strings"
	"time"
)

// NoticeStatus 通知状态。
type NoticeStatus string

const (
	// StatusDraft 草稿：仅管理员可见，居民访问返回 404。
	StatusDraft NoticeStatus = "draft"
	// StatusPublished 已发布：居民可见，参与已读/未读判定。
	StatusPublished NoticeStatus = "published"
)

// IsValid 校验通知状态是否合法。
func (s NoticeStatus) IsValid() bool {
	return s == StatusDraft || s == StatusPublished
}

const (
	// PriorityLow 低优先级。
	PriorityLow = 10
	// PriorityNormal 普通优先级。
	PriorityNormal = 50
	// PriorityHigh 高优先级。
	PriorityHigh = 80
	// PriorityUrgent 紧急优先级。
	PriorityUrgent = 99
)

// Notice 通知公告实体。
//
// UpdatedAt 是"已读/未读"判定的基准：居民已读当且仅当
// 存在阅读记录 ReadRecord.ReadAt >= Notice.UpdatedAt。
// 管理员更新通知会前移 UpdatedAt，使历史阅读失效，居民回到未读。
type Notice struct {
	ID         string       `json:"id"`
	Title      string       `json:"title"`
	Content    string       `json:"content"`
	Status     NoticeStatus `json:"status"`
	Priority   int          `json:"priority"`
	Pinned     bool         `json:"pinned"`
	Category   string       `json:"category"`
	AuthorID   string       `json:"author_id"`
	AuthorName string       `json:"author_name,omitempty"`
	PublishAt  *time.Time   `json:"publish_at,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

// IsPublished 是否已发布。
func (n Notice) IsPublished() bool { return n.Status == StatusPublished }

// IsDraft 是否草稿。
func (n Notice) IsDraft() bool { return n.Status == StatusDraft }

// NoticeInput 创建/更新通知的输入参数。
type NoticeInput struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	Priority  int    `json:"priority"`
	Pinned    bool   `json:"pinned"`
	Category  string `json:"category"`
	Status    NoticeStatus `json:"status"`
}

// Validate 校验通知输入。
func (in NoticeInput) Validate(isCreate bool) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Content = strings.TrimSpace(in.Content)
	in.Category = strings.TrimSpace(in.Category)
	if len(in.Title) < 1 || len(in.Title) > 200 {
		return ErrInvalidTitle
	}
	if len(in.Content) < 1 || len(in.Content) > 20000 {
		return ErrInvalidContent
	}
	if in.Priority < 0 || in.Priority > 999 {
		return ErrInvalidPriority
	}
	if len(in.Category) > 32 {
		return ErrInvalidCategory
	}
	if isCreate {
		if in.Status != "" && !in.Status.IsValid() {
			return ErrInvalidStatus
		}
	}
	return nil
}

// NoticeFilter 通知列表查询过滤条件。
type NoticeFilter struct {
	// Status 仅返回指定状态；为空表示不限。居民查询强制为 published。
	Status NoticeStatus `json:"status,omitempty"`
	// Category 按分类精确匹配；为空表示不限。
	Category string `json:"category,omitempty"`
	// Keyword 标题模糊匹配（子串，忽略大小写）。
	Keyword string `json:"keyword,omitempty"`
	// PinnedOnly 仅返回置顶。
	PinnedOnly bool `json:"pinned_only,omitempty"`
	// AuthorID 按作者过滤。
	AuthorID string `json:"author_id,omitempty"`
	// Limit 限制返回数量；<=0 表示默认上限。
	Limit int `json:"limit,omitempty"`
}

// ResidentListOrder 居民列表排序：置顶优先 -> 优先级降序 -> 发布时间倒序。
//
// 返回 true 表示 a 应排在 b 之前。
func ResidentListOrder(a, b Notice) bool {
	if a.Pinned != b.Pinned {
		return a.Pinned // 置顶优先
	}
	if a.Priority != b.Priority {
		return a.Priority > b.Priority // 优先级降序
	}
	var ap, bp time.Time
	if a.PublishAt != nil {
		ap = *a.PublishAt
	}
	if b.PublishAt != nil {
		bp = *b.PublishAt
	}
	return ap.After(bp) // 发布时间倒序
}
