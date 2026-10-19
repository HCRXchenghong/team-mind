"""Super admin approval UI and notification workflow on an isolated test DB."""
import asyncio
import json
import os
import secrets
import urllib.error
import urllib.request
from playwright.async_api import async_playwright

BASE = os.getenv('TEAMMIND_WEB_BASE', 'http://127.0.0.1:5174')
API = os.getenv('TEAMMIND_API_BASE', 'http://127.0.0.1:8188/api/v1')
ADMIN_EMAIL = os.environ.get('ACCEPTANCE_SUPERADMIN_EMAIL', '')
ADMIN_PASSWORD = os.environ.get('ACCEPTANCE_SUPERADMIN_PASSWORD', '')
SUFFIX = secrets.token_hex(4)

if not ADMIN_EMAIL or not ADMIN_PASSWORD:
    raise SystemExit('Set ACCEPTANCE_SUPERADMIN_EMAIL and ACCEPTANCE_SUPERADMIN_PASSWORD for the isolated test super admin.')

def call(path, method='GET', data=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(
        API + path,
        data=None if data is None else json.dumps(data).encode(),
        headers=headers,
        method=method,
    )
    try:
        with urllib.request.urlopen(request, timeout=15) as response:
            return response.status, json.load(response)['data']
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)

def need(actual, expected, message):
    assert actual == expected, f'{message}: expected {expected}, got {actual}'
    print('PASS', message)

def register_and_login(name):
    email = f'{name.lower().replace(" ", "-")}-{SUFFIX}@teammind.test'
    password = 'Acceptance-Platform-' + SUFFIX + '!'
    need(call('/auth/register', 'POST', {'email': email, 'name': name, 'password': password})[0], 201, name + ' registration')
    status, result = call('/auth/login', 'POST', {'email': email, 'password': password})
    need(status, 200, name + ' login')
    return email, password, result['accessToken']

async def main():
    malformed_status, _ = call('/auth/register', 'POST', {
        'email': 'Invalid Applicant-' + SUFFIX + '@teammind.test',
        'name': 'Malformed Applicant',
        'password': 'Acceptance-Malformed-' + SUFFIX + '!',
    })
    need(malformed_status, 400, 'reject malformed registration email')
    creator_email, creator_password, creator_token = register_and_login('Review Applicant')
    approved_status, approved_team = call('/teams', 'POST', {'name': 'Approval ' + SUFFIX, 'description': '', 'timezone': 'Asia/Shanghai'}, creator_token)
    need(approved_status, 201, 'create team awaiting platform review')
    need(approved_team['status'], 'pending_review', 'approval mode leaves new team pending')
    rejected_status, rejected_team = call('/teams', 'POST', {'name': 'Rejection ' + SUFFIX, 'description': '', 'timezone': 'Asia/Shanghai'}, creator_token)
    need(rejected_status, 201, 'create team for rejection')
    admin_login_status, admin_login = call('/auth/login', 'POST', {'email': ADMIN_EMAIL, 'password': ADMIN_PASSWORD})
    need(admin_login_status, 200, 'bootstrap super admin login')
    admin_token = admin_login['accessToken']
    need(call(f'/platform/teams/{rejected_team["id"]}/decision', 'POST', {'decision': 'rejected', 'reason': ''}, admin_token)[0], 400, 'rejection without a reason is rejected')
    need(call('/me/agent/tools/execute', 'POST', {'tool': 'list_team_tasks', 'teamId': approved_team['id'], 'confirmTeam': True}, admin_token)[0], 403, 'super admin cannot use team agent tool without active membership')
    private_memory_status, private_memory = call('/me/memories', 'POST', {
        'category': 'profile', 'content': 'Private to the review applicant',
    }, creator_token)
    need(private_memory_status, 201, 'creator stores private memory for super admin isolation check')
    admin_memories = call('/me/memories', token=admin_token)[1]
    need(any(row['id'].lower() == private_memory['id'].lower() for row in admin_memories), False, 'super admin memory list excludes another user private memory')
    need(call(f'/me/memories/{private_memory["id"]}', 'DELETE', token=admin_token)[0], 404, 'super admin cannot delete copied private memory id')
    private_conversation_status, private_conversation = call('/me/conversations', 'POST', {}, creator_token)
    need(private_conversation_status, 201, 'creator starts private conversation for super admin isolation check')
    need(call(f'/me/conversations/{private_conversation["id"]}/messages', token=admin_token)[0], 404, 'super admin cannot read copied private conversation id')

    async with async_playwright() as pw:
        browser = await pw.chromium.launch(headless=True)
        page = await browser.new_page(viewport={'width': 390, 'height': 844}, is_mobile=True, has_touch=True)
        await page.goto(BASE + '/login')
        await page.locator('input[autocomplete="username"]').wait_for(state='visible')
        await page.locator('input[autocomplete="username"]').fill(ADMIN_EMAIL)
        await page.locator('input[autocomplete="current-password"]').fill(ADMIN_PASSWORD)
        await page.get_by_role('button', name='登录', exact=True).tap()
        await page.wait_for_function("location.pathname === '/teammind/overview'", timeout=10000)
        await page.goto(BASE + '/teammind/platform')
        approved_card = page.locator('article.tm-panel').filter(has=page.get_by_role('heading', name='Approval ' + SUFFIX))
        await approved_card.get_by_role('button', name='批准').tap()
        await page.locator('.arco-modal:visible').get_by_role('button', name='确定').tap()
        await page.get_by_text('审核已完成').wait_for()
        await approved_card.get_by_text('active').wait_for()
        rejected_card = page.locator('article.tm-panel').filter(has=page.get_by_role('heading', name='Rejection ' + SUFFIX))
        await rejected_card.get_by_role('button', name='拒绝').tap()
        await page.locator('.arco-modal:visible textarea').fill('Incomplete application details')
        await page.locator('.arco-modal:visible').get_by_role('button', name='确定').tap()
        await page.get_by_text('审核已完成').wait_for()
        await rejected_card.get_by_text('suspended').wait_for()
        await page.get_by_text('批准团队 · Approval ' + SUFFIX).wait_for()
        await page.get_by_text('拒绝团队 · Rejection ' + SUFFIX).wait_for()
        dimensions = await page.evaluate('({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})')
        assert dimensions['scroll'] <= dimensions['client'] + 1, dimensions
        creator_page = await browser.new_page(viewport={'width': 390, 'height': 844}, is_mobile=True, has_touch=True)
        await creator_page.goto(BASE + '/login')
        await creator_page.locator('input[autocomplete="username"]').wait_for(state='visible')
        await creator_page.locator('input[autocomplete="username"]').fill(creator_email)
        await creator_page.locator('input[autocomplete="current-password"]').fill(creator_password)
        await creator_page.get_by_role('button', name='登录', exact=True).tap()
        try:
            await creator_page.wait_for_function("location.pathname === '/teammind/overview'", timeout=10000)
        except Exception as error:
            body = (await creator_page.locator('body').inner_text())[:1000]
            raise AssertionError(f'creator touch login did not reach overview: url={creator_page.url}; body={body}') from error
        await creator_page.goto(BASE + '/teammind/notifications')
        await creator_page.get_by_role('heading', name='最近投递记录').wait_for()
        await creator_page.locator('.tm-delivery-row .status-delivered').first.wait_for()

        rate_limit_triggered = False
        for attempt in range(40):
            status, _ = call('/auth/login', 'POST', {
                'email': f'rate-{SUFFIX}-{attempt}@teammind.test',
                'password': 'wrong-platform-password',
            })
            if status == 429:
                rate_limit_triggered = True
                break
            assert status == 401, f'unexpected failed-login status: {status}'
        assert rate_limit_triggered, 'source login limit did not trigger'
        await page.get_by_role('button', name='刷新', exact=True).click()
        await page.get_by_text('登录请求已临时限流').wait_for()
        await page.get_by_text('来源标识').wait_for()
        await browser.close()

    _, notifications = call('/me/notifications', token=creator_token)
    need(any(n['teamId'].lower() == approved_team['id'].lower() and n['type'] == 'team_review' for n in notifications), True, 'approval notification persisted to creator')
    need(any(n['teamId'].lower() == rejected_team['id'].lower() and n['title'] == '团队申请未通过' and n['body'] == 'Incomplete application details' for n in notifications), True, 'rejection notification with reason persisted to creator')
    _, delivery_rows = call('/me/notification-deliveries', token=creator_token)
    review_deliveries = [row for row in delivery_rows if row['status'] == 'delivered' and row['type'] == 'team_review']
    need(len(review_deliveries) >= 2, True, 'both platform review notifications have delivered records')
    review_rows = call('/platform/teams', token=admin_token)[1]
    reviewed = next(row for row in review_rows if row['id'].lower() == rejected_team['id'].lower())
    need(reviewed['reviewReason'], 'Incomplete application details', 'rejection reason is persisted for audit')
    audit_rows = call('/platform/audit-log', token=admin_token)[1]
    need(any(row['teamId'].lower() == approved_team['id'].lower() and row['action'] == 'team.review.approved' for row in audit_rows), True, 'platform approval appears in audit API')
    need(any(row['teamId'].lower() == rejected_team['id'].lower() and row['action'] == 'team.review.rejected' and row['reason'] == 'Incomplete application details' for row in audit_rows), True, 'platform rejection reason appears in audit API')
    need(call(f'/teams/{rejected_team["id"]}', token=creator_token)[0], 403, 'rejected team remains inaccessible')
    need(call(f'/platform/teams/{approved_team["id"]}/decision', 'POST', {'decision': 'approved'}, creator_token)[0], 403, 'team owner cannot use platform API')
    need(call('/platform/audit-log', token=creator_token)[0], 403, 'team owner cannot read platform audit')
    print('PASS mobile super admin approval/rejection, login rate-limit alert, creator notifications, and rejected-team access denial')

asyncio.run(main())
