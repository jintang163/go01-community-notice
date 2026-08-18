package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go01-community-notice/internal/auth"
	"go01-community-notice/internal/model"
	"go01-community-notice/internal/service"
	"go01-community-notice/internal/store"
)

// testEnv for handler 包，构造完整服务（带会话与真实哈希）。
type hTestEnv struct {
	store    *store.MemoryStore
	hasher   *auth.PasswordHasher
	sessions *auth.SessionManager
	svc      *service.Services
	mux      *http.ServeMux
	clock    *hClock
	admin    model.User
	resident model.User
	adminToken  string
	resToken    string
}

type hClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *hClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *hClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type hSeqGen struct {
	mu sync.Mutex
	n  int
}

func (g *hSeqGen) next(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

func newHandlerEnv(t *testing.T) *hTestEnv {
	t.Helper()
	fc := &hClock{t: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)}
	gen := &hSeqGen{}
	st := store.NewMemoryStore(fc.Now, gen.next)
	hasher := auth.NewPasswordHasher()
	sessions := auth.NewSessionManager(time.Hour)
	svc := service.NewServices(st, hasher, sessions, fc)
	h := New(svc, st, sessions, nil)
	mux := http.NewServeMux()
	h.Routes(mux)

	env := &hTestEnv{store: st, hasher: hasher, sessions: sessions, svc: svc, mux: mux, clock: fc}
	// 创建 admin + resident，并各自登录拿 token。
	salt, hash, iters, _ := hasher.Hash("admin123")
	admin, _ := st.CreateUser(context.Background(), model.User{Username: "admin", Role: model.RoleAdmin, DisplayName: "管理员", PasswordHash: hash, PasswordSalt: salt, Iterations: iters})
	env.admin = admin
	atok, _ := sessions.Create(admin)
	env.adminToken = atok

	salt2, hash2, iters2, _ := hasher.Hash("res123")
	res, _ := st.CreateUser(context.Background(), model.User{Username: "resident", Role: model.RoleResident, DisplayName: "居民", PasswordHash: hash2, PasswordSalt: salt2, Iterations: iters2})
	env.resident = res
	rtok, _ := sessions.Create(res)
	env.resToken = rtok
	return env
}

func (e *hTestEnv) do(method, path string, token string, body any) (*httptest.ResponseRecorder, []byte) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec, rec.Body.Bytes()
}

func TestHandlerHealthz(t *testing.T) {
	env := newHandlerEnv(t)
	rec, _ := env.do("GET", "/healthz", "", nil)
	if rec.Code != 200 {
		t.Fatalf("healthz: %d", rec.Code)
	}
}

func TestHandlerLoginAndMe(t *testing.T) {
	env := newHandlerEnv(t)
	rec, body := env.do("POST", "/api/auth/login", "", model.LoginRequest{Username: "admin", Password: "admin123"})
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, body)
	}
	var resp model.LoginResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected token")
	}
	// /me with token
	rec, body = env.do("GET", "/api/auth/me", resp.Token, nil)
	if rec.Code != 200 {
		t.Fatalf("me: %d", rec.Code)
	}
}

func TestHandlerLoginBadCredentials(t *testing.T) {
	env := newHandlerEnv(t)
	rec, _ := env.do("POST", "/api/auth/login", "", model.LoginRequest{Username: "admin", Password: "wrong"})
	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHandlerCreateNoticeFlow(t *testing.T) {
	env := newHandlerEnv(t)
	// 管理员创建草稿。
	rec, body := env.do("POST", "/api/notices", env.adminToken, model.CreateNoticeRequest{
		Title: "停水通知", Content: "明日停水", Priority: 5, Category: "设施维护",
	})
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, body)
	}
	var n model.Notice
	json.Unmarshal(body, &n)
	if n.Status != model.StatusDraft {
		t.Errorf("expected draft, got %s", n.Status)
	}
	// 发布。
	rec, _ = env.do("POST", "/api/notices/"+n.ID+"/publish", env.adminToken, nil)
	if rec.Code != 200 {
		t.Fatalf("publish: %d", rec.Code)
	}
	// 居民可见。
	rec, body = env.do("GET", "/api/notices/"+n.ID, env.resToken, nil)
	if rec.Code != 200 {
		t.Fatalf("resident get published: %d", rec.Code)
	}
	// 居民访问应已标记已读（ViewDetail）。
	rec, _ = env.do("GET", "/api/notices/"+n.ID+"/read-status", env.resToken, nil)
	if rec.Code != 200 {
		t.Fatalf("read-status: %d", rec.Code)
	}
}

func TestHandlerResidentCannotAccessDraft(t *testing.T) {
	env := newHandlerEnv(t)
	rec, body := env.do("POST", "/api/notices", env.adminToken, model.CreateNoticeRequest{Title: "草稿", Content: "c"})
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, body)
	}
	var n model.Notice
	json.Unmarshal(body, &n)
	// 居民访问草稿应 404。
	rec, _ = env.do("GET", "/api/notices/"+n.ID, env.resToken, nil)
	if rec.Code != 404 {
		t.Errorf("expected 404 for resident on draft, got %d", rec.Code)
	}
}

func TestHandlerResidentCannotCreateNotice(t *testing.T) {
	env := newHandlerEnv(t)
	rec, _ := env.do("POST", "/api/notices", env.resToken, model.CreateNoticeRequest{Title: "t", Content: "c"})
	if rec.Code != 403 {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestHandlerUnauthenticated(t *testing.T) {
	env := newHandlerEnv(t)
	rec, _ := env.do("GET", "/api/notices", "", nil)
	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHandlerUpdateMeansUnreadViaAPI(t *testing.T) {
	env := newHandlerEnv(t)
	// 创建+发布。
	rec, body := env.do("POST", "/api/notices", env.adminToken, model.CreateNoticeRequest{Title: "通知", Content: "原文", Status: model.StatusPublished})
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, body)
	}
	var n model.Notice
	json.Unmarshal(body, &n)
	env.clock.Advance(time.Minute)
	// 居民标记已读。
	env.do("POST", "/api/notices/"+n.ID+"/read", env.resToken, nil)
	rec, body = env.do("GET", "/api/notices/"+n.ID+"/read-status", env.resToken, nil)
	if rec.Code != 200 {
		t.Fatalf("read-status: %d", rec.Code)
	}
	var rs model.ReadStatusResponse
	json.Unmarshal(body, &rs)
	if !rs.Read {
		t.Fatal("expected read before update")
	}
	// 管理员更新 -> 未读。
	env.clock.Advance(time.Minute)
	newContent := "更新后的正文内容"
	rec, _ = env.do("PUT", "/api/notices/"+n.ID, env.adminToken, model.UpdateNoticeRequest{Content: &newContent})
	if rec.Code != 200 {
		t.Fatalf("update: %d", rec.Code)
	}
	rec, body = env.do("GET", "/api/notices/"+n.ID+"/read-status", env.resToken, nil)
	json.Unmarshal(body, &rs)
	if rs.Read {
		t.Error("EXPECTED UNREAD via API after update")
	}
}

func TestHandlerUnreadCount(t *testing.T) {
	env := newHandlerEnv(t)
	// 发布两条。
	for i, title := range []string{"t1", "t2"} {
		rec, body := env.do("POST", "/api/notices", env.adminToken, model.CreateNoticeRequest{Title: title, Content: "c", Status: model.StatusPublished})
		if rec.Code != 201 {
			t.Fatalf("create %d: %d %s", i, rec.Code, body)
		}
	}
	rec, body := env.do("GET", "/api/me/unread-count", env.resToken, nil)
	if rec.Code != 200 {
		t.Fatalf("unread-count: %d", rec.Code)
	}
	var uc model.UnreadCountResponse
	json.Unmarshal(body, &uc)
	if uc.Unread != 2 || uc.Total != 2 {
		t.Errorf("expected 2/2, got %d/%d", uc.Unread, uc.Total)
	}
}

func TestHandlerStatsAdmin(t *testing.T) {
	env := newHandlerEnv(t)
	rec, _ := env.do("GET", "/api/stats", env.adminToken, nil)
	if rec.Code != 200 {
		t.Errorf("stats admin: %d", rec.Code)
	}
	// 居民不可访问统计。
	rec, _ = env.do("GET", "/api/stats", env.resToken, nil)
	if rec.Code != 403 {
		t.Errorf("expected 403 for resident stats, got %d", rec.Code)
	}
}

func TestHandlerBadRequestJSON(t *testing.T) {
	env := newHandlerEnv(t)
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("expected 400 bad json, got %d", rec.Code)
	}
}

func TestHandlerCreateUser(t *testing.T) {
	env := newHandlerEnv(t)
	rec, body := env.do("POST", "/api/users", env.adminToken, model.UserInput{Username: "newres", Password: "pw1234", Role: model.RoleResident, DisplayName: "新"})
	if rec.Code != 201 {
		t.Fatalf("create user: %d %s", rec.Code, body)
	}
	var u model.AuthUserResponse
	json.Unmarshal(body, &u)
	if u.Username != "newres" {
		t.Errorf("username: %s", u.Username)
	}
}

func TestHandlerDeleteNotice(t *testing.T) {
	env := newHandlerEnv(t)
	rec, body := env.do("POST", "/api/notices", env.adminToken, model.CreateNoticeRequest{Title: "t", Content: "c", Status: model.StatusPublished})
	var n model.Notice
	json.Unmarshal(body, &n)
	rec, _ = env.do("DELETE", "/api/notices/"+n.ID, env.adminToken, nil)
	if rec.Code != 204 {
		t.Errorf("delete: %d", rec.Code)
	}
	// 删除后访问 404。
	rec, _ = env.do("GET", "/api/notices/"+n.ID, env.adminToken, nil)
	if rec.Code != 404 {
		t.Errorf("expected 404 after delete, got %d", rec.Code)
	}
}
