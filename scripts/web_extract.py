#!/usr/bin/env python3
"""Extract main article text with Trafilatura; JSON in / JSON out."""
import json
import sys

try:
    import trafilatura
except Exception as exc:  # pragma: no cover - reported to the Go caller
    print(json.dumps({"error": f"trafilatura_unavailable: {exc}"}))
    sys.exit(2)

payload = json.load(sys.stdin)
html = payload.get("html") or ""
url = payload.get("url") or None
text = trafilatura.extract(
    html,
    url=url,
    include_comments=False,
    include_tables=False,
) or ""
title = ""
try:
    metadata = trafilatura.extract_metadata(html)
    title = getattr(metadata, "title", "") or ""
except Exception:
    title = ""
print(json.dumps({"title": title, "text": text}, ensure_ascii=False))
