"""登录按钮六状态及鼠标、键盘和触屏真实 API 回归。"""
import asyncio
import json
import os
import secrets
import urllib.request
from urllib.parse import urlsplit

from playwright.async_api import async_playwright

WEB = os.getenv('TEAMMIND_WEB_BASE', 'http://127.0.0.1:5174').rstrip('/')
API = os.getenv('TEAMMIND_API_BASE', 'http://127.0.0.1:8188/api/v1').rstrip('/')
ENGINE = os.getenv('TEAMMIND_BROWSER', 'webkit')
if ENGINE not in {'chromium', 'firefox', 'webkit'}:
    raise ValueError('TEAMMIND_BROWSER must be chromium, firefox, or webkit')

suffix = secrets.token_hex(6)
email = f'login-controls-{suffix}@teammind.test'
password = f'login-controls-{suffix}-Passw0rd!'


def register():
    request = urllib.request.Request(
        API + '/auth/register',
        data=json.dumps({'email': email, 'name': '登录控件验收', 'password': password}).encode(),
        headers={'Content-Type': 'application/json'},
        method='POST',
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        assert response.status == 201


def contrast_ratio(foreground, background):
    def rgb(value):
        return [int(channel.strip()) for channel in value[value.index('(') + 1:value.index(')')].split(',')[:3]]

    def luminance(value):
        channels = [channel / 255 for channel in rgb(value)]
        linear = [channel / 12.92 if channel <= 0.04045 else ((channel + 0.055) / 1.055) ** 2.4 for channel in channels]
        return sum(channel * weight for channel, weight in zip(linear, (0.2126, 0.7152, 0.0722)))

    light, dark = sorted((luminance(foreground), luminance(background)), reverse=True)
    return (light + 0.05) / (dark + 0.05)


async def main():
    register()
    web_origin = f'{urlsplit(WEB).scheme}://{urlsplit(WEB).netloc}'
    api_origin = f'{urlsplit(API).scheme}://{urlsplit(API).netloc}'

    async with async_playwright() as playwright:
        browser = await getattr(playwright, ENGINE).launch(headless=True)

        async def new_page(viewport, mobile=False):
            page = await browser.new_page(viewport=viewport, is_mobile=mobile, has_touch=mobile)
            page.set_default_timeout(8000)

            async def route_api(route):
                target = route.request.url.replace(web_origin, api_origin, 1)
                response = await route.fetch(url=target)
                await route.fulfill(response=response)

            await page.route('**/api/v1/**', route_api)
            await page.goto(WEB + '/login', wait_until='domcontentloaded')
            return page

        async def real_mouse_click(page, button, horizontal='center'):
            box = await button.bounding_box()
            assert box and box['width'] >= 180 and box['height'] >= 44, box
            x = box['x'] + 1 if horizontal == 'left' else box['x'] + box['width'] - 1 if horizontal == 'right' else box['x'] + box['width'] / 2
            await page.mouse.click(x, box['y'] + box['height'] / 2)

        async def fill_credentials(page, entered_password=password):
            await page.locator('input[autocomplete="username"]').fill(email)
            await page.locator('input[autocomplete="current-password"]').fill(entered_password)

        async def sign_out(page):
            token = await page.evaluate("sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken')")
            assert token
            response = await page.request.post(API + '/auth/logout', headers={'Authorization': 'Bearer ' + token})
            assert response.status == 200, await response.text()
            await page.evaluate("sessionStorage.removeItem('teammind.accessToken'); localStorage.removeItem('teammind.accessToken')")
            await page.goto(WEB + '/login', wait_until='domcontentloaded')

        desktop = await new_page({'width': 1280, 'height': 900})
        button = desktop.get_by_role('button', name='登录', exact=True)
        no_hover = await button.evaluate('''element => {
          const style = getComputedStyle(element), rect = element.getBoundingClientRect();
          return {display:style.display, visibility:style.visibility, opacity:style.opacity,
            color:style.color, background:style.backgroundColor, disabled:element.disabled,
            hover:element.matches(':hover'), width:rect.width, height:rect.height};
        }''')
        assert no_hover['display'] != 'none' and no_hover['visibility'] == 'visible' and no_hover['opacity'] == '1', no_hover
        assert not no_hover['disabled'] and not no_hover['hover'] and no_hover['height'] >= 44, no_hover
        assert contrast_ratio(no_hover['color'], no_hover['background']) >= 4.5, no_hover
        hit_targets = await button.evaluate('''element => {
          const rect = element.getBoundingClientRect();
          return [[rect.left + rect.width / 2, rect.top + rect.height / 2],
            [rect.left + 1, rect.top + rect.height / 2], [rect.right - 1, rect.top + rect.height / 2]]
            .map(([x, y]) => element.contains(document.elementFromPoint(x, y)));
        }''')
        assert all(hit_targets), hit_targets
        await button.hover()
        assert await button.evaluate("element => element.matches(':hover')")

        # Native form submission must show field-level validation on pointer input.
        await real_mouse_click(desktop, button)
        await desktop.get_by_text('请输入邮箱').wait_for()
        await desktop.get_by_text('请输入密码').wait_for()

        # A bad password reports the API error and a real center click submits.
        await fill_credentials(desktop, 'incorrect-password')
        await real_mouse_click(desktop, button)
        await desktop.get_by_role('alert').filter(has_text='邮箱或密码错误').wait_for()

        # Hold the real login API response to verify disabled/loading, then click
        # the right edge of the visible button and allow the same request through.
        intercepted = asyncio.Event()
        release = asyncio.Event()

        async def hold_login(route):
            intercepted.set()
            await release.wait()
            target = route.request.url.replace(web_origin, api_origin, 1)
            response = await route.fetch(url=target)
            await route.fulfill(response=response)

        await desktop.route('**/api/v1/auth/login', hold_login)
        await desktop.locator('input[autocomplete="current-password"]').fill(password)
        click_task = asyncio.create_task(real_mouse_click(desktop, button, 'right'))
        await asyncio.wait_for(intercepted.wait(), timeout=8)
        loading = await button.evaluate('''element => ({disabled:element.disabled,
          loading:element.classList.contains('arco-btn-loading')})''')
        assert loading == {'disabled': True, 'loading': True}, loading
        release.set()
        await click_task
        await desktop.wait_for_url('**/teammind/overview')
        assert await button.count() == 0
        await desktop.unroute('**/api/v1/auth/login', hold_login)
        await sign_out(desktop)

        # The other horizontal edge must also submit the form.
        await fill_credentials(desktop)
        await real_mouse_click(desktop, button, 'left')
        await desktop.wait_for_url('**/teammind/overview')
        await sign_out(desktop)

        # Enter from the password field and Space from a keyboard-visible focus.
        await fill_credentials(desktop)
        await desktop.locator('input[autocomplete="current-password"]').press('Enter')
        await desktop.wait_for_url('**/teammind/overview')
        await sign_out(desktop)

        await fill_credentials(desktop)
        password_input = desktop.locator('input[autocomplete="current-password"]')
        await password_input.press('Tab')
        await desktop.keyboard.press('Tab')
        await desktop.keyboard.press('Tab')
        focus = await button.evaluate('''element => ({active:document.activeElement === element,
          visible:element.matches(':focus-visible'), outline:getComputedStyle(element).outlineStyle,
          width:getComputedStyle(element).outlineWidth})''')
        assert focus == {'active': True, 'visible': True, 'outline': 'solid', 'width': '3px'}, focus
        await desktop.keyboard.press('Space')
        await desktop.wait_for_url('**/teammind/overview')
        await sign_out(desktop)

        # Use an actual touch input event on a 390 CSS-pixel mobile viewport.
        mobile = await new_page({'width': 390, 'height': 844}, mobile=True)
        await fill_credentials(mobile)
        button = mobile.get_by_role('button', name='登录', exact=True)
        box = await button.bounding_box()
        assert box and box['height'] >= 44, box
        await mobile.touchscreen.tap(box['x'] + box['width'] / 2, box['y'] + box['height'] / 2)
        await mobile.wait_for_url('**/teammind/overview')
        token = await mobile.evaluate("sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken')")
        deleted = await mobile.request.post(
            API + '/me/account',
            data={'currentPassword': password, 'confirmation': '删除账号'},
            headers={'Authorization': 'Bearer ' + token},
        )
        assert deleted.status == 200, await deleted.text()
        await browser.close()

    print(f'登录控件回归通过（{ENGINE}）：未悬停/悬停/焦点/加载/失败/成功、鼠标中心与两侧边缘、Enter、Space、390px 触屏；使用真实 API 并注销清理测试账号。')


asyncio.run(main())
