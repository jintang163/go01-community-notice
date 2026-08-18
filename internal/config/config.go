// Package config 从环境变量加载应用配置，提供合理默认值。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 应用配置。
type Config struct {
	Addr            string        // 监听地址
	DataPath        string        // 数据持久化文件路径
	SessionTTL      time.Duration // 会话有效期
	SeedAdmin       bool          // 启动时若无管理员则创建种子管理员
	AdminUsername   string        // 种子管理员用户名
	AdminPassword   string        // 种子管理员口令
	ShutdownTimeout time.Duration // 优雅关闭超时
	ReadTimeout     time.Duration // HTTP 读超时
	WriteTimeout    time.Duration // HTTP 写超时
	CORSOrigins     []string      // CORS 允许来源
}

// Load 从环境变量加载配置。prefix 为环境变量前缀（默认 APP_）。
func Load() (Config, error) {
	get := envOr("APP_", "")
	c := Config{
		Addr:            get("ADDR", ":8080"),
		DataPath:        get("DATA_PATH", "data/store.json"),
		SeedAdmin:       envBool("APP_SEED_ADMIN", true),
		AdminUsername:   get("ADMIN_USERNAME", "admin"),
		AdminPassword:   get("ADMIN_PASSWORD", "admin123"),
		ShutdownTimeout: envDur("APP_SHUTDOWN_TIMEOUT", 10*time.Second),
		ReadTimeout:     envDur("APP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:    envDur("APP_WRITE_TIMEOUT", 15*time.Second),
		CORSOrigins:     envList("APP_CORS_ORIGINS"),
	}
	ttl, err := envDurErr("APP_SESSION_TTL", 24*time.Hour)
	if err != nil {
		return c, fmt.Errorf("parse APP_SESSION_TTL: %w", err)
	}
	c.SessionTTL = ttl
	return c, nil
}

// envOr 取环境变量值，缺省返回 def。
func envOr(prefix, _ string) func(key, def string) string {
	return func(key, def string) string {
		v := strings.TrimSpace(os.Getenv(prefix + key))
		if v == "" {
			return def
		}
		return v
	}
}

// envBool 取布尔环境变量，缺省返回 def。
func envBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return def
	}
}

// envDur 取 duration 环境变量，解析失败返回 def。
func envDur(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// envDurErr 取 duration 环境变量，解析失败返回错误。
func envDurErr(key string, def time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def, err
	}
	return d, nil
}

// envList 取逗号分隔列表环境变量。
func envList(key string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// envInt 取整数环境变量，缺省返回 def。
func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
