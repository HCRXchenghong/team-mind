"""对设置页和通知偏好执行逐项浏览器回归。"""
import asyncio
import base64
import hashlib
import hmac
import json
import os
import secrets
import struct
import time
import urllib.request
from datetime import datetime, timedelta
from pathlib import Path
from urllib.parse import urljoin
from zoneinfo import ZoneInfo

from playwright.async_api import async_playwright, expect

BASE = os.getenv('TEAMMIND_WEB_BASE', 'http://127.0.0.1:5174')
API = os.getenv('TEAMMIND_API_BASE', 'http://127.0.0.1:8188/api/v1')
ENGINE = os.getenv('TEAMMIND_BROWSER', 'chromium')
if ENGINE not in {'chromium', 'firefox', 'webkit'}:
    raise ValueError('TEAMMIND_BROWSER must be chromium, firefox, or webkit')
SUFFIX = secrets.token_hex(5)
EMAIL = f'settings-{SUFFIX}@teammind.test'
PASSWORD = 'settings-browser-' + SUFFIX
MFA_SECRET = None
MFA_LAST_STEP = -1
WIDTHS = [320, 360, 375, 390, 430, 768, 1024, 1280]


def totp(secret, counter=None):
    raw = base64.b32decode(secret + '=' * ((8 - len(secret) % 8) % 8))
    if counter is None:
        counter = int(time.time() // 30)
    digest = hmac.new(raw, struct.pack('>Q', counter), hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    value = int.from_bytes(digest[offset:offset + 4], 'big') & 0x7FFFFFFF
    return f'{value % 1000000:06d}'


async def fresh_totp(secret):
    global MFA_LAST_STEP
    current_step = int(time.time() // 30)
    target_step = max(current_step, MFA_LAST_STEP + 1)
    if target_step > current_step:
        await asyncio.sleep(target_step * 30 - time.time() + 0.2)
    MFA_LAST_STEP = target_step
    return totp(secret, target_step)


def register():
    request = urllib.request.Request(API + '/auth/register', data=json.dumps({
        'email': EMAIL, 'name': 'Settings User', 'password': PASSWORD,
    }).encode(), headers={'Content-Type': 'application/json'}, method='POST')
    with urllib.request.urlopen(request, timeout=10) as response:
        assert response.status == 201


def authenticated_status(path, token):
    request = urllib.request.Request(
        API + path,
        headers={'Authorization': 'Bearer ' + token},
        method='GET',
    )
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code


def authenticated_data(path, token):
    request = urllib.request.Request(
        API + path,
        headers={'Authorization': 'Bearer ' + token},
        method='GET',
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        envelope = json.load(response)
    assert envelope.get('code') == 'OK', envelope
    return envelope['data']


def active_sessions(token):
    request = urllib.request.Request(
        API + '/auth/sessions',
        headers={'Authorization': 'Bearer ' + token},
        method='GET',
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        envelope = json.load(response)
    assert envelope.get('code') == 'OK' and isinstance(envelope.get('data'), list), envelope
    return envelope['data']


async def login(page, password=PASSWORD):
    await page.goto(BASE + '/login', wait_until='networkidle')
    try:
        await page.locator('input[autocomplete="username"]').wait_for(timeout=10000)
    except Exception as error:
        body = await page.locator('body').inner_text()
        raise AssertionError(f'login form did not render at {page.url}: {body[:500]}') from error
    await page.locator('input[autocomplete="username"]').fill(EMAIL)
    await page.locator('input[autocomplete="current-password"]').fill(password)
    await page.get_by_role('button', name='登录', exact=True).click()
    if MFA_SECRET:
        code_input = page.locator('input[autocomplete="one-time-code"]')
        await code_input.wait_for(timeout=10000)
        await code_input.fill(await fresh_totp(MFA_SECRET))
        await page.get_by_role('button', name='验证并登录').click()
    try:
        await page.wait_for_url('**/teammind/overview', timeout=12000)
    except Exception as error:
        body = (await page.locator('body').inner_text())[:800]
        raise AssertionError(f'login failed at {page.url}: {body!r}') from error


async def check_widths(page, path):
    for width in WIDTHS:
        await page.set_viewport_size({'width': width, 'height': 844})
        await page.goto(BASE + path)
        try:
            await page.locator('.tm-page').wait_for(timeout=10000)
        except Exception as error:
            body = (await page.locator('body').inner_text())[:800]
            raise AssertionError(
                f'page did not render at width={width}, requested={path}, '
                f'actual={page.url}, title={await page.title()!r}, body={body!r}'
            ) from error
        dimensions = await page.evaluate('({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})')
        assert dimensions['scroll'] <= dimensions['client'] + 1, (path, width, page.url, dimensions)


async def run():
    global EMAIL, MFA_SECRET, MFA_LAST_STEP
    register()
    async with async_playwright() as pw:
        browser = await getattr(pw, ENGINE).launch(headless=True)
        page = await browser.new_page(viewport={'width': 1280, 'height': 844}, is_mobile=True, has_touch=True)
        await login(page)
        token = await page.evaluate("sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken')")
        assert token, 'settings acceptance login did not store an access token'
        if not authenticated_data('/me/mfa', token).get('available'):
            cleanup = urllib.request.Request(
                API + '/me/account',
                data=json.dumps({'currentPassword': PASSWORD, 'confirmation': '删除账号'}).encode(),
                headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token},
                method='POST',
            )
            with urllib.request.urlopen(cleanup, timeout=10) as response:
                assert response.status == 200
            await browser.close()
            raise RuntimeError('MFA settings acceptance requires MFA_ENCRYPTION_KEY; test account was removed')
        await check_widths(page, '/teammind/settings')
        await page.set_viewport_size({'width': 1280, 'height': 844})
        await page.goto(BASE + '/teammind/settings')

        async with page.expect_download() as export_download:
            await page.get_by_role('button', name='下载我的数据副本').click()
        exported_file = await export_download.value
        export_data = json.loads(Path(await exported_file.path()).read_text())
        assert export_data['account']['email'] == EMAIL
        assert 'projects' not in export_data and 'tasks' not in export_data
        assert 'personalMemories' in export_data
        assert 'sessions' in export_data
        assert 'passwordHash' not in json.dumps(export_data)
        assert 'tokenHash' not in json.dumps(export_data)

        profile = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='个人资料'))
        assert await profile.locator('input[type="email"]').is_disabled()
        await profile.locator('input').nth(1).fill('Persistent Profile')
        await profile.locator('input').nth(2).fill('Not/A_Real_Timezone')
        await profile.get_by_role('button', name='保存资料').click()
        await page.get_by_role('alert').filter(has_text='IANA 时区').wait_for()
        assert await profile.locator('input').nth(1).input_value() == 'Persistent Profile'
        await profile.locator('input').nth(2).fill('Asia/Tokyo')
        await profile.get_by_role('button', name='保存资料').click()
        await page.get_by_text('资料已保存').wait_for()
        await page.reload()
        await page.wait_for_function("document.querySelectorAll('.tm-panel input')[1]?.value === 'Persistent Profile' && document.querySelectorAll('.tm-panel input')[2]?.value === 'Asia/Tokyo'")

        create_team = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='创建团队'))
        team_name = 'Persistent Team ' + SUFFIX
        await create_team.locator('input').nth(0).fill(team_name)
        await create_team.locator('input').nth(1).fill('Not/A_Real_Timezone')
        await create_team.get_by_role('button', name='创建团队').click()
        await page.get_by_role('alert').filter(has_text='IANA 时区').wait_for()
        await create_team.locator('input').nth(1).fill('Asia/Singapore')
        await create_team.get_by_role('button', name='创建团队').click()
        await page.get_by_text('团队已创建').wait_for()
        await page.reload()
        current_team = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='当前团队'))
        await current_team.locator('form').wait_for()
        assert await current_team.locator('input').nth(0).input_value() == team_name
        assert await current_team.locator('input').nth(1).input_value() == 'Asia/Singapore'

        edited_team_name = team_name + ' 已更新'
        await current_team.locator('input').nth(0).fill(edited_team_name)
        await current_team.locator('textarea').fill('Settings regression description')
        await current_team.locator('input').nth(1).fill('Not/A_Real_Timezone')
        await current_team.get_by_role('button', name='保存团队设置').click()
        await page.get_by_role('alert').filter(has_text='IANA 时区').wait_for()
        await current_team.locator('input').nth(1).fill('Asia/Seoul')
        await current_team.get_by_role('button', name='保存团队设置').click()
        await page.get_by_text('团队设置已保存').wait_for()
        await page.reload()
        current_team = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='当前团队'))
        assert await current_team.locator('input').nth(0).input_value() == edited_team_name
        assert await current_team.locator('textarea').input_value() == 'Settings regression description'
        assert await current_team.locator('input').nth(1).input_value() == 'Asia/Seoul'

        audit = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='团队审计记录'))
        await audit.get_by_role('button', name='刷新记录').click()
        await page.wait_for_function("!document.querySelector('.tm-audit-panel .arco-btn-loading')")
        assert await audit.locator('.tm-audit-row').count() > 0

        password_panel = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='修改密码'))
        updated_password = 'settings-updated-' + SUFFIX
        await password_panel.locator('input').nth(0).fill('incorrect-password')
        await password_panel.locator('input').nth(1).fill(updated_password)
        await password_panel.get_by_role('button', name='修改并退出所有会话').click()
        await page.get_by_role('alert').filter(has_text='当前密码错误').wait_for()
        assert await password_panel.locator('input').nth(0).input_value() == 'incorrect-password'
        assert await password_panel.locator('input').nth(1).input_value() == updated_password
        await password_panel.locator('input').nth(0).fill(PASSWORD)
        await password_panel.get_by_role('button', name='修改并退出所有会话').click()
        # Auth middleware preserves the requested page in `?redirect=...`
        # after revoking the current session, so accept both login URL forms.
        await page.wait_for_url('**/login**')
        await login(page, updated_password)

        # Verify a mailbox change from a separate browser with no prior login.
        await page.goto(BASE + '/teammind/settings')
        email_panel = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='更改邮箱'))
        changed_email = f'email-changed-{SUFFIX}@teammind.test'
        await email_panel.locator('input').nth(0).fill(changed_email)
        await email_panel.locator('input').nth(1).fill(updated_password)
        await email_panel.get_by_role('button', name='发送验证邮件').click()
        await email_panel.get_by_text('开发模式：验证令牌仅用于本地验收').wait_for()
        verification_link = email_panel.get_by_role('link', name='验证新邮箱')
        verification_url = await verification_link.get_attribute('href')
        assert verification_url and '/verify-email-change#verifyEmailToken=' in verification_url
        old_browser_token = await page.evaluate("sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken')")
        assert old_browser_token

        verification_context = await browser.new_context(viewport={'width': 390, 'height': 844})
        verification_page = await verification_context.new_page()
        await verification_page.goto(urljoin(BASE, verification_url))
        await verification_page.wait_for_url('**/login?emailChanged=1')
        assert await verification_page.get_by_text('邮箱已验证并更新，请使用新邮箱重新登录。').count() == 1
        old_session_status = authenticated_status('/me', old_browser_token)
        assert old_session_status == 401, old_session_status
        await verification_page.locator('input[autocomplete="username"]').fill(changed_email)
        await verification_page.locator('input[autocomplete="current-password"]').fill(updated_password)
        await verification_page.get_by_role('button', name='登录', exact=True).click()
        await verification_page.wait_for_url('**/teammind/overview')
        await verification_context.close()
        EMAIL = changed_email
        await page.context.close()
        refreshed_context = await browser.new_context(
            viewport={'width': 1280, 'height': 844}, is_mobile=True, has_touch=True
        )
        page = await refreshed_context.new_page()
        await login(page, updated_password)

        # Enroll TOTP in settings, save the one-time recovery codes, and sign in
        # from a clean browser context using the second factor.
        await page.goto(BASE + '/teammind/settings')
        mfa_panel = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='多因素认证'))
        await mfa_panel.get_by_label('当前密码').fill(updated_password)
        await mfa_panel.get_by_role('button', name='开始设置').click()
        secret_node = mfa_panel.locator('.tm-mfa-secret')
        await secret_node.wait_for()
        MFA_SECRET = await secret_node.inner_text()
        await mfa_panel.get_by_label('身份验证器验证码').fill(await fresh_totp(MFA_SECRET))
        await mfa_panel.get_by_role('button', name='验证并启用').click()
        await mfa_panel.get_by_text('请现在保存这些一次性恢复码').wait_for()
        assert await mfa_panel.locator('.tm-recovery-codes code').count() == 10
        async with page.expect_download() as recovery_download:
            await mfa_panel.get_by_role('button', name='下载恢复码').click()
        recovery_file = await recovery_download.value
        assert len(Path(await recovery_file.path()).read_text().splitlines()[3:]) == 10
        await check_widths(page, '/teammind/settings')

        mfa_context = await browser.new_context(viewport={'width': 390, 'height': 844})
        mfa_page = await mfa_context.new_page()
        await login(mfa_page, updated_password)
        second_factor_token = await mfa_page.evaluate("sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken')")
        assert second_factor_token
        await mfa_context.close()

        await check_widths(page, '/teammind/notifications')
        await page.set_viewport_size({'width': 390, 'height': 844})
        await page.goto(BASE + '/teammind/notifications')
        await page.get_by_role('heading', name='通知偏好').wait_for()
        notification_settings = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='通知偏好'))
        enabled = notification_settings.get_by_role('checkbox', name='接收通知')
        email = notification_settings.get_by_role('checkbox', name='邮件通知')
        push = notification_settings.get_by_role('checkbox', name='浏览器推送')
        notification_token = await page.evaluate("sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken')")
        channel_status = authenticated_data('/me/notification-channels', notification_token)
        saved_preferences = authenticated_data('/me/notification-preferences', notification_token)
        assert await email.is_disabled() == (not channel_status['emailConfigured'] and not saved_preferences['emailEnabled'])
        assert await push.is_disabled() == (not channel_status['pushConfigured'] and not saved_preferences['pushEnabled'])
        await page.get_by_text('邮件未配置', exact=False).wait_for()
        if channel_status['pushConfigured']:
            enable_push = page.get_by_role('button', name='在此设备启用推送')
            assert not await enable_push.is_disabled()
            assert channel_status['pushSubscriptions'] == 0
            assert await page.get_by_role('button', name='关闭此设备推送').is_disabled()
            if not await page.evaluate('window.isSecureContext'):
                await enable_push.click()
                await page.get_by_text('此浏览器不支持推送，或页面尚未通过 HTTPS/localhost 安全连接打开。', exact=True).wait_for()
        else:
            await page.get_by_text('浏览器推送未配置', exact=False).wait_for()
            assert await page.get_by_role('button', name='在此设备启用推送').is_disabled()
            assert await page.get_by_role('button', name='关闭此设备推送').is_disabled()
        save_prefs = notification_settings.get_by_role('button', name='保存偏好')
        await expect(save_prefs).to_be_enabled()

        await enabled.uncheck()
        await save_prefs.click()
        await page.get_by_text('偏好已保存').wait_for()
        await page.reload()
        try:
            await page.get_by_role('heading', name='通知偏好').wait_for(timeout=15000)
        except Exception as error:
            body = (await page.locator('body').inner_text())[:800]
            raise AssertionError(f'notification settings did not render after reload at {page.url}: {body!r}') from error
        notification_settings = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='通知偏好'))
        enabled = notification_settings.get_by_role('checkbox', name='接收通知')
        save_prefs = notification_settings.get_by_role('button', name='保存偏好')
        await expect(save_prefs).to_be_enabled(timeout=15000)
        assert not await enabled.is_checked()
        await enabled.check()
        await notification_settings.get_by_label('免打扰开始').fill('23:15')
        await notification_settings.get_by_label('结束').fill('07:30')
        await save_prefs.click()
        await page.get_by_text('偏好已保存').wait_for()
        await page.reload()
        notification_settings = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='通知偏好'))
        await expect(notification_settings.get_by_role('button', name='保存偏好')).to_be_enabled()
        assert await notification_settings.get_by_label('接收通知').is_checked()
        assert await notification_settings.get_by_label('免打扰开始').input_value() == '23:15'
        assert await notification_settings.get_by_label('结束').input_value() == '07:30'

        reminder_panel = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='定时提醒'))
        reminder_title = 'Setting reminder ' + SUFFIX
        reminder_time = (datetime.now(ZoneInfo('Asia/Seoul')) + timedelta(minutes=20)).strftime('%Y-%m-%dT%H:%M')
        await reminder_panel.get_by_label('提醒范围').select_option('personal')
        await reminder_panel.get_by_label('提醒时间').fill(reminder_time)
        await reminder_panel.get_by_label('标题').fill(reminder_title)
        await reminder_panel.get_by_role('button', name='创建提醒').click()
        reminder_row = page.locator('.tm-reminder-row').filter(has_text=reminder_title)
        await reminder_row.wait_for()
        await reminder_row.get_by_role('button', name='取消').click()
        await reminder_row.get_by_text('已取消').wait_for()

        team_reminder_title = 'Team setting reminder ' + SUFFIX
        team_reminder_time = (datetime.now(ZoneInfo('Asia/Seoul')) + timedelta(minutes=25)).strftime('%Y-%m-%dT%H:%M')
        await reminder_panel.get_by_label('提醒范围').select_option('team')
        await reminder_panel.get_by_label('提醒时间').fill(team_reminder_time)
        await reminder_panel.get_by_label('标题').fill(team_reminder_title)
        await reminder_panel.get_by_role('button', name='创建提醒').click()
        team_reminder_row = page.locator('.tm-reminder-row').filter(has_text=team_reminder_title)
        await team_reminder_row.wait_for()
        await team_reminder_row.get_by_role('button', name='取消').click()
        await team_reminder_row.get_by_text('已取消').wait_for()

        await page.goto(BASE + '/teammind/settings')
        session_panel = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='有效会话'))
        second_context = await browser.new_context(viewport={'width': 1280, 'height': 844})
        second_page = await second_context.new_page()
        await login(second_page, updated_password)
        second_page_token = await second_page.evaluate("sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken')")
        assert second_page_token
        second_page_sessions = active_sessions(second_page_token)
        target_session = next(session for session in second_page_sessions if session['current'])
        await session_panel.get_by_role('button', name='刷新会话').click()
        await page.wait_for_function("[...document.querySelectorAll('.tm-row')].some(row => row.innerText.includes('当前设备'))")
        rows = session_panel.locator('.tm-row')
        visible_sessions = active_sessions(await page.evaluate("sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken')"))
        assert await rows.filter(has_text='当前设备').count() == 1
        assert await rows.count() == len(visible_sessions) >= 2
        target_index = next(index for index, session in enumerate(visible_sessions) if session['id'] == target_session['id'])
        assert not visible_sessions[target_index]['current']
        await rows.nth(target_index).get_by_role('button', name='撤销').click()
        await page.locator('.arco-modal:visible').last.get_by_role('button', name='确定').click()
        # The confirmation callback sends DELETE asynchronously from the
        # modal's onOk handler. Wait for the server-success toast before
        # asserting that the other browser's bearer token has been revoked.
        await page.get_by_text('会话已撤销', exact=True).wait_for(timeout=10000)
        assert authenticated_status('/me', second_page_token) == 401
        await second_context.close()
        await session_panel.get_by_role('button', name='续期当前会话').click()
        await page.get_by_text('会话已续期').wait_for()
        await session_panel.locator('.tm-row').filter(has_text='当前设备').get_by_role('button', name='撤销').click()
        await page.locator('.arco-modal:visible').last.get_by_role('button', name='确定').click()
        await page.wait_for_url('**/login**')

        await login(page, updated_password)
        await page.goto(BASE + '/teammind/settings')
        mfa_panel = page.locator('.tm-panel').filter(has=page.get_by_role('heading', name='多因素认证'))
        await mfa_panel.get_by_label('当前密码').fill(updated_password)
        await mfa_panel.get_by_label('动态验证码或未使用的恢复码').fill(await fresh_totp(MFA_SECRET))
        await mfa_panel.get_by_role('button', name='关闭并退出所有设备').click()
        await page.locator('.arco-modal:visible').last.get_by_role('button', name='确定').click()
        await page.wait_for_url('**/login**')
        revoked_status = authenticated_status('/me', second_factor_token)
        assert revoked_status == 401, revoked_status
        MFA_SECRET = None
        MFA_LAST_STEP = -1

        await login(page, updated_password)
        await page.goto(BASE + '/teammind/settings')
        await check_widths(page, '/teammind/settings')
        await browser.close()

    print(f'PASS {ENGINE} settings: personal data export, profile/team settings, email verification across browsers, TOTP MFA enrollment/login/disable and recovery-code download, MFA session revocation, session management, notifications/reminders and 320–1280px settings/notification/MFA layouts')


asyncio.run(run())
