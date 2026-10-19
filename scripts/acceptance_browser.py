"""Chromium browser regression against a running local Vite + Go/MySQL stack.

Requires Python Playwright and its Chromium binary. Screenshots are written to
/tmp/teammind-browser-current, with no password or access token in artifacts.
"""
import asyncio
import json
import secrets
import urllib.error
import urllib.request
from pathlib import Path
from playwright.async_api import async_playwright

BASE = 'http://127.0.0.1:5174'
API = 'http://127.0.0.1:8188/api/v1'
OUT = Path('/tmp/teammind-browser-current')
OUT.mkdir(exist_ok=True)
email = 'browser-' + secrets.token_hex(5) + '@teammind.test'
password = 'browser-test-' + secrets.token_hex(12)
payload = json.dumps({'email': email, 'name': 'Browser Tester', 'password': password}).encode()
req = urllib.request.Request(API + '/auth/register', data=payload, headers={'Content-Type': 'application/json'}, method='POST')
with urllib.request.urlopen(req, timeout=10) as response:
    assert response.status == 201

async def run():
    async with async_playwright() as pw:
        browser = await pw.chromium.launch(headless=True)
        page = await browser.new_page(viewport={'width': 1280, 'height': 800})
        # Exercise the real public registration and one-use reset flow in the
        # browser, not only through direct setup API calls.
        ui_email = 'browser-auth-' + secrets.token_hex(5) + '@teammind.test'
        ui_password = 'browser-auth-' + secrets.token_hex(12)
        reset_password = ui_password + '-reset'
        await page.goto(BASE + '/register')
        await page.locator('input[type="email"]').fill(ui_email)
        await page.locator('input[autocomplete="name"]').fill('Browser Auth Tester')
        await page.locator('input[autocomplete="new-password"]').fill(ui_password)
        await page.get_by_role('button', name='注册账号').click()
        await page.wait_for_function("location.pathname === '/login'")
        await page.get_by_role('link', name='忘记密码？').click()
        await page.locator('input[type="email"]').fill(ui_email)
        await page.get_by_role('button', name='找回密码').click()
        await page.wait_for_function("location.pathname === '/reset-password' && new URLSearchParams(location.search).has('token')")
        await page.locator('input[autocomplete="new-password"]').fill(reset_password)
        await page.get_by_role('button', name='重置密码').click()
        await page.wait_for_function("location.pathname === '/login'")
        await page.locator('input[autocomplete="username"]').fill(ui_email)
        await page.locator('input[autocomplete="current-password"]').fill(reset_password)
        await page.get_by_role('button', name='登录', exact=True).click()
        await page.wait_for_url('**/teammind/overview')
        await page.get_by_role('button', name='账号菜单').click()
        await page.get_by_text('退出登录', exact=True).click()
        await page.wait_for_url('**/login')
        print('PASS browser registration, development reset-link navigation, one-use password reset and sign-in')
        await page.goto(BASE + '/login')
        button = page.get_by_role('button', name='登录', exact=True)
        await button.wait_for()
        css = await button.evaluate('el => { const s=getComputedStyle(el); return {background:s.backgroundColor,color:s.color,opacity:s.opacity,display:s.display,visibility:s.visibility,pointerEvents:s.pointerEvents} }')
        assert css['background'] not in ('rgba(0, 0, 0, 0)', 'transparent'), css
        assert css['display'] != 'none' and css['visibility'] == 'visible' and css['pointerEvents'] != 'none', css
        hit = await button.evaluate('el => {const r=el.getBoundingClientRect(); return [[r.left+r.width/2,r.top+r.height/2],[r.left+3,r.top+3],[r.right-3,r.bottom-3]].map(([x,y]) => el.contains(document.elementFromPoint(x,y)))}')
        assert all(hit), f'button center and edges must receive pointer input: {hit}'
        def rgb(value):
            return [int(v.strip()) for v in value[value.index('(')+1:value.index(')')].split(',')[:3]]
        def luminance(color):
            channels = [v/255 for v in rgb(color)]
            linear = [v/12.92 if v <= .04045 else ((v+.055)/1.055)**2.4 for v in channels]
            return sum(a*b for a,b in zip(linear,[.2126,.7152,.0722]))
        fg, bg = luminance(css['color']), luminance(css['background'])
        assert (max(fg,bg)+.05)/(min(fg,bg)+.05) >= 4.5, css
        await page.screenshot(path=str(OUT/'login-unhovered.png'))
        await button.hover()
        await page.screenshot(path=str(OUT/'login-hover.png'))
        await button.focus()
        await page.screenshot(path=str(OUT/'login-focus.png'))
        await button.click()
        assert await page.get_by_text('请输入邮箱').count() > 0
        assert await page.get_by_text('请输入密码').count() > 0
        await page.locator('input[autocomplete="username"]').fill(email)
        await page.locator('input[autocomplete="current-password"]').fill('invalid-password-123')
        intercepted = asyncio.Event()
        release = asyncio.Event()
        async def hold(route):
            intercepted.set()
            await release.wait()
            await route.continue_()
        await page.route('**/api/v1/auth/login', hold)
        click_task = asyncio.create_task(button.click())
        await asyncio.wait_for(intercepted.wait(), timeout=10)
        assert await button.is_disabled(), 'login button must block duplicate submits while loading'
        await page.screenshot(path=str(OUT/'login-loading.png'))
        release.set()
        await click_task
        await page.get_by_role('alert').filter(has_text='邮箱或密码错误').wait_for()
        await page.unroute('**/api/v1/auth/login', hold)
        calls = 0
        async def observe(route):
            nonlocal calls
            calls += 1
            await route.continue_()
        await page.route('**/api/v1/auth/login', observe)
        await page.locator('input[autocomplete="current-password"]').press('Enter')
        await page.get_by_role('alert').filter(has_text='邮箱或密码错误').wait_for()
        assert calls == 1, calls
        await page.screenshot(path=str(OUT/'login-failed.png'))
        await button.focus()
        await page.keyboard.press('Space')
        await page.wait_for_timeout(300)
        assert calls == 2, calls
        await page.locator('input[autocomplete="current-password"]').fill(password)
        await button.click(position={'x': 3, 'y': 3})
        await page.wait_for_url('**/teammind/overview')
        await page.screenshot(path=str(OUT/'login-success.png'))
        await page.reload()
        assert '/teammind/overview' in page.url
        await page.goto(BASE + '/teammind/memories')
        await page.get_by_role('button', name='添加').click()
        await page.locator('.arco-modal:visible textarea').fill('Private browser fact')
        await page.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await page.get_by_text('Private browser fact').wait_for()
        await page.reload()
        await page.get_by_text('Private browser fact').wait_for()
        await page.goto(BASE + '/teammind/assistant')
        await page.get_by_text('本地模拟模式：回复不来自大模型').wait_for()
        await page.locator('.tm-chat-compose textarea').fill('Browser AI test')
        await page.get_by_role('button', name='发送').click()
        await page.get_by_text('[本地模拟模式] 已收到你的消息：Browser AI test').wait_for()
        await page.reload()
        await page.get_by_role('button', name='Browser AI test').click()
        await page.get_by_text('[本地模拟模式] 已收到你的消息：Browser AI test').wait_for()
        await page.get_by_role('button', name='保存为记忆候选').last.click()
        await page.locator('.arco-modal:visible textarea').fill('Browser reviewed memory candidate')
        await page.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await page.get_by_text('记忆候选已保存，待你确认').wait_for()
        await page.goto(BASE + '/teammind/memories')
        candidate_card = page.locator('article.tm-panel').filter(has_text='Browser reviewed memory candidate')
        await candidate_card.get_by_text('待确认').wait_for()
        await candidate_card.get_by_text('来源对话').first.wait_for()
        await candidate_card.get_by_role('button', name='查看来源对话').click()
        await page.wait_for_url('**/teammind/assistant?**')
        await page.wait_for_function("document.querySelectorAll('[data-message-id]').length >= 2")
        assert 'sourceMessageId=' in page.url
        await page.goto(BASE + '/teammind/memories')
        candidate_card = page.locator('article.tm-panel').filter(has_text='Browser reviewed memory candidate')
        await candidate_card.get_by_role('button', name='编辑/确认').click()
        await page.locator('.arco-modal:visible input[type="date"]').nth(0).fill('2026-11-17')
        await page.locator('.arco-modal:visible input[type="date"]').nth(1).fill('2027-11-17')
        await page.locator('.arco-modal:visible input[type="checkbox"]').check()
        await page.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await candidate_card.get_by_text('已确认').wait_for()
        await candidate_card.get_by_text('复核日期：2026-11-17').wait_for()
        token = await page.evaluate('sessionStorage.getItem("teammind.accessToken")')
        await page.get_by_role('button', name='账号菜单').click()
        await page.get_by_text('退出登录', exact=True).click()
        await page.wait_for_url('**/login')
        check = urllib.request.Request(API + '/me', headers={'Authorization': 'Bearer ' + token})
        try:
            urllib.request.urlopen(check, timeout=10)
            raise AssertionError('logged out token still works')
        except urllib.error.HTTPError as error:
            assert error.code == 401
        await page.evaluate('(t) => sessionStorage.setItem("teammind.accessToken",t)', token)
        await page.goto(BASE + '/teammind/overview')
        await page.wait_for_url('**/login?**')
        assert 'redirect=' in page.url, 'expired session must preserve intended route'
        await page.locator('input[autocomplete="username"]').fill(email)
        await page.locator('input[autocomplete="current-password"]').fill(password)
        await page.route('**/api/v1/auth/login', lambda route: route.abort('failed'))
        await page.get_by_role('button', name='登录', exact=True).click()
        await page.get_by_role('alert').filter(has_text='无法连接服务器').wait_for()
        assert await page.get_by_role('button', name='登录', exact=True).is_enabled()
        await page.screenshot(path=str(OUT/'login-network-failed.png'))
        await page.unroute('**/api/v1/auth/login')
        async def timeout_login(route):
            await asyncio.sleep(21)
            try:
                await route.abort('timedout')
            except Exception:
                pass  # Axios has already timed out and closed this request.
        await page.route('**/api/v1/auth/login', timeout_login)
        await page.get_by_role('button', name='登录', exact=True).click()
        await page.get_by_role('alert').filter(has_text='请求超时').wait_for(timeout=25000)
        assert await page.get_by_role('button', name='登录', exact=True).is_enabled()
        await page.screenshot(path=str(OUT/'login-timeout.png'))
        await page.unroute('**/api/v1/auth/login', timeout_login)
        await page.wait_for_timeout(1200)

        # Separate touch browser context, same real account. Test the login
        # control and every visible TeamMind route for page-wide overflow.
        mobile = await browser.new_context(viewport={'width': 390, 'height': 844}, is_mobile=True, has_touch=True, device_scale_factor=1)
        phone = await mobile.new_page()
        await phone.goto(BASE + '/login')
        await phone.locator('input[autocomplete="username"]').fill(email)
        await phone.locator('input[autocomplete="current-password"]').fill(password)
        await phone.get_by_role('button', name='登录', exact=True).tap()
        await phone.wait_for_url('**/teammind/overview')
        routes = ['overview','projects','tasks','gantt','approvals','members','assistant','memories','notifications','settings','wechat']
        for width in (320,360,375,390,430,768,1024,1280):
            await phone.set_viewport_size({'width':width,'height':844})
            for route in routes:
                await phone.goto(BASE + '/teammind/' + route)
                await phone.wait_for_timeout(250)
                dimensions = await phone.evaluate('({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})')
                assert dimensions['scroll'] <= dimensions['client'] + 1, (width,route,dimensions)
            print('PASS route viewport', width)
        await phone.set_viewport_size({'width':390,'height':844})
        await phone.goto(BASE + '/teammind/settings')
        team_panel = phone.locator('.tm-panel').filter(has=phone.get_by_role('heading', name='创建团队'))
        await team_panel.locator('input').first.fill('Browser WeChat simulation')
        await team_panel.get_by_role('button', name='创建团队').tap()
        await phone.get_by_text('团队已创建').wait_for()
        await phone.goto(BASE + '/teammind/wechat')
        await phone.get_by_text('本地模拟渠道：不能用于真实微信扫码或消息收发。').wait_for()
        await phone.get_by_role('button', name='连接微信').tap()
        await phone.get_by_role('img', name='微信渠道二维码').wait_for()
        await phone.get_by_role('button', name='模拟确认（测试专用）').tap()
        await phone.get_by_text('模拟确认，仅测试').wait_for()
        await phone.get_by_text('尚无已确认绑定').wait_for()
        await phone.get_by_role('button', name='取消').tap()
        await phone.screenshot(path=str(OUT/'wechat-390.png'))
        print('PASS Chromium six login states, contrast, center/edge hit, Enter, Space, touch, logout, session expiry, network/timeout feedback, refresh, route widths and explicitly simulated WeChat QR/confirmation without a fake binding')
        await browser.close()

asyncio.run(run())
