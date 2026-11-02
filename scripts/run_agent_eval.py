#!/usr/bin/env python3
"""Run the golden agent tasks against a live TeamMind API."""
import json, os, sys, urllib.request, urllib.error, pathlib

API = os.getenv('TEAMMIND_API', 'http://127.0.0.1:8188/api/v1')
EMAIL = os.getenv('EVAL_EMAIL', 'verify-1792468162@example.com')
PASSWORD = os.getenv('EVAL_PASSWORD', 'Verify-Pass-2026!')
TEAM = os.getenv('EVAL_TEAM_ID', '877DD3B7D9E5A8A48328FFD6E5BB9348')
ROOT = pathlib.Path(__file__).resolve().parent.parent

def call(method, path, data=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(API + path, data=json.dumps(data).encode() if data is not None else None, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            return resp.status, json.loads(resp.read() or b'{}')
    except urllib.error.HTTPError as exc:
        return exc.code, json.loads(exc.read() or b'{}')

def main():
    status, body = call('POST', '/auth/login', {'email': EMAIL, 'password': PASSWORD})
    token = (body.get('data') or {}).get('accessToken')
    if not token:
        print('login failed', status, body)
        sys.exit(1)
    cases = json.loads((ROOT / 'evals/golden_tasks.json').read_text(encoding='utf-8'))
    passed = 0
    for case in cases:
        status, conv = call('POST', '/me/conversations', {'teamId': TEAM, 'confirmTeam': True}, token)
        cid = (conv.get('data') or {}).get('id')
        payload = {'instruction': case['instruction'], 'confirmTeam': True}
        for key in ('allowCodeExecution', 'allowWebAccess'):
            if case.get(key):
                payload[key] = True
        status, result = call('POST', f'/me/conversations/{cid}/agent', payload, token)
        data = result.get('data') or {}
        tool = data.get('tool') or ''
        reply = data.get('reply') or ''
        ok = status == 200 and (tool == case.get('expectTool') or any(word in reply for word in case.get('expectAny', [])))
        passed += 1 if ok else 0
        print(('PASS' if ok else 'FAIL'), case['id'], 'tool=' + tool, 'reply=' + reply[:80].replace('\n', ' '))
        if cid:
            call('DELETE', f'/me/conversations/{cid}', None, token)
    print(f'{passed}/{len(cases)} golden tasks passed')
    sys.exit(0 if passed == len(cases) else 1)

if __name__ == '__main__':
    main()
