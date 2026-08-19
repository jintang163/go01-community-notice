package model

import "time"

// LoginRequest 登录请求。
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse 登录响应。
type LoginResponse struct {
	Token string           `json:"token"`
	User  AuthUserResponse `json:"user"`
}

// NewLoginResponse 构造仅包含公开用户资料的登录响应。
func NewLoginResponse(token string, user User) LoginResponse {
	return LoginResponse{
		Token: token,
		User:  AuthUserResponse{}.FromUser(user),
	}
}

// AuthUserResponse 当前登录用户响应（脱敏）。
type AuthUserResponse struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Role        UserRole  `json:"role"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// FromUser 由 User 构造脱敏响应。
func (AuthUserResponse) FromUser(u User) AuthUserResponse {
	return AuthUserResponse{
		ID:          u.ID,
		Username:    u.Username,
		Role:        u.Role,
		DisplayName: u.DisplayName,
		CreatedAt:   u.CreatedAt,
	}
}

// NoticeResponse 通知详情响应（脱敏，含作者名）。
type NoticeResponse struct {
	Notice
	AuthorName string `json:"author_name"`
}

// NoticeListItem 列表项。
type NoticeListItem struct {
	ID         string       `json:"id"`
	Title      string       `json:"title"`
	Status     NoticeStatus `json:"status"`
	Priority   int          `json:"priority"`
	Pinned     bool         `json:"pinned"`
	Category   string       `json:"category"`
	AuthorName string       `json:"author_name"`
	PublishAt  *time.Time   `json:"publish_at,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
	Read       bool         `json:"read,omitempty"`
}

// ToListItem 由 Notice 构造列表项。
func ToListItem(n Notice, authorName string, read bool) NoticeListItem {
	return NoticeListItem{
		ID:         n.ID,
		Title:      n.Title,
		Status:     n.Status,
		Priority:   n.Priority,
		Pinned:     n.Pinned,
		Category:   n.Category,
		AuthorName: authorName,
		PublishAt:  n.PublishAt,
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
		Read:       read,
	}
}

// CreateNoticeRequest 创建通知请求。
type CreateNoticeRequest struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	Priority int    `json:"priority"`
	Pinned   bool   `json:"pinned"`
	Category string `json:"category"`
	Status   NoticeStatus `json:"status"`
}

// ToInput 转为 NoticeInput。
func (r CreateNoticeRequest) ToInput() NoticeInput {
	return NoticeInput{
		Title:    r.Title,
		Content:  r.Content,
		Priority: r.Priority,
		Pinned:   r.Pinned,
		Category: r.Category,
		Status:   r.Status,
	}
}

// UpdateNoticeRequest 更新通知请求。所有字段可选；指针为 nil 表示不改。
type UpdateNoticeRequest struct {
	Title    *string `json:"title,omitempty"`
	Content  *string `json:"content,omitempty"`
	Priority *int    `json:"priority,omitempty"`
	Pinned   *bool   `json:"pinned,omitempty"`
	Category *string `json:"category,omitempty"`
}

// ReadStatusResponse 已读状态响应。
type ReadStatusResponse struct {
	NoticeID string `json:"notice_id"`
	Read     bool   `json:"read"`
}

// UnreadCountResponse 未读数量响应。
type UnreadCountResponse struct {
	Unread int `json:"unread"`
	Total  int `json:"total"`
}

// GlobalStats 全局统计看板。
type GlobalStats struct {
	NoticeTotal     int `json:"notice_total"`
	NoticeDraft     int `json:"notice_draft"`
	NoticePublished int `json:"notice_published"`
	NoticePinned    int `json:"notice_pinned"`
	UserTotal       int `json:"user_total"`
	UserAdmin       int `json:"user_admin"`
	UserResident    int `json:"user_resident"`
	ReadTotal       int `json:"read_total"`
	ReadToday       int `json:"read_today"`
}

// NoticeStats 单条通知阅读统计。
type NoticeStats struct {
	NoticeID      string  `json:"notice_id"`
	Title         string  `json:"title"`
	ResidentTotal int     `json:"resident_total"`
	ReadCount     int     `json:"read_count"`
	UnreadCount   int     `json:"unread_count"`
	ReadRate      float64 `json:"read_rate"`
}

// ErrorResponse 统一错误响应。
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// HealthResponse 健康检查响应。
type HealthResponse struct {
	Status string `json:"status"`
	Time   time.Time `json:"time"`
}
