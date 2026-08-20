// Package store 定义社区通知公告系统的数据访问层。
//
// Store 是面向 service 层的抽象接口，MemoryStore 为内存实现，
// FileStore 在内存实现之上叠加 JSON 文件持久化。service 层只依赖 Store 接口，
// 便于替换底层实现（如后续替换为关系型数据库）。
package store

import (
	"context"

	"go01-community-notice/internal/model"
)

// Store 数据访问接口。
//
// 所有方法返回赋值后的实体副本（含生成的 ID），调用方应使用返回值而非传入值。
// 所有方法应在 context 取消时尽快返回（内存实现通过显式检查实现）。
type Store interface {
	// ---- 用户 ----

	// CreateUser 创建用户，返回带 ID 与时间戳的实体。
	CreateUser(ctx context.Context, u model.User) (model.User, error)
	// GetUserByUsername 按用户名查询。
	GetUserByUsername(ctx context.Context, username string) (model.User, error)
	// GetUserByID 按 ID 查询。
	GetUserByID(ctx context.Context, id string) (model.User, error)
	// ListUsers 列出所有用户，可按角色过滤。
	ListUsers(ctx context.Context, role model.UserRole) ([]model.User, error)
	// UpdateUser 更新用户（角色、显示名、口令等），按 ID 匹配。
	UpdateUser(ctx context.Context, u model.User) (model.User, error)
	// DeleteUser 删除用户，并级联删除其阅读记录。
	DeleteUser(ctx context.Context, id string) error

	// ---- 通知 ----

	// CreateNotice 创建通知。
	CreateNotice(ctx context.Context, n model.Notice) (model.Notice, error)
	// GetNotice 按 ID 查询。
	GetNotice(ctx context.Context, id string) (model.Notice, error)
	// ListNotices 列出通知，支持过滤。
	ListNotices(ctx context.Context, f model.NoticeFilter) ([]model.Notice, error)
	// UpdateNotice 更新通知。
	UpdateNotice(ctx context.Context, n model.Notice) (model.Notice, error)
	// SetNoticePinned 仅切换置顶位。不前移 UpdatedAt、不改变 PublishAt，
	// 因此不会使已读记录失效（置顶是展示属性，不构成"内容更新"）。
	SetNoticePinned(ctx context.Context, id string, pinned bool) (model.Notice, error)
	// DeleteNotice 删除通知，并级联删除其阅读记录。
	DeleteNotice(ctx context.Context, id string) error

	// ---- 阅读记录 ----

	// UpsertReadRecord 插入或更新阅读记录（按 UserID+NoticeID 唯一）。
	UpsertReadRecord(ctx context.Context, rr model.ReadRecord) (model.ReadRecord, error)
	// GetReadRecord 查询单条阅读记录。
	GetReadRecord(ctx context.Context, userID, noticeID string) (model.ReadRecord, error)
	// ListReadRecordsByUser 列出某用户的所有阅读记录。
	ListReadRecordsByUser(ctx context.Context, userID string) ([]model.ReadRecord, error)
	// CountReadRecordsByNotice 统计某通知的阅读记录数。
	CountReadRecordsByNotice(ctx context.Context, noticeID string) (int, error)
	// DeleteReadRecordsByNotice 删除某通知的所有阅读记录。
	DeleteReadRecordsByNotice(ctx context.Context, noticeID string) error
	// DeleteReadRecordsByUser 删除某用户的所有阅读记录。
	DeleteReadRecordsByUser(ctx context.Context, userID string) error
}

// assert that MemoryStore will implement Store (compile-time check).
var _ Store = (*MemoryStore)(nil)
