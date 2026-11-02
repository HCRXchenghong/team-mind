#!/usr/bin/env bash
# Stop the TeamMind local stack started by scripts/dev-up.sh.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_DIR="${TEAMMIND_RUNTIME_DIR:-$ROOT/.run}"
API_PORT="${API_PORT:-8188}"

stop_pid_file() {
  local name="$1" file="$2" pid=""
  if [ -f "$file" ]; then pid="$(cat "$file" 2>/dev/null || true)"; fi
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
    for _ in $(seq 1 20); do kill -0 "$pid" 2>/dev/null || break; sleep 0.25; done
    kill -KILL "$pid" 2>/dev/null || true
    echo "已停止 $name (pid $pid)"
  else
    echo "$name 未运行"
  fi
  rm -f "$file"
}

stop_pid_file "Worker watchdog" "$RUNTIME_DIR/worker-watchdog.pid"
stop_pid_file "Worker" "$RUNTIME_DIR/wechat-worker.pid"
stop_pid_file "API" "$RUNTIME_DIR/api.pid"
stop_pid_file "Web" "$RUNTIME_DIR/web.pid"
# Stop a listener on the expected API port when its command matches the service.
stop_port_listener() {
  local name="$1" port="$2" pattern="$3" pid command
  command -v lsof >/dev/null 2>&1 || return 0
  for pid in $(lsof -ti "tcp:${port}" -sTCP:LISTEN 2>/dev/null || true); do
    command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    case "$command" in
      *"$pattern"*)
        kill -TERM "$pid" 2>/dev/null || true
        echo "已停止 $name 监听进程 (pid $pid)"
        continue
        ;;
    esac
    # Some sandboxes block ps. Fall back to the process working directory.
    cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1)"
    if [ "$cwd" = "$ROOT" ]; then
      kill -TERM "$pid" 2>/dev/null || true
      echo "已停止 $name 监听进程 (pid $pid)"
    fi
  done
}

stop_port_listener "API" "$API_PORT" "teammind-server"
stop_port_listener "Web" "5174" "vite"

# Remove worker children left behind by an interrupted parent process.
if command -v pkill >/dev/null 2>&1; then
  pkill -f "teammind-server wechat-worker" 2>/dev/null || true
fi

if [ -x "$ROOT/scripts/wechat-gateway.sh" ]; then
  "$ROOT/scripts/wechat-gateway.sh" stop >/dev/null 2>&1 || true
  echo "已停止 OpenClaw Gateway"
fi

echo "完成。MySQL 不会被停止。"
