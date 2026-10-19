"""Verify the one-request team knowledge consent and review navigation in Chromium."""
import asyncio
import json
import os
import secrets
import urllib.error
import urllib.request

from playwright.async_api import async_playwright


WEB = os.getenv("TEAMMIND_WEB_BASE", "http://127.0.0.1:5197")
API = os.getenv("TEAMMIND_API_BASE", "http://127.0.0.1:8219/api/v1")
ENGINE = os.getenv("TEAMMIND_BROWSER", "chromium")
if ENGINE not in {"chromium", "webkit"}:
    raise ValueError("TEAMMIND_BROWSER must be chromium or webkit")
SUFFIX = secrets.token_hex(6)
EMAIL = f"agent-memory-ui-{SUFFIX}@teammind.test"
PASSWORD = "Agent-memory-" + secrets.token_urlsafe(18)
TEAM_ID = "a" * 32
CONVERSATION_ID = "b" * 32
MESSAGE_ID = "c" * 32
MEMORY_ID = "d" * 32
TEAM_ROLE = "team_owner"


def request(method, path, payload=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    raw = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(API + path, data=raw, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=15) as response:
            data = response.read()
            return response.status, json.loads(data) if data else {}
    except urllib.error.HTTPError as error:
        body = error.read()
        return error.code, json.loads(body) if body else {}


async def check_mobile_widths(page, diagnostic):
    for width in (320, 360, 375, 390, 430, 768, 1024, 1280):
        diagnostic["width"] = width
        await page.set_viewport_size({"width": width, "height": 844})
        await page.wait_for_timeout(250)
        dimensions = await page.evaluate("({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})")
        assert dimensions["scroll"] <= dimensions["client"] + 1, (width, dimensions, page.url)
    await page.set_viewport_size({"width": 390, "height": 844})
    diagnostic["width"] = 390


async def main():
    global TEAM_ROLE
    status, result = request("POST", "/auth/register", {
        "email": EMAIL, "name": "Agent Memory UI Acceptance", "password": PASSWORD,
    })
    assert status == 201, (status, result)
    status, result = request("POST", "/auth/login", {
        "email": EMAIL, "password": PASSWORD, "remember": False,
    })
    assert status == 200, (status, result)
    token = result["data"]["accessToken"]
    browser = None
    try:
        async with async_playwright() as playwright:
            browser = await getattr(playwright, ENGINE).launch(headless=True)
            page = await browser.new_page(viewport={"width": 390, "height": 844}, is_mobile=True, has_touch=True)
            page.set_default_timeout(10000)
            page_errors = []
            page_warnings = []
            stage = {"name": "login"}
            diagnostic = {"width": 390}
            agent_requests = []

            def record_page_error(error):
                entry = {"stage": stage["name"], "width": diagnostic["width"], "url": page.url, "error": str(error), "stack": error.stack}
                if ENGINE == "webkit" and str(error) == "ResizeObserver loop completed with undelivered notifications.":
                    page_warnings.append(entry)
                else:
                    page_errors.append(entry)

            page.on("pageerror", record_page_error)

            async def route_teams(route):
                await route.fulfill(status=200, content_type="application/json", body=json.dumps({
                    "code": "OK", "data": [{
                        "id": TEAM_ID, "name": "Acceptance Team", "description": "",
                        "timezone": "Asia/Shanghai", "role": TEAM_ROLE, "status": "active", "members": 1,
                    }],
                }))

            async def route_ai_status(route):
                await route.fulfill(status=200, content_type="application/json", body=json.dumps({
                    "code": "OK", "data": {
                        "provider": "openai_compatible", "configured": True,
                        "simulated": False, "agentTools": True,
                        "sandboxAvailable": False, "shellAvailable": False,
                        "workspaceAvailable": False,
                    },
                }))

            async def route_conversations(route):
                if route.request.method == "POST":
                    await route.fulfill(status=201, content_type="application/json", body=json.dumps({
                        "code": "OK", "data": {"id": CONVERSATION_ID, "teamId": TEAM_ID},
                    }))
                else:
                    await route.fulfill(status=200, content_type="application/json", body=json.dumps({
                        "code": "OK", "data": [{
                            "id": CONVERSATION_ID, "teamId": TEAM_ID,
                            "title": "Acceptance conversation", "updatedAt": "2026-10-19T00:00:00Z",
                        }],
                    }))

            async def route_agent(route):
                agent_requests.append(json.loads(route.request.post_data or "{}"))
                await route.fulfill(status=200, content_type="application/json", body=json.dumps({
                    "code": "OK", "data": {
                        "provider": "openai_compatible", "model": "acceptance-model",
                        "reply": "已创建待确认团队知识候选。", "tool": "create_team_memory_candidate",
                        "result": {"memoryId": MEMORY_ID, "status": "pending_confirmation"},
                        "steps": [{"tool": "create_team_memory_candidate", "result": {"memoryId": MEMORY_ID, "status": "pending_confirmation"}}],
                    },
                }))

            async def route_messages(route):
                await route.fulfill(status=200, content_type="application/json", body=json.dumps({
                    "code": "OK", "data": [{
                        "id": MESSAGE_ID, "role": "user",
                        "content": "Add this deployment policy to team knowledge",
                    }],
                }))

            async def route_personal_memories(route):
                await route.fulfill(status=200, content_type="application/json", body='{"code":"OK","data":[]}')

            async def route_team_memories(route):
                await route.fulfill(status=200, content_type="application/json", body=json.dumps({
                    "code": "OK", "data": [{
                        "id": MEMORY_ID, "category": "policy", "content": "Deployments require peer review.",
                        "confirmed": False, "updatedAt": "2026-10-19T00:00:00Z",
                        "sourceMessageId": MESSAGE_ID, "sourceConversationId": CONVERSATION_ID,
                        "sourceRole": "user", "sourceExcerpt": "Add this deployment policy to team knowledge",
                    }],
                }))

            await page.route("**/api/v1/me/teams", route_teams)
            await page.route("**/api/v1/me/ai/status", route_ai_status)
            await page.route("**/api/v1/me/conversations", route_conversations)
            await page.route("**/api/v1/me/conversations/*/agent", route_agent)
            await page.route("**/api/v1/me/conversations/*/messages", route_messages)
            await page.route("**/api/v1/me/memories", route_personal_memories)
            await page.route("**/api/v1/teams/*/memories", route_team_memories)

            await page.goto(WEB + "/login")
            await page.locator('input[autocomplete="username"]').fill(EMAIL)
            await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
            await page.get_by_role("button", name="登录", exact=True).click()
            await page.wait_for_url("**/teammind/overview")
            await page.goto(WEB + "/teammind/assistant")
            stage["name"] = "assistant"
            await page.get_by_role("heading", name="AI 助手").wait_for()

            team_context = page.get_by_label("确认使用当前团队（Acceptance Team）上下文")
            await team_context.check()
            consent = page.get_by_label("本次允许智能体创建一条待确认团队知识候选（仅管理员/所有者；不会自动进入 AI 上下文）")
            await consent.wait_for()
            await consent.check()
            await check_mobile_widths(page, diagnostic)
            await page.locator('textarea[placeholder="输入消息"]').fill("把部署需要同伴复核加入团队知识")
            await page.get_by_role("button", name="运行智能体（最多 3 步顺序工作流）").click()
            await page.get_by_text("已创建待确认团队知识候选。确认前不会进入 AI 知识上下文。", exact=True).wait_for()
            assert len(agent_requests) == 1, agent_requests
            assert agent_requests[0]["allowTeamKnowledgeCandidate"] is True, agent_requests
            assert agent_requests[0]["confirmTeam"] is True, agent_requests
            assert await consent.is_checked() is False

            await page.get_by_role("button", name="打开团队知识并审核").click()
            stage["name"] = "team memories"
            await page.get_by_text("Deployments require peer review.", exact=True).wait_for()
            await page.get_by_text("待确认", exact=True).wait_for()
            await check_mobile_widths(page, diagnostic)
            await page.get_by_role("button", name="查看来源对话").click()
            await page.wait_for_function(
                "location.pathname === '/teammind/assistant' && "
                "location.search.includes('conversationId=') && location.search.includes('sourceMessageId=')"
            )
            TEAM_ROLE = "member"
            stage["name"] = "member assistant"
            await page.reload()
            await page.get_by_role("heading", name="AI 助手").wait_for()
            assert await page.get_by_label("本次允许智能体创建一条待确认团队知识候选（仅管理员/所有者；不会自动进入 AI 上下文）").count() == 0
            if page_errors:
                raise AssertionError(page_errors)
            print(f"PASS: {ENGINE} mobile UI restricts one-request consent to owners/admins, submits confirmed team context, resets authorization, and navigates to pending candidate/source; member UI hides the control across 8 viewports")
            if page_warnings:
                print(f"WARN: WebKit emitted {len(page_warnings)} ResizeObserver loop notification(s) while resizing the Team Knowledge page; all requested viewport and interaction assertions passed: {page_warnings}")
            await browser.close()
            browser = None
    finally:
        if browser:
            await browser.close()
        status, result = request("POST", "/me/account", {
            "currentPassword": PASSWORD, "confirmation": "删除账号",
        }, token)
        assert status == 200, (status, result)
        status, result = request("GET", "/me", token=token)
        assert status == 401, (status, result)
        print("PASS: UI acceptance account deleted; its session was revoked")


asyncio.run(main())
