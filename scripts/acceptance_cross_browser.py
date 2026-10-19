"""Browser-engine smoke check for Chromium, Firefox and WebKit.

The WebKit context emulates an iPhone-class Safari viewport. This is useful
coverage for layout and login behavior, but is not a physical iOS Safari test.
"""
import asyncio
import json
import os
import secrets
import urllib.request
from playwright.async_api import async_playwright

BASE = os.getenv('TEAMMIND_WEB_BASE', 'http://127.0.0.1:5174')
API = os.getenv('TEAMMIND_API_BASE', 'http://127.0.0.1:8188/api/v1')
ROUTES = ['overview', 'projects', 'tasks', 'gantt', 'approvals', 'members', 'assistant', 'memories', 'notifications', 'settings', 'wechat']
AUTH_ROUTES = [('register', '注册账号'), ('forgot-password', '找回密码'), ('reset-password', '重置密码')]
HEADINGS = {
    'overview': '团队概览', 'projects': '项目管理', 'tasks': '任务中心',
    'gantt': '项目甘特图', 'approvals': '审批中心', 'members': '团队成员',
    'assistant': 'AI 助手', 'memories': '记忆与知识', 'notifications': '通知中心',
    'settings': '设置', 'wechat': '微信渠道绑定',
}
WIDTHS = [320, 360, 375, 390, 430, 768, 1024, 1280]
SUFFIX = secrets.token_hex(5)
EMAIL = f'cross-browser-{SUFFIX}@teammind.test'
PASSWORD = 'Cross-browser-' + SUFFIX + '!'

request = urllib.request.Request(
    API + '/auth/register',
    data=json.dumps({'email': EMAIL, 'name': 'Cross Browser Tester', 'password': PASSWORD}).encode(),
    headers={'Content-Type': 'application/json'},
    method='POST',
)
with urllib.request.urlopen(request, timeout=10) as response:
    assert response.status == 201

async def verify_engine(browser_type, mobile=False):
    browser = await browser_type.launch(headless=True)
    if mobile:
        page = await browser.new_page(viewport={'width': 390, 'height': 844}, is_mobile=True, has_touch=True, device_scale_factor=1)
    else:
        page = await browser.new_page(viewport={'width': 1280, 'height': 800})
    page_errors = []
    failed_responses = []
    page.on('pageerror', lambda error: page_errors.append(str(error)))
    page.on('response', lambda response: failed_responses.append((response.status, response.url.split('?', 1)[0])) if response.status >= 400 else None)
    for width in WIDTHS:
        await page.set_viewport_size({'width': width, 'height': 844})
        for path, title in AUTH_ROUTES:
            await page.goto(f'{BASE}/{path}', wait_until='domcontentloaded')
            await page.get_by_role('heading', name=title, exact=True).wait_for(state='visible', timeout=20000)
            dimensions = await page.evaluate('({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})')
            assert dimensions['scroll'] <= dimensions['client'] + 1, (browser_type.name, width, path, dimensions)
    await page.goto(BASE + '/login', wait_until='domcontentloaded')
    button = page.get_by_role('button', name='登录', exact=True)
    await page.locator('input[autocomplete="username"]').wait_for(state='visible')
    style = await button.evaluate("el => {const s=getComputedStyle(el);return {background:s.backgroundColor,color:s.color,display:s.display,visibility:s.visibility,opacity:s.opacity,pointerEvents:s.pointerEvents}}")
    assert style['display'] != 'none' and style['visibility'] == 'visible' and style['pointerEvents'] != 'none' and float(style['opacity']) > 0, style
    assert style['background'] not in ('transparent', 'rgba(0, 0, 0, 0)'), style
    await page.locator('input[autocomplete="username"]').fill(EMAIL)
    await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
    if mobile:
        await button.tap()
    else:
        await button.click()
    await page.wait_for_function("location.pathname === '/teammind/overview'", timeout=15000)
    for width in WIDTHS:
        await page.set_viewport_size({'width': width, 'height': 844})
        for route in ROUTES:
            await page.goto(f'{BASE}/teammind/{route}', wait_until='domcontentloaded')
            try:
                await page.get_by_role('heading', name=HEADINGS[route], exact=True).wait_for(state='visible', timeout=20000)
            except Exception as error:
                body = (await page.locator('body').inner_text())[:1000]
                raise AssertionError(
                    f'{browser_type.name} width={width} route={route} heading missing; '
                    f'url={page.url}; body={body!r}; failed_responses={failed_responses[-10:]!r}; '
                    f'page_errors={page_errors[-5:]!r}'
                ) from error
            dimensions = await page.evaluate('({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})')
            assert dimensions['scroll'] <= dimensions['client'] + 1, (browser_type.name, width, route, dimensions)
        print('PASS', browser_type.name, 'mobile-emulation' if mobile else 'desktop', width)
    assert not page_errors, (browser_type.name, page_errors)
    await browser.close()

async def main():
    try:
        async with async_playwright() as playwright:
            await verify_engine(playwright.firefox)
            await verify_engine(playwright.webkit, mobile=True)
    finally:
        login = urllib.request.Request(
            API + '/auth/login',
            data=json.dumps({'email': EMAIL, 'password': PASSWORD, 'remember': False}).encode(),
            headers={'Content-Type': 'application/json'},
            method='POST',
        )
        with urllib.request.urlopen(login, timeout=10) as response:
            token = json.load(response)['data']['accessToken']
        deletion = urllib.request.Request(
            API + '/me/account',
            data=json.dumps({'currentPassword': PASSWORD, 'confirmation': '删除账号'}).encode(),
            headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token},
            method='POST',
        )
        with urllib.request.urlopen(deletion, timeout=10) as response:
            assert response.status == 200
    print('PASS Firefox and WebKit login, register, forgot/reset password, and responsive route smoke; WebKit emulation is not a physical iOS Safari test')

asyncio.run(main())
