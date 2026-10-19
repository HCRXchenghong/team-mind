"""Chromium and MySQL acceptance for deleting an owned assistant conversation."""
import asyncio
import json
import os
import secrets
import urllib.request

from playwright.async_api import async_playwright

WEB = os.getenv('TEAMMIND_WEB_BASE', 'http://127.0.0.1:5174')
API = os.getenv('TEAMMIND_API_BASE', 'http://127.0.0.1:8188/api/v1')
EMAIL = f'conversation-delete-{secrets.token_hex(5)}@teammind.test'
PASSWORD = 'conversation-delete-' + secrets.token_hex(8)


def call(path, method='GET', payload=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    data = None if payload is None else json.dumps(payload).encode()
    request = urllib.request.Request(API + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=15) as response:
            return response.status, json.loads(response.read())
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read())


async def main():
    assert call('/auth/register', 'POST', {'email': EMAIL, 'name': 'Conversation Delete', 'password': PASSWORD})[0] == 201
    status, login = call('/auth/login', 'POST', {'email': EMAIL, 'password': PASSWORD, 'remember': True})
    assert status == 200, login
    token = login['data']['accessToken']
    status, created = call('/me/conversations', 'POST', {'teamId': '', 'confirmTeam': False}, token)
    assert status == 201, created
    conversation_id = created['data']['id']
    status, _ = call(f'/me/conversations/{conversation_id}/messages', 'POST', {'content': 'Delete this private conversation', 'confirmTeam': False}, token)
    assert status == 201, status

    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch(headless=True)
        page = await browser.new_page(viewport={'width': 390, 'height': 844}, is_mobile=True, has_touch=True)
        await page.goto(WEB + '/login')
        await page.locator('input[autocomplete="username"]').fill(EMAIL)
        await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
        await page.get_by_role('button', name='登录', exact=True).click()
        await page.wait_for_url('**/teammind/overview')
        await page.goto(WEB + '/teammind/assistant?conversationId=' + conversation_id)
        await page.get_by_text('Delete this private conversation', exact=True).last.wait_for()
        await page.get_by_role('button', name='删除当前对话').click()
        dialog = page.locator('.arco-popconfirm:visible')
        await dialog.get_by_role('button', name='永久删除').click()
        await page.get_by_text('对话和消息已删除', exact=False).wait_for()
        assert await page.get_by_text('Delete this private conversation').count() == 0
        assert await page.locator('.tm-conversation').filter(has_text=conversation_id).count() == 0
        await browser.close()

    status, data = call(f'/me/conversations/{conversation_id}/messages', token=token)
    assert status == 404, (status, data)
    print('PASS conversation deletion: mobile assistant confirmation, message purge, and post-delete API rejection')


asyncio.run(main())
