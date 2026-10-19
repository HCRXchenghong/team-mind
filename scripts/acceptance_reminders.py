"""Real Chromium reminder workflow against a disposable migrated database."""
import asyncio
import json
import secrets
import subprocess
import urllib.request
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo

from playwright.async_api import async_playwright

BASE = 'http://127.0.0.1:5174'
API = 'http://127.0.0.1:8188/api/v1'
VERIFY_DB = 'teammind_verify_reminder_20261017a'
SUFFIX = secrets.token_hex(5)
EMAIL = f'reminder-browser-{SUFFIX}@teammind.test'
PASSWORD = 'Reminder-browser-' + SUFFIX + '!'
TITLE = 'Browser reminder ' + SUFFIX
CANCEL_TITLE = 'Cancelled reminder ' + SUFFIX


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
    with urllib.request.urlopen(request, timeout=15) as response:
        return response.status, json.load(response)['data']


def future_account_time(minutes):
    return (datetime.now(ZoneInfo('Asia/Shanghai')) + timedelta(minutes=minutes)).strftime('%Y-%m-%dT%H:%M')


def force_due_for_acceptance(user_id, title):
    query = (
        "UPDATE scheduled_jobs SET run_at=UTC_TIMESTAMP(3)-INTERVAL 1 SECOND "
        "WHERE owner_user_id=UNHEX('" + user_id + "') AND job_type='notification.reminder' "
        "AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.title'))='" + title + "';"
    )
    if len(user_id) != 32 or any(char not in '0123456789ABCDEFabcdef' for char in user_id):
        raise AssertionError('invalid test user id')
    subprocess.run(
        ['docker', 'compose', 'exec', '-T', 'mysql', 'sh', '-c',
         'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --user=root "$1"', 'sh', VERIFY_DB],
        input=query.encode(), check=True, stdout=subprocess.DEVNULL,
    )


async def run():
    call('/auth/register', 'POST', {'email': EMAIL, 'name': 'Reminder Browser', 'password': PASSWORD})
    _, auth = call('/auth/login', 'POST', {'email': EMAIL, 'password': PASSWORD, 'remember': False})
    _, user = call('/me', token=auth['accessToken'])

    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch(headless=True)
        page = await browser.new_page(viewport={'width': 390, 'height': 844}, is_mobile=True, has_touch=True)
        await page.goto(BASE + '/login')
        await page.locator('input[autocomplete="username"]').fill(EMAIL)
        await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
        await page.get_by_role('button', name='登录', exact=True).tap()
        await page.wait_for_url('**/teammind/overview')
        await page.goto(BASE + '/teammind/notifications')
        await page.get_by_role('heading', name='定时提醒').wait_for()
        await page.get_by_label('提醒时间').fill(future_account_time(6))
        expected_wall_clock = datetime.strptime(await page.get_by_label('提醒时间').input_value(), '%Y-%m-%dT%H:%M')
        await page.get_by_label('标题').fill(TITLE)
        await page.get_by_label('内容（可选）').fill('worker delivery from persisted MySQL job')
        await page.get_by_role('button', name='创建提醒').tap()
        row = page.locator('.tm-reminder-row').filter(has_text=TITLE)
        await row.wait_for()
        assert '待执行' in await row.inner_text()
        reminders = call('/me/reminders', token=auth['accessToken'])[1]
        first = next(item for item in reminders if item['title'] == TITLE)
        assert first['status'] == 'scheduled'
        # Verify the submitted UTC timestamp represents the displayed account-zone wall clock.
        scheduled_wall_clock = datetime.fromisoformat(first['runAt'].replace('Z', '+00:00')).astimezone(ZoneInfo('Asia/Shanghai')).replace(tzinfo=None)
        assert scheduled_wall_clock == expected_wall_clock, (scheduled_wall_clock, expected_wall_clock)

        await page.get_by_label('提醒时间').fill(future_account_time(30))
        await page.get_by_label('标题').fill(CANCEL_TITLE)
        await page.get_by_role('button', name='创建提醒').tap()
        cancel_row = page.locator('.tm-reminder-row').filter(has_text=CANCEL_TITLE)
        await cancel_row.wait_for()
        await cancel_row.get_by_role('button', name='取消').tap()
        await cancel_row.get_by_text('已取消').wait_for()
        cancelled = next(item for item in call('/me/reminders', token=auth['accessToken'])[1] if item['title'] == CANCEL_TITLE)
        assert cancelled['status'] == 'cancelled'

        force_due_for_acceptance(user['id'], TITLE)
        for _ in range(40):
            first = next(item for item in call('/me/reminders', token=auth['accessToken'])[1] if item['title'] == TITLE)
            if first['status'] == 'succeeded':
                break
            await asyncio.sleep(0.25)
        assert first['status'] == 'succeeded', f"worker did not deliver reminder: {first}"
        notifications = call('/me/notifications', token=auth['accessToken'])[1]
        assert any(item['title'] == TITLE and not item['read'] for item in notifications)

        await page.reload()
        await page.get_by_role('heading', name=TITLE).wait_for()
        await page.set_viewport_size({'width': 320, 'height': 740})
        assert await page.evaluate('document.documentElement.scrollWidth <= window.innerWidth')
        print('PASS Chromium mobile login touch, create/cancel reminder, MySQL worker delivery, inbox visibility, reload persistence, 320px overflow')
        await browser.close()


asyncio.run(run())
