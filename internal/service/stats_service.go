package service

import (
	"context"
	"time"

	"go01-community-notice/internal/model"
	"go01-community-notice/internal/store"
)

// StatsService 统计服务。
type StatsService struct {
	store store.Store
	clock Clock
}

// NewStatsService 创建统计服务。
func NewStatsService(s store.Store, clock Clock) *StatsService {
	return &StatsService{store: s, clock: clock}
}

// Global 全局统计看板（仅管理员语义，权限由 handler 控制）。
func (s *StatsService) Global(ctx context.Context) (model.GlobalStats, error) {
	var st model.GlobalStats
	users, err := s.store.ListUsers(ctx, "")
	if err != nil {
		return st, err
	}
	for _, u := range users {
		st.UserTotal++
		switch u.Role {
		case model.RoleAdmin:
			st.UserAdmin++
		case model.RoleResident:
			st.UserResident++
		}
	}
	notices, err := s.store.ListNotices(ctx, model.NoticeFilter{})
	if err != nil {
		return st, err
	}
	for _, n := range notices {
		st.NoticeTotal++
		switch n.Status {
		case model.StatusDraft:
			st.NoticeDraft++
		case model.StatusPublished:
			st.NoticePublished++
		}
		if n.Pinned {
			st.NoticePinned++
		}
	}
	readTotal, readToday, err := s.currentReadStats(ctx, users, notices)
	if err != nil {
		return st, err
	}
	st.ReadTotal = readTotal
	st.ReadToday = readToday
	return st, nil
}

// currentReadStats 按“阅读时间不早于通知最后更新时间”统计当前有效阅读。
func (s *StatsService) currentReadStats(ctx context.Context, users []model.User, notices []model.Notice) (total, today int, err error) {
	noticeByID := make(map[string]model.Notice, len(notices))
	for _, notice := range notices {
		noticeByID[notice.ID] = notice
	}
	start := s.startOfToday()
	for _, u := range users {
		if !u.IsResident() {
			continue
		}
		reads, err := s.store.ListReadRecordsByUser(ctx, u.ID)
		if err != nil {
			return 0, 0, err
		}
		for _, rr := range reads {
			notice, ok := noticeByID[rr.NoticeID]
			if !ok || rr.ReadAt.Before(notice.UpdatedAt) {
				continue
			}
			total++
			if !rr.ReadAt.Before(start) {
				today++
			}
		}
	}
	return total, today, nil
}

// startOfToday 当天 00:00（注入时钟）。
func (s *StatsService) startOfToday() time.Time {
	now := time.Now()
	if s.clock != nil {
		now = s.clock.Now()
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// NoticeByID 单条通知阅读统计。
func (s *StatsService) NoticeByID(ctx context.Context, noticeID string) (model.NoticeStats, error) {
	notice, err := s.store.GetNotice(ctx, noticeID)
	if err != nil {
		return model.NoticeStats{}, err
	}
	residents, err := s.store.ListUsers(ctx, model.RoleResident)
	if err != nil {
		return model.NoticeStats{}, err
	}
	total := len(residents)
	read := 0
	for _, u := range residents {
		ok, err := s.isRead(ctx, u.ID, notice)
		if err != nil {
			return model.NoticeStats{}, err
		}
		if ok {
			read++
		}
	}
	unread := total - read
	if unread < 0 {
		unread = 0
	}
	rate := 0.0
	if total > 0 {
		rate = float64(read) / float64(total)
	}
	return model.NoticeStats{
		NoticeID:      notice.ID,
		Title:         notice.Title,
		ResidentTotal: total,
		ReadCount:     read,
		UnreadCount:   unread,
		ReadRate:      rate,
	}, nil
}

// isRead 内部判定：ReadAt >= UpdatedAt。
func (s *StatsService) isRead(ctx context.Context, userID string, notice model.Notice) (bool, error) {
	rr, err := s.store.GetReadRecord(ctx, userID, notice.ID)
	if model.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !rr.ReadAt.Before(notice.UpdatedAt), nil
}
