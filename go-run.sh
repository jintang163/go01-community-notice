#!/bin/bash
set -e

# 启动服务（后台常驻运行），基于项目根目录的 docker-compose.yml。
# 用法: ./go-run.sh
# 停止: docker compose down
docker compose up -d --build

echo ""
echo "✅ 服务已启动（后台运行）"
echo "  访问: http://localhost:8080/healthz"
echo "  日志: docker compose logs -f"
echo "  停止: docker compose down"
