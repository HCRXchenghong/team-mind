"""Run a small settings acceptance against Android Chrome through adb/CDP.

Prerequisites: Android emulator/device with Chrome, adb USB debugging,
``adb forward tcp:9222 localabstract:chrome_devtools_remote``, ``adb reverse
tcp:5190 tcp:5190``, a Vite frontend on 5190, and an isolated TeamMind API on
8199 (the Vite proxy target used by this workspace's emulator setup).
This is still an emulator test when run against an emulator, not a physical
Android device test.
"""
import asyncio
import json
import os
import secrets
import urllib.error
import urllib.request
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo

from playwright.async_api import async_playwright


WEB = os.getenv("TEAMMIND_WEB_BASE", "http://127.0.0.1:5190")
API = os.getenv("TEAMMIND_API_BASE", "http://127.0.0.1:8199/api/v1")
CDP = os.getenv("ANDROID_CHROME_CDP", "http://127.0.0.1:9222")
SUFFIX = secrets.token_hex(5)
EMAIL = f"android-chrome-{SUFFIX}@teammind.test"
PASSWORD = f"AndroidChrome{SUFFIX}A"


def request(method, path, payload=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    body = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(API + path, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=15) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read())


async def touch(page, locator):
    await locator.scroll_into_view_if_needed()
    box = await locator.bounding_box()
    assert box, "touch target has no visible box"
    x = box["x"] + box["width"] / 2
    y = box["y"] + box["height"] / 2
    cdp = await page.context.new_cdp_session(page)
    await cdp.send(
        "Input.dispatchTouchEvent",
        {"type": "touchStart", "touchPoints": [{"x": x, "y": y}]},
    )
    await cdp.send("Input.dispatchTouchEvent", {"type": "touchEnd", "touchPoints": []})
    await cdp.detach()


async def main():
    status, result = request(
        "POST", "/auth/register",
        {"email": EMAIL, "name": "Android Chrome Tester", "password": PASSWORD},
    )
    assert status == 201, (status, result)

    cleanup_token = ""
    browser = None
    try:
        status, result = request(
            "POST", "/auth/login",
            {"email": EMAIL, "password": PASSWORD, "remember": False},
        )
        assert status == 200, (status, result)
        cleanup_token = result["data"]["accessToken"]

        async with async_playwright() as playwright:
            browser = await playwright.chromium.connect_over_cdp(CDP, timeout=15000)
            if browser.contexts and browser.contexts[0].pages:
                page = browser.contexts[0].pages[0]
            else:
                page = await browser.contexts[0].new_page()
            page.set_default_timeout(15000)
            page_errors = []
            page.on("pageerror", lambda error: page_errors.append(str(error)))

            await page.goto(WEB + "/login")
            await page.locator('input[autocomplete="username"]').fill(EMAIL)
            await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
            await touch(page, page.get_by_role("button", name="登录", exact=True))
            await page.wait_for_url("**/teammind/overview", timeout=20000)

            await page.goto(WEB + "/teammind/settings")
            await page.get_by_role("heading", name="设置", exact=True).wait_for()
            dimensions = await page.evaluate(
                "({width:innerWidth,height:innerHeight,dpr:devicePixelRatio,"
                "scroll:document.documentElement.scrollWidth,"
                "client:document.documentElement.clientWidth})"
            )
            assert dimensions["scroll"] <= dimensions["client"] + 1, dimensions

            new_name = "Android Chrome Verified"
            await page.get_by_label("姓名").fill(new_name)
            async with page.expect_response(
                lambda response: response.request.method == "PATCH"
                and response.url.endswith("/api/v1/me")
            ) as response_info:
                await touch(page, page.get_by_role("button", name="保存资料"))
            response = await response_info.value
            assert response.status == 200, (response.status, await response.text())
            await page.reload()
            await page.get_by_role("heading", name="设置", exact=True).wait_for()
            assert await page.get_by_label("姓名").input_value() == new_name

            await page.goto(WEB + "/teammind/notifications")
            await page.get_by_role("heading", name="通知中心", exact=True).wait_for()
            notification_dimensions = await page.evaluate(
                "({width:innerWidth,height:innerHeight,scroll:document.documentElement.scrollWidth,"
                "client:document.documentElement.clientWidth})"
            )
            assert notification_dimensions["scroll"] <= notification_dimensions["client"] + 1, notification_dimensions
            quiet_times = page.locator('input[type="time"]')
            assert await quiet_times.count() == 2
            await quiet_times.nth(0).fill("22:00")
            await quiet_times.nth(1).fill("07:00")
            async with page.expect_response(
                lambda response: response.request.method == "PATCH"
                and response.url.endswith("/api/v1/me/notification-preferences")
            ) as response_info:
                await touch(page, page.get_by_role("button", name="保存偏好"))
            response = await response_info.value
            assert response.status == 200, (response.status, await response.text())
            await page.reload()
            await page.get_by_role("heading", name="通知中心", exact=True).wait_for()
            await page.wait_for_function(
                """() => {
                  const inputs = [...document.querySelectorAll('input[type="time"]')];
                  return inputs.length === 2 && inputs[0].value === '22:00' && inputs[1].value === '07:00';
                }"""
            )
            assert await quiet_times.nth(0).input_value() == "22:00"
            assert await quiet_times.nth(1).input_value() == "07:00"

            run_at = (
                datetime.now(ZoneInfo("Asia/Shanghai")) + timedelta(minutes=10)
            ).strftime("%Y-%m-%dT%H:%M")
            reminder_title = f"Android Chrome {SUFFIX}"
            await page.get_by_label("提醒时间").fill(run_at)
            await page.get_by_label("标题").fill(reminder_title)
            async with page.expect_response(
                lambda response: response.request.method == "POST"
                and response.url.endswith("/api/v1/me/reminders")
            ) as response_info:
                await touch(page, page.get_by_role("button", name="创建提醒"))
            response = await response_info.value
            assert response.status == 201, (response.status, await response.text())
            reminder = page.locator("article").filter(has_text=reminder_title)
            await reminder.wait_for()
            async with page.expect_response(
                lambda response: response.request.method == "DELETE"
                and "/api/v1/me/reminders/" in response.url
            ) as response_info:
                await touch(page, reminder.get_by_role("button", name="取消"))
            response = await response_info.value
            assert response.status == 200, (response.status, await response.text())
            await page.reload()
            await page.get_by_role("heading", name="通知中心", exact=True).wait_for()
            reminder = page.locator("article").filter(has_text=reminder_title)
            await reminder.wait_for()
            assert "已取消" in await reminder.inner_text()
            assert await reminder.get_by_role("button", name="取消").count() == 0

            assert not page_errors, page_errors

            print(
                "PASS Android Chrome touch login, persisted profile/notification preferences, and reminder create/cancel; "
                f"viewport={dimensions['width']}x{dimensions['height']} "
                f"dpr={dimensions['dpr']} scrollWidth={dimensions['scroll']} "
                f"clientWidth={dimensions['client']} "
                f"notificationScrollWidth={notification_dimensions['scroll']}"
            )
            await browser.close()
            browser = None
    finally:
        if browser:
            await browser.close()
        if cleanup_token:
            status, result = request(
                "POST", "/me/account",
                {"currentPassword": PASSWORD, "confirmation": "删除账号"},
                cleanup_token,
            )
            assert status == 200, (status, result)


asyncio.run(main())
