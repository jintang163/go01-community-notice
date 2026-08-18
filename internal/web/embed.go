// Package web 嵌入前端静态资源（HTML/CSS/JS）。
// 资源在编译期通过 //go:embed 打包进二进制，运行时由 handler 提供服务。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:assets
var assets embed.FS

// Files 返回静态资源文件系统，路径前缀为 "assets"。
func Files() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}
