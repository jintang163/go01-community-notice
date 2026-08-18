# BENZHI_README

## 项目是做什么的

**go01-community-notice** 是一个社区通知公告系统，使用 **纯 Go 标准库**（零第三方依赖）实现。

- **管理员（admin）** 发布、编辑、置顶、删除社区通知公告，查看每条通知的已读 / 未读统计。
- **居民（resident）** 查看面向自己的通知，点开即标记已读，可主动标记已读，查看未读数量。
- **核心业务规则**：管理员更新已发布通知后，所有已读居民自动回到“未读”（“更新即未读”），通过 `阅读记录.ReadAt ≥ 通知.UpdatedAt` 的时间戳比较实现。
- 内置前端页面（HTML/CSS/JS，`embed` 打包）与文件级 JSON 数据持久化（`data/store.json`，原子落盘，重启自动恢复）。
- 单一 Go 二进制，可通过 Docker 独立运行，适合离线受限环境交付。

技术栈：Go 1.22、`net/http`（Go 1.22 `ServeMux` 方法路由）、`encoding/json`、`embed`、`sync`、`crypto/rand`、`crypto/sha256`。

---

## 构建命令

```bash
# 本地构建（需本地安装 Go 1.22+）
go build ./...

# 质检镜像构建（基于 benzhi.Dockerfile，linux/amd64）
bash ./build_benzhi_docker.sh go01-community-notice
```

## 运行命令

```bash
# 方式一：本地直接运行
go run .

# 方式二：Docker Compose 一键起服务（后台常驻，:8080，种子管理员 admin/admin123）
bash ./go-run.sh
#   等价于：docker compose up -d --build
#   访问：http://localhost:8080/healthz
#   日志：docker compose logs -f
#   停止：docker compose down
```

## 测试命令

```bash
# 方式一：本地测试
go test ./...

# 方式二：质检环境测试（先构建 benzhi 镜像，再在容器内跑 go test）
bash ./go-test.sh go01-community-notice "go test ./..."
```

---

## 目录与质检文件说明

| 文件 | 是否可改 | 说明 |
|------|----------|------|
| `benzhi.Dockerfile` | ❌ 勿改 | 质检镜像（`golang:1.22`，`go mod download` + `go build ./...`） |
| `build_benzhi_docker.sh` | ❌ 勿改 | 质检镜像构建脚本 |
| `go-test.sh` | ✅ 可改 | 质检测试脚本（构建镜像后在容器内执行测试命令） |
| `go-run.sh` | ❌ 勿改 | 运行脚本（`docker compose up -d --build`） |
| `Dockerfile` | ✅ | 运行镜像（单阶段 `golang:1.22`，避免 alpine 拉取超时） |
| `docker-compose.yml` | ✅ | 服务编排（:8080，挂载 `./data` 持久化） |

> 约束：`go.mod` 声明 `go 1.22`，不使用 Go 1.23+ API（如 `crypto/pbkdf2`）；零第三方依赖，确保 `go mod download` 无需联网即可在 `golang:1.22` 镜像内离线构建与测试。

## 默认账号

- 管理员：`admin / admin123`（首次启动自动创建，可通过环境变量 `APP_ADMIN_USERNAME` / `APP_ADMIN_PASSWORD` 覆盖）

## 快速验证

```bash
# 1. 登录获取 token
curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}'

# 2. 用返回的 token 创建并发布一条通知
curl -s -X POST http://localhost:8080/api/notices \
  -H "Authorization: Bearer <token>" -H 'Content-Type: application/json' \
  -d '{"title":"停水通知","content":"明日 8:00-12:00 停水","priority":5,"category":"设施维护"}'

# 3. 浏览器访问 http://localhost:8080/login 查看前端页面
```
