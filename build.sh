#!/bin/bash

# sing-box manager 构建脚本
# 支持 Linux/macOS 的 arm64/amd64 架构
# 前端代码会自动嵌入到二进制文件中

set -e

VERSION=${VERSION:-"0.2.13"}
BUILD_TIME=$(date -u '+%Y-%m-%d %H:%M:%S')
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

OUTPUT_DIR="dist"
BINARY_NAME="sbm"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# 检查前端是否已构建
check_frontend() {
    if [ ! -f "web/dist/index.html" ]; then
        return 1
    fi
    return 0
}

# 构建前端
build_frontend() {
    info "构建前端..."

    if [ ! -d "web" ]; then
        error "未找到 web 目录"
    fi

    cd web

    # 检查 npm/pnpm（优先使用 npm，因为 package-lock.json 存在）
    if command -v npm &> /dev/null; then
        PKG_MGR="npm"
    elif command -v pnpm &> /dev/null; then
        PKG_MGR="pnpm"
    else
        error "需要安装 npm 或 pnpm"
    fi

    if [ ! -d "node_modules" ]; then
        info "安装前端依赖 (使用 $PKG_MGR)..."
        $PKG_MGR install
    fi

    info "编译前端代码..."
    $PKG_MGR run build
    cd ..

    info "前端构建完成"
}

# 确保前端已构建
ensure_frontend() {
    if ! check_frontend; then
        warn "前端未构建，开始构建前端..."
        build_frontend
    else
        info "前端已构建，跳过"
    fi
}

# 构建单个目标
build_target() {
    local os=$1
    local arch=$2
    local output_name="${BINARY_NAME}-${os}-${arch}"

    if [ "$os" = "windows" ]; then
        output_name="${output_name}.exe"
    fi

    info "构建 ${os}/${arch}..."

    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build \
        -ldflags "-s -w -X main.Version=${VERSION} -X 'main.BuildTime=${BUILD_TIME}' -X main.GitCommit=${GIT_COMMIT}" \
        -o "${OUTPUT_DIR}/${output_name}" \
        ./cmd/sbm/

    if [ $? -eq 0 ]; then
        local size=$(ls -lh "${OUTPUT_DIR}/${output_name}" | awk '{print $5}')
        info "完成: ${output_name} (${size})"
    else
        error "构建 ${os}/${arch} 失败"
    fi
}

# 清理构建目录
clean() {
    info "清理构建目录..."
    rm -rf "${OUTPUT_DIR}"
    mkdir -p "${OUTPUT_DIR}"
}

# 显示帮助
show_help() {
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  all          构建所有平台 (默认)"
    echo "  linux        仅构建 Linux 版本"
    echo "  darwin       仅构建 macOS 版本"
    echo "  current      仅构建当前平台"
    echo "  frontend     仅构建前端"
    echo "  clean        清理构建目录"
    echo "  help         显示帮助"
    echo ""
    echo "环境变量:"
    echo "  VERSION      版本号 (默认: ${VERSION})"
    echo "  SKIP_FRONTEND=1  跳过前端构建"
    echo ""
    echo "示例:"
    echo "  $0                    # 构建所有平台（包含前端）"
    echo "  $0 current            # 构建当前平台"
    echo "  VERSION=1.0.0 $0      # 指定版本号构建"
    echo "  SKIP_FRONTEND=1 $0    # 跳过前端构建"
}

# 构建所有目标
build_all() {
    # 先构建前端
    if [ "${SKIP_FRONTEND}" != "1" ]; then
        ensure_frontend
    fi

    clean

    # Linux
    build_target linux amd64
    build_target linux arm64

    # macOS
    build_target darwin amd64
    build_target darwin arm64

    info "所有构建完成!"
    echo ""
    info "构建产物:"
    ls -lh "${OUTPUT_DIR}/"
}

# 仅构建 Linux
build_linux() {
    if [ "${SKIP_FRONTEND}" != "1" ]; then
        ensure_frontend
    fi
    clean
    build_target linux amd64
    build_target linux arm64
    info "Linux 构建完成!"
}

# 仅构建 macOS
build_darwin() {
    if [ "${SKIP_FRONTEND}" != "1" ]; then
        ensure_frontend
    fi
    clean
    build_target darwin amd64
    build_target darwin arm64
    info "macOS 构建完成!"
}

# 构建当前平台
build_current() {
    if [ "${SKIP_FRONTEND}" != "1" ]; then
        ensure_frontend
    fi
    clean
    local os=$(go env GOOS)
    local arch=$(go env GOARCH)
    build_target $os $arch

    # 创建软链接方便使用
    ln -sf "${BINARY_NAME}-${os}-${arch}" "${OUTPUT_DIR}/${BINARY_NAME}"
    info "当前平台构建完成!"
}

# 主函数
main() {
    echo "========================================"
    echo "  sing-box manager 构建脚本"
    echo "  版本: ${VERSION}"
    echo "  提交: ${GIT_COMMIT}"
    echo "========================================"
    echo ""

    case "${1:-all}" in
        all)
            build_all
            ;;
        linux)
            build_linux
            ;;
        darwin|macos)
            build_darwin
            ;;
        current)
            build_current
            ;;
        frontend|web)
            build_frontend
            ;;
        clean)
            clean
            info "清理完成"
            ;;
        help|-h|--help)
            show_help
            ;;
        *)
            error "未知选项: $1"
            show_help
            ;;
    esac
}

main "$@"
