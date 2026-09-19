#!/usr/bin/env bash
# ============================================================
# Naspic 全新部署：从一台干净的 Linux 服务器到浏览器能打开
#
# 用法（在服务器上执行，代码目录默认是 /home/ubuntu/naspic）：
#   bash deploy/fresh-deploy.sh                 # 全新部署（装好 Docker 的前提下）
#   bash deploy/fresh-deploy.sh --install-docker # 连 Docker 一起装（Ubuntu/Debian/CentOS）
#   bash deploy/fresh-deploy.sh --mirror        # 国内源加速（apt + npm）
#   bash deploy/fresh-deploy.sh --no-build      # 镜像已存在，跳过编译直接启动
#
# 可选参数：
#   --code-dir /path    代码目录（默认 /home/ubuntu/naspic）
#   --data-dir /path    应用数据目录（默认 /home/ubuntu/naspic-data）
#   --photos /path      宿主机已有照片目录（默认 /photos）
#   --port 8080         宿主机端口（默认 8080，改端口需同步改 compose）
#
# 做了什么（按顺序）：
#   1) 检查 / 安装 Docker 与 compose 插件
#   2) 创建数据目录（与代码目录分离，避免同步代码时误删）
#   3) 生成 deploy/.env（没有的话从 .env.example 复制）
#   4) 编译镜像（deploy/build.sh，约 5~15 分钟）
#   5) docker compose up -d 启动 mysql / redis / naspic
#   6) 轮询健康检查，直到 healthy 或超时
# ============================================================
set -euo pipefail

# ---------- 默认参数 ----------
CODE_DIR="/home/ubuntu/naspic"
DATA_DIR="/home/ubuntu/naspic-data"
PHOTOS_DIR="/photos"
INSTALL_DOCKER=0
USE_MIRROR=0
NO_BUILD=0

while [ $# -gt 0 ]; do
  case "$1" in
    --code-dir) CODE_DIR="$2"; shift 2 ;;
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --photos) PHOTOS_DIR="$2"; shift 2 ;;
    --install-docker) INSTALL_DOCKER=1; shift ;;
    --mirror) USE_MIRROR=1; shift ;;
    --no-build) NO_BUILD=1; shift ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
    *) echo "未知参数: $1（用 --help 看用法）"; exit 1 ;;
  esac
done

MYSQL_DIR="$(dirname "$DATA_DIR")/naspic-mysql/data"
APT_MIRROR=""
NPM_REGISTRY=""
if [ "$USE_MIRROR" = "1" ]; then
  APT_MIRROR="mirrors.aliyun.com"
  NPM_REGISTRY="https://registry.npmmirror.com"
fi

say() { echo; echo ">>> $*"; }
die() { echo; echo "!!! $*"; exit 1; }

# ---------- 0. 环境检查 ----------
say "0/6 检查环境"
echo "    代码目录: $CODE_DIR"
echo "    数据目录: $DATA_DIR"
echo "    MySQL目录: $MYSQL_DIR"
echo "    照片目录: $PHOTOS_DIR"

[ -d "$CODE_DIR" ] || die "代码目录不存在: $CODE_DIR（先把代码放上去，见 docs/12 第 3 章）"
[ -f "$CODE_DIR/deploy/docker-compose.yml" ] || die "$CODE_DIR 下没找到 deploy/docker-compose.yml，代码不完整"

# ---------- 1. Docker ----------
say "1/6 检查 Docker"
if ! command -v docker >/dev/null 2>&1; then
  if [ "$INSTALL_DOCKER" = "1" ]; then
    echo "    未检测到 docker，开始安装…"
    if [ -f /etc/debian_version ]; then
      sudo apt-get update -y
      sudo apt-get install -y ca-certificates curl
      curl -fsSL https://get.docker.com | sudo sh
    elif [ -f /etc/redhat-release ]; then
      sudo yum install -y yum-utils
      sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
      sudo yum install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
    else
      die "无法识别发行版，请手工安装 Docker 后重跑本脚本"
    fi
    sudo systemctl enable --now docker
  else
    die "未检测到 docker。加上 --install-docker 让脚本装，或手工安装后重跑：
       curl -fsSL https://get.docker.com | sudo sh
       sudo systemctl enable --now docker"
  fi
fi

if ! docker compose version >/dev/null 2>&1; then
  die "docker compose 插件不可用（需要 v2）。安装：
       sudo apt-get install -y docker-compose-plugin   # Ubuntu/Debian
       sudo yum install -y docker-compose-plugin        # CentOS/Rocky"
fi

if ! docker info >/dev/null 2>&1; then
  echo "    当前用户无 docker 权限，后续命令会自动加 sudo（或执行：sudo usermod -aG docker \$USER 后重登录）"
  DOCKER="sudo docker"
else
  DOCKER="docker"
fi
echo "    $($DOCKER --version)"
echo "    $($DOCKER compose version)"

# ---------- 2. 数据目录 ----------
say "2/6 创建数据目录"
mkdir -p "$DATA_DIR" "$MYSQL_DIR"
[ -d "$PHOTOS_DIR" ] || { echo "    照片目录 $PHOTOS_DIR 不存在，先建一个空目录（后面可在页面上改挂载）"; mkdir -p "$PHOTOS_DIR"; }
echo "    已就绪"

# 数据目录绝不能在代码目录里 —— 同步代码会 rm -rf 重建，数据会一起没
case "$DATA_DIR" in
  "$CODE_DIR"*) die "数据目录不能放在代码目录里（$DATA_DIR），重新同步代码会删光数据！" ;;
esac

# ---------- 3. .env ----------
say "3/6 准备配置"
cd "$CODE_DIR/deploy"
if [ ! -f .env ]; then
  cp .env.example .env
  # 按本次参数改写关键路径
  sed -i "s|^NASPIC_DATA_DIR=.*|NASPIC_DATA_DIR=$DATA_DIR|" .env
  sed -i "s|^NASPIC_PHOTOS_DIR=.*|NASPIC_PHOTOS_DIR=$PHOTOS_DIR|" .env
  sed -i "s|^MYSQL_DATA_DIR=.*|MYSQL_DATA_DIR=$MYSQL_DIR|" .env
  # 随机化 MySQL 密码，别用默认的
  if command -v openssl >/dev/null 2>&1; then
    PWD_NEW="naspic_$(openssl rand -hex 4)"
    sed -i "s|^MYSQL_PASSWORD=.*|MYSQL_PASSWORD=$PWD_NEW|" .env
    sed -i "s|^MYSQL_ROOT_PASSWORD=.*|MYSQL_ROOT_PASSWORD=root_$(openssl rand -hex 4)|" .env
  fi
  echo "    已从 .env.example 生成 .env（MySQL 密码已随机化）"
else
  echo "    .env 已存在，保持不动（如需改路径请手工编辑）"
fi
grep -E '^(NASPIC_DATA_DIR|NASPIC_PHOTOS_DIR|MYSQL_DATA_DIR)=' .env | sed 's/^/    /'

# ---------- 4. 编译镜像 ----------
say "4/6 编译镜像"
chmod +x ./*.sh
if [ "$NO_BUILD" = "1" ]; then
  echo "    --no-build：跳过编译"
else
  APT_MIRROR="$APT_MIRROR" NPM_REGISTRY="$NPM_REGISTRY" bash ./build.sh
fi

# ---------- 5. 启动 ----------
say "5/6 启动容器"
$DOCKER compose up -d

# ---------- 6. 健康检查 ----------
say "6/6 等待服务就绪"
IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
OK=0
for i in $(seq 1 40); do
  if curl -fsS --max-time 3 "http://127.0.0.1:8080/api/v1/health" >/tmp/naspic-health.json 2>/dev/null; then
    OK=1
    break
  fi
  sleep 3
done

echo
echo "============================================================"
if [ "$OK" = "1" ]; then
  echo " 部署完成 ✓"
  cat /tmp/naspic-health.json; echo
else
  echo " 健康检查未通过（40 次重试仍失败），看日志排查："
  echo "   cd $CODE_DIR/deploy && docker compose logs --tail=100 naspic"
fi
echo "------------------------------------------------------------"
echo " 访问地址： http://${IP:-服务器IP}:8080"
echo " 默认账号： admin / naspic123   （登录后请立刻改密码）"
echo " 容器状态： cd $CODE_DIR/deploy && docker compose ps"
echo "============================================================"
echo
echo " 重要提醒（血泪教训）："
echo "  · 新建「托管库」时目录路径请留空，程序会自动用 $DATA_DIR/managed/<库名>"
echo "    —— 千万别填 /mypic 这种容器外的路径，没挂卷的话重启容器原图就全没了"
echo "  · 已有照片建议用「挂载目录」（只读索引），原图不动最安全"
echo
