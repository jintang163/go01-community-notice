package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go01-community-notice/internal/model"
	"go01-community-notice/internal/respond"
	"go01-community-notice/internal/service"
)

// maxBodySize 请求体最大字节数，防止超大请求耗尽内存。
const maxBodySize = 1 << 20 // 1 MiB

// bearerPrefix Authorization 头前缀。
const bearerPrefix = "Bearer "

// extractBearer 从 Authorization 头提取 Bearer token。
func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" || !strings.HasPrefix(h, bearerPrefix) {
		return ""
	}
	return strings.TrimSpace(h[len(bearerPrefix):])
}

// decodeJSON 解码请求体 JSON 到 dst，带大小限制与基本校验。
// 解码失败时已写入错误响应，返回 false，调用方应直接 return。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		switch {
		case errors.Is(err, io.EOF):
			respond.Error(w, http.StatusBadRequest, "bad_request", "请求体为空")
		case strings.Contains(err.Error(), "unknown field"):
			respond.Error(w, http.StatusBadRequest, "bad_request", "请求包含未知字段: "+err.Error())
		default:
			respond.Error(w, http.StatusBadRequest, "bad_request", "请求体格式错误: "+err.Error())
		}
		return false
	}
	// 拒绝尾随数据（多个 JSON 值）。
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		respond.Error(w, http.StatusBadRequest, "bad_request", "请求体只能包含一个 JSON 对象")
		return false
	}
	return true
}

// queryStr 取查询参数，去空白。
func queryStr(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

// queryInt 取整数查询参数，缺省返回 def，非法返回 def。
func queryInt(r *http.Request, key string, def int) int {
	s := queryStr(r, key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

// queryBool 取布尔查询参数，缺省返回 def。
func queryBool(r *http.Request, key string, def bool) bool {
	s := strings.ToLower(queryStr(r, key))
	switch s {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return def
	}
}

// pathID 取 {id} 路径参数。
func pathID(r *http.Request) string {
	return r.PathValue("id")
}

// userFrom 请求 context 取当前用户（已过 RequireAuth）。
func userFrom(r *http.Request) model.User {
	u, _ := service.UserFromContext(r.Context())
	return u
}
