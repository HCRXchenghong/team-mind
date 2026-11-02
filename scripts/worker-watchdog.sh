#!/usr/bin/env bash
# Keep the TeamMind WeChat worker alive. The worker exits on process-level
# failures (panic, OOM, manual kill); without a supervisor every message sent
# afterwards is never fetched from Tencent and never appears on the web.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_DIR="${TEAMMIND_RUNTIME_DIR:-$ROOT/.run}"
LOG_DIR="$RUNTIME_DIR/logs"
LOG_FILE="$LOG_DIR/worker-watchdog.log"
PID_FILE="$RUNTIME_DIR/worker-watchdog.pid"
WORKER_PID_FILE="$RUNTIME_DIR/wechat-worker.pid"
BINARY="$RUNTIME_DIR/teammind-server"
mkdir -p "$LOG_DIR"

if [ -n "${TEAMMIND_ENV_FILE:-}" ]; then
  ENV_FILE="$TEAMMIND_ENV_FILE"
elif [ -f "$ROOT/.env" ]; then
  ENV_FILE="$ROOT/.env"
elif [ -f "$RUNTIME_DIR/backend.env" ]; then
  ENV_FILE="$RUNTIME_DIR/backend.env"
else
  echo "worker watchdog: missing env file" >&2
  exit 1
fi

SOURCED="$RUNTIME_DIR/env.worker"
( umask 077; tr -d '\r' < "$ENV_FILE" > "$SOURCED" )
set -a
# shellcheck disable=SC1090
. "$SOURCED"
set +a
rm -f "$SOURCED"

echo $$ >"$PID_FILE"
echo "$(date '+%Y-%m-%d %H:%M:%S') worker watchdog started (pid $$)" >>"$LOG_FILE"
cleanup() {
  rm -f "$PID_FILE"
  exit 0
}
trap cleanup INT TERM

while true; do
  if ! pgrep -f "teammind-server wechat-worker" >/dev/null 2>&1; then
    if [ ! -x "$BINARY" ]; then
      echo "$(date '+%Y-%m-%d %H:%M:%S') worker binary missing at $BINARY" >>"$LOG_FILE"
      sleep 60
      continue
    fi
    echo "$(date '+%Y-%m-%d %H:%M:%S') worker is down, restarting" >>"$LOG_FILE"
    nohup "$BINARY" wechat-worker </dev/null >>"$LOG_DIR/wechat-worker.log" 2>&1 &
    echo $! >"$WORKER_PID_FILE"
    sleep 2
    if pgrep -f "teammind-server wechat-worker" >/dev/null 2>&1; then
      echo "$(date '+%Y-%m-%d %H:%M:%S') worker restarted" >>"$LOG_FILE"
    else
      echo "$(date '+%Y-%m-%d %H:%M:%S') worker failed to start, retry in 60s" >>"$LOG_FILE"
      sleep 58
    fi
  fi
  sleep 30
done
