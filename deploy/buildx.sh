#!/usr/bin/env bash
# ============================================================
# Naspic 多架构镜像构建脚本（linux/amd64 + linux/arm64）
# 用法：
#   ./deploy/buildx.sh naspic/naspic:0.5.0            # 构建并推送
#   ./deploy/buildx.sh naspic/naspic:0.5.0 --load     # 仅本地导入（不支持多架构同时 load）
# ============================================================
set -euo pipefail

IMAGE="${1:-naspic/naspic:latest}"
EXTRA="${2:-}"
VERSION="${VERSION:-$(cat ../VERSION 2>/dev/null || echo dev)}"
TAGS="${TAGS-vips}"
BUILDER="${BUILDER:-naspic-builder}"

echo ">>> 镜像: ${IMAGE}"
echo ">>> 版本: ${VERSION}"
echo ">>> 标签: ${TAGS:-（空，纯 Go 后端）}"

# 1. 确保 buildx 可用
docker buildx version >/dev/null 2>&1 || { echo "需要 Docker 19.03+ 与 buildx 插件"; exit 1; }

# 2. 创建/复用 builder（启用 QEMU 模拟 arm64）
if ! docker buildx inspect "${BUILDER}" >/dev/null 2>&1; then
  echo ">>> 创建 builder ${BUILDER}"
  docker run --rm --privileged multiarch/qemu-user-static --reset -p yes >/dev/null 2>&1 || true
  docker buildx create --name "${BUILDER}" --driver docker-container --use
fi
docker buildx inspect "${BUILDER}" --bootstrap

# 3. 构建并推送多架构镜像
cd "$(dirname "$0")/.."
echo ">>> 开始构建（首次 arm64 构建会走 QEMU 模拟，耗时较长）"
docker buildx build \
  --builder "${BUILDER}" \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION="${VERSION}" \
  --build-arg TAGS="${TAGS}" \
  -f deploy/Dockerfile \
  -t "${IMAGE}" \
  -t "${IMAGE%:*}:latest" \
  ${EXTRA} \
  .

echo ">>> 完成：${IMAGE}"
echo ">>> 查看 manifest: docker buildx imagetools inspect ${IMAGE}"
