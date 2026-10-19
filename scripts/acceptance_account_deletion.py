"""Browser acceptance for authenticated account deletion and data erasure."""
import asyncio
import json
import os
import secrets
import urllib.request

from playwright.async_api import async_playwright

WEB = os.getenv('TEAMMIND_WEB_BASE', 'http://127.0.0.1:5174')
API = os.getenv('TEAMMIND_API_BASE', 'http://127.0.0.1:8188/api/v1')
EMAIL = f'account-delete-{secrets.token_hex(5)}@teammind.test'
PASSWORD = 'account-delete-' + secrets.token_hex(8)


def post(path, payload, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(API + path, data=json.dumps(payload).encode(), headers=headers, method='POST')
    try:
        with urllib.request.urlopen(request, timeout=15) as response:
            return response.status, json.loads(response.read())
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read())


async def main():
    status, _ = post('/auth/register', {'email': EMAIL, 'name': 'Deletion Test', 'password': PASSWORD})
    assert status == 201, status
    status, envelope = post('/auth/login', {'email': EMAIL, 'password': PASSWORD, 'remember': True})
    assert status == 200, envelope
    token = envelope['data']['accessToken']

    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch(headless=True)
        page = await browser.new_page(viewport={'width': 390, 'height': 844}, is_mobile=True, has_touch=True)
        await page.goto(WEB + '/login')
        await page.locator('input[autocomplete="username"]').fill(EMAIL)
        await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
        await page.get_by_role('button', name='登录', exact=True).click()
        await page.wait_for_url('**/teammind/overview')
        await page.goto(WEB + '/teammind/settings')
        panel = page.locator('.tm-account-delete-panel')
        await panel.get_by_role('heading', name='删除账号').wait_for()

        for width in (320, 390, 1280):
            await page.set_viewport_size({'width': width, 'height': 844})
            dimensions = await page.evaluate('({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})')
            assert dimensions['scroll'] <= dimensions['client'] + 1, (width, dimensions)

        await panel.get_by_label('当前密码').fill('wrong-password')
        await panel.get_by_label('确认文字').fill('删除账号')
        await panel.get_by_role('button', name='永久删除账号').click()
        await panel.get_by_role('alert').filter(has_text='当前密码错误').wait_for()
        assert await page.evaluate("sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken')")

        await panel.get_by_label('当前密码').fill(PASSWORD)
        await panel.get_by_role('button', name='永久删除账号').click()
        await page.wait_for_url('**/login?accountDeleted=1')
        await page.get_by_text('账号已删除，私人数据已清除。团队共享记录和安全审计以匿名形式保留。').wait_for()
        status, _ = post('/me/account', {'currentPassword': PASSWORD, 'confirmation': '删除账号'}, token)
        assert status == 401, status
        status, result = post('/auth/login', {'email': EMAIL, 'password': PASSWORD})
        assert status == 401 and result['code'] == 'INVALID_CREDENTIALS', result
        await browser.close()

    print('PASS account deletion: mobile settings form, wrong-password recovery, confirmed deletion, session revocation, and deleted-account login rejection')


asyncio.run(main())
