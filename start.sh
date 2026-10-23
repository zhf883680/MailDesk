#!/usr/bin/env bash
# MailDesk AI 启动脚本（macOS / Linux）
set -e
cd "$(dirname "$0")"

OS="$(uname -s)"
ARCH="$(uname -m)"
BIN=""

if [ "$OS" = "Darwin" ]; then
  if [ "$ARCH" = "arm64" ]; then BIN="bin/maildesk-darwin-arm64"; else BIN="bin/maildesk-darwin-amd64"; fi
elif [ "$OS" = "Linux" ]; then
  if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then BIN="bin/maildesk-linux-arm64"; else BIN="bin/maildesk-linux-amd64"; fi
fi

if [ -z "$BIN" ] || [ ! -f "$BIN" ]; then
  echo "未找到适用于当前系统的可执行文件（$OS / $ARCH）。"
  echo "请到 bin/ 目录手动选择，或参考 README 从源码构建。"
  exit 1
fi

chmod +x "$BIN" 2>/dev/null || true
ADDR="${ADDR:-:8080}"
echo "正在启动 MailDesk AI，监听 ${ADDR} …"
exec "$BIN" -addr "$ADDR" -data ./data "$@"
