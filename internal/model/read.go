package model

import "time"

// ReadRecord 阅读记录实体。
//
// 每个居民对每条通知至多存在一条阅读记录（以 UserID+NoticeID 唯一）。
// ReadAt 表示居民最近一次"打开详情/主动标记已读"的时间。
//
// 已读判定：ReadRecord 存在 且 ReadAt >= Notice.UpdatedAt。
// 当管理员更新通知（UpdatedAt 前移）后，ReadAt < 新 UpdatedAt，
// 已读状态自动变为未读，直到居民再次阅读更新 ReadAt。
type ReadRecord struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	NoticeID  string    `json:"notice_id"`
	ReadAt    time.Time `json:"read_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ReadKey 阅读记录唯一键。
type ReadKey struct {
	UserID   string
	NoticeID string
}

// NoticeReadStatus 单条通知对当前用户的已读状态。
type NoticeReadStatus struct {
	NoticeID string `json:"notice_id"`
	Read     bool   `json:"read"`
	ReadAt   *time.Time `json:"read_at,omitempty"`
}

// NoticeWithReadStatus 带已读状态的通知（居民列表使用）。
type NoticeWithReadStatus struct {
	Notice
	Read bool `json:"read"`
}
