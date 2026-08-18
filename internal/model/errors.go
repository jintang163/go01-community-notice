package model

import "errors"

// 领域错误。HTTP 层根据 Code 映射 HTTP 状态码（见 internal/handler/errors.go）。
var (
	// ErrNotFound 通用未找到。
	ErrNotFound = errors.New("resource not found")
	// ErrAlreadyExists 资源已存在（如用户名重复）。
	ErrAlreadyExists = errors.New("resource already exists")
	// ErrUnauthorized 未登录或会话失效。
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden 已登录但无权限。
	ErrForbidden = errors.New("forbidden")
	// ErrInvalidCredentials 用户名或口令错误。
	ErrInvalidCredentials = errors.New("invalid username or password")
	// ErrValidation 通用校验错误。
	ErrValidation = errors.New("validation error")
	// ErrConflict 状态冲突（如对已删除/已发布对象做不合法操作）。
	ErrConflict = errors.New("conflict")
	// ErrInternal 内部错误。
	ErrInternal = errors.New("internal error")

	// 具体校验错误。
	ErrInvalidUsername    = errors.New("invalid username: 3-32 letters, digits or underscore")
	ErrInvalidPassword    = errors.New("invalid password: 6-64 characters")
	ErrInvalidRole        = errors.New("invalid role: must be admin or resident")
	ErrInvalidDisplayName = errors.New("invalid display name: max 32 characters")
	ErrInvalidTitle       = errors.New("invalid title: 1-200 characters")
	ErrInvalidContent     = errors.New("invalid content: 1-20000 characters")
	ErrInvalidPriority    = errors.New("invalid priority: 0-999")
	ErrInvalidCategory    = errors.New("invalid category: max 32 characters")
	ErrInvalidStatus      = errors.New("invalid status: must be draft or published")
)

// IsNotFound 判断错误是否为"未找到"类。
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// IsAlreadyExists 判断错误是否为"已存在"类。
func IsAlreadyExists(err error) bool { return errors.Is(err, ErrAlreadyExists) }

// IsUnauthorized 判断错误是否为"未授权"类。
func IsUnauthorized(err error) bool { return errors.Is(err, ErrUnauthorized) }

// IsForbidden 判断错误是否为"禁止访问"类。
func IsForbidden(err error) bool { return errors.Is(err, ErrForbidden) }

// IsInvalidCredentials 判断错误是否为"凭据无效"类。
func IsInvalidCredentials(err error) bool { return errors.Is(err, ErrInvalidCredentials) }

// IsConflict 判断错误是否为"状态冲突"类。
func IsConflict(err error) bool { return errors.Is(err, ErrConflict) }

// IsValidation 判断错误是否为校验类错误。
func IsValidation(err error) bool {
	switch {
	case errors.Is(err, ErrValidation),
		errors.Is(err, ErrInvalidUsername),
		errors.Is(err, ErrInvalidPassword),
		errors.Is(err, ErrInvalidRole),
		errors.Is(err, ErrInvalidDisplayName),
		errors.Is(err, ErrInvalidTitle),
		errors.Is(err, ErrInvalidContent),
		errors.Is(err, ErrInvalidPriority),
		errors.Is(err, ErrInvalidCategory),
		errors.Is(err, ErrInvalidStatus):
		return true
	}
	return false
}
