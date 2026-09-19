#!/usr/bin/env bash
# ============================================================
# Naspic 镜像构建脚本（在 Linux 服务器上本地构建，单架构）
#
# 用法：
#   ./deploy/build.sh                       # 构建 naspic:1.0.1 + latest
#   ./deploy/build.sh myrepo/naspic:1.0.1   # 指定镜像名
#   TAGS= ./deploy/build.sh                 # 不带 libvips，纯 Go 缩略图后端
#   VERSION=1.0.2 ./deploy/build.sh         # 指定版本号
#
# 注意：必须在【项目根目录】执行，构建上下文是项目根目录。
# ============================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${ROOT}"

VERSION="${VERSION:-$(cat VERSION 2>/dev/null || echo dev)}"
IMAGE="${1:-naspic:${VERSION}}"
TAGS="${TAGS-vips}"          # 默认启用 libvips；置空则纯 Go 后端
GO_VERSION="${GO_VERSION:-1.22}"
NODE_VERSION="${NODE_VERSION:-20}"
# 国内服务器建议设置：export NPM_REGISTRY=https://registry.npmmirror.com
NPM_REGISTRY="${NPM_REGISTRY:-}"
# 国内服务器建议设置：export APT_MIRROR=mirrors.aliyun.com（否则 deb.debian.org 可能只有几十 KB/s）
APT_MIRROR="${APT_MIRROR:-}"

echo ">>> 项目根目录: ${ROOT}"
echo ">>> 镜像:       ${IMAGE}"
echo ">>> 版本:       ${VERSION}"
echo ">>> 构建标签:   ${TAGS:-（空，纯 Go 后端）}"

docker version >/dev/null 2>&1 || { echo "错误：未检测到 docker，请先安装 Docker"; exit 1; }

# 构建前清理宿主机空间（CLEAN=1 开启，小磁盘机器强烈建议开启）
# 磁盘 < 8G 可用时自动触发，避免构建到一半因 No space left on device 失败
AVAIL_KB=$(df -Pk / | awk 'NR==2{print $4}')
if [ "${CLEAN:-0}" = "1" ] || [ "${AVAIL_KB:-999999}" -lt 8388608 ]; then
  echo ">>> 可用空间 $((AVAIL_KB / 1024)) MB，执行构建前清理"
  chmod +x "${ROOT}/deploy/cleanup.sh" 2>/dev/null || true
  "${ROOT}/deploy/cleanup.sh"
  echo
fi

echo ">>> 开始构建（首次约 5~15 分钟，取决于网络和机器性能）"
docker build \
  --build-arg VERSION="${VERSION}" \
  --build-arg TAGS="${TAGS}" \
  --build-arg GO_VERSION="${GO_VERSION}" \
  --build-arg NODE_VERSION="${NODE_VERSION}" \
  --build-arg NPM_REGISTRY="${NPM_REGISTRY}" \
  --build-arg APT_MIRROR="${APT_MIRROR}" \
  -f deploy/Dockerfile \
  -t "${IMAGE}" \
  -t "${IMAGE%:*}:latest" \
  .

echo
echo ">>> 构建完成"
docker images "${IMAGE%:*}" --format 'table {{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}'
echo
echo ">>> 下一步：docker run 或 docker compose up -d（见 docs/12-服务器Docker编译与部署教程.md）"
