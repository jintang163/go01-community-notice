package handler

import (
	"net/http"

	"go01-community-notice/internal/model"
	"go01-community-notice/internal/respond"
)

// ReadStatus 查询某通知对当前居民的已读状态。GET /api/notices/{id}/read-status
//
// 注意：仅判定，不产生标记已读副作用（避免"查询即已读"）。
func (h *Handler) ReadStatus(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := pathID(r)
	if id == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "缺少通知 id")
		return
	}
	// 校验通知存在；居民访问草稿视为未读的 404。
	notice, err := h.services.Notice.Get(r.Context(), id, u)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	read, err := h.services.Read.IsRead(r.Context(), u.ID, notice.ID)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	respond.OK(w, model.ReadStatusResponse{NoticeID: notice.ID, Read: read})
}

// MarkRead 主动标记已读（幂等）。POST /api/notices/{id}/read
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := pathID(r)
	if id == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "缺少通知 id")
		return
	}
	if err := h.services.Read.MarkRead(r.Context(), u.ID, id); err != nil {
		respond.DomainError(w, err)
		return
	}
	respond.NoContent(w)
}

// MyNotices 我的通知列表（含已读/未读）。GET /api/me/notices
func (h *Handler) MyNotices(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	f := model.NoticeFilter{
		Category:   queryStr(r, "category"),
		Keyword:    queryStr(r, "q"),
		PinnedOnly: queryBool(r, "pinned", false),
		Limit:      queryInt(r, "limit", 0),
	}
	list, err := h.services.Read.ListForResident(r.Context(), u.ID, f)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	out := make([]model.NoticeListItem, 0, len(list))
	for _, item := range list {
		out = append(out, model.ToListItem(item.Notice, h.authorName(r, item.Notice.AuthorID), item.Read))
	}
	respond.OK(w, out)
}

// UnreadCount 我的未读数量。GET /api/me/unread-count
func (h *Handler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	unread, total, err := h.services.Read.UnreadCount(r.Context(), u.ID)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	respond.OK(w, model.UnreadCountResponse{Unread: unread, Total: total})
}
