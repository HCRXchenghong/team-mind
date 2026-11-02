#!/usr/bin/env bash
# Start the TeamMind local stack on macOS or Ubuntu:
# Web UI -> Go API -> optional OpenClaw WeChat gateway and worker.
#
#   scripts/dev-up.sh [--no-wechat]
#
# The script is idempotent: services already listening are left untouched.
# Prerequisites: MySQL 8, Go >=1.18, Node.js + npm. The WeChat gateway has its
# own Node 24/26 requirement and is handled by scripts/wechat-gateway.sh.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_DIR="${TEAMMIND_RUNTIME_DIR:-$ROOT/.run}"
LOG_DIR="$RUNTIME_DIR/logs"
API_BIN="$RUNTIME_DIR/teammind-server"
API_PID="$RUNTIME_DIR/api.pid"
WORKER_PID="$RUNTIME_DIR/wechat-worker.pid"
WEB_PID="$RUNTIME_DIR/web.pid"
WITH_WECHAT=1
for argument in "$@"; do
  case "$argument" in
    --no-wechat) WITH_WECHAT=0 ;;
    *) echo "未知参数: $argument" >&2; exit 1 ;;
  esac
done

if [ -n "${TEAMMIND_ENV_FILE:-}" ]; then
  ENV_FILE="$TEAMMIND_ENV_FILE"
elif [ -f "$ROOT/.env" ]; then
  ENV_FILE="$ROOT/.env"
elif [ -f "$RUNTIME_DIR/backend.env" ]; then
  ENV_FILE="$RUNTIME_DIR/backend.env"
else
  echo "找不到环境文件：请先 cp .env.example .env 并填写密钥" >&2
  exit 1
fi
mkdir -p "$RUNTIME_DIR" "$LOG_DIR"

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "缺少依赖：$1" >&2; exit 1; }
}
need go
need curl
need node
need npm

if [ "${WECHAT_MODE:-unconfigured}" = "real" ] && [ ! -d "$ROOT/backend/wechat-plugin-bridge/node_modules" ]; then
  echo "缺少微信插件依赖：请运行" >&2
  echo "  cd backend/wechat-plugin-bridge && npm ci" >&2
  exit 1
fi

port_open() {
  curl -fsS -m 2 -o /dev/null "$1" 2>/dev/null
}

# Load a CRLF-normalized copy so files edited on Windows or copied through
# chat tools still parse. The copy lives in the runtime directory (0600).
ENV_SOURCED="$RUNTIME_DIR/env.sourced"
( umask 077; tr -d '\r' < "$ENV_FILE" > "$ENV_SOURCED" )
# shellcheck disable=SC1090
set -a
. "$ENV_SOURCED"
set +a
rm -f "$ENV_SOURCED"

API_ADDR_VALUE="${API_ADDR:-127.0.0.1:8188}"
API_PORT="${API_ADDR_VALUE##*:}"
API_BASE="http://127.0.0.1:${API_PORT}"

echo "==> 构建 API"
(cd "$ROOT/backend" && go build -o "$API_BIN" ./cmd/server)

if port_open "$API_BASE/healthz"; then
  echo "==> API 已在运行：$API_BASE"
else
  echo "==> 启动 API"
  nohup "$API_BIN" </dev/null >>"$LOG_DIR/api.log" 2>&1 &
  echo $! >"$API_PID"
  for _ in $(seq 1 60); do
    if port_open "$API_BASE/readyz"; then break; fi
    sleep 0.5
  done
  if ! port_open "$API_BASE/readyz"; then
    echo "API 未就绪，最近日志：" >&2
    tail -30 "$LOG_DIR/api.log" >&2 || true
    if grep -q "schema_migrations\|migration\|迁移" "$LOG_DIR/api.log" 2>/dev/null; then
      echo "提示：新数据库需要先运行 scripts/migrate.sh <数据库名>" >&2
    fi
    exit 1
  fi
fi
echo "    API ready: $API_BASE"

if [ ! -f "$ROOT/package.json" ]; then
  # 前端代码已清空，等待按 docs/FRONTEND_FUNCTIONS_API.md 重建后再接入。
  echo "==> 前端待重建（未发现 package.json），本次跳过 Web"
elif [ ! -d "$ROOT/node_modules" ]; then
  echo "==> 安装 Web 依赖"
  (cd "$ROOT" && npm ci --no-audit --no-fund)
fi
if [ -f "$ROOT/package.json" ] && port_open "http://127.0.0.1:5174/"; then
  echo "==> Web 已在运行：http://127.0.0.1:5174"
elif [ -f "$ROOT/package.json" ]; then
  echo "==> 启动 Web"
  (cd "$ROOT" && nohup npm run dev </dev/null >>"$LOG_DIR/web.log" 2>&1 & echo $! >"$WEB_PID")
  for _ in $(seq 1 40); do
    if port_open "http://127.0.0.1:5174/"; then break; fi
    sleep 0.5
  done
  if ! port_open "http://127.0.0.1:5174/"; then
    echo "Web 未就绪，最近日志：" >&2
    tail -30 "$LOG_DIR/web.log" >&2 || true
    exit 1
  fi
fi

if [ "$WITH_WECHAT" = "1" ] && [ "${WECHAT_MODE:-unconfigured}" = "real" ]; then
  echo "==> 启动 OpenClaw 微信 Gateway"
  "$ROOT/scripts/wechat-gateway.sh" start
  if [ "${TEAMMIND_GATEWAY_WATCHDOG:-1}" != "0" ]; then
    if [ -f "$RUNTIME_DIR/openclaw-gateway-watchdog.pid" ] && kill -0 "$(cat "$RUNTIME_DIR/openclaw-gateway-watchdog.pid")" 2>/dev/null; then
      echo "==> Gateway watchdog 已在运行"
    else
      nohup "$ROOT/scripts/wechat-gateway.sh" watchdog </dev/null >>"$LOG_DIR/openclaw-watchdog.log" 2>&1 &
      echo $! >"$RUNTIME_DIR/openclaw-gateway-watchdog.pid"
      echo "==> Gateway watchdog 已启动（每 30 秒检查）"
    fi
  fi
  if [ -f "$WORKER_PID" ] && kill -0 "$(cat "$WORKER_PID")" 2>/dev/null; then
    echo "==> 微信 worker 已在运行"
  else
    echo "==> 启动微信 worker"
    nohup "$API_BIN" wechat-worker </dev/null >>"$LOG_DIR/wechat-worker.log" 2>&1 &
    echo $! >"$WORKER_PID"
  fi
  if [ "${TEAMMIND_WORKER_WATCHDOG:-1}" != "0" ]; then
    if [ -f "$RUNTIME_DIR/worker-watchdog.pid" ] && kill -0 "$(cat "$RUNTIME_DIR/worker-watchdog.pid")" 2>/dev/null; then
      echo "==> Worker watchdog 已在运行"
    else
      nohup "$ROOT/scripts/worker-watchdog.sh" </dev/null >>"$LOG_DIR/worker-watchdog.log" 2>&1 &
      echo $! >"$RUNTIME_DIR/worker-watchdog.pid"
      echo "==> Worker watchdog 已启动（每 30 秒检查）"
    fi
  fi
elif [ "$WITH_WECHAT" = "1" ]; then
  echo "==> 跳过微信 Gateway（WECHAT_MODE=${WECHAT_MODE:-unconfigured}）"
fi

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  if ! docker image inspect teammind-sandbox-python:local >/dev/null 2>&1; then
    echo "提示：代码沙箱镜像缺失，运行 scripts/sandbox/build-image.sh 后可启用 Python/Bash 沙箱" >&2
  fi
fi

echo "==> 运行时"
echo "    Go:     $(go version | awk '{print $3}')"
echo "    Node:   $(node -v 2>/dev/null || echo '未安装') ($(command -v node 2>/dev/null || echo 'n/a'))"
echo "    Docker: $(docker --version 2>/dev/null || echo '未安装（macOS 沙箱不可用）')"

WEB_LINE="http://127.0.0.1:5174"
[ -f "$ROOT/package.json" ] || WEB_LINE="待重建（前端代码已清空）"

cat <<EOF

TeamMind 已启动
  Web:      ${WEB_LINE}
  API:      ${API_BASE}
  AI 状态:  ${API_BASE}/api/v1/me/ai/status（需登录）
  微信:     $([ "$WITH_WECHAT" = "1" ] && echo "http://127.0.0.1:${WECHAT_GATEWAY_PORT:-18793}" || echo "已跳过")
  日志:     $LOG_DIR
  停止:     scripts/dev-down.sh
EOF
