package handler

import (
	"net/http"

	"go01-community-notice/internal/respond"
)

// GlobalStats 全局统计（管理员）。GET /api/stats
func (h *Handler) GlobalStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.services.Stats.Global(r.Context())
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	respond.OK(w, stats)
}

// NoticeStats 单条通知阅读统计（管理员）。GET /api/stats/notices/{id}
func (h *Handler) NoticeStats(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if id == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "缺少通知 id")
		return
	}
	stats, err := h.services.Stats.NoticeByID(r.Context(), id)
	if err != nil {
		respond.DomainError(w, err)
		return
	}
	respond.OK(w, stats)
}
