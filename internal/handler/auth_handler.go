package handler

import (
	"net/http"
	"strings"

	"go01-community-notice/internal/model"
	"go01-community-notice/internal/respond"
)

// Login 登录。POST /api/auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req model.LoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "用户名与口令不能为空")
		return
	}
	token, user, err := h.services.Auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if model.IsInvalidCredentials(err) {
			respond.Error(w, http.StatusUnauthorized, "invalid_credentials", "用户名或口令错误")
			return
		}
		respond.DomainError(w, err)
		return
	}
	respond.OK(w, model.LoginResponse{
		Token: token,
		User:  user,
	})
}

// Logout 登出。POST /api/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	token := extractBearer(r)
	h.services.Auth.Logout(token)
	respond.NoContent(w)
}

// Me 当前登录用户。GET /api/auth/me
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	respond.OK(w, model.AuthUserResponse{}.FromUser(u))
}

// ListUsers 用户列表（管理员）。GET /api/users
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	users, err := h.services.Auth.ListUsers(r.Context(), u)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	out := make([]model.AuthUserResponse, 0, len(users))
	for _, usr := range users {
		out = append(out, model.AuthUserResponse{}.FromUser(usr))
	}
	respond.OK(w, out)
}

// CreateUser 创建用户（管理员）。POST /api/users
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	caller := userFrom(r)
	var in model.UserInput
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := h.services.Auth.CreateUser(r.Context(), in, caller)
	if err != nil {
		if model.IsAlreadyExists(err) {
			respond.Error(w, http.StatusConflict, "already_exists", "用户名已存在")
			return
		}
		respond.DomainError(w, err)
		return
	}
	respond.Created(w, model.AuthUserResponse{}.FromUser(u))
}

// DeleteUser 删除用户（管理员）。DELETE /api/users/{id}
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	caller := userFrom(r)
	id := pathID(r)
	if id == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "缺少用户 id")
		return
	}
	if id == caller.ID {
		respond.Error(w, http.StatusConflict, "conflict", "不能删除当前登录用户")
		return
	}
	if err := h.services.Auth.DeleteUser(r.Context(), id, caller); err != nil {
		respond.DomainError(w, err)
		return
	}
	respond.NoContent(w)
}
