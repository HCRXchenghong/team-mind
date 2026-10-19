"""对智能体私人持久工作区执行实际浏览器验收。"""
import asyncio
import json
import os
import secrets
import urllib.request
from pathlib import Path

from playwright.async_api import async_playwright

BASE = os.getenv('TEAMMIND_WEB_BASE', 'http://127.0.0.1:5179')
API = os.getenv('TEAMMIND_API_BASE', 'http://127.0.0.1:8193/api/v1')
ENGINE = os.getenv('TEAMMIND_BROWSER', 'chromium')
if ENGINE not in {'chromium', 'firefox', 'webkit'}:
    raise ValueError('TEAMMIND_BROWSER must be chromium, firefox, or webkit')
SUFFIX = secrets.token_hex(5)
EMAIL = f'agent-workspace-{SUFFIX}@teammind.test'
PASSWORD = 'agent-workspace-' + SUFFIX


def register():
    request = urllib.request.Request(API + '/auth/register', data=json.dumps({
        'email': EMAIL, 'name': 'Workspace Browser User', 'password': PASSWORD,
    }).encode(), headers={'Content-Type': 'application/json'}, method='POST')
    with urllib.request.urlopen(request, timeout=10) as response:
        assert response.status == 201


async def run():
    register()
    async with async_playwright() as pw:
        browser = await getattr(pw, ENGINE).launch(headless=True)
        page = await browser.new_page(viewport={'width': 390, 'height': 844}, is_mobile=True, has_touch=True)
        web_origin = BASE.split('/')[0] + '//' + BASE.split('/')[2]
        api_origin = API.rsplit('/api/v1', 1)[0]

        async def route_api(route):
            target = route.request.url.replace(web_origin, api_origin, 1)
            response = await route.fetch(url=target)
            await route.fulfill(response=response)

        # Route directly to the isolated API under test. This keeps the browser
        # workflow independent from whichever API the local Vite proxy targets.
        await page.route('**/api/v1/**', route_api)
        await page.goto(BASE + '/login')
        await page.locator('input[autocomplete="username"]').fill(EMAIL)
        await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
        await page.get_by_role('button', name='登录', exact=True).click()
        await page.wait_for_url('**/teammind/overview')

        await page.goto(BASE + '/teammind/assistant')
        workspace = page.locator('.tm-workspace-files')
        await workspace.wait_for()
        await workspace.get_by_text('我的私人持久工作区').wait_for()
        assert await workspace.locator('label.tm-workspace-import input[type="file"]').count() == 1
        await workspace.get_by_text('私人工作区为空。').wait_for()

        file_input = workspace.locator('label.tm-workspace-import input[type="file"]')
        await file_input.set_input_files({
            'name': 'browser-notes.md',
            'mimeType': 'text/markdown',
            'buffer': b'first private version\n',
        })
        row = workspace.locator('.tm-agent-upload').filter(has_text='browser-notes.md')
        await row.wait_for()

        await row.get_by_role('button', name='查看/编辑').click()
        dialog = page.locator('.arco-modal').filter(has_text='编辑私人工作区文件')
        await dialog.wait_for()
        await dialog.locator('input').fill('browser-notes.exe')
        await dialog.get_by_role('button', name='确定').click()
        await dialog.get_by_role('alert').wait_for()
        assert await dialog.locator('textarea').input_value() == 'first private version\n'
        await dialog.locator('input').fill('browser-notes.md')
        await dialog.locator('textarea').fill('edited private workspace content\n')
        await dialog.get_by_role('button', name='确定').click()
        await page.get_by_text('私人工作区文件已更新').wait_for()

        await page.reload()
        workspace = page.locator('.tm-workspace-files')
        row = workspace.locator('.tm-agent-upload').filter(has_text='browser-notes.md')
        await row.wait_for()
        await page.set_viewport_size({'width': 320, 'height': 740})
        dimensions = await page.evaluate('({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})')
        assert dimensions['scroll'] <= dimensions['client'] + 1, dimensions

        await page.goto(BASE + '/teammind/settings')
        async with page.expect_download() as export_download:
            await page.get_by_role('button', name='下载我的数据副本').click()
        download = await export_download.value
        data = json.loads(Path(await download.path()).read_text())
        files = data['personalAgentWorkspaceFiles']
        assert len(files) == 1 and files[0]['name'] == 'browser-notes.md'
        assert files[0]['content'] == 'edited private workspace content\n'

        await page.goto(BASE + '/teammind/assistant')
        workspace = page.locator('.tm-workspace-files')
        row = workspace.locator('.tm-agent-upload').filter(has_text='browser-notes.md')
        await row.get_by_role('button', name='删除', exact=True).click()
        await page.get_by_text('永久删除这份私人工作区文件？').wait_for()
        await page.get_by_role('button', name='删除', exact=True).last.click()
        await row.wait_for(state='detached')
        await page.reload()
        await page.locator('.tm-workspace-files').get_by_text('私人工作区为空。').wait_for()
        token = await page.evaluate("sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken')")
        deletion = await page.request.post(
            API.rsplit('/api/v1', 1)[0] + '/api/v1/me/account',
            data={'currentPassword': PASSWORD, 'confirmation': '删除账号'},
            headers={'Authorization': 'Bearer ' + token},
        )
        assert deletion.status == 200, await deletion.text()
        await browser.close()
    print(f'私人工作区浏览器验收通过（{ENGINE}）：创建、读写、失败重试、刷新持久化、导出、删除和 320px 手机视口。')


asyncio.run(run())
