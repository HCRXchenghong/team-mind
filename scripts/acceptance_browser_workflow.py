"""Real browser workflow against a migrated local API and Vite UI."""
import asyncio
import json
import os
import secrets
import urllib.request
from playwright.async_api import async_playwright

BASE = os.getenv('TEAMMIND_WEB_BASE', 'http://127.0.0.1:5174')
API = os.getenv('TEAMMIND_API_BASE', 'http://127.0.0.1:8188/api/v1')
SUFFIX = secrets.token_hex(5)
PASSWORD = 'browser-workflow-' + SUFFIX
ENGINE = os.getenv('TEAMMIND_BROWSER', 'chromium')
if ENGINE not in {'chromium', 'firefox', 'webkit'}:
    raise ValueError('TEAMMIND_BROWSER must be chromium, firefox, or webkit')

def register(kind):
    email = f'{kind}-{SUFFIX}@teammind.test'
    request = urllib.request.Request(API + '/auth/register', data=json.dumps({
        'email': email, 'name': kind.title(), 'password': PASSWORD,
    }).encode(), headers={'Content-Type': 'application/json'}, method='POST')
    with urllib.request.urlopen(request, timeout=10) as response:
        assert response.status == 201
    return email

async def sign_in(page, email):
    await page.goto(BASE + '/login')
    await page.locator('input[autocomplete="username"]').fill(email)
    await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
    await page.get_by_role('button', name='登录', exact=True).click()
    await page.wait_for_url('**/teammind/overview')

async def run():
    owner_email = register('workflow-owner')
    member_email = register('workflow-member')
    async with async_playwright() as pw:
        browser = await getattr(pw, ENGINE).launch(headless=True)
        owner = await browser.new_page(viewport={'width': 1280, 'height': 800})
        browser_errors = []
        failed_responses = []
        auth_responses = []
        owner.on('pageerror', lambda error: browser_errors.append(str(error)))
        def record_failed_response(response):
            if response.status < 400:
                return
            shape = 'unreadable'
            if response.request.post_data:
                try:
                    payload = json.loads(response.request.post_data)
                    shape = {
                        'keys': sorted(payload),
                        'types': {key: type(value).__name__ for key, value in payload.items()},
                    } if isinstance(payload, dict) else type(payload).__name__
                except (TypeError, ValueError):
                    shape = 'invalid-json'
            failed_responses.append((response.status, response.url.split('?', 1)[0], shape))
        owner.on('response', record_failed_response)
        async def track_auth_response(response):
            if '/api/v1/me' not in response.url:
                return
            try:
                payload = await response.json()
                auth_responses.append((response.url.split('?', 1)[0], response.status, payload.get('code'), type(payload.get('data')).__name__))
            except Exception:
                auth_responses.append((response.url.split('?', 1)[0], response.status, 'unreadable', 'unknown'))
        owner.on('response', lambda response: asyncio.create_task(track_auth_response(response)))
        await sign_in(owner, owner_email)
        await owner.get_by_role('button', name='账号菜单').click()
        await owner.get_by_text('设置', exact=True).last.click()
        await owner.wait_for_url('**/teammind/settings')
        await owner.get_by_role('button', name='切换主题').click()
        assert await owner.locator('body').get_attribute('arco-theme') == 'dark'
        assert await owner.evaluate("localStorage.getItem('teammind.theme')") == 'dark'
        await owner.reload()
        await owner.wait_for_function("document.body.getAttribute('arco-theme') === 'dark'")
        assert await owner.locator('body').get_attribute('arco-theme') == 'dark'
        await owner.get_by_role('button', name='切换主题').click()
        assert await owner.evaluate("localStorage.getItem('teammind.theme')") == 'light'
        assert await owner.locator('body').get_attribute('arco-theme') is None
        await owner.goto(BASE + '/teammind/settings')
        profile_panel = owner.locator('.tm-panel').filter(has=owner.get_by_role('heading', name='个人资料'))
        await profile_panel.locator('input').nth(1).fill('Workflow-Owner-Profile-Test')
        await profile_panel.locator('input').nth(2).fill('Not/A_Real_Timezone')
        await profile_panel.get_by_role('button', name='保存资料').click()
        await owner.get_by_role('alert').filter(has_text='IANA 时区').wait_for()
        await profile_panel.locator('input').nth(2).fill('Europe/London')
        await profile_panel.get_by_role('button', name='保存资料').click()
        await owner.get_by_text('资料已保存').wait_for()
        await owner.reload()
        await owner.wait_for_function("document.querySelectorAll('.tm-panel input')[1]?.value === 'Workflow-Owner-Profile-Test' && document.querySelectorAll('.tm-panel input')[2]?.value === 'Europe/London'")
        await profile_panel.locator('input').nth(1).fill('Workflow-Owner')
        await profile_panel.locator('input').nth(2).fill('Asia/Shanghai')
        await profile_panel.get_by_role('button', name='保存资料').click()
        await owner.get_by_text('资料已保存').wait_for()
        team_panel = owner.locator('.tm-panel').filter(has=owner.get_by_role('heading', name='创建团队'))
        await team_panel.locator('input').first.fill('Workflow ' + SUFFIX)
        await team_panel.locator('input').nth(1).fill('Not/A_Real_Timezone')
        await team_panel.get_by_role('button', name='创建团队').click()
        await owner.get_by_role('alert').filter(has_text='IANA 时区').wait_for()
        await team_panel.locator('input').nth(1).fill('Asia/Tokyo')
        await team_panel.get_by_role('button', name='创建团队').click()
        await owner.get_by_text('团队已创建').wait_for()
        assert await owner.evaluate("sessionStorage.getItem('teammind.teamId')")
        await owner.get_by_role('heading', name='团队审计记录').wait_for()
        await owner.get_by_text('team.create').wait_for()
        current_team_panel = owner.locator('.tm-panel').filter(has=owner.get_by_role('heading', name='当前团队'))
        updated_team_name = 'Workflow updated ' + SUFFIX
        await current_team_panel.locator('input').first.fill(updated_team_name)
        await current_team_panel.locator('input').nth(1).fill('Asia/Tokyo')
        await current_team_panel.get_by_role('button', name='保存团队设置').click()
        await owner.get_by_text('团队设置已保存').wait_for()
        await owner.get_by_role('button', name='刷新记录').click()
        await owner.get_by_text('team.update').wait_for()
        await owner.set_viewport_size({'width': 320, 'height': 780})
        await owner.goto(BASE + '/teammind/settings')
        audit_dimensions = await owner.evaluate('({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})')
        assert audit_dimensions['scroll'] <= audit_dimensions['client'] + 1, audit_dimensions
        await owner.set_viewport_size({'width': 1280, 'height': 800})
        await owner.reload()
        await owner.wait_for_function("(teamName) => [...document.querySelectorAll('.tm-panel')].find(e => e.innerText.includes('当前团队'))?.querySelector('input')?.value === teamName", arg=updated_team_name)
        current_team_panel = owner.locator('.tm-panel').filter(has=owner.get_by_role('heading', name='当前团队'))
        assert await current_team_panel.locator('input').nth(1).input_value() == 'Asia/Tokyo'
        secondary_owner = await browser.new_page(viewport={'width': 1280, 'height': 800})
        await sign_in(secondary_owner, owner_email)
        secondary_token = await secondary_owner.evaluate('sessionStorage.getItem("teammind.accessToken")')
        session_panel = owner.locator('.tm-panel').filter(has=owner.get_by_role('heading', name='有效会话'))
        await session_panel.get_by_role('button', name='续期当前会话').click()
        await owner.get_by_text('会话已续期').wait_for()
        sessions = session_panel.locator('.tm-row')
        await owner.wait_for_function("() => [...document.querySelectorAll('.tm-panel')].some(panel => panel.innerText.includes('有效会话') && panel.querySelectorAll('.tm-row').length >= 2)")
        assert await sessions.count() >= 2
        current_session_rows = sessions.filter(has_text='当前设备')
        assert await current_session_rows.count() == 1, 'session list must identify the active browser session'
        revoke_session = None
        for index in range(await sessions.count()):
            candidate = sessions.nth(index)
            if '当前设备' not in await candidate.inner_text():
                revoke_session = candidate
                break
        assert revoke_session is not None, 'expected another browser session to revoke'
        await revoke_session.get_by_role('button', name='撤销').click()
        await owner.locator('.arco-modal:visible').last.get_by_role('button', name='确定').click()
        await owner.get_by_text('会话已撤销').wait_for()
        secondary_status = await secondary_owner.evaluate("async (token) => (await fetch('/api/v1/me', {headers:{Authorization:'Bearer '+token}})).status", secondary_token)
        assert secondary_status == 401, secondary_status
        await secondary_owner.close()
        await owner.goto(BASE + '/teammind/projects')
        await owner.get_by_role('button', name='创建项目').click()
        await owner.locator('.arco-modal:visible input').first.fill('Browser project ' + SUFFIX)
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_role('heading', name='Browser project ' + SUFFIX).wait_for()
        project_card = owner.locator('article.tm-panel').filter(has=owner.get_by_role('heading', name='Browser project ' + SUFFIX))
        await project_card.get_by_role('button', name='编辑').click()
        await owner.locator('.arco-modal:visible input[type="date"]').nth(0).fill('2026-10-18')
        await owner.locator('.arco-modal:visible input[type="date"]').nth(1).fill('2026-11-20')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await project_card.get_by_text('截止：2026-11-20').wait_for()
        project_token = await owner.evaluate('sessionStorage.getItem("teammind.accessToken")')
        project_team_id = await owner.evaluate('sessionStorage.getItem("teammind.teamId")')
        page_fixture_ids = await owner.evaluate("async ({token,teamId,suffix}) => { const ids=[]; for (let i=0;i<12;i++) { const r=await fetch(`/api/v1/teams/${teamId}/projects`, {method:'POST',headers:{'Content-Type':'application/json',Authorization:'Bearer '+token},body:JSON.stringify({name:`Pagination Probe ${suffix}-${i}`,description:'',status:'planning'})}); const body=await r.json(); if(r.status!==201) throw new Error(body.message || 'fixture project creation failed'); ids.push(body.data.id); } return ids; }", {'token': project_token, 'teamId': project_team_id, 'suffix': SUFFIX})
        await owner.reload()
        await owner.locator('.arco-pagination-item-active').wait_for()
        page_two = owner.locator('.arco-pagination-item').filter(has_text='2')
        await page_two.click()
        await owner.wait_for_function("document.querySelector('.arco-pagination-item-active')?.textContent === '2'")
        await owner.locator('.tm-toolbar .arco-select').click()
        await owner.locator('.arco-select-dropdown .arco-select-option').filter(has_text='规划中').last.click()
        await owner.locator('.tm-card-grid article').first.wait_for()
        project_statuses = await owner.locator('.tm-card-grid article').all_inner_texts()
        assert project_statuses and all('状态：planning' in row for row in project_statuses), project_statuses
        project_search = owner.get_by_placeholder('搜索项目')
        await project_search.fill('Pagination Probe ' + SUFFIX + '-0')
        await owner.get_by_role('button', name='搜索', exact=True).click()
        await owner.get_by_role('heading', name='Pagination Probe ' + SUFFIX + '-0').wait_for()
        await owner.evaluate("async ({ids,token,teamId}) => { for (const id of ids) { const r=await fetch(`/api/v1/teams/${teamId}/projects/${id}`,{method:'DELETE',headers:{Authorization:'Bearer '+token}}); if(!r.ok) throw new Error('fixture cleanup failed'); } }", {'ids': page_fixture_ids, 'token': project_token, 'teamId': project_team_id})
        await owner.goto(BASE + '/teammind/projects')
        project_id = await owner.evaluate("async ({token,teamId,name}) => { const r=await fetch(`/api/v1/teams/${teamId}/projects?q=${encodeURIComponent(name)}`,{headers:{Authorization:'Bearer '+token}}); return (await r.json()).data.items[0].id }", {'token': project_token, 'teamId': project_team_id, 'name': 'Browser project ' + SUFFIX})
        project_row = owner.locator('article.tm-panel').filter(has=owner.get_by_role('heading', name='Browser project ' + SUFFIX))
        await project_row.get_by_role('button', name='查看任务').click()
        await owner.wait_for_url('**/teammind/tasks?projectId=' + project_id)
        await owner.goto(BASE + '/teammind/projects')
        project_row = owner.locator('article.tm-panel').filter(has=owner.get_by_role('heading', name='Browser project ' + SUFFIX))
        await project_row.get_by_role('button', name='甘特图').click()
        await owner.wait_for_url('**/teammind/gantt?projectId=' + project_id)
        await owner.goto(BASE + '/teammind/tasks')
        await owner.get_by_role('button', name='创建任务').click()
        task_form = owner.locator('.arco-modal:visible')
        await task_form.locator('select').first.select_option(label='Browser project ' + SUFFIX)
        await task_form.locator('input').first.fill('Disposable task ' + SUFFIX)
        await task_form.get_by_role('button', name='确定').click()
        disposable_task = owner.locator('article.tm-panel').filter(has=owner.get_by_role('heading', name='Disposable task ' + SUFFIX))
        await disposable_task.get_by_role('button', name='删除').click()
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await disposable_task.wait_for(state='detached')
        await owner.goto(BASE + '/teammind/projects')
        await owner.get_by_role('button', name='创建项目').click()
        await owner.locator('.arco-modal:visible input').first.fill('Disposable project ' + SUFFIX)
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        disposable_card = owner.locator('article.tm-panel').filter(has=owner.get_by_role('heading', name='Disposable project ' + SUFFIX))
        await disposable_card.wait_for()
        await disposable_card.get_by_role('button', name='删除').click()
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_role('heading', name='Disposable project ' + SUFFIX).wait_for(state='detached')
        await owner.goto(BASE + '/teammind/tasks')
        await owner.get_by_role('button', name='创建任务').click()
        await owner.locator('.arco-modal:visible select').first.select_option(label='Browser project ' + SUFFIX)
        await owner.locator('.arco-modal:visible input').first.fill('Browser task ' + SUFFIX)
        await owner.locator('.arco-modal:visible input[type="date"]').nth(0).fill('2026-10-20')
        await owner.locator('.arco-modal:visible input[type="date"]').nth(1).fill('2026-10-28')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_role('heading', name='Browser task ' + SUFFIX).wait_for()
        await owner.get_by_role('button', name='搜索任务').click()
        await owner.locator('.arco-modal:visible input').fill('Browser task ' + SUFFIX)
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.wait_for_function("new URL(location.href).searchParams.get('q')?.startsWith('Browser task')")
        await owner.get_by_role('heading', name='Browser task ' + SUFFIX).wait_for()
        await owner.goto(BASE + '/teammind/tasks')
        await owner.get_by_role('button', name='创建任务').click()
        await owner.locator('.arco-modal:visible select').first.select_option(label='Browser project ' + SUFFIX)
        await owner.locator('.arco-modal:visible input').first.fill('Browser prerequisite ' + SUFFIX)
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_role('heading', name='Browser prerequisite ' + SUFFIX).wait_for()
        task_card = owner.locator('article.tm-panel').filter(has=owner.get_by_role('heading', name='Browser task ' + SUFFIX))
        await task_card.get_by_role('button', name='详情').click()
        await owner.locator('.arco-modal:visible select').first.select_option('doing')
        await owner.locator('.arco-modal:visible input[type="number"]').fill('20')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='保存状态').click()
        await owner.get_by_text('任务状态已保存').wait_for()
        task_search = owner.get_by_placeholder('搜索任务')
        await task_search.fill('Browser task ' + SUFFIX)
        await task_search.press('Enter')
        await owner.locator('.tm-card-grid').get_by_role('heading', name='Browser task ' + SUFFIX).wait_for()
        await owner.locator('.tm-toolbar .arco-select').nth(0).click()
        await owner.locator('.arco-select-dropdown .arco-select-option').filter(has_text='doing').last.click()
        await owner.wait_for_function("(suffix) => { const cards=[...document.querySelectorAll('.tm-card-grid article')]; return cards.length === 1 && cards[0].textContent.includes('Browser task ' + suffix) }", arg=SUFFIX)
        filtered_tasks = await owner.locator('.tm-card-grid article').all_inner_texts()
        assert len(filtered_tasks) == 1 and 'Browser task ' + SUFFIX in filtered_tasks[0], filtered_tasks
        await owner.locator('.tm-toolbar .arco-select').nth(3).click()
        await owner.locator('.arco-select-dropdown .arco-select-option').filter(has_text='标题').last.click()
        await owner.locator('.tm-card-grid').get_by_role('heading', name='Browser task ' + SUFFIX).wait_for()
        await owner.locator('.tm-toolbar .arco-select').nth(0).click()
        await owner.locator('.arco-select-dropdown .arco-select-option').filter(has_text='全部状态').last.click()
        await task_search.fill('')
        await task_search.press('Enter')
        await task_card.get_by_role('button', name='详情').click()
        dependency = owner.locator('.arco-modal:visible .tm-toolbar select').first
        await dependency.select_option(label='Browser prerequisite ' + SUFFIX)
        await owner.locator('.arco-modal:visible').get_by_role('button', name='添加依赖').click()
        await owner.get_by_text('Browser prerequisite ' + SUFFIX).last.wait_for()
        await owner.locator('.arco-modal:visible input[placeholder="写评论"]').fill('Browser comment ' + SUFFIX)
        await owner.locator('.arco-modal:visible').get_by_role('button', name='发送').click()
        await owner.get_by_text('Browser comment ' + SUFFIX).wait_for()
        comment_row = owner.locator('.tm-comment-row').filter(has_text='Browser comment ' + SUFFIX)
        await comment_row.get_by_role('button', name='编辑').click()
        await owner.locator('.arco-modal:visible textarea[aria-label="评论内容"]').fill('Edited browser comment ' + SUFFIX)
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_text('Edited browser comment ' + SUFFIX).wait_for()
        comment_row = owner.locator('.tm-comment-row').filter(has_text='Edited browser comment ' + SUFFIX)
        await comment_row.get_by_role('button', name='删除').click()
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_text('评论已删除').wait_for()
        await owner.goto(BASE + '/teammind/tasks')
        task_card = owner.locator('article.tm-panel').filter(has=owner.get_by_role('heading', name='Browser task ' + SUFFIX))
        await task_card.get_by_role('button', name='申请任务变更').click()
        await owner.locator('.arco-modal:visible input').first.fill('Browser task proposed title ' + SUFFIX)
        await owner.locator('.arco-modal:visible textarea').first.fill('Approved description from browser')
        await owner.locator('.arco-modal:visible select').nth(1).select_option('high')
        await owner.locator('.arco-modal:visible input[type="date"]').nth(1).fill('2026-11-05')
        await owner.locator('.arco-modal:visible textarea').nth(1).fill('Browser schedule change')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.wait_for_url('**/teammind/approvals')
        await owner.goto(BASE + '/teammind/gantt')
        await owner.locator('.tm-gantt-meta').filter(has_text='2026-10-20 ~ 2026-10-28').wait_for()
        await owner.locator('.tm-gantt-title').get_by_text('Browser task ' + SUFFIX).wait_for()
        await owner.get_by_text('依赖：Browser prerequisite ' + SUFFIX).wait_for()
        await owner.goto(BASE + '/teammind/approvals')
        await owner.get_by_role('button', name='修改后批准', exact=True).click()
        approval_modal = owner.locator('.arco-modal:visible')
        await approval_modal.locator('input[maxlength="240"]').fill('Browser task edited approval ' + SUFFIX)
        await approval_modal.get_by_label('状态').select_option('doing')
        await approval_modal.get_by_label('进度（0–100）').fill('65')
        await approval_modal.locator('label').filter(has_text='优先级').locator('select').select_option('urgent')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        try:
            await owner.get_by_text('审批已完成', exact=True).wait_for(timeout=15000)
        except Exception as error:
            form_errors = await owner.locator('.arco-modal:visible .tm-error').all_inner_texts()
            raise AssertionError(
                'approval decision did not complete; '
                f'form_errors={form_errors!r}; '
                f'failed_responses={failed_responses[-8:]!r}; '
                f'browser_errors={browser_errors[-5:]!r}'
            ) from error
        await owner.goto(BASE + '/teammind/tasks')
        approved_task = owner.locator('article.tm-panel').filter(has=owner.get_by_role('heading', name='Browser task edited approval ' + SUFFIX))
        await approved_task.wait_for()
        await approved_task.get_by_text('负责人：Workflow-Owner · doing · urgent · 65%', exact=True).wait_for()
        await owner.goto(BASE + '/teammind/gantt')
        await owner.locator('.tm-gantt-meta').filter(has_text='2026-10-20 ~ 2026-11-05').wait_for()
        gantt_selects = owner.locator('.tm-toolbar .arco-select')
        await gantt_selects.nth(0).click()
        await owner.get_by_text('Browser project ' + SUFFIX, exact=True).last.click()
        await gantt_selects.nth(1).click()
        await owner.get_by_text('进行中', exact=True).last.click()
        await gantt_selects.nth(2).click()
        await owner.get_by_text('Workflow-Owner', exact=True).last.click()
        await owner.locator('input[aria-label="计划开始日期"]').fill('2026-10-21')
        await owner.locator('input[aria-label="计划结束日期"]').fill('2026-11-04')
        await owner.get_by_role('button', name='筛选', exact=True).click()
        await owner.locator('.tm-gantt-meta').filter(has_text='2026-10-20 ~ 2026-11-05').wait_for()
        assert await owner.locator('.tm-gantt-title').get_by_text('Browser prerequisite ' + SUFFIX).count() == 0
        await owner.locator('input[aria-label="定位日期"]').fill('2026-10-30')
        await owner.get_by_role('button', name='定位', exact=True).click()
        await owner.locator('.tm-gantt-focus-marker').first.wait_for()
        await owner.goto(BASE + '/teammind/tasks')
        prerequisite_card = owner.locator('article.tm-panel').filter(has=owner.get_by_role('heading', name='Browser prerequisite ' + SUFFIX))
        await prerequisite_card.get_by_role('button', name='申请任务变更').click()
        await owner.locator('.arco-modal:visible input').first.fill('Browser prerequisite proposed ' + SUFFIX)
        await owner.locator('.arco-modal:visible input[type="date"]').nth(0).fill('2026-11-06')
        await owner.locator('.arco-modal:visible input[type="date"]').nth(1).fill('2026-11-10')
        await owner.locator('.arco-modal:visible textarea').nth(1).fill('Browser clarification request')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.wait_for_url('**/teammind/approvals')
        clarification_card = owner.locator('article.tm-panel').filter(has_text='Browser prerequisite ' + SUFFIX)
        await clarification_card.get_by_role('button', name='要求补充信息').click()
        await owner.locator('.arco-modal:visible textarea').fill('Please explain the handoff')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await clarification_card.locator('p').filter(has_text='状态：needs_info').wait_for()
        clarification_card = owner.locator('article.tm-panel').filter(has_text='Browser prerequisite ' + SUFFIX)
        await clarification_card.get_by_role('button', name='补充后重新提交').click()
        await owner.locator('.arco-modal:visible input[maxlength="240"]').fill('Browser prerequisite clarified ' + SUFFIX)
        await owner.locator('.arco-modal:visible input[type="date"]').nth(1).fill('2026-11-12')
        await owner.locator('.arco-modal:visible textarea').nth(1).fill('Browser resubmission details')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_text('pending').wait_for()
        await owner.goto(BASE + '/teammind/tasks')
        await owner.get_by_role('heading', name='Browser prerequisite ' + SUFFIX).wait_for()
        await owner.goto(BASE + '/teammind/gantt')
        await owner.get_by_text('尚未排期', exact=True).wait_for()
        await owner.goto(BASE + '/teammind/approvals')
        clarification_card = owner.locator('article.tm-panel').filter(has_text='Browser prerequisite ' + SUFFIX)
        await clarification_card.get_by_role('button', name='批准', exact=True).click()
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_text('审批已完成').last.wait_for()
        await owner.goto(BASE + '/teammind/tasks')
        await owner.get_by_role('heading', name='Browser prerequisite clarified ' + SUFFIX).wait_for()
        await owner.goto(BASE + '/teammind/overview')
        await owner.locator('.tm-stats article strong').nth(4).wait_for()
        team_summary = await owner.evaluate("async () => { const token=sessionStorage.getItem('teammind.accessToken'); const teamId=sessionStorage.getItem('teammind.teamId'); const r=await fetch(`/api/v1/teams/${teamId}/summary`,{headers:{Authorization:'Bearer '+token}}); return (await r.json()).data }")
        displayed_stats = [int(value) for value in await owner.locator('.tm-stats article strong').all_inner_texts()]
        assert displayed_stats == [team_summary['projects'], team_summary['tasks'], team_summary['blocked'], team_summary['pending'], team_summary['members']], (displayed_stats, team_summary)
        recent_task_row = owner.locator('.tm-panel a.tm-row').filter(has_text='Browser task edited approval ' + SUFFIX)
        await recent_task_row.click()
        await owner.wait_for_url('**/teammind/tasks?task=*')
        await owner.locator('.arco-modal:visible').get_by_text('Browser task edited approval ' + SUFFIX).wait_for()
        await owner.locator('.arco-modal:visible .arco-modal-close-btn').click()
        auth_checkpoint = await owner.evaluate("async () => { const token=sessionStorage.getItem('teammind.accessToken') || localStorage.getItem('teammind.accessToken'); if(!token) return {hasToken:false}; const r=await fetch('/api/v1/me',{headers:{Authorization:'Bearer '+token}}); const body=await r.json(); return {hasToken:true,status:r.status,code:body.code}; }")
        if auth_checkpoint.get('status') != 200:
            print(f'Workflow auth checkpoint before final Gantt route: {auth_checkpoint}', flush=True)
        await owner.goto(BASE + '/teammind/tasks')
        await owner.get_by_role('heading', name='任务中心', exact=True).wait_for()
        await owner.goto(BASE + '/teammind/gantt')
        try:
            await owner.get_by_role('heading', name='项目甘特图', exact=True).wait_for()
        except Exception as error:
            body = (await owner.locator('body').inner_text())[-1800:]
            storage = await owner.evaluate("() => ({sessionToken:!!sessionStorage.getItem('teammind.accessToken'), localToken:!!localStorage.getItem('teammind.accessToken'), teamId:sessionStorage.getItem('teammind.teamId')})")
            raise AssertionError(f'Gantt route failed to render in {ENGINE}: url={owner.url}; body={body!r}; storage={storage}; authResponses={auth_responses[-20:]}; pageErrors={browser_errors}; failedResponses={failed_responses[-20:]}') from error
        # The page can retain its prior date filter when the router reuses the
        # view. Clear that earlier October/November 4 range before asserting
        # the newly approved November 6–12 schedule is visible.
        date_filters = owner.locator('.tm-toolbar input[type="date"]')
        await date_filters.nth(0).wait_for(state='visible')
        assert await date_filters.count() == 3, 'Gantt date filters did not render'
        await date_filters.nth(0).fill('')
        await date_filters.nth(1).fill('')
        await owner.get_by_role('button', name='筛选', exact=True).click()
        await owner.locator('.tm-gantt-meta').filter(has_text='2026-11-06 ~ 2026-11-12').wait_for()
        await owner.goto(BASE + '/teammind/members')
        await owner.get_by_role('button', name='邀请成员').click()
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.locator('input.tm-link').wait_for(state='visible')
        invite = await owner.locator('input.tm-link').input_value()
        assert invite.startswith(BASE + '/teammind/join/'), f'invite length={len(invite)}, prefix={invite[:30]}'
        if ENGINE != 'webkit':
            await owner.context.grant_permissions(['clipboard-read', 'clipboard-write'])
        await owner.get_by_role('button', name='复制链接').click()
        if ENGINE == 'webkit':
            await owner.get_by_text('链接已复制', exact=True).wait_for()
        else:
            assert await owner.evaluate('navigator.clipboard.readText()') == invite

        phone_context = await browser.new_context(viewport={'width': 390, 'height': 844}, is_mobile=True, has_touch=True)
        phone = await phone_context.new_page()
        await phone.goto(invite)
        try:
            await phone.wait_for_function("location.pathname === '/login'", timeout=10000)
        except Exception as error:
            body = (await phone.locator('body').inner_text())[-1000:]
            raise AssertionError(f'invitation did not route to login: url={phone.url}; body={body}') from error
        assert '/teammind/join/' in phone.url
        await phone.locator('input[autocomplete="username"]').fill(member_email)
        await phone.locator('input[autocomplete="current-password"]').fill(PASSWORD)
        await phone.get_by_role('button', name='登录', exact=True).tap()
        await phone.wait_for_url('**/teammind/join/**')
        await phone.get_by_role('button', name='接受邀请').tap()
        await phone.get_by_text('已成功加入团队。').wait_for()
        await phone.get_by_role('link', name='进入团队').tap()
        await phone.wait_for_url('**/teammind/overview')
        await phone.get_by_role('button', name='打开菜单').tap()
        drawer = phone.locator('.arco-drawer:visible')
        task_menu_item = drawer.get_by_text('任务中心')
        if not await task_menu_item.is_visible():
            await drawer.get_by_text('TeamMind 工作空间').tap()
            await phone.wait_for_timeout(400)
        await task_menu_item.tap()
        await phone.wait_for_url('**/teammind/tasks')
        await owner.goto(BASE + '/teammind/members')
        member_card = owner.locator('article.tm-member').filter(has_text='Workflow-Member')
        await member_card.get_by_role('button', name='角色与状态').click()
        await owner.locator('.arco-modal:visible select').nth(0).select_option('team_admin')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_text('成员已更新').last.wait_for()
        await member_card.get_by_text('team_admin').wait_for()
        await member_card.get_by_role('button', name='角色与状态').click()
        await owner.locator('.arco-modal:visible select').nth(0).select_option('member')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_text('成员已更新').last.wait_for()
        await member_card.locator('p').filter(has_text=' · member · ').wait_for()
        await member_card.get_by_role('button', name='角色与状态').click()
        await owner.locator('.arco-modal:visible select').nth(1).select_option('suspended')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.locator('.arco-modal:visible').last.get_by_role('button', name='确定').click()
        await owner.get_by_text('成员已更新').last.wait_for()
        await member_card.get_by_text('suspended').wait_for()
        member_token = await phone.evaluate('sessionStorage.getItem("teammind.accessToken")')
        team_id = await owner.evaluate('sessionStorage.getItem("teammind.teamId")')
        suspended_status = await phone.evaluate("async ({teamId, token}) => (await fetch(`/api/v1/teams/${teamId}/projects`, {headers:{Authorization:'Bearer '+token}})).status", {'teamId': team_id, 'token': member_token})
        assert suspended_status == 403, suspended_status
        await member_card.get_by_role('button', name='角色与状态').click()
        await owner.locator('.arco-modal:visible select').nth(1).select_option('active')
        await owner.locator('.arco-modal:visible').last.get_by_role('button', name='确定').click()
        await owner.get_by_text('成员已更新').last.wait_for()
        await member_card.get_by_text('active').wait_for()
        member_token = await phone.evaluate('sessionStorage.getItem("teammind.accessToken")')
        project_proposal_id = await phone.evaluate("async ({teamId,token,projectId,suffix}) => { const r=await fetch(`/api/v1/teams/${teamId}/project-proposals`,{method:'POST',headers:{'Content-Type':'application/json',Authorization:'Bearer '+token},body:JSON.stringify({projectId,name:`Browser project proposal ${suffix}`,status:'active',reason:'Align project delivery scope',impactSummary:'This changes the agreed team roadmap'})}); const body=await r.json(); if(r.status!==201) throw new Error(body.message || 'project proposal failed'); return body.data.id }", {'teamId': team_id, 'token': member_token, 'projectId': project_id, 'suffix': SUFFIX})
        await owner.goto(BASE + '/teammind/approvals')
        project_proposal_card = owner.locator('article.tm-panel').filter(has_text='Browser project ' + SUFFIX)
        await project_proposal_card.get_by_role('button', name='要求补充信息').click()
        await owner.locator('.arco-modal:visible textarea').fill('Please explain the updated delivery impact')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_text('项目提案已处理').wait_for()
        await phone.goto(BASE + '/teammind/approvals')
        member_project_proposal = phone.locator('article.tm-panel').filter(has_text='Browser project ' + SUFFIX)
        await member_project_proposal.get_by_role('button', name='补充后重新提交').click()
        await phone.locator('.arco-modal:visible input[maxlength="200"]').fill('Browser project resubmitted ' + SUFFIX)
        await phone.locator('.arco-modal:visible textarea').last.fill('Clarified the roadmap and delivery impact')
        await phone.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await phone.get_by_text('项目提案已处理').wait_for()
        await owner.goto(BASE + '/teammind/approvals')
        project_proposal_card = owner.locator('article.tm-panel').filter(has_text='Browser project ' + SUFFIX)
        await project_proposal_card.get_by_role('button', name='修改后批准').click()
        await owner.locator('.arco-modal:visible input[maxlength="200"]').fill('Browser project approved ' + SUFFIX)
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.get_by_text('项目提案已处理').wait_for()
        await owner.goto(BASE + '/teammind/projects')
        await owner.get_by_role('heading', name='Browser project approved ' + SUFFIX).wait_for()
        await phone.goto(BASE + '/teammind/settings')
        assert await phone.get_by_role('heading', name='团队审计记录').count() == 0
        unread_from_api = await owner.evaluate("async () => { const token=sessionStorage.getItem('teammind.accessToken'); const r=await fetch('/api/v1/me/notifications/unread-count',{headers:{Authorization:'Bearer '+token}}); return (await r.json()).data.count }")
        notification_button = owner.get_by_role('button', name=f'通知，{unread_from_api} 条未读')
        await notification_button.click()
        await owner.wait_for_url('**/teammind/notifications')
        await owner.get_by_role('heading', name='最近投递记录').wait_for()
        delivery_rows = await owner.evaluate("async () => { const token=sessionStorage.getItem('teammind.accessToken'); const r=await fetch('/api/v1/me/notification-deliveries',{headers:{Authorization:'Bearer '+token}}); if(!r.ok) throw new Error('notification delivery request failed: '+r.status); return (await r.json()).data }")
        in_app_deliveries = [row for row in delivery_rows if row.get('channel') == 'in_app' and row.get('status') == 'delivered']
        assert in_app_deliveries, 'approval notifications must create delivered in-app records'
        await owner.wait_for_function('(expected) => document.querySelectorAll(".tm-delivery-row").length === expected', arg=len(delivery_rows))
        assert await owner.locator('.tm-delivery-row').count() == len(delivery_rows), 'delivery history UI must match the persisted API list'
        assert await owner.locator('.tm-delivery-row .status-delivered').count() >= len(in_app_deliveries), 'delivered in-app records must be visible in the delivery history'
        await owner.get_by_role('button', name='保存偏好').wait_for(state='visible')
        await owner.wait_for_function("![...document.querySelectorAll('button')].find(e => e.textContent.includes('保存偏好'))?.disabled")
        await owner.locator('input[type="time"]').nth(0).fill('22:00')
        await owner.locator('input[type="time"]').nth(1).fill('08:00')
        async with owner.expect_response(lambda response: response.url.endswith('/me/notification-preferences') and response.request.method == 'PATCH') as preference_response:
            await owner.get_by_role('button', name='保存偏好').click()
        response = await preference_response.value
        assert response.status == 200, await response.text()
        await owner.reload()
        await owner.wait_for_function("document.querySelectorAll('input[type=time]')[0]?.value === '22:00' && document.querySelectorAll('input[type=time]')[1]?.value === '08:00'")
        unread_card = owner.locator('article.tm-panel').filter(has=owner.locator('h2 small'))
        if await unread_card.count():
            await unread_card.first.get_by_role('button', name='标记已读').click()
            await owner.wait_for_timeout(200)
        read_all = owner.get_by_role('button', name='全部已读')
        if await read_all.is_enabled():
            await read_all.click()
            await owner.wait_for_timeout(200)
        await owner.goto(BASE + '/teammind/tasks')
        task_card = owner.locator('article.tm-panel').filter(has=owner.get_by_role('heading', name='Browser task edited approval ' + SUFFIX))
        await task_card.get_by_role('button', name='详情').click()
        await owner.locator('.arco-modal:visible select').nth(2).select_option(label='Workflow-Member')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='添加协作者').click()
        await owner.get_by_text('Workflow-Member · 有效').wait_for()
        await phone.goto(BASE + '/teammind/tasks')
        member_task = phone.locator('article.tm-panel').filter(has=phone.get_by_role('heading', name='Browser task edited approval ' + SUFFIX))
        try:
            await member_task.get_by_role('button', name='详情').wait_for(timeout=5000)
        except Exception as error:
            body = (await phone.locator('body').inner_text())[-1000:]
            teams = await phone.evaluate("async (token) => (await fetch('/api/v1/me/teams', {headers:{Authorization:'Bearer '+token}})).text", member_token)
            raise AssertionError(f'member task unavailable after membership restore: url={phone.url}; body={body}; teams={teams}') from error
        await member_task.get_by_role('button', name='详情').tap()
        await phone.get_by_text('Workflow-Member · 有效').wait_for()
        await phone.locator('.arco-modal:visible select').first.select_option('review')
        await phone.locator('.arco-modal:visible input[type="number"]').fill('35')
        await phone.locator('.arco-modal:visible').get_by_role('button', name='保存状态').tap()
        await phone.get_by_text('任务状态已保存').wait_for()
        await phone.reload()
        await phone.wait_for_timeout(500)
        if not await phone.locator('.tm-card-grid').get_by_text('Browser task edited approval ' + SUFFIX).count():
            print('Mobile task card after refresh:', (await phone.locator('body').inner_text())[-1200:])
        await phone.get_by_text('Browser task edited approval ' + SUFFIX).wait_for()
        await phone.goto(BASE + '/teammind/gantt')
        await phone.locator('input[aria-label="定位日期"]').fill('2026-10-30')
        await phone.get_by_role('button', name='定位', exact=True).tap()
        await phone.wait_for_timeout(250)
        horizontal_offset = await phone.locator('.tm-gantt').evaluate('(element) => element.scrollLeft')
        assert horizontal_offset > 0, f'phone Gantt timeline did not move: {horizontal_offset}'
        dimensions = await phone.evaluate('({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})')
        assert dimensions['scroll'] <= dimensions['client'] + 1, dimensions
        await phone.goto(BASE + '/teammind/assistant')
        await phone.locator('.tm-chat-compose input[type="checkbox"]').nth(0).check()
        await phone.locator('.tm-chat-compose textarea').fill('Mobile agent permission check ' + SUFFIX)
        await phone.get_by_role('button', name='发送', exact=True).tap()
        await phone.get_by_text('[本地模拟模式] 已收到你的消息：Mobile agent permission check ' + SUFFIX, exact=False).wait_for()
        await phone.get_by_role('button', name='查询当前团队任务（只读工具）').tap()
        await phone.get_by_role('status').filter(has_text='只读工具返回').wait_for()
        await phone.get_by_text('Browser task edited approval ' + SUFFIX).wait_for()
        dimensions = await phone.evaluate('({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})')
        assert dimensions['scroll'] <= dimensions['client'] + 1, dimensions
        await owner.goto(BASE + '/teammind/members')
        member_card = owner.locator('article.tm-member').filter(has_text='Workflow-Member')
        await member_card.get_by_role('button', name='角色与状态').click()
        await owner.locator('.arco-modal:visible select').nth(1).select_option('left')
        await owner.locator('.arco-modal:visible').last.get_by_role('button', name='确定').click()
        await owner.locator('.arco-modal:visible').last.get_by_role('button', name='确定').click()
        await owner.get_by_text('成员已更新').last.wait_for()
        await member_card.wait_for(state='detached')
        await owner.goto(BASE + '/teammind/settings')
        await owner.get_by_role('button', name='刷新记录').click()
        await owner.get_by_text('member.update').first.wait_for()
        await owner.goto(BASE + '/teammind/members')
        await owner.get_by_role('heading', name='邀请链接').wait_for()
        # The first invite was consumed when the mobile member joined. Create
        # a separate unused invite so revocation is verified against the
        # intended live-link case rather than a fully used token.
        await owner.get_by_role('button', name='邀请成员').click()
        await owner.locator('.arco-modal:visible input').nth(1).fill('1')
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await owner.locator('.tm-link').wait_for(state='visible')
        unused_invite = await owner.locator('.tm-link').input_value()
        unused_token = unused_invite.rsplit('/', 1)[-1]
        await owner.locator('.arco-modal:visible').last.locator('.arco-modal-close-btn').click()
        revoke_buttons = owner.get_by_role('button', name='撤销')
        if await revoke_buttons.count() == 0:
            team_id = await owner.evaluate('sessionStorage.getItem("teammind.teamId")')
            token = await owner.evaluate('sessionStorage.getItem("teammind.accessToken")')
            invitations = await owner.evaluate("async ({teamId, token}) => (await fetch(`/api/v1/teams/${teamId}/invitations`, {headers:{Authorization:'Bearer '+token}})).text", {'teamId': team_id, 'token': token})
            raise AssertionError(f'invitation revoke button missing: team={team_id}; body={(await owner.locator("body").inner_text())[-1000:]}; api={invitations}')
        unused_invitation_row = owner.locator('.tm-row').filter(has_text='0/1 次')
        await unused_invitation_row.get_by_role('button', name='撤销').click()
        await owner.locator('.arco-modal:visible').get_by_role('button', name='确定').click()
        await unused_invitation_row.get_by_text('已撤销').wait_for()
        revoked_status, _ = await owner.evaluate("async ({token, inviteToken}) => { const r = await fetch('/api/v1/invitations/accept', {method:'POST', headers:{'Content-Type':'application/json', Authorization:'Bearer '+token}, body:JSON.stringify({token:inviteToken})}); return [r.status, await r.text()] }", {'token': member_token, 'inviteToken': unused_token})
        assert revoked_status in (400, 410), f'revoked invitation unexpectedly accepted: {revoked_status}'
        await owner.goto(BASE + '/teammind/settings')
        secondary_team_name = 'Secondary ' + SUFFIX
        secondary_team_panel = owner.locator('.tm-panel').filter(has=owner.get_by_role('heading', name='创建团队'))
        await secondary_team_panel.locator('input').first.fill(secondary_team_name)
        await secondary_team_panel.get_by_role('button', name='创建团队').click()
        await owner.get_by_text('团队已创建').wait_for()
        await owner.wait_for_url('**/teammind/settings')
        await owner.goto(BASE + '/teammind/tasks')
        await owner.get_by_text('暂无任务。先创建项目，再添加任务。', exact=True).wait_for()
        # Hold a response from the empty secondary team until after switching
        # back to the populated team. The late response must not replace the
        # new team's overview state.
        first_summary_started = asyncio.Event()
        release_old_summary = asyncio.Event()
        summary_requests = 0
        async def hold_old_summary(route):
            nonlocal summary_requests
            summary_requests += 1
            if summary_requests == 1:
                first_summary_started.set()
                await release_old_summary.wait()
                await route.continue_()
            else:
                await route.continue_()
        await owner.route('**/api/v1/teams/*/summary', hold_old_summary)
        await owner.goto(BASE + '/teammind/overview')
        await asyncio.wait_for(first_summary_started.wait(), timeout=10)
        team_switcher = owner.get_by_label('切换团队')
        await team_switcher.click()
        await owner.get_by_text(updated_team_name + ' · team_owner', exact=True).last.click()
        await owner.wait_for_url('**/teammind/overview')
        await owner.wait_for_function("document.querySelector('.tm-stats article strong')?.textContent === '1'")
        async with owner.expect_response(lambda response: response.url.endswith('/summary')) as old_response:
            release_old_summary.set()
        assert (await old_response.value).status == 200
        await owner.wait_for_timeout(150)
        assert await owner.locator('.tm-stats article strong').first.inner_text() == '1'
        assert await owner.get_by_text('Browser project ' + SUFFIX, exact=True).count() == 0
        await owner.unroute('**/api/v1/teams/*/summary', hold_old_summary)
        await owner.goto(BASE + '/teammind/assistant')
        await owner.locator('.tm-chat-compose input[type="checkbox"]').nth(0).check()
        assistant_prompt = 'Browser streamed team chat ' + SUFFIX
        await owner.locator('.tm-chat-compose textarea').fill(assistant_prompt)
        await owner.get_by_role('button', name='发送', exact=True).click()
        await owner.get_by_text('[本地模拟模式] 已收到你的消息：' + assistant_prompt, exact=False).wait_for()
        await owner.reload()
        assistant_conversation = owner.locator('.tm-conversation').filter(has_text=assistant_prompt)
        await assistant_conversation.wait_for()
        await assistant_conversation.click()
        await owner.get_by_text(assistant_prompt, exact=True).wait_for()
        await owner.locator('.tm-chat-compose input[type="checkbox"]').nth(0).check()
        await owner.get_by_role('button', name='查询当前团队任务（只读工具）').click()
        await owner.get_by_role('status').filter(has_text='只读工具返回').wait_for()
        await owner.get_by_text('Browser task edited approval ' + SUFFIX).wait_for()
        await owner.goto(BASE + '/teammind/tasks')
        await owner.get_by_role('heading', name='Browser task edited approval ' + SUFFIX).wait_for()
        await owner.goto(BASE + '/teammind/settings')
        password_panel = owner.locator('.tm-panel').filter(has=owner.get_by_role('heading', name='修改密码', exact=True))
        await password_panel.locator('input[autocomplete="current-password"]').fill(PASSWORD)
        await password_panel.locator('input[autocomplete="new-password"]').fill(PASSWORD + 'changed')
        await password_panel.get_by_role('button', name='修改并退出所有会话').click()
        await owner.wait_for_url('**/login')
        await owner.locator('input[autocomplete="username"]').fill(owner_email)
        await owner.locator('input[autocomplete="current-password"]').fill(PASSWORD + 'changed')
        await owner.get_by_role('button', name='登录', exact=True).click()
        await owner.wait_for_url('**/teammind/overview')
        final_token = await owner.evaluate('sessionStorage.getItem("teammind.accessToken")')
        await owner.get_by_role('button', name='账号菜单').click()
        await owner.get_by_text('退出登录', exact=True).last.click()
        await owner.wait_for_url('**/login')
        revoked_status = await owner.evaluate("async token => (await fetch('/api/v1/me',{headers:{Authorization:'Bearer '+token}})).status", final_token)
        assert revoked_status == 401, revoked_status
        print('PASS two-browser persistent theme and top-bar search, notification entry, mobile drawer, overview stats and recent task, profile/team settings and switching/isolation, password change/session renewal, project CRUD and approved project proposal/resubmission, pagination/filtering/links, member role edits, notification preferences/read actions, task, edited approval and resubmission, dependency, Gantt filters/date positioning, desktop/mobile read-only agent tool, collaborator persistence, invitation copy/revoke and mobile acceptance')
        await browser.close()

asyncio.run(run())
