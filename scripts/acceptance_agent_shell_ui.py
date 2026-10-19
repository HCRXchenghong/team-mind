"""Exercise the agent shell consent UI with an isolated deterministic model response.

This browser test validates consent handoff and private file saving. Actual Bash
execution and isolation are verified by Go/MySQL integration tests; this script
does not represent a live commercial model connection.
"""
import asyncio
import json
import os
import secrets
import urllib.error
import urllib.request

from playwright.async_api import async_playwright


WEB = os.getenv("TEAMMIND_WEB_BASE", "http://127.0.0.1:5195")
API = os.getenv("TEAMMIND_API_BASE", "http://127.0.0.1:8203/api/v1")
SUFFIX = secrets.token_hex(6)
EMAIL = f"agent-shell-{SUFFIX}@teammind.test"
PASSWORD = "Shell-" + secrets.token_urlsafe(18)


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


async def main():
    status, result = request("POST", "/auth/register", {
        "email": EMAIL, "name": "Agent Shell UI Acceptance", "password": PASSWORD,
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
            browser = await playwright.chromium.launch(headless=True)
            page = await browser.new_page(viewport={"width": 390, "height": 844})
            page.set_default_timeout(8000)
            page_errors = []
            page.on("pageerror", lambda error: page_errors.append(str(error)))
            agent_requests = []

            async def fake_ai_status(route):
                await route.fulfill(status=200, content_type="application/json", body=json.dumps({
                    "code": "OK",
                    "data": {
                        "provider": "openai_compatible", "configured": True,
                        "simulated": False, "agentTools": True,
                        "sandboxAvailable": True, "shellAvailable": True,
                        "workspaceAvailable": True,
                    },
                }))

            async def deterministic_agent_result(route):
                payload = json.loads(route.request.post_data or "{}")
                agent_requests.append(payload)
                if len(agent_requests) > 1:
                    await route.fulfill(status=200, content_type="application/json", body=json.dumps({
                        "code": "OK",
                        "data": {
                            "provider": "openai_compatible", "model": "acceptance-model",
                            "reply": "我查询了任务，并创建了待审批提案。", "tool": "propose_task_change",
                            "result": {"proposalId": "proposal-ui-check", "status": "pending"},
                            "steps": [
                                {"tool": "list_team_tasks", "result": {"count": 1, "items": [{"id": "task-ui-check"}]}},
                                {"tool": "propose_task_change", "result": {"proposalId": "proposal-ui-check", "status": "pending"}},
                            ],
                        },
                    }))
                    return
                await route.fulfill(status=200, content_type="application/json", body=json.dumps({
                    "code": "OK",
                    "data": {
                        "provider": "openai_compatible", "model": "acceptance-model",
                        "reply": "沙箱结果可供你查看。", "tool": "run_shell",
                        "result": {
                            "stdout": "shell ui ok", "exitCode": 0,
                            "files": {"shell-result.txt": "shell ui ok"},
                            "temporary": True, "persisted": False,
                        },
                    },
                }))

            await page.route("**/api/v1/me/ai/status", fake_ai_status)
            await page.route("**/api/v1/me/conversations/*/agent", deterministic_agent_result)
            await page.goto(WEB + "/login")
            await page.locator('input[autocomplete="username"]').fill(EMAIL)
            await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
            await page.get_by_role("button", name="登录", exact=True).click()
            await page.wait_for_url("**/teammind/overview")

            await page.goto(WEB + "/teammind/assistant")
            await page.get_by_role("heading", name="AI 助手").wait_for()
            shell_consent = page.get_by_label("本次允许模型运行 Shell 命令（断网临时沙箱）")
            python_consent = page.get_by_label("本次允许模型运行 Python（断网临时沙箱）")
            await shell_consent.check()
            await page.locator('textarea[placeholder="输入消息"]').fill("创建一个文本结果并告诉我是否成功")
            await page.get_by_role("button", name="运行智能体（最多 3 步顺序工作流）").click()
            await page.get_by_text("智能体工具执行：断网临时 Shell 沙箱").wait_for()
            assert len(agent_requests) == 1, agent_requests
            assert agent_requests[0]["allowShellExecution"] is True, agent_requests
            assert agent_requests[0]["allowCodeExecution"] is False, agent_requests
            assert agent_requests[0]["persistWorkspaceFiles"] is False, agent_requests
            assert await shell_consent.is_checked() is False
            assert await python_consent.is_checked() is False
            await page.get_by_text("查看沙箱输出文件", exact=True).click()

            async with page.expect_response(
                lambda response: response.request.method == "POST"
                and response.url.endswith("/api/v1/me/agent/workspace/files")
            ) as response_info:
                await page.get_by_role("button", name="保存到私人工作区").click()
            saved = await response_info.value
            assert saved.status == 201, (saved.status, await saved.text())

            status, files = request("GET", "/me/agent/workspace/files", token=token)
            assert status == 200, (status, files)
            assert any(item["name"] == "shell-result.txt" for item in files["data"]["files"]), files

            await page.locator('textarea[placeholder="输入消息"]').fill("先查询任务再提交计划提案")
            await page.get_by_role("button", name="运行智能体（最多 3 步顺序工作流）").click()
            await page.get_by_text("智能体完成 2 步顺序工作流").wait_for()
            assert await page.locator(".tm-agent-steps li").count() == 2
            await page.get_by_text("读取当前团队任务", exact=True).wait_for()
            await page.get_by_text("创建待审批计划提案", exact=True).wait_for()
            assert not page_errors, page_errors
            print("PASS: Chromium UI enforces separate one-call Shell consent, resets it, saves authorized output, and displays the sequential task-query/proposal workflow")
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
        print("PASS: test account deleted; its session was revoked")


asyncio.run(main())
