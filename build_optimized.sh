#!/bin/bash
#
# 内存优化构建脚本
# 使用编译参数减少二进制大小和初始内存占用
#

set -e

echo "🚀 构建优化版 sing-box-manager..."
echo ""

# 版本信息
VERSION=$(git describe --tags 2>/dev/null || echo "dev")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')

echo "版本: $VERSION"
echo "提交: $COMMIT"
echo "时间: $BUILD_TIME"
echo ""

# 构建参数
LDFLAGS="-s -w"
LDFLAGS="$LDFLAGS -X main.version=$VERSION"
LDFLAGS="$LDFLAGS -X main.commit=$COMMIT"
LDFLAGS="$LDFLAGS -X main.buildTime=$BUILD_TIME"

# 编译选项
export CGO_ENABLED=0  # 静态链接
export GOOS=${GOOS:-$(go env GOOS)}
export GOARCH=${GOARCH:-$(go env GOARCH)}

OUTPUT_DIR="dist"
OUTPUT_NAME="sbm"

if [ "$GOOS" = "windows" ]; then
    OUTPUT_NAME="${OUTPUT_NAME}.exe"
fi

mkdir -p "$OUTPUT_DIR"

echo "目标平台: $GOOS/$GOARCH"
echo "输出文件: $OUTPUT_DIR/$OUTPUT_NAME"
echo ""

# 执行构建
echo "📦 编译中..."
go build \
    -ldflags="$LDFLAGS" \
    -trimpath \
    -o "$OUTPUT_DIR/$OUTPUT_NAME" \
    ./cmd/sbm

if [ $? -eq 0 ]; then
    echo "✅ 编译成功!"
    echo ""

    # 显示文件信息
    echo "📊 文件信息:"
    ls -lh "$OUTPUT_DIR/$OUTPUT_NAME"

    # 显示优化效果
    echo ""
    echo "🎯 优化效果:"
    echo "  ✅ 去除调试符号 (-s)"
    echo "  ✅ 去除 DWARF 信息 (-w)"
    echo "  ✅ 静态链接 (CGO_ENABLED=0)"
    echo "  ✅ 裁剪路径 (-trimpath)"
    echo ""
    echo "预期内存优化:"
    echo "  - 二进制大小: ~21 MB (vs 31 MB 正常构建)"
    echo "  - 初始内存: ~13-14 MB (vs 16-17 MB 正常构建)"
    echo ""
else
    echo "❌ 编译失败"
    exit 1
fi

# 可选：UPX 压缩
if command -v upx &> /dev/null; then
    read -p "是否使用 UPX 压缩? (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "🗜️  UPX 压缩中..."
        upx --best --lzma "$OUTPUT_DIR/$OUTPUT_NAME"
        echo "✅ 压缩完成"
        ls -lh "$OUTPUT_DIR/$OUTPUT_NAME"
    fi
fi

echo ""
echo "🎉 构建完成！"
echo ""
echo "运行示例:"
echo "  $OUTPUT_DIR/$OUTPUT_NAME -port 9090"
echo ""
echo "环境变量优化（可选）:"
echo "  GOMEMLIMIT=50MiB $OUTPUT_DIR/$OUTPUT_NAME"
