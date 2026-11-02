#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${1:-$ROOT/.env.production}"
missing=()
if [[ ! -f "$ENV_FILE" ]]; then
  echo "FAIL  env file not found: $ENV_FILE" >&2
  echo "      copy .env.production.example and fill real values" >&2
  exit 1
fi
get() {
  local key="$1"
  grep -E "^${key}=" "$ENV_FILE" | tail -1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//'
}
for key in APP_ENV API_ADDR CORS_ORIGIN MYSQL_DSN \
  LOGIN_RATE_LIMIT_HMAC_KEY MFA_ENCRYPTION_KEY AGENT_WORKSPACE_ENCRYPTION_KEY \
  AI_CONFIG_ENCRYPTION_KEY GANTT_SHARE_SECRET NOTIFICATION_ENCRYPTION_KEY \
  WECHAT_ENCRYPTION_KEY VAPID_PRIVATE_KEY; do
  value="$(get "$key" || true)"
  if [[ -z "$value" || "$value" == *REPLACE* || "$value" == *example.com* ]]; then
    missing+=("$key")
  fi
done
if [[ "$(get APP_ENV || true)" != "production" ]]; then
  echo "FAIL  APP_ENV must be production" >&2
  exit 1
fi
if (( ${#missing[@]} > 0 )); then
  echo "FAIL  production env is incomplete:" >&2
  printf '  - %s\n' "${missing[@]}" >&2
  exit 1
fi
if curl -fsS --max-time 5 http://127.0.0.1:8188/readyz >/dev/null 2>&1; then
  echo "PASS  API /readyz"
else
  echo "FAIL  API /readyz is not reachable on 127.0.0.1:8188" >&2
  exit 1
fi
echo "PASS  bootstrap env present and API ready"
echo "NEXT  configure SMTP, STT, Public URL, VAPID subject, WeChat gateway, and AI provider in the platform admin UI"
echo "      then open GET /api/v1/platform/readiness (super admin) and confirm status=ready"
