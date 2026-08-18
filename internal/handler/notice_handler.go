package handler

import (
	"net/http"
	"strconv"

	"go01-community-notice/internal/model"
	"go01-community-notice/internal/respond"
)

// ListNotices 通知列表。GET /api/notices
// 居民仅见已发布；管理员可通过 status/category/q/pinned/limit 筛选。
func (h *Handler) ListNotices(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	f := model.NoticeFilter{
		Status:     model.NoticeStatus(queryStr(r, "status")),
		Category:   queryStr(r, "category"),
		Keyword:    queryStr(r, "q"),
		AuthorID:   queryStr(r, "author_id"),
		PinnedOnly: queryBool(r, "pinned", false),
		Limit:      queryInt(r, "limit", 0),
	}
	notices, err := h.services.Notice.List(r.Context(), f, u)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	out := make([]model.NoticeListItem, 0, len(notices))
	for _, n := range notices {
		out = append(out, model.ToListItem(n, h.authorName(r, n.AuthorID), false))
	}
	respond.OK(w, out)
}

// GetNotice 通知详情。GET /api/notices/{id}
// 居民访问草稿返回 404；居民访问已发布会标记已读。
func (h *Handler) GetNotice(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := pathID(r)
	if id == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "缺少通知 id")
		return
	}
	notice, err := h.services.Read.ViewDetail(r.Context(), id, u)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	notice.AuthorName = h.authorName(r, notice.AuthorID)
	respond.OK(w, notice)
}

// CreateNotice 创建通知（管理员）。POST /api/notices
func (h *Handler) CreateNotice(w http.ResponseWriter, r *http.Request) {
	caller := userFrom(r)
	var req model.CreateNoticeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	notice, err := h.services.Notice.Create(r.Context(), req.ToInput(), caller)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	notice.AuthorName = h.authorName(r, notice.AuthorID)
	respond.Created(w, notice)
}

// UpdateNotice 更新通知（管理员）。PUT /api/notices/{id}
func (h *Handler) UpdateNotice(w http.ResponseWriter, r *http.Request) {
	caller := userFrom(r)
	id := pathID(r)
	if id == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "缺少通知 id")
		return
	}
	var req model.UpdateNoticeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	notice, err := h.services.Notice.Update(r.Context(), id, req, caller)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	notice.AuthorName = h.authorName(r, notice.AuthorID)
	respond.OK(w, notice)
}

// DeleteNotice 删除通知（管理员）。DELETE /api/notices/{id}
func (h *Handler) DeleteNotice(w http.ResponseWriter, r *http.Request) {
	caller := userFrom(r)
	id := pathID(r)
	if id == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "缺少通知 id")
		return
	}
	if err := h.services.Notice.Delete(r.Context(), id, caller); err != nil {
		respond.DomainError(w, err)
		return
	}
	respond.NoContent(w)
}

// PublishNotice 发布通知（管理员）。POST /api/notices/{id}/publish
func (h *Handler) PublishNotice(w http.ResponseWriter, r *http.Request) {
	caller := userFrom(r)
	id := pathID(r)
	if id == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "缺少通知 id")
		return
	}
	notice, err := h.services.Notice.Publish(r.Context(), id, caller)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	notice.AuthorName = h.authorName(r, notice.AuthorID)
	respond.OK(w, notice)
}

// UnpublishNotice 下架通知（管理员）。POST /api/notices/{id}/unpublish
func (h *Handler) UnpublishNotice(w http.ResponseWriter, r *http.Request) {
	caller := userFrom(r)
	id := pathID(r)
	if id == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "缺少通知 id")
		return
	}
	notice, err := h.services.Notice.Unpublish(r.Context(), id, caller)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	notice.AuthorName = h.authorName(r, notice.AuthorID)
	respond.OK(w, notice)
}

// TogglePin 切换置顶（管理员）。POST /api/notices/{id}/pin
func (h *Handler) TogglePin(w http.ResponseWriter, r *http.Request) {
	caller := userFrom(r)
	id := pathID(r)
	if id == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "缺少通知 id")
		return
	}
	notice, err := h.services.Notice.TogglePin(r.Context(), id, caller)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	notice.AuthorName = h.authorName(r, notice.AuthorID)
	respond.OK(w, notice)
}

// authorName 取作者显示名（取不到返回空）。
func (h *Handler) authorName(r *http.Request, authorID string) string {
	if authorID == "" {
		return ""
	}
	u, err := h.store.GetUserByID(r.Context(), authorID)
	if err != nil {
		return ""
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// parseIntOrDef 解析整数，失败返回默认值（保留以备 handler 复用）。
func parseIntOrDef(s string, def int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
