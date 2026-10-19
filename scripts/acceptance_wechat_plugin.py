"""Exercise the real Tencent OpenClaw plugin QR bridge through TeamMind's API.

Requires an isolated migrated MySQL database, WECHAT_MODE=real, the authenticated
loopback Gateway, and TEAMMIND_API_BASE. It deliberately does not print QR data
or bind a WeChat account; a person must scan and confirm for that acceptance.
"""
import json
import os
import secrets
import urllib.error
import urllib.request


BASE = os.environ.get("TEAMMIND_API_BASE", "http://127.0.0.1:8188/api/v1").rstrip("/")
SUFFIX = secrets.token_hex(8)
PASSWORD = "Wechat-Bridge-" + secrets.token_urlsafe(18)


def call(path, method="GET", data=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = urllib.request.Request(
        BASE + path,
        data=None if data is None else json.dumps(data).encode(),
        headers=headers,
        method=method,
    )
    try:
        with urllib.request.urlopen(request, timeout=50) as response:
            return response.status, json.load(response)["data"]
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def require(condition, message):
    if not condition:
        raise AssertionError(message)
    print("PASS", message)


def main():
    email = f"wechat-plugin-{SUFFIX}@example.invalid"
    status, _ = call("/auth/register", "POST", {
        "email": email,
        "name": "WeChat Plugin Acceptance",
        "password": PASSWORD,
    })
    require(status == 201, "register isolated test account")
    status, login = call("/auth/login", "POST", {
        "email": email,
        "password": PASSWORD,
        "remember": False,
    })
    require(status == 200, "log in isolated test account")
    token = login["accessToken"]
    status, team = call("/teams", "POST", {
        "name": "WeChat Plugin " + SUFFIX,
        "description": "Isolated real plugin bridge acceptance",
        "timezone": "Asia/Shanghai",
    }, token)
    require(status == 201, "create isolated test team")
    status, channel = call("/me/channels/wechat", token=token)
    require(status == 200 and channel.get("mode") == "real", "API reports real Tencent plugin mode")
    status, session = call("/me/channels/wechat/sessions", "POST", {"teamId": team["id"]}, token)
    require(status == 201 and session.get("simulated") is False, "create non-simulated plugin QR session")
    require(bool(session.get("qrContent")), "Tencent plugin returned QR content to authenticated user")
    status, state = call(f"/me/channels/wechat/sessions/{session['id']}", token=token)
    require(status == 200 and state.get("status") in {"wait", "scaned", "need_verifycode"}, "poll real plugin QR status")
    status, cancelled = call(f"/me/channels/wechat/sessions/{session['id']}", "DELETE", token=token)
    require(status == 200 and cancelled.get("cancelled"), "cancel plugin login and abort Tencent long poll")
    status, state = call(f"/me/channels/wechat/sessions/{session['id']}", token=token)
    require(status == 200 and state.get("status") == "cancelled", "cancellation persists in TeamMind session state")
    status, channel = call("/me/channels/wechat", token=token)
    require(status == 200 and not channel.get("bindings"), "QR creation alone does not create a binding")
    print("INFO QR bytes were not printed; no person scanned or confirmed this session.")


if __name__ == "__main__":
    main()
