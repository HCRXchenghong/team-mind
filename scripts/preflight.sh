#!/usr/bin/env bash
# Read-only deployment preflight. Run before dev-up.sh on a new Mac/Ubuntu host.
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
if [ -f "$ROOT/.run/backend.env" ]; then set -a; . "$ROOT/.run/backend.env"; set +a; fi
if [ -f "$ROOT/.env" ]; then set -a; . "$ROOT/.env"; set +a; fi
FAIL=0
pass() { printf 'PASS  %s\n' "$1"; }
warn() { printf 'WARN  %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1"; FAIL=$((FAIL+1)); }

printf '== TeamMind preflight (%s) ==\n' "$(uname -s)"
NODE_BIN="$(command -v node 2>/dev/null || true)"
if [ -x "$ROOT/.run/node/bin/node" ]; then NODE_BIN="$ROOT/.run/node/bin/node"; fi
if [ -n "$NODE_BIN" ]; then
  NODE_VERSION="$($NODE_BIN -v 2>/dev/null | sed 's/^v//')"
  MAJOR="${NODE_VERSION%%.*}"; REST="${NODE_VERSION#*.}"; MINOR="${REST%%.*}"
  if { [ "$MAJOR" -eq 24 ] && [ "$MINOR" -ge 16 ]; } || { [ "$MAJOR" -ge 26 ] && [ "$MINOR" -ge 1 ]; }; then
    pass "Node $NODE_VERSION (supported, $NODE_BIN)"
  else
    warn "Node $NODE_VERSION on PATH is outside 24.16+/26.1+; gateway scripts search NVM/Homebrew for a supported Node (or set NODE_BIN)"
  fi
else
  warn "node not on PATH; dev-up.sh will try to download Node 24.19"
fi
if command -v go >/dev/null 2>&1; then pass "Go $(go version | awk '{print $3}')"; else fail "Go toolchain not found"; fi

if [ -n "${MYSQL_DSN:-}" ]; then
  python3 - "$MYSQL_DSN" <<'PY'
import os, re, subprocess, sys, urllib.parse
dsn = sys.argv[1]
match = re.match(r'^(?P<user>[^:@]+)(?::(?P<password>.*))?@tcp\((?P<host>[^:)]+)(?::(?P<port>\d+))?\)/(?P<db>[^?]+)', dsn)
if match:
    user, password, host, port, db = match.group('user'), match.group('password') or '', match.group('host'), match.group('port') or '3306', match.group('db')
else:
    u = urllib.parse.urlparse(dsn)
    user, password, host, port, db = u.username or 'root', u.password or '', u.hostname or '127.0.0.1', str(u.port or 3306), (u.path or '').lstrip('/')
env = os.environ.copy(); env['MYSQL_PWD'] = password
base = ['mysql', '-h', host, '-P', port, '-u', user, db, '-N', '-e']
try:
    subprocess.run(base + ['SELECT 1'], env=env, check=True, capture_output=True, text=True, timeout=8)
    print('PASS  MySQL connection')
except Exception:
    print('FAIL  MySQL connection (check MYSQL_DSN and server)'); sys.exit(1)
try:
    out = subprocess.run(base + ["SELECT COUNT(*) FROM schema_migrations WHERE version='000043_ai_provider_created_by_nullable'"], env=env, check=True, capture_output=True, text=True, timeout=8).stdout.strip()
    print('PASS  migration 000043_ai_provider_created_by_nullable' if out == '1' else 'FAIL  migration 000043_ai_provider_created_by_nullable is not applied; run scripts/migrate.sh')
    if out != '1': sys.exit(1)
except Exception:
    print('FAIL  migration check (schema_migrations unavailable)'); sys.exit(1)
PY
  [ $? -ne 0 ] && FAIL=$((FAIL+1))
else
  fail "MYSQL_DSN is not set"
fi

if curl -sf -m 3 http://127.0.0.1:8188/readyz >/dev/null 2>&1; then pass "API /readyz"; else warn "API is not running yet (expected before dev-up)"; fi
if [ -n "${OLLAMA_BASE_URL:-}" ]; then OLLAMA_CHECK="$OLLAMA_BASE_URL"; else OLLAMA_CHECK="http://127.0.0.1:11434"; fi
if TAGS="$(curl -sf -m 5 "$OLLAMA_CHECK/api/tags" 2>/dev/null)"; then
  pass "Ollama reachable at $OLLAMA_CHECK"
  echo "$TAGS" | grep -q 'qwen3:14b' && pass "model qwen3:14b present" || warn "model qwen3:14b missing: ollama pull qwen3:14b"
  echo "$TAGS" | grep -q 'nomic-embed-text' && pass "embedding nomic-embed-text present" || warn "embedding model missing (lexical memory fallback): ollama pull nomic-embed-text"
else
  warn "Ollama not reachable at $OLLAMA_CHECK; AI replies need a configured provider"
fi
if [ -n "${SEARXNG_URL:-}" ]; then
  if curl -sf -m 5 "$SEARXNG_URL/search?q=preflight&format=json" >/dev/null 2>&1; then pass "SearXNG JSON API at $SEARXNG_URL"; else warn "SearXNG unreachable; native search fallback will be used"; fi
else
  warn "SEARXNG_URL not set; native search fallback will be used"
fi
if command -v bwrap >/dev/null 2>&1; then pass "bubblewrap sandbox"; elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then pass "Docker sandbox fallback"; else warn "no bwrap/docker; code sandbox tools unavailable"; fi
if [ "${WECHAT_MODE:-}" = "real" ]; then
  if [ -n "${STT_API_KEY:-}" ] || [ "${STT_BASE_URL:-}" = "http://127.0.0.1:8080/v1" ]; then pass "WeChat voice STT configured"; else warn "WeChat voice STT not configured; voice without WeChat transcript will be rejected"; fi
fi
if [ -n "${SMTP_HOST:-}" ] && [ -n "${SMTP_USER:-}" ] && [ -n "${SMTP_PASSWORD:-}" ] && [ -n "${SMTP_FROM:-}" ] && [ -n "${PUBLIC_URL:-}" ]; then pass "SMTP configured"; else warn "SMTP not configured; email delivery will be suppressed"; fi
if [ -n "${MEMORY_EMBEDDING_MODEL:-}" ]; then pass "memory embedding model: $MEMORY_EMBEDDING_MODEL"; fi

printf '== %d blocking failure(s) ==\n' "$FAIL"
exit "$FAIL"
