"""End-to-end acceptance for the configured model, web, sandbox, collaboration and Gantt upgrade.

Requires a running API with a configured AI provider. Creates an isolated
account and team, then exercises the real endpoints. Run:

    TEAMMIND_API_BASE=http://127.0.0.1:8188/api/v1 \
    python3 scripts/acceptance_ai_upgrade.py
"""
import json
import os
import secrets
import sys
import urllib.error
import urllib.request

API = os.getenv('TEAMMIND_API_BASE', 'http://127.0.0.1:8188/api/v1').rstrip('/')
suffix = secrets.token_hex(5)
EMAIL = f'ai-upgrade-{suffix}@teammind.test'
PASSWORD = f'Ai-Upgrade-{suffix}-Passw0rd!'
results = []


def call(method, path, data=None, token=None, timeout=240, raw=False):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    body = json.dumps(data).encode() if data is not None else None
    request = urllib.request.Request(API + path, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            payload = response.read()
            return response.status, (payload if raw else json.loads(payload or b'{}'))
    except urllib.error.HTTPError as error:
        return error.code, error.read().decode()[:500]


def check(name, condition, detail=''):
    results.append((name, bool(condition), detail))
    print(('PASS ' if condition else 'FAIL ') + name + (f' :: {detail}' if detail and not condition else ''))


def main():
    status, body = call('POST', '/auth/register', {'email': EMAIL, 'name': 'AI 升级验收', 'password': PASSWORD})
    check('register isolated account', status == 201, str(body))
    status, body = call('POST', '/auth/login', {'email': EMAIL, 'password': PASSWORD})
    check('login', status == 200, str(body))
    token = body['data']['accessToken']

    status, body = call('GET', '/me/ai/status', token=token)
    capabilities = body.get('data', {}) if isinstance(body, dict) else {}
    try:
        check('status declares real model', capabilities.get('configured') is True and capabilities.get('simulated') is False)
        check('web tools available', capabilities.get('webAvailable') is True)
        check('sandbox available', capabilities.get('sandboxAvailable') is True)
        check('workspace available', capabilities.get('workspaceAvailable') is True)
    except AttributeError:
        check('status shape', False, str(body))

    status, body = call('POST', '/teams', {'name': f'AI 升级验收团队 {suffix}', 'description': 'acceptance', 'timezone': 'Asia/Shanghai'}, token)
    check('create team', status in (200, 201), str(body))
    team_id = body.get('data', {}).get('id') or body.get('data')
    status, body = call('POST', f'/teams/{team_id}/projects', {
        'name': 'AI 升级验收项目', 'description': 'acceptance project', 'status': 'active',
        'startDate': '2026-10-01', 'dueDate': '2026-12-31',
    }, token)
    check('create project', status in (200, 201), str(body))
    project_id = body.get('data', {}).get('id')
    status, body = call('POST', f'/teams/{team_id}/tasks', {
        'projectId': project_id, 'title': '验收任务 Alpha', 'description': '用于 AI 提案与排期体检',
        'priority': 'high', 'startDate': '2026-10-10', 'dueDate': '2026-10-24', 'milestone': False,
    }, token)
    check('create task', status in (200, 201), str(body))
    task_id = body.get('data', {}).get('id')

    # Streaming chat with team context.
    status, body = call('POST', '/me/conversations', {'teamId': team_id, 'confirmTeam': True}, token)
    chat_id = body['data']['id']
    request = urllib.request.Request(API + f'/me/conversations/{chat_id}/stream', data=json.dumps({'content': '用一句话说明当前团队最该处理什么。', 'confirmTeam': True}).encode(), headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token}, method='POST')
    chunks = []
    model = ''
    with urllib.request.urlopen(request, timeout=180) as response:
        for line in response:
            text = line.decode('utf-8', 'ignore').strip()
            if not text.startswith('data: '):
                continue
            try:
                event = json.loads(text[6:])
            except json.JSONDecodeError:
                continue
            if event.get('content'):
                chunks.append(event['content'])
            if event.get('model'):
                model = event['model']
    answer = ''.join(chunks).strip()
    check('real streaming reply', len(answer) >= 10, answer[:80])
    expected_model = os.getenv('EXPECTED_MODEL', 'mimo-v2.6-flash')
    check('reply model is ' + expected_model, model == expected_model, model)

    # Single agent: web search.
    status, body = call('POST', '/me/conversations', {}, token)
    web_conv = body['data']['id']
    status, body = call('POST', f'/me/conversations/{web_conv}/agent', {'instruction': '联网查一下 OpenAI 是什么公司，给出来源链接', 'allowWebAccess': True}, token)
    web_data = body.get('data', {}) if isinstance(body, dict) else {}
    web_steps = web_data.get('steps') or []
    web_results = [step for step in web_steps if step.get('tool') == 'web_search']
    check('agent web_search executed', status == 200 and bool(web_results), str(body)[:200])
    check('web_search returns at least 3 results', bool(web_results) and (web_results[0].get('result') or {}).get('count', 0) >= 3, str(web_results)[:200])
    check('web reply cites a source', 'http' in (web_data.get('reply') or ''), (web_data.get('reply') or '')[:120])

    # Single agent: Python sandbox.
    status, body = call('POST', '/me/conversations', {}, token)
    sandbox_conv = body['data']['id']
    status, body = call('POST', f'/me/conversations/{sandbox_conv}/agent', {'instruction': '用 Python 计算 2 的 10 次方并写到 answer.txt', 'allowCodeExecution': True}, token)
    sandbox_data = body.get('data', {}) if isinstance(body, dict) else {}
    sandbox_result = sandbox_data.get('result') or {}
    check('agent run_python executed', status == 200 and sandbox_data.get('tool') == 'run_python', str(body)[:200])
    check('sandbox produced a file', (sandbox_result.get('exitCode') == 0) and bool(sandbox_result.get('files')), str(sandbox_result)[:200])
    check('sandbox reply is natural language', len(sandbox_data.get('reply') or '') >= 5 and 'qwen3:14b' not in (sandbox_data.get('reply') or ''), (sandbox_data.get('reply') or '')[:80])

    # Single agent: approval proposal.
    status, body = call('POST', '/me/conversations', {'teamId': team_id, 'confirmTeam': True}, token)
    proposal_conv = body['data']['id']
    status, body = call('POST', f'/me/conversations/{proposal_conv}/agent', {
        'instruction': '把任务“验收任务 Alpha”的截止日期延后到 2026-11-20，提交审批',
        'confirmTeam': True,
    }, token)
    proposal_data = body.get('data', {}) if isinstance(body, dict) else {}
    proposal_reply = proposal_data.get('reply') or ''
    check('agent creates pending proposal', status == 200 and proposal_data.get('tool') == 'propose_task_change', str(body)[:200])
    check('proposal reply says it is pending', '提案已提交审批' in proposal_reply and '尚未' in proposal_reply, proposal_reply[:120])

    # Multi-agent collaboration.
    status, body = call('POST', '/me/conversations', {'teamId': team_id, 'confirmTeam': True}, token)
    collab_conv = body['data']['id']
    status, body = call('POST', f'/me/conversations/{collab_conv}/collaborate', {
        'instruction': '结合当前团队任务，指出最可能延期的一项并给出本周补救动作。',
        'confirmTeam': True,
        'allowWebAccess': True,
    }, token)
    collab = body.get('data', {}) if isinstance(body, dict) else {}
    stages = collab.get('stages') or []
    check('collaboration returns three stages', status == 200 and [stage.get('role') for stage in stages] == ['planner', 'executor', 'reviewer'], str(body)[:300])
    executor_stage = next((stage for stage in stages if stage.get('role') == 'executor'), {})
    check('executor produced a grounded draft', len(executor_stage.get('content') or '') >= 20, str(executor_stage)[:200])
    check('collaboration has a final reply', len(collab.get('reply') or '') >= 10, (collab.get('reply') or '')[:100])

    # Gantt insights.
    status, body = call('POST', f'/teams/{team_id}/gantt-insights', {'projectId': '', 'status': '', 'assigneeId': '', 'dateFrom': '', 'dateTo': ''}, token)
    insight = (body.get('data') or {}).get('analysis', '') if isinstance(body, dict) else ''
    check('gantt insights returns analysis', status == 200 and len(insight) >= 50, str(body)[:200])
    check('gantt insights reports task count', (body.get('data') or {}).get('taskCount', 0) >= 1, str(body)[:120])

    passed = sum(1 for _, ok, _ in results if ok)
    print(f'\n{passed}/{len(results)} checks passed; team={team_id}; task={task_id}; model={model}')
    if passed != len(results):
        sys.exit(1)


if __name__ == '__main__':
    main()
