"""Check remembered-login recovery after a new browser tab/app process."""
import asyncio
import json
import os
import secrets
import urllib.request

from playwright.async_api import async_playwright

BASE = os.getenv('TEAMMIND_WEB_BASE', 'http://127.0.0.1:5174').rstrip('/')
API = os.getenv('TEAMMIND_API_BASE', 'http://127.0.0.1:8188/api/v1').rstrip('/')
EMAIL = f'remembered-{secrets.token_hex(8)}@example.invalid'
PASSWORD = 'Remembered-Session-' + secrets.token_urlsafe(18)


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
    with urllib.request.urlopen(request, timeout=10) as response:
        return response.status, json.load(response)


async def run():
    status, _ = call('/auth/register', 'POST', {
        'email': EMAIL, 'name': 'Remembered Session Test', 'password': PASSWORD,
    })
    assert status == 201
    try:
        async with async_playwright() as playwright:
            browser = await playwright.chromium.launch(headless=True)
            context = await browser.new_context()
            first = await context.new_page()
            await first.goto(BASE + '/login')
            await first.locator('input[autocomplete="username"]').fill(EMAIL)
            await first.locator('input[autocomplete="current-password"]').fill(PASSWORD)
            await first.get_by_text('保持登录', exact=True).click()
            assert await first.locator('.arco-checkbox-target').is_checked()
            await first.get_by_role('button', name='登录', exact=True).click()
            await first.wait_for_url('**/teammind/overview')
            stored = await first.evaluate('''password => ({
              localToken: !!localStorage.getItem('teammind.accessToken'),
              sessionToken: !!sessionStorage.getItem('teammind.accessToken'),
              passwordStored: [localStorage, sessionStorage].some(s =>
                Object.keys(s).some(k => /password/i.test(k)) ||
                Object.values(s).some(v => v === password)
              )
            })''', PASSWORD)
            assert stored == {'localToken': True, 'sessionToken': False, 'passwordStored': False}, stored
            await first.close()

            reopened = await context.new_page()
            await reopened.goto(BASE + '/login?redirect=%2Fteammind%2Fsettings')
            await reopened.wait_for_url('**/teammind/settings', timeout=15000)
            identity = await reopened.evaluate('''async () => {
              const token = localStorage.getItem('teammind.accessToken');
              const response = await fetch('/api/v1/me', {headers:{Authorization:'Bearer '+token}});
              return {status:response.status, email:(await response.json()).data?.email};
            }''')
            assert identity == {'status': 200, 'email': EMAIL}, identity
            token = await reopened.evaluate("localStorage.getItem('teammind.accessToken')")
            logout_status = await reopened.evaluate('''async token => (await fetch('/api/v1/auth/logout', {
              method:'POST', headers:{Authorization:'Bearer '+token}
            })).status''', token)
            assert logout_status == 200, logout_status
            stale_status = await reopened.evaluate('''async token => (await fetch('/api/v1/me', {
              headers:{Authorization:'Bearer '+token}
            })).status''', token)
            assert stale_status == 401, stale_status
            await browser.close()
        print('PASS remembered login restores a fresh tab, stores no plaintext password, and logout revokes the token')
    finally:
        _, login = call('/auth/login', 'POST', {'email': EMAIL, 'password': PASSWORD, 'remember': False})
        call('/me/account', 'POST', {'currentPassword': PASSWORD, 'confirmation': '删除账号'}, login['data']['accessToken'])


asyncio.run(run())
