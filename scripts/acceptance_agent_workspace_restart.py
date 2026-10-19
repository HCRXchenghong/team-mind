"""Verify encrypted private workspace reads across real API process restarts.

Requires MYSQL_DSN for a disposable database already migrated through 000016.
The same fresh, test-only encryption key is deliberately kept for both API
processes; no production key is read or modified.
"""
import json
import os
import secrets
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
EMAIL = f"workspace-restart-{secrets.token_hex(6)}@teammind.test"
PASSWORD = "Restart-" + secrets.token_urlsafe(20)


def free_port():
    with socket.socket() as server:
        server.bind(("127.0.0.1", 0))
        return server.getsockname()[1]


def api_request(base, path, method="GET", data=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = urllib.request.Request(
        base + path,
        data=None if data is None else json.dumps(data).encode(),
        headers=headers,
        method=method,
    )
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status, json.load(response)["data"]
    except urllib.error.HTTPError as error:
        body = error.read().decode("utf-8", "replace")
        raise AssertionError(f"{method} {path} returned {error.code}: {body}") from error


def start_api(env, port, binary):
    process = subprocess.Popen(
        [str(binary)],
        env={**env, "API_ADDR": f"127.0.0.1:{port}", "APP_ENV": "development"},
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )
    base = f"http://127.0.0.1:{port}"
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise AssertionError(f"API exited during startup with status {process.returncode}")
        try:
            with urllib.request.urlopen(base + "/readyz", timeout=1) as response:
                if response.status == 200:
                    return process, base
        except Exception:
            time.sleep(0.2)
    process.send_signal(signal.SIGTERM)
    process.wait(timeout=10)
    raise AssertionError("API did not become ready within 45 seconds")


def stop_api(process):
    if process.poll() is None:
        process.send_signal(signal.SIGTERM)
    try:
        process.wait(timeout=12)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)
        raise AssertionError("API did not stop cleanly after SIGTERM")


def main():
    if not os.getenv("MYSQL_DSN"):
        raise SystemExit("MYSQL_DSN must point to a disposable database migrated through 000016")
    key = secrets.token_bytes(32)
    env = {
        **os.environ,
        "AGENT_WORKSPACE_ENCRYPTION_KEY": __import__("base64").b64encode(key).decode(),
    }
    with tempfile.TemporaryDirectory(prefix="teammind-workspace-restart-") as temporary:
        binary = Path(temporary) / "teammind-api"
        subprocess.run(
            ["go", "build", "-o", str(binary), "./cmd/server"],
            cwd=ROOT / "backend",
            env=env,
            check=True,
            stdout=subprocess.DEVNULL,
        )
        port = free_port()
        process = None
        token = None
        file_id = None
        user_id = None
        base = f"http://127.0.0.1:{port}"
        try:
            process, base = start_api(env, port, binary)
            api_request(base, "/api/v1/auth/register", "POST", {
                "email": EMAIL,
                "name": "Workspace Restart Acceptance",
                "password": PASSWORD,
            })
            _, session = api_request(base, "/api/v1/auth/login", "POST", {
                "email": EMAIL,
                "password": PASSWORD,
                "remember": False,
            })
            token = session["accessToken"]
            _, user = api_request(base, "/api/v1/me", token=token)
            user_id = user["id"]
            original = {
                "name": "restart-proof.md",
                "content": "Encrypted workspace survives an API process restart.\n",
            }
            _, created = api_request(
                base, "/api/v1/me/agent/workspace/files", "POST", original, token
            )
            file_id = created["id"]

            stop_api(process)
            process = None
            process, base = start_api(env, port, binary)
            _, restored = api_request(
                base, f"/api/v1/me/agent/workspace/files/{file_id}", token=token
            )
            assert restored["name"] == original["name"], restored
            assert restored["content"] == original["content"], restored
            print("PASS: encrypted MySQL workspace file was readable through the API after a clean process restart with the same key")
        finally:
            if process is not None:
                stop_api(process)
            if token and user_id:
                cleanup_process, cleanup_base = start_api(env, port, binary)
                try:
                    if file_id:
                        api_request(
                            cleanup_base,
                            f"/api/v1/me/agent/workspace/files/{file_id}",
                            "DELETE",
                            token=token,
                        )
                    api_request(
                        cleanup_base,
                        "/api/v1/me/account",
                        "POST",
                        {"currentPassword": PASSWORD, "confirmation": "删除账号"},
                        token,
                    )
                finally:
                    stop_api(cleanup_process)


if __name__ == "__main__":
    main()
