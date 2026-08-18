set -e

# 通用测试脚本：构建镜像 -> 容器内执行测试（跑完即删容器）
# 项目名、测试命令均必传，例如：./go-test.sh go01-community-notice "go test ./..."
[ -n "$1" ] && [ -n "$2" ] || { echo "用法: $0 <项目名> <测试命令>"; exit 1; }
PROJECT_NAME="$1"
TEST_CMD="$2"

./build_benzhi_docker.sh "$PROJECT_NAME"      # 构建镜像（默认 linux/amd64，另一架构见上一节）
if docker run --rm "${PROJECT_NAME}:latest" sh -c "$TEST_CMD"; then
    echo "✅ 测试全部通过"
else
    echo "❌ 测试存在失败"
    exit 1
fi
