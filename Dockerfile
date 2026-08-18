# 运行镜像：单阶段 golang:1.22
#
# 说明：本机 Docker Desktop 访问 registry-1.docker.io 不稳定（alpine 拉取超时），
# 故采用单阶段 golang:1.22 直接构建运行，避免多阶段 alpine 镜像拉取失败。
# 体积较大（~300MB+），但可在受限网络下独立构建运行。
FROM golang:1.22

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct
ENV CGO_ENABLED=0

# 先复制依赖文件利用缓存（零第三方依赖，go mod download 无额外下载）。
COPY go.mod ./
COPY go.sum* ./
RUN go mod download

# 复制源码与前端资源（embed 需要 internal/web/assets 存在）。
COPY . .

# 构建二进制到固定路径。
RUN go build -o /usr/local/bin/cn-server .

# 数据持久化目录（运行时由 compose 挂载卷覆盖）。
RUN mkdir -p /app/data

ENV APP_ADDR=:8080
ENV APP_DATA_PATH=/app/data/store.json
ENV APP_ADMIN_USERNAME=admin
ENV APP_ADMIN_PASSWORD=admin123

EXPOSE 8080

# 健康检查。
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/bin/bash", "-c", "curl -fsS http://localhost:8080/healthz || exit 1"]

CMD ["cn-server"]
