"""Exercise member identity proposal submission and approval in a real browser.

This uses an isolated migrated TeamMind database and its Vite/API processes. It
creates disposable acceptance users and a team; no mock API responses are used.
"""
import asyncio
import json
import os
import secrets
import urllib.error
import urllib.request

from playwright.async_api import async_playwright


WEB = os.getenv("TEAMMIND_WEB_BASE", "http://127.0.0.1:5190").rstrip("/")
API = os.getenv("TEAMMIND_API_BASE", "http://127.0.0.1:8240/api/v1").rstrip("/")
ENGINE = os.getenv("TEAMMIND_BROWSER", "chromium")
if ENGINE not in {"chromium", "firefox", "webkit"}:
    raise ValueError("TEAMMIND_BROWSER must be chromium, firefox, or webkit")
SUFFIX = secrets.token_hex(6)
PASSWORD = "Member-proposal-" + secrets.token_urlsafe(18)


def call(method, path, payload=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    body = None if payload is None else json.dumps(payload).encode()
    request = urllib.request.Request(API + path, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as error:
        raw = error.read()
        return error.code, json.loads(raw) if raw else {}


def data(method, path, payload=None, token=None, expected=None):
    status, result = call(method, path, payload, token)
    if expected is not None and status != expected:
        raise AssertionError(f"{method} {path}: expected HTTP {expected}, got {status}: {result}")
    if status >= 400:
        raise AssertionError(f"{method} {path}: HTTP {status}: {result}")
    return result.get("data", result)


def create_user(label):
    email = f"member-proposal-{label.lower().replace(' ', '-')}-{SUFFIX}@teammind.test"
    data("POST", "/auth/register", {"email": email, "name": label, "password": PASSWORD}, expected=201)
    login = data("POST", "/auth/login", {"email": email, "password": PASSWORD, "remember": False})
    return {"email": email, "token": login["accessToken"]}


async def main():
    owner = create_user("Proposal Owner")
    proposer = create_user("Proposal Requester")
    target = create_user("Proposal Target")
    team = data("POST", "/teams", {
        "name": "Member Proposal Acceptance " + SUFFIX,
        "description": "Browser acceptance fixture",
        "timezone": "Asia/Shanghai",
    }, owner["token"], expected=201)
    invitation = data("POST", f"/teams/{team['id']}/invitations", {
        "expiresInHours": 1,
        "maxUses": 2,
    }, owner["token"], expected=201)
    for user in (proposer, target):
        data("POST", "/invitations/accept", {"token": invitation["token"]}, user["token"], expected=200)
    members = data("GET", f"/teams/{team['id']}/members", token=owner["token"])
    target_member = next(member for member in members if member["email"] == target["email"])
    data("POST", f"/teams/{team['id']}/member-proposals", {
        "memberId": target_member["id"],
        "status": "suspended",
        "reason": "Pause access pending the security review",
        "impactSummary": "The member cannot access this team while the review is open",
    }, proposer["token"], expected=201)
    before = data("GET", f"/teams/{team['id']}/members", token=owner["token"])
    assert next(member for member in before if member["id"] == target_member["id"])["status"] == "active"

    async with async_playwright() as playwright:
        browser = await getattr(playwright, ENGINE).launch(headless=True)
        page = await browser.new_page(viewport={"width": 390, "height": 844}, is_mobile=True, has_touch=True)
        page.set_default_timeout(12000)
        errors = []
        member_responses = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.on("response", lambda response: member_responses.append((response.status, response.url.split('?', 1)[0])) if "/member-proposals/" in response.url else None)
        await page.goto(WEB + "/login")
        await page.locator('input[autocomplete="username"]').fill(owner["email"])
        await page.locator('input[autocomplete="current-password"]').fill(PASSWORD)
        await page.get_by_role("button", name="登录", exact=True).click()
        await page.wait_for_url("**/teammind/overview")
        await page.goto(WEB + "/teammind/approvals")
        await page.get_by_role("heading", name="成员身份变更").wait_for()
        card = page.locator("article.tm-panel").filter(has=page.get_by_role("heading", name="Proposal Target"))
        await card.get_by_text("建议：普通成员 / 已停用", exact=True).wait_for()
        for width in (320, 360, 375, 390, 430, 768, 1024, 1280):
            await page.set_viewport_size({"width": width, "height": 844})
            await page.wait_for_timeout(100)
            dimensions = await page.evaluate("({scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth})")
            assert dimensions["scroll"] <= dimensions["client"] + 1, (width, dimensions)
        await page.set_viewport_size({"width": 390, "height": 844})
        card = page.locator("article.tm-panel").filter(has=page.get_by_role("heading", name="Proposal Target"))
        await card.get_by_role("button", name="批准", exact=True).tap()
        dialog = page.locator(".arco-modal:visible").last
        await dialog.locator(".arco-modal-title").get_by_text("处理成员身份提案", exact=True).wait_for()
        await dialog.get_by_role("button", name="确定", exact=True).tap()
        await page.wait_for_timeout(800)
        if not any("member-proposals/" in url and status < 400 for status, url in member_responses):
            raise AssertionError(f"member decision API did not succeed: {member_responses}; dialog={await dialog.inner_text()}")
        await card.get_by_text("已批准", exact=False).wait_for()
        await browser.close()

    if errors:
        raise AssertionError(f"browser runtime errors: {errors}")
    after = data("GET", f"/teams/{team['id']}/members", token=owner["token"])
    target_after = next(member for member in after if member["id"] == target_member["id"])
    assert target_after["status"] == "suspended", target_after
    print(f"PASS: {ENGINE} mobile approval page submitted and committed the real membership change; 8 widths had no horizontal overflow; member stayed active until approval")


asyncio.run(main())
