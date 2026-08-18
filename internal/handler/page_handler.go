package handler

import (
	"io/fs"
	"net/http"
	"strings"

	"go01-community-notice/internal/respond"
)

// registerPageRoutes 注册前端页面与静态资源路由。
//
// 页面由 embed 的静态文件系统提供；若 assets 为 nil 则跳过。
// /             -> 重定向到 /login
// /login        -> 登录页
// /admin        -> 管理员后台
// /resident     -> 居民首页
// /notices/{id} -> 通知详情页
// /static/*     -> 静态资源
func (h *Handler) registerPageRoutes(mux *http.ServeMux) {
	if h.assets == nil {
		return
	}

	// 静态资源：/static/ 下直接服务文件。
	staticFS, err := fs.Sub(h.assets, "static")
	if err == nil {
		mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	}

	// 页面路由：返回对应 HTML 文件。
	mux.HandleFunc("GET /", h.servePage("index.html"))
	mux.HandleFunc("GET /login", h.servePage("login.html"))
	mux.HandleFunc("GET /admin", h.servePage("admin.html"))
	mux.HandleFunc("GET /resident", h.servePage("resident.html"))
	mux.HandleFunc("GET /notices/{id}", h.servePage("notice.html"))
}

// servePage 返回一个处理器，从 assets 读取指定 HTML 文件并写入。
func (h *Handler) servePage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// "/" 重定向到 /login（未登录友好入口）。
		if r.URL.Path == "/" && name == "index.html" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		data, err := fs.ReadFile(h.assets, name)
		if err != nil {
			respond.Error(w, http.StatusNotFound, "not_found", "页面不存在")
			return
		}
		w.Header().Set("Content-Type", contentTypeForHTML(name))
		_, _ = w.Write(data)
	}
}

// contentTypeForHTML 根据扩展名返回 Content-Type。
func contentTypeForHTML(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	default:
		return "text/plain; charset=utf-8"
	}
}
