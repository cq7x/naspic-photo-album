#!/usr/bin/env bash
# ============================================================
# Naspic —— Docker 镜像源体检与加速器配置
#
# 专治这类报错：
#   error pulling image configuration: download failed after attempts=6:
#   dial tcp [2a03:2880:...]:443: connect: connection timed out
#   Get "https://registry-1.docker.io/v2/": net/http: request canceled
#
# 用法（需要 root 或 sudo）：
#   sudo bash deploy/docker-mirror.sh                 # 自动体检 + 自动配置
#   sudo bash deploy/docker-mirror.sh --check         # 只体检，不改任何配置
#   sudo bash deploy/docker-mirror.sh --mirror URL    # 手工指定加速器（可多次）
#   sudo bash deploy/docker-mirror.sh --reset         # 清空加速器，恢复官方源
#
# 做了什么（按顺序）：
#   1) 试拉 hello-world，能通说明官方源没问题，直接退出（不动配置）
#   2) 通不了，但 IPv4 能直连官方源 → 说明只是走了不可达的 IPv6，
#      往 /etc/hosts 固定 IPv4 记录（最小改动，不改 daemon.json）
#   3) IPv4 也不通（国内机器常见）→ 逐个测速候选加速器，
#      把可用的写进 /etc/docker/daemon.json 的 registry-mirrors
#   4) 重启 docker，再拉一次 hello-world 验证
#   5) 全都失败 → 给出「离线导入镜像」的兜底方案
# ============================================================
set -uo pipefail

MODE="auto"
USER_MIRRORS=()
DAEMON_JSON="/etc/docker/daemon.json"

while [ $# -gt 0 ]; do
  case "$1" in
    --check) MODE="check"; shift ;;
    --reset) MODE="reset"; shift ;;
    --mirror) USER_MIRRORS+=("$2"); shift 2 ;;
    -h|--help) sed -n '2,26p' "$0"; exit 0 ;;
    *) echo "未知参数: $1（用 --help 看用法）"; exit 1 ;;
  esac
done

# 候选加速器：按国内可用性从高到低排列，脚本会逐个测，可用的全写进去
# （Docker 会按 registry-mirrors 顺序逐个尝试，写多个比只写一个稳）
CANDIDATES=(
  "https://docker.m.daocloud.io"
  "https://docker.1ms.run"
  "https://docker.xuanyuan.me"
  "https://hub.rat.dev"
  "https://dockerhub.icu"
  "https://docker.mirrors.ustc.edu.cn"
  "https://docker.nju.edu.cn"
  "https://mirror.ccs.tencentyun.com"
  "https://dockerproxy.com"
  "https://docker.1panel.live"
  "https://docker.chenby.cn"
)

say() { echo; echo ">>> $*"; }
info() { echo "    $*"; }
die() { echo; echo "!!! $*"; exit 1; }

command -v docker >/dev/null 2>&1 || die "未检测到 docker，先装 Docker 再跑本脚本"
command -v curl >/dev/null 2>&1 || echo "    提示：没有 curl，测速会退化成直接 pull（慢一些）"

# docker 命令是否需要 sudo
if docker info >/dev/null 2>&1; then DOCKER="docker"; else DOCKER="sudo docker"; fi

# 拉取测试：hello-world 只有几 KB，超时 60s 足够判断
pull_ok() {
  timeout 60 $DOCKER pull hello-world:latest >/dev/null 2>&1
}

# HTTP 探测：registry v2 未认证会返回 401，能拿到 200/401/307 都算通
http_code_of() {
  local url="$1" code
  code="$(timeout 10 curl -sS -o /dev/null -w '%{http_code}' "$url" 2>/dev/null || echo 000)"
  [ -n "$code" ] || code="000"
  echo "$code"
}

reachable() {
  case "$(http_code_of "$1")" in
    200|204|301|302|307|401|403) return 0 ;;
    *) return 1 ;;
  esac
}

say "1/4 体检：官方源能不能直连"
if pull_ok; then
  info "官方源正常（hello-world 拉取成功），无需配置加速器 ✓"
  info "如果你仍然在构建时卡住，一般是某个具体镜像的网络抖动，重跑一次即可："
  info "  cd 项目根目录 && bash deploy/build.sh"
  exit 0
fi
info "官方源拉取失败，开始排查原因…"

# ---------- 2. IPv6 路由不通？ ----------
say "2/4 判断：是 IPv6 路由不通，还是整个外网不通"
IPV4_OK=0
if reachable "https://registry-1.docker.io/v2/"; then
  IPV4_OK=1
  info "IPv4 直连官方源可达 → 多半是 docker 走了不可达的 IPv6（报错里那个 [...] 地址）"
else
  info "IPv4 直连官方源也不可达 → 需要镜像加速器"
fi

if [ "$MODE" = "check" ]; then
  say "体检结果（--check，未做任何修改）"
  info "官方源直连：失败"
  info "IPv4 可达：  $([ "$IPV4_OK" = "1" ] && echo 是 || echo 否)"
  echo "    候选加速器："
  for m in "${CANDIDATES[@]}"; do
    if reachable "$m/v2/"; then
      echo "      ✓ $m"
    else
      echo "      ✗ $m"
    fi
  done
  exit 0
fi

if [ "$(id -u)" != "0" ]; then
  echo "    需要 root 权限修改配置，自动加 sudo 重跑："
  echo "      sudo bash $0 $*"
  exec sudo bash "$0" "$@"
fi

# ---------- 3. 先试最小改动：/etc/hosts 固定 IPv4 ----------
if [ "$IPV4_OK" = "1" ]; then
  say "3/4 最小修复：给官方源域名固定 IPv4 记录"
  CHANGED=0
  for h in registry-1.docker.io auth.docker.io production.cloudflare.docker.com index.docker.io; do
    IP="$(getent ahostsv4 "$h" 2>/dev/null | awk '{print $1; exit}')"
    [ -n "$IP" ] || continue
    if grep -qE "^[0-9].*[[:space:]]$h\$" /etc/hosts; then
      info "$h 已有 hosts 记录，跳过"
    else
      echo "$IP $h" >> /etc/hosts
      info "hosts 新增：$IP $h"
      CHANGED=1
    fi
  done
  if [ "$CHANGED" = "1" ]; then
    # 让 docker daemon 重新解析（daemon 有自己的 DNS 缓存，重启最稳）
    info "重启 docker 使 hosts 生效…"
    (systemctl restart docker 2>/dev/null || service docker restart 2>/dev/null || true)
    sleep 3
    if pull_ok; then
      say "搞定 ✓ 已通过 /etc/hosts 固定 IPv4，官方源可正常拉取"
      info "可以回去继续构建：cd 项目根目录 && bash deploy/build.sh"
      exit 0
    fi
    info "固定 IPv4 后仍不通，继续配置加速器…"
  fi
else
  say "3/4 跳过 hosts 修复（IPv4 也不可达）"
fi

# ---------- 4. 配置 registry-mirrors ----------
say "4/4 配置镜像加速器"

if [ "$MODE" = "reset" ]; then
  [ -f "$DAEMON_JSON" ] && cp -a "$DAEMON_JSON" "${DAEMON_JSON}.bak.$(date +%s)"
  printf '{\n  "registry-mirrors": []\n}\n' > "$DAEMON_JSON"
  info "已清空加速器配置（备份在 ${DAEMON_JSON}.bak.*）"
  (systemctl restart docker 2>/dev/null || service docker restart 2>/dev/null || true)
  sleep 3
  exit 0
fi

if [ ${#USER_MIRRORS[@]} -gt 0 ]; then
  POOL=("${USER_MIRRORS[@]}")
else
  POOL=("${CANDIDATES[@]}")
fi

GOOD=()
for m in "${POOL[@]}"; do
  m="${m%/}"
  printf '    测速 %-45s' "$m"
  if reachable "$m/v2/"; then
    echo "可用 ✓"
    GOOD+=("$m")
  else
    echo "不可用 ✗"
  fi
done

if [ ${#GOOD[@]} -eq 0 ]; then
  echo
  echo "!!! 所有候选加速器都不可用，本服务器大概率是纯内网/出口被限。"
  echo "    兜底方案 —— 在有网的电脑上把镜像打包带过来："
  echo
  echo "      # 有网的机器上（能正常 pull 的机器）"
  echo "      docker pull node:20-bookworm-slim"
  echo "      docker pull golang:1.22-bookworm"
  echo "      docker pull debian:bookworm-slim"
  echo "      docker pull mysql:8.0"
  echo "      docker pull redis:7-alpine"
  echo "      docker save -o naspic-images.tar node:20-bookworm-slim golang:1.22-bookworm debian:bookworm-slim mysql:8.0 redis:7-alpine"
  echo
  echo "      # 拷到服务器后导入（离线，不依赖外网）"
  echo "      docker load -i naspic-images.tar"
  echo
  echo "    或者手工指定你手里可用的加速器地址："
  echo "      sudo bash $0 --mirror https://你的加速器地址"
  exit 1
fi

# 合并写 daemon.json：有 jq 用 jq，没有用 python3，再没有就整份覆盖
mkdir -p /etc/docker
if [ -f "$DAEMON_JSON" ] && [ -s "$DAEMON_JSON" ]; then
  cp -a "$DAEMON_JSON" "${DAEMON_JSON}.bak.$(date +%s)"
  info "原配置已备份：${DAEMON_JSON}.bak.*"
  EXISTING=1
else
  EXISTING=0
fi

JSON_LIST=""
for m in "${GOOD[@]}"; do
  [ -n "$JSON_LIST" ] && JSON_LIST="$JSON_LIST, "
  JSON_LIST="$JSON_LIST\"$m\""
done

if [ "$EXISTING" = "1" ] && command -v jq >/dev/null 2>&1; then
  jq --argjson m "[$JSON_LIST]" \
     '. + {"registry-mirrors": $m, "ipv6": false, "dns": ["223.5.5.5", "114.114.114.114"]}' \
     "$DAEMON_JSON" > "${DAEMON_JSON}.tmp" && mv "${DAEMON_JSON}.tmp" "$DAEMON_JSON"
  info "已用 jq 合并进现有 daemon.json"
elif [ "$EXISTING" = "1" ] && command -v python3 >/dev/null 2>&1; then
  MIRRORS_ENV="$(printf '%s\n' "${GOOD[@]}")" python3 - <<'PY'
import json, os
path = "/etc/docker/daemon.json"
cfg = {}
try:
    with open(path) as f:
        cfg = json.load(f) or {}
except Exception:
    cfg = {}
cfg["registry-mirrors"] = [x.strip() for x in os.environ["MIRRORS_ENV"].splitlines() if x.strip()]
cfg.setdefault("ipv6", False)
cfg.setdefault("dns", ["223.5.5.5", "114.114.114.114"])
with open(path, "w") as f:
    json.dump(cfg, f, ensure_ascii=False, indent=2)
print("    已用 python3 合并进现有 daemon.json")
PY
else
  cat > "$DAEMON_JSON" <<JSON
{
  "registry-mirrors": [$JSON_LIST],
  "ipv6": false,
  "dns": ["223.5.5.5", "114.114.114.114"],
  "log-driver": "json-file",
  "log-opts": { "max-size": "10m", "max-file": "3" }
}
JSON
  info "已生成新的 daemon.json"
fi

info "生效的加速器："
sed -n '/registry-mirrors/,/]/p' "$DAEMON_JSON" | sed 's/^/      /'

info "重启 docker…"
(systemctl restart docker 2>/dev/null || service docker restart 2>/dev/null || true)
sleep 4

echo
if pull_ok; then
  echo "============================================================"
  echo " 镜像源已修好 ✓  docker pull 恢复正常"
  echo " 继续部署："
  echo "   cd 项目根目录 && bash deploy/fresh-deploy.sh --mirror"
  echo " 或只构建镜像："
  echo "   bash deploy/build.sh"
  echo "============================================================"
else
  echo "============================================================"
  echo " 加速器已写入，但 hello-world 仍拉不下来，可能原因："
  echo "   1) docker 没真正重启：systemctl status docker"
  echo "   2) 加速器限速/需要登录：docker info | grep -A5 'Registry Mirrors'"
  echo "   3) 出口防火墙只放行白名单：让网络同事放行 registry-1.docker.io"
  echo " 兜底：用离线方式导入镜像（$0 --help 里有命令）"
  echo "============================================================"
  exit 1
fi
