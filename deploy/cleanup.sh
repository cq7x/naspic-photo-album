#!/usr/bin/env bash
# ============================================================
# Naspic 宿主机硬盘清理（构建前执行）
#
# 镜像构建会在 /var/lib/docker 与 /var/lib/containerd 里堆积大量中间层，
# 20G 系统盘很容易被撑满导致 build 中断。这个脚本把「安全可删」的部分清掉：
#
#   1. 停止中的容器、悬空 & 未被使用的镜像、构建缓存（buildx cache）
#   2. 未被引用的 volume
#   3. containerd 内容仓库里的孤儿 blob
#   4. apt 缓存 / 日志 / 临时文件 / 旧内核
#
# 安全边界：
#   - 只删「未使用」的镜像（docker image prune -a 会删掉所有未被容器引用的镜像，
#     正在运行的 naspic 容器依赖的镜像不会被删）
#   - **绝不**触碰 /data、/photos 等业务数据目录
#
# 用法：
#   sudo ./deploy/cleanup.sh           # 普通清理
#   sudo ./deploy/cleanup.sh --deep    # 深度清理（额外清 apt/日志/内核）
# ============================================================
set -uo pipefail

DEEP=0
[ "${1:-}" = "--deep" ] && DEEP=1

need_sudo() {
  if [ "$(id -u)" -ne 0 ]; then
    SUDO=sudo
  else
    SUDO=
  fi
}
need_sudo

fmt() {
  # 字节 → 人类可读
  awk -v b="$1" 'BEGIN{
    split("B KB MB GB TB", u, " ")
    i=1; while (b >= 1024 && i < 5) { b/=1024; i++ }
    printf "%.1f%s", b, u[i]
  }'
}

free_now() { df -Pk / | awk 'NR==2{print $4*1024}'; }

BEFORE=$(free_now)
echo "=============================================================="
echo " Naspic 宿主机清理  $(date '+%F %T')"
echo "=============================================================="
echo "清理前可用：$(fmt "${BEFORE}")"
echo

# ---------- 0. 当前占用 Top ----------
echo ">>> [0/6] 空间占用概览"
${SUDO} du -sh /var/lib/docker /var/lib/containerd /var/log /tmp 2>/dev/null | sort -hr | head
echo

# ---------- 1. 停止中的容器 ----------
echo ">>> [1/6] 清理已停止的容器"
${SUDO} docker container prune -f 2>/dev/null || echo "  (跳过)"
echo

# ---------- 2. 未使用的镜像 ----------
echo ">>> [2/6] 清理未被容器引用的镜像"
${SUDO} docker image prune -a -f 2>/dev/null || echo "  (跳过)"
echo

# ---------- 3. 构建缓存 ----------
echo ">>> [3/6] 清理构建缓存（buildx / legacy builder）"
${SUDO} docker builder prune -a -f 2>/dev/null || echo "  (跳过)"
echo

# ---------- 4. 未使用的卷 ----------
echo ">>> [4/6] 清理未被使用的 volume"
${SUDO} docker volume prune -f 2>/dev/null || echo "  (跳过)"
echo

# ---------- 5. containerd 孤儿 blob ----------
echo ">>> [5/6] 清理 containerd 内容仓库"
if command -v ctr >/dev/null 2>&1; then
  ${SUDO} ctr content ls 2>/dev/null | awk '$3=="" && $1!="DIGEST" {print $1}' | head -2000 \
    | while read -r d; do ${SUDO} ctr content rm "$d" 2>/dev/null; done
  echo "  已处理"
else
  echo "  (没有 ctr，跳过)"
fi
echo

# ---------- 6. 系统级垃圾 ----------
echo ">>> [6/6] 系统级清理"
${SUDO} apt-get clean 2>/dev/null || true
${SUDO} rm -rf /var/lib/apt/lists/* 2>/dev/null || true
${SUDO} journalctl --vacuum-size=100M 2>/dev/null || true
${SUDO} find /var/log -type f -name '*.gz' -delete 2>/dev/null || true
${SUDO} find /tmp -type f -mtime +3 -delete 2>/dev/null || true
${SUDO} rm -f /var/log/*.1 2>/dev/null || true
echo "  已完成"
echo

if [ "${DEEP}" = "1" ]; then
  echo ">>> [附加] 深度清理：旧内核 + 缩略图缓存"
  ${SUDO} apt-get -y autoremove --purge 2>/dev/null | tail -3 || true
  echo "  已完成"
  echo
fi

AFTER=$(free_now)
echo "=============================================================="
echo " 清理前可用：$(fmt "${BEFORE}")"
echo " 清理后可用：$(fmt "${AFTER}")"
echo " 释放空间：  $(fmt "$((AFTER - BEFORE))")"
echo "=============================================================="
echo
echo "当前磁盘："
df -h / | tail -1
echo
echo "剩余镜像："
docker images --format 'table {{.Repository}}\t{{.Tag}}\t{{.Size}}' 2>/dev/null | head -20
