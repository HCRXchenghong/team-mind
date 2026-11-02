#!/usr/bin/env bash
# Optional Trafilatura extractor. Use the system Python because some Homebrew
# Python builds ship a broken pip/pyexpat combination.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHON="${PYTHON:-/usr/bin/python3}"
mkdir -p "$ROOT/.run/py-deps"
"$PYTHON" -m pip install --target "$ROOT/.run/py-deps" trafilatura lxml_html_clean
printf 'Installed. Set in backend env:\n'
printf '  TRAFILATURA_PYTHON=%s\n  TRAFILATURA_PYTHONPATH=%s/.run/py-deps\n  TRAFILATURA_SCRIPT=%s/scripts/web_extract.py\n' "$PYTHON" "$ROOT" "$ROOT"
