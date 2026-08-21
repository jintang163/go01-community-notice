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
	// SetUserPassword 更新用户口令并自增凭据版本（仅传入新口令哈希三件套）。
	//
	// 与 SetNoticeStatus 同属"只改特定字段"的读-改-写：在写锁内重新读取当前
	// 用户，仅写入新的盐/哈希/迭代次数并自增 CredentialVersion（从当前存储值
	// 自增），保留 ID/用户名/角色/显示名/CreatedAt。凭据版本的自增必须在写锁
	// 内从当前值进行，而非用调用方在并发更新前读到的可能陈旧值自增——否则两位
	// 居民并发改密时各自从同一陈旧基线自增、最终都写入同一版本（"更新丢失"），
	// 落盘的凭据版本与会话颁发版本（RotateCredentials 在各自锁内从当前值自增）
	// 不一致：最终落盘的新口令在 Login 时读到旧版本，被 Create 以
	// ErrCredentialsRotated 拒绝，账号被永久锁在登录流程外。
	SetUserPassword(ctx context.Context, userID, salt, hash string, iterations int) (model.User, error)
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
	// UpdateNoticeMetadata 更新非内容元数据（置顶）。
	UpdateNoticeMetadata(ctx context.Context, n model.Notice) (model.Notice, error)
	// SetNoticeStatus 转换通知状态（发布/下架）。
	//
	// 仅在写锁内把目标状态（及发布时刻）应用到当前存储的通知，其余字段
	// （标题/正文/优先级/分类/作者/置顶/CreatedAt）一律保留当前值。发布/下架
	// 是状态转换，不应回写调用方在并发更新之前读到的整条通知，否则会覆盖
	// 另一位管理员并发保存的正文编辑（"更新丢失"）。状态转换前移 UpdatedAt
	// 以触发"更新即未读"，与 UpdateNotice 的语义一致。
	SetNoticeStatus(ctx context.Context, id string, status model.NoticeStatus) (model.Notice, error)
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
