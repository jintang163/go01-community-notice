// Package model 定义社区通知公告系统的领域模型、DTO 与领域错误。
//
// 本包只包含纯数据结构与方法（构造器、校验），不依赖 store / service / handler 层，
// 也不导入任何第三方库，保证可被任意层引用而不会产生循环依赖。
package model

import (
	"strings"
	"time"
	"unicode/utf8"
)

// UserRole 用户角色类型。
type UserRole string

const (
	// RoleAdmin 管理员：可发布/编辑/删除通知、管理居民账号、查看阅读统计。
	RoleAdmin UserRole = "admin"
	// RoleResident 普通居民：可查看已发布通知、标记已读、查看未读数量。
	RoleResident UserRole = "resident"
)

// IsValid 校验角色是否合法。
func (r UserRole) IsValid() bool {
	return r == RoleAdmin || r == RoleResident
}

// User 用户实体。
//
// 口令不存储明文，而是保存盐值与迭代后的哈希（见 internal/auth.PasswordHasher）。
// ID 由 store 层在创建时统一生成并回填到返回值。
type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	PasswordSalt string `json:"password_salt"`
	Iterations   int    `json:"iterations"`
	// CredentialVersion 凭据版本（口令版本号）。每次修改口令时自增。
	// 与 SessionManager 内的会话颁发版本对照：Login 读取用户时随快照一起取得
	// 该版本，Create 在持锁阶段与当前版本比较，不符则拒绝建会话。这样"改密前
	// 发起、改密后才执行到 Create"的旧口令登录携带的是改密前的版本，与已自增
	// 的当前版本不符而被拒绝；而改密后用新口令发起的登录读到的是新版本，可正常
	// 建立会话。
	CredentialVersion uint64    `json:"credential_version"`
	Role              UserRole  `json:"role"`
	DisplayName       string    `json:"display_name"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// IsAdmin 是否管理员。
func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// IsResident 是否居民。
func (u User) IsResident() bool { return u.Role == RoleResident }

// UserInput 创建用户的输入参数，供服务层校验。
type UserInput struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Role        UserRole `json:"role"`
	DisplayName string   `json:"display_name"`
}

// Validate 校验创建用户输入。返回领域错误，错误 Code 用于 HTTP 层映射状态码。
func (in UserInput) Validate() error {
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if !isValidUsername(in.Username) {
		return ErrInvalidUsername
	}
	if len(in.Password) < 6 || len(in.Password) > 64 {
		return ErrInvalidPassword
	}
	if !in.Role.IsValid() {
		return ErrInvalidRole
	}
	if utf8.RuneCountInString(in.DisplayName) > 32 {
		return ErrInvalidDisplayName
	}
	return nil
}

// isValidUsername 用户名：3-32 字符，仅字母/数字/下划线。
func isValidUsername(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}

// UserFilter 用户列表查询过滤条件（目前仅用于后台展示）。
type UserFilter struct {
	Role UserRole `json:"role,omitempty"`
}

// UserStats 单用户的阅读统计。
type UserStats struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	ReadCount   int    `json:"read_count"`
}
