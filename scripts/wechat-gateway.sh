#!/usr/bin/env bash
# Portable OpenClaw WeChat gateway launcher for TeamMind.
#
# Supported on macOS and Ubuntu. It finds a Node.js version accepted by
# OpenClaw (>=24.16 <25 or >=26.1), keeps all mutable state inside the repo or
# a configured runtime directory, and never depends on the previous operator's
# home directory or on the machine's default node version.
#
#   scripts/wechat-gateway.sh start|stop|restart|status|logs|reset-state|foreground
#
# Environment:
#   TEAMMIND_ENV_FILE   .env to read (default: <repo>/.env, then <repo>/.run/backend.env)
#   TEAMMIND_RUNTIME_DIR mutable directory (default: <repo>/.run)
#   OPENCLAW_STATE_DIR  gateway state (default: <runtime>/openclaw-weixin-state)
#   NODE_BIN            force a specific Node.js binary
#   WECHAT_GATEWAY_PORT default 18793
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BRIDGE_DIR="$ROOT/backend/wechat-plugin-bridge"
RUNTIME_DIR="${TEAMMIND_RUNTIME_DIR:-$ROOT/.run}"
LOG_DIR="$RUNTIME_DIR/logs"
LOG_FILE="$LOG_DIR/openclaw-gateway.log"
PID_FILE="$RUNTIME_DIR/openclaw-gateway.pid"
WATCHDOG_PID="$RUNTIME_DIR/openclaw-gateway-watchdog.pid"
CACHE_DIR="$RUNTIME_DIR/openclaw-cache"
TMP_ROOT="$RUNTIME_DIR/tmp/openclaw"
mkdir -p "$RUNTIME_DIR" "$LOG_DIR" "$CACHE_DIR" "$TMP_ROOT"

if [ -n "${TEAMMIND_ENV_FILE:-}" ]; then
  ENV_FILE="$TEAMMIND_ENV_FILE"
elif [ -f "$ROOT/.env" ]; then
  ENV_FILE="$ROOT/.env"
elif [ -f "$RUNTIME_DIR/backend.env" ]; then
  ENV_FILE="$RUNTIME_DIR/backend.env"
else
  ENV_FILE="$ROOT/.env"
fi

read_env() {
  local key="$1"
  if [ -n "${!key:-}" ]; then return 0; fi
  [ -f "$ENV_FILE" ] || return 0
  local line value
  line="$(grep -E "^[[:space:]]*${key}[[:space:]]*=" "$ENV_FILE" | tr -d '\r' | tail -1 || true)"
  [ -n "$line" ] || return 0
  value="${line#*=}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  value="${value%\"}"; value="${value#\"}"
  value="${value%\'}"; value="${value#\'}"
  export "$key=$value"
}

read_env WECHAT_GATEWAY_BRIDGE_TOKEN
read_env WECHAT_GATEWAY_PORT
read_env OPENCLAW_STATE_DIR
WECHAT_GATEWAY_PORT="${WECHAT_GATEWAY_PORT:-18793}"
STATE_DIR="${OPENCLAW_STATE_DIR:-$RUNTIME_DIR/openclaw-weixin-state}"
mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR" 2>/dev/null || true

if [ -z "${WECHAT_GATEWAY_BRIDGE_TOKEN:-}" ] || [ "${#WECHAT_GATEWAY_BRIDGE_TOKEN}" -lt 32 ]; then
  cat >&2 <<'EOF'
缺少 WECHAT_GATEWAY_BRIDGE_TOKEN（至少 32 字节）。
在 .env 中生成一个并让 API、worker、gateway 共用：
  openssl rand -hex 32
EOF
  exit 1
fi

node_supported() {
  "$1" -e 'const [a,b]=process.versions.node.split(".").map(Number); process.exit((a===24&&b>=16)||a>=26?0:1)' >/dev/null 2>&1
}

find_node() {
  local candidate
  if [ -n "${NODE_BIN:-}" ] && [ -x "$NODE_BIN" ] && node_supported "$NODE_BIN"; then
    printf '%s\n' "$NODE_BIN"; return 0
  fi
  if command -v node >/dev/null 2>&1 && node_supported "$(command -v node)"; then
    command -v node; return 0
  fi
  local patterns=(
    "${NVM_DIR:-$HOME/.nvm}/versions/node/*/bin/node"
    "$HOME/.nvm/versions/node/*/bin/node"
    "$HOME/.local/share/fnm/node-versions/*/installation/bin/node"
    "$HOME/.volta/tools/image/node/*/bin/node"
    "$HOME/.asdf/installs/nodejs/*/bin/node"
    "/opt/homebrew/opt/node@26/bin/node"
    "/opt/homebrew/opt/node@24/bin/node"
    "/usr/local/opt/node@26/bin/node"
    "/usr/local/opt/node@24/bin/node"
    "/opt/homebrew/Cellar/node@26/*/bin/node"
    "/opt/homebrew/Cellar/node@24/*/bin/node"
    "/opt/homebrew/Cellar/node/*/bin/node"
    "/usr/local/Cellar/node@24/*/bin/node"
    "/usr/local/Cellar/node/*/bin/node"
    # Local toolchain fallback used by some coding environments; harmless when absent.
    "$HOME/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node"
  )
  local matches=()
  shopt -s nullglob
  for pattern in "${patterns[@]}"; do
    for candidate in $pattern; do
      [ -x "$candidate" ] && matches+=("$candidate")
    done
  done
  shopt -u nullglob
  if [ "${#matches[@]}" -gt 0 ]; then
    while IFS= read -r candidate; do
      if node_supported "$candidate"; then printf '%s\n' "$candidate"; return 0; fi
    done < <(printf '%s\n' "${matches[@]}" | sort -Vr)
  fi
  return 1
}

# Last resort: fetch an official portable Node build into the runtime directory.
# This keeps fresh macOS/Ubuntu machines working without root or Homebrew, and
# can be disabled with TEAMMIND_SKIP_NODE_DOWNLOAD=1.
download_node() {
  [ "${TEAMMIND_SKIP_NODE_DOWNLOAD:-0}" = "1" ] && return 1
  command -v curl >/dev/null 2>&1 || return 1
  command -v tar >/dev/null 2>&1 || return 1
  local version="${TEAMMIND_NODE_VERSION:-24.19.0}" os arch name dir url
  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) return 1 ;;
  esac
  case "$(uname -m)" in
    arm64|aarch64) arch=arm64 ;;
    x86_64|amd64) arch=x64 ;;
    *) return 1 ;;
  esac
  name="node-v${version}-${os}-${arch}"
  dir="$RUNTIME_DIR/$name"
  if [ -x "$dir/bin/node" ] && node_supported "$dir/bin/node"; then
    printf '%s\n' "$dir/bin/node"
    return 0
  fi
  echo "未找到可用的 Node.js，正在下载便携版 Node ${version}（${os}-${arch}）..." >&2
  for url in "https://npmmirror.com/mirrors/node/v${version}/${name}.tar.gz" "https://nodejs.org/dist/v${version}/${name}.tar.gz"; do
    if curl -fL --connect-timeout 15 --retry 2 -o "$RUNTIME_DIR/${name}.tar.gz" "$url"; then
      if tar -xzf "$RUNTIME_DIR/${name}.tar.gz" -C "$RUNTIME_DIR" && [ -x "$dir/bin/node" ] && node_supported "$dir/bin/node"; then
        rm -f "$RUNTIME_DIR/${name}.tar.gz"
        printf '%s\n' "$dir/bin/node"
        return 0
      fi
    fi
  done
  rm -f "$RUNTIME_DIR/${name}.tar.gz"
  return 1
}

ensure_node() {
  if [ -n "${NODE:-}" ] && node_supported "$NODE"; then return 0; fi
  if NODE="$(find_node)"; then return 0; fi
  NODE="$(download_node || true)"
  if [ -n "${NODE:-}" ] && node_supported "$NODE"; then return 0; fi
  cat >&2 <<EOF
没有找到 OpenClaw 支持的 Node.js（需要 >=24.16 <25 或 >=26.1）。
当前 node: $(command -v node || echo '未安装') $(node -v 2>/dev/null || true)

macOS:
  brew install node@24
  export NODE_BIN="\$(brew --prefix node@24)/bin/node"

Ubuntu:
  sudo apt-get install -y curl
  curl -fsSL https://deb.nodesource.com/setup_24.x | sudo -E bash -
  sudo apt-get install -y nodejs
  或使用 nvm: nvm install 24 && nvm use 24

然后重新运行 scripts/wechat-gateway.sh start
EOF
  return 1
}

port_open() {
  curl -fsS -m 2 -o /dev/null "http://127.0.0.1:${WECHAT_GATEWAY_PORT}/" 2>/dev/null
}

port_wait_closed() {
  for _ in $(seq 1 30); do
    port_open || return 0
    sleep 0.2
  done
  return 1
}

stop_gateway() {
  if [ -f "$WATCHDOG_PID" ]; then
    local watchdog=""
    watchdog="$(cat "$WATCHDOG_PID" 2>/dev/null || true)"
    if [ -n "$watchdog" ] && kill -0 "$watchdog" 2>/dev/null; then
      kill -TERM "$watchdog" 2>/dev/null || true
    fi
    rm -f "$WATCHDOG_PID"
  fi
  local pid=""
  [ -f "$PID_FILE" ] && pid="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
    for _ in $(seq 1 20); do kill -0 "$pid" 2>/dev/null || break; sleep 0.25; done
    kill -KILL "$pid" 2>/dev/null || true
  fi
  rm -f "$PID_FILE"
  if command -v lsof >/dev/null 2>&1; then
    local listeners
    listeners="$(lsof -ti "tcp:${WECHAT_GATEWAY_PORT}" -sTCP:LISTEN 2>/dev/null || true)"
    if [ -n "$listeners" ]; then
      kill -TERM $listeners 2>/dev/null || true
      for _ in $(seq 1 16); do
        lsof -ti "tcp:${WECHAT_GATEWAY_PORT}" -sTCP:LISTEN >/dev/null 2>&1 || break
        sleep 0.25
      done
      listeners="$(lsof -ti "tcp:${WECHAT_GATEWAY_PORT}" -sTCP:LISTEN 2>/dev/null || true)"
      [ -n "$listeners" ] && kill -KILL $listeners 2>/dev/null || true
    fi
  fi
  port_wait_closed || true
}

start_gateway() {
  ensure_node || exit 1
  if port_open; then
    echo "OpenClaw Gateway 已在运行：http://127.0.0.1:${WECHAT_GATEWAY_PORT}"
    return 0
  fi
  if ! port_wait_closed; then
    echo "端口 ${WECHAT_GATEWAY_PORT} 已被其他进程占用，请先停止它" >&2
    exit 1
  fi
  echo "Node: $("$NODE" -v) ($NODE)"
  echo "State: $STATE_DIR"
  echo "Env:   $ENV_FILE"
  ( cd "$BRIDGE_DIR" && "$NODE" ./patch-wechat-plugin.mjs ) >>"$LOG_FILE" 2>&1
  (
    cd "$BRIDGE_DIR"
    nohup env \
      XDG_CACHE_HOME="$CACHE_DIR" \
      TMPDIR="$TMP_ROOT" \
      TEAMMIND_ENV_FILE="$ENV_FILE" \
      OPENCLAW_STATE_DIR="$STATE_DIR" \
      WECHAT_GATEWAY_PORT="$WECHAT_GATEWAY_PORT" \
      WECHAT_GATEWAY_BRIDGE_TOKEN="$WECHAT_GATEWAY_BRIDGE_TOKEN" \
      "$NODE" ./run-gateway.mjs >>"$LOG_FILE" 2>&1 &
    echo $! >"$PID_FILE"
  )
  local pid
  pid="$(cat "$PID_FILE")"
  for _ in $(seq 1 60); do
    if port_open; then
      echo "OpenClaw Gateway 已启动：http://127.0.0.1:${WECHAT_GATEWAY_PORT} (pid $pid)"
      return 0
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "Gateway 进程已退出，最近日志：" >&2
      tail -20 "$LOG_FILE" >&2 || true
      exit 1
    fi
    sleep 0.5
  done
  echo "Gateway 30 秒内未就绪，最近日志：" >&2
  tail -20 "$LOG_FILE" >&2 || true
  exit 1
}

watchdog_loop() {
  ensure_node || exit 1
  echo $$ >"$WATCHDOG_PID"
  echo "$(date '+%Y-%m-%d %H:%M:%S') watchdog started" >>"$LOG_DIR/openclaw-watchdog.log"
  trap 'rm -f "$WATCHDOG_PID"; exit 0' INT TERM
  while true; do
    if ! port_open; then
      echo "$(date '+%Y-%m-%d %H:%M:%S') gateway is down, restarting" >>"$LOG_DIR/openclaw-watchdog.log"
      if start_gateway >>"$LOG_DIR/openclaw-watchdog.log" 2>&1; then
        echo "$(date '+%Y-%m-%d %H:%M:%S') gateway restarted" >>"$LOG_DIR/openclaw-watchdog.log"
      else
        echo "$(date '+%Y-%m-%d %H:%M:%S') restart failed, retry in 60s" >>"$LOG_DIR/openclaw-watchdog.log"
        sleep 60
        continue
      fi
    fi
    sleep 30
  done
}

case "${1:-start}" in
  start) start_gateway ;;
  stop) stop_gateway; echo "OpenClaw Gateway 已停止" ;;
  restart) stop_gateway; start_gateway ;;
  status)
    if port_open; then
      echo "running http://127.0.0.1:${WECHAT_GATEWAY_PORT}"
      [ -f "$PID_FILE" ] && echo "pid $(cat "$PID_FILE")"
    else
      echo "not running"
      exit 1
    fi
    ;;
  logs) tail -n "${2:-80}" "$LOG_FILE" ;;
  foreground)
    ensure_node || exit 1
    stop_gateway
    cd "$BRIDGE_DIR"
    exec env XDG_CACHE_HOME="$CACHE_DIR" TMPDIR="$TMP_ROOT" TEAMMIND_ENV_FILE="$ENV_FILE" \
      OPENCLAW_STATE_DIR="$STATE_DIR" WECHAT_GATEWAY_PORT="$WECHAT_GATEWAY_PORT" \
      WECHAT_GATEWAY_BRIDGE_TOKEN="$WECHAT_GATEWAY_BRIDGE_TOKEN" "$NODE" ./run-gateway.mjs
    ;;
  watchdog) watchdog_loop ;;
  reset-state)
    stop_gateway
    if [ -d "$STATE_DIR" ]; then
      backup="${STATE_DIR}.bak-$(date +%Y%m%d-%H%M%S)"
      mv "$STATE_DIR" "$backup"
      echo "旧状态已备份到 $backup（微信绑定需要重新扫码）"
    fi
    start_gateway
    ;;
  *)
    echo "用法: $0 start|stop|restart|status|logs|foreground|watchdog|reset-state" >&2
    exit 1
    ;;
esac
