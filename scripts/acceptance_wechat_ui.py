"""Exercise the real Tencent QR flow from the mobile-sized TeamMind web page.

Requires an isolated MySQL database, a running real-mode TeamMind API, the
authenticated OpenClaw bridge, and Playwright WebKit. It never prints QR data,
does not bind a WeChat account, and cancels the temporary login session.
"""
import asyncio
import json
import os
import secrets
import urllib.error
import urllib.request

from playwright.async_api import async_playwright


BASE = os.getenv("TEAMMIND_WEB_BASE", "http://127.0.0.1:5174").rstrip("/")
API = os.getenv("TEAMMIND_API_BASE", "http://127.0.0.1:8188/api/v1").rstrip("/")
ENGINE = os.getenv("TEAMMIND_BROWSER", "webkit")
SUFFIX = secrets.token_hex(8)
EMAIL = f"wechat-ui-{SUFFIX}@example.invalid"
PASSWORD = "Wechat-UI-" + secrets.token_urlsafe(18)


def api(path, method="GET", data=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = urllib.request.Request(
        API + path,
        data=None if data is None else json.dumps(data).encode(),
        headers=headers,
        method=method,
    )
    try:
        with urllib.request.urlopen(request, timeout=50) as response:
            return response.status, json.load(response)["data"]
    except urllib.error.HTTPError as error:
        try:
            body = json.load(error)
        except Exception:
            body = {"code": "HTTP_ERROR"}
        return error.code, body


def require(condition, message):
    if not condition:
        raise AssertionError(message)
    print("PASS", message)


async def main():
    if ENGINE not in {"chromium", "firefox", "webkit"}:
        raise ValueError("TEAMMIND_BROWSER must be chromium, firefox, or webkit")
    status, _ = api("/auth/register", "POST", {
        "email": EMAIL,
        "name": "WeChat UI Acceptance",
        "password": PASSWORD,
    })
    require(status == 201, "register isolated acceptance account")
    status, login = api("/auth/login", "POST", {"email": EMAIL, "password": PASSWORD})
    require(status == 200, "create authenticated acceptance session")
    token = login["accessToken"]
    status, team = api("/teams", "POST", {
        "name": "WeChat UI " + SUFFIX,
        "description": "Isolated real plugin web acceptance",
        "timezone": "Asia/Shanghai",
    }, token)
    require(status == 201, "create isolated team for channel binding")
    status, channel = api("/me/channels/wechat", token=token)
    require(status == 200 and channel.get("mode") == "real", "web acceptance uses real plugin mode")
    require(channel.get("messagingReady") is False, "an unbound test account is not falsely reported as receiving messages")

    async with async_playwright() as playwright:
        browser = await getattr(playwright, ENGINE).launch(headless=True)
        page = await browser.new_page(
            viewport={"width": 390, "height": 844},
            is_mobile=True,
            has_touch=True,
        )
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        await page.goto(BASE + "/login")
        await page.locator('input[autocomplete="username"]').fill(EMAIL)
        await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
        await page.get_by_role("button", name="登录", exact=True).click()
        await page.wait_for_url("**/teammind/overview")
        await page.goto(BASE + "/teammind/wechat")
        await page.get_by_role("heading", name="微信渠道绑定").wait_for()
        await page.get_by_text("按腾讯维护的 OpenClaw 微信渠道扫码流程连接").wait_for()
        assert await page.get_by_text("本地模拟渠道：不能用于真实微信扫码或消息收发。").count() == 0
        assert await page.get_by_text("微信通道未配置。", exact=False).count() == 0

        async with page.expect_response(
            lambda response: response.url.endswith("/me/channels/wechat/sessions")
            and response.request.method == "POST"
        ) as started:
            await page.get_by_role("button", name="连接微信").click()
        response = await started.value
        assert response.status == 201, await response.text()
        session = (await response.json())["data"]
        require(session.get("simulated") is False and bool(session.get("qrContent")), "page receives non-simulated Tencent QR")
        await page.locator('img[alt="微信渠道二维码"]').wait_for()
        qr_rendered = await page.locator('img[alt="微信渠道二维码"]').evaluate(
            "img => img.complete && img.naturalWidth >= 200 && img.naturalHeight >= 200 && img.src.startsWith('data:image/png;base64,')"
        )
        require(qr_rendered, "real plugin QR is rendered as an image in the mobile web page")
        await page.get_by_text("状态：等待扫码", exact=False).wait_for()
        await page.get_by_text("手机浏览器上请在另一台设备展示二维码，再用微信扫码并在手机上确认。", exact=False).wait_for()
        no_overflow = await page.evaluate("document.documentElement.scrollWidth <= window.innerWidth")
        require(no_overflow, "390px channel page has no document-level horizontal overflow")

        await page.get_by_role("button", name="取消").click()
        await page.locator('img[alt="微信渠道二维码"]').wait_for(state="detached")
        status, state = api(
            f"/me/channels/wechat/sessions/{session['id']}", token=token
        )
        require(status == 200 and state.get("status") == "cancelled", "page cancellation persists and clears the temporary login")
        status, after = api("/me/channels/wechat", token=token)
        require(status == 200 and not after.get("bindings"), "opening and cancelling QR never creates a binding")
        await browser.close()
    require(not errors, "mobile-sized channel page has no browser runtime errors")
    print("INFO QR contents were not printed; no person scanned or confirmed this session.")


if __name__ == "__main__":
    asyncio.run(main())
