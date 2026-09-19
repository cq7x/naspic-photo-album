# ============================================================
# 部署前准备：生成 .env 并创建数据目录
#
# 用法（在服务器上，任意目录）：
#   bash /home/ubuntu/naspic/deploy/prepare.sh
#
# 为什么要这一步：
#   docker compose 的数据卷路径来自 .env。没有 .env 时 compose 会用
#   compose 文件里的默认值（./data、./mysql/data），也就是**代码目录**——
#   而代码目录每次同步都会被 rm -rf 重建，数据库会跟着一起没。
#   本项目已随仓库附带 deploy/.env，正常情况下不需要手工执行本脚本；
#   只有在你想改数据目录或密码时才用到。
# ============================================================
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$DIR"

echo ">>> 部署目录: $DIR"

# 1. .env
if [ ! -f .env ]; then
  cp .env.example .env
  echo ">>> 已从 .env.example 生成 .env —— 请按需修改后重新执行本脚本"
  exit 0
fi
echo ">>> .env 已存在，跳过生成"

# 2. 数据目录（读取 .env 里的值，不存在就建）
pick() { grep -E "^$1=" .env | tail -1 | cut -d= -f2- | tr -d '\r'; }

for KEY in NASPIC_DATA_DIR MYSQL_DATA_DIR; do
  D="$(pick "$KEY")"
  [ -z "$D" ] && continue
  if [ ! -d "$D" ]; then
    echo ">>> 创建 $KEY=$D"
    if ! mkdir -p "$D" 2>/dev/null; then
      echo ">>> 需要权限，尝试 sudo…"
      sudo mkdir -p "$D"
    fi
  else
    echo ">>> $KEY 已存在: $D"
  fi
done

# 3. 确认数据卷不在代码目录里（这条最容易出事，显式拦一下）
CODE_ROOT="$(cd "$DIR/.." && pwd)"
for KEY in NASPIC_DATA_DIR MYSQL_DATA_DIR; do
  D="$(pick "$KEY")"
  [ -z "$D" ] && continue
  case "$D" in
    "$CODE_ROOT"*)
      echo ">>> [警告] $KEY 指向代码目录内部（$D），重新同步代码会丢数据！"
      echo ">>>        请把 .env 里的该路径改到代码目录之外。"
      ;;
  esac
done

echo
echo ">>> 准备完成。下一步："
echo "    cd $DIR && docker compose up -d"
