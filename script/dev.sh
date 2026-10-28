#!/bin/bash
# dev: 调试脚本（macOS / Linux 版，对应 dev.bat）
# 链路：frontend build -> 复制 dist 到 core/web/dist -> go build（release/dev/）-> npm run start
# release/dev 是本地调试目录：构建产物与运行时数据都在这里
#
# 用法：
#   script/dev.sh              自动选端口（5260 被占用则递增）
#   script/dev.sh --port 5270  指定端口
#   script/dev.sh --no-build   跳过构建，直接启动（只改前端/Go 后想快速重启时用）
set -euo pipefail

# 打开 modeldump（每次模型请求全量输入的打印，进终端与 data/logs/）；正式版不设即关闭
export EZ_MODEL_DUMP=1

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PORT=""
DO_BUILD=1
while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="${2:-}"; shift 2 ;;
    --no-build) DO_BUILD=0; shift ;;
    -h|--help) sed -n '2,9p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) echo "[dev] 未知参数: $1" >&2; exit 1 ;;
  esac
done

# Go 不在 PATH 时补上常见安装位置（brew / 官方包 / 用户目录解压）
if ! command -v go >/dev/null 2>&1; then
  for d in /opt/homebrew/bin /usr/local/go/bin "$HOME/sdk/go/bin" "$HOME/go/bin"; do
    [ -x "$d/go" ] && PATH="$d:$PATH" && break
  done
fi
command -v go >/dev/null 2>&1 || { echo "[dev] 找不到 go，请先安装或加入 PATH" >&2; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "[dev] 找不到 npm，请先安装 Node.js" >&2; exit 1; }

# 端口选择：显式指定优先；否则从 5260 起找第一个空闲端口，
# 避免与已在运行的 ezharness 正式版（同样默认 5260）撞车
if [ -z "$PORT" ]; then
  PORT=5260
  while lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; do
    PORT=$((PORT + 1))
  done
fi

DEV_DIR="$ROOT/release/dev"
CORE_EXE="$DEV_DIR/ezharness-core.exe"   # 内核按该文件名查找 core，macOS 下沿用

if [ "$DO_BUILD" = 1 ]; then
  echo "[dev] building frontend..."
  ( cd frontend && npm run build )

  echo "[dev] copying dist -> core/web/dist..."
  rm -rf core/web/dist
  cp -R frontend/dist core/web/dist

  echo "[dev] building core..."
  mkdir -p "$DEV_DIR"
  ( cd core && go build -o "$CORE_EXE" . )
fi

[ -x "$CORE_EXE" ] || { echo "[dev] core 不存在: $CORE_EXE（去掉 --no-build 重新构建）" >&2; exit 1; }
chmod +x "$CORE_EXE"

# 应用根配置：端口与数据目录。data 控制在 release/dev 下，与正式版数据隔离
mkdir -p "$DEV_DIR/data"
cat > "$DEV_DIR/ezharness.json" <<EOF
{
  "port": $PORT,
  "listen": "127.0.0.1",
  "dataDir": "$DEV_DIR/data"
}
EOF
echo "[dev] 端口 $PORT，数据目录 $DEV_DIR/data"

if [ ! -d desktop/node_modules ]; then
  echo "[dev] installing desktop deps..."
  ( cd desktop && npm install )
fi

echo "[dev] starting desktop..."
cd desktop
npm run start
