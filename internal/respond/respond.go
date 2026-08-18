// Package respond 提供 HTTP JSON 响应的通用辅助函数，
// 供 middleware 与 handler 共享，避免重复实现与不一致。
package respond

import (
	"encoding/json"
	"net/http"

	"go01-community-notice/internal/model"
)

// JSON 写入 HTTP JSON 响应（指定状态码与任意 body）。
func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	enc := json.NewEncoder(w)
	_ = enc.Encode(body)
}

// OK 写 200 响应。
func OK(w http.ResponseWriter, body any) {
	JSON(w, http.StatusOK, body)
}

// Created 写 201 响应。
func Created(w http.ResponseWriter, body any) {
	JSON(w, http.StatusCreated, body)
}

// NoContent 写 204 响应。
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Error 写 JSON 错误响应。
func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, model.ErrorResponse{Code: code, Message: message})
}

// DomainError 将领域错误映射为合适的 HTTP 状态码并写入响应。
// 未识别的错误默认 500。
func DomainError(w http.ResponseWriter, err error) {
	switch {
	case model.IsNotFound(err):
		Error(w, http.StatusNotFound, "not_found", err.Error())
	case model.IsUnauthorized(err), model.IsInvalidCredentials(err):
		Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
	case model.IsForbidden(err):
		Error(w, http.StatusForbidden, "forbidden", err.Error())
	case model.IsAlreadyExists(err):
		Error(w, http.StatusConflict, "already_exists", err.Error())
	case errIsConflict(err):
		Error(w, http.StatusConflict, "conflict", err.Error())
	case model.IsValidation(err):
		Error(w, http.StatusBadRequest, "validation_error", err.Error())
	default:
		Error(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

// errIsConflict 判断 errors.Is(err, ErrConflict)。
// 单独函数以避免在文件顶部 import errors 又与 DomainError 重复。
func errIsConflict(err error) bool {
	return model.IsConflict(err)
}
