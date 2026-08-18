package model

// id.go 提供生成各类资源 ID 的工具函数。
// 这里不导入 crypto/rand（避免与 store 层耦合），由 store 层注入 ID 生成器；
// 本文件仅提供格式化帮助函数，保持 model 层零依赖。

// NoticeIDPrefix 通知 ID 前缀。
const NoticeIDPrefix = "n_"
// UserIDPrefix 用户 ID 前缀。
const UserIDPrefix = "u_"
// ReadIDPrefix 阅读记录 ID 前缀。
const ReadIDPrefix = "r_"
// TokenPrefix 会话 Token 前缀。
const TokenPrefix = "t_"
