"""Smoke-test an already built image with authentication and a read-only filesystem."""
import argparse
import json
import os
import secrets
import subprocess
import time
import urllib.error
import urllib.request


def check(image):
    token = secrets.token_hex(32)
    env = {**os.environ, "GATEWAY_TOKEN": token}
    container = subprocess.check_output(
        ["docker", "run", "--detach", "--rm", "--read-only", "--cap-drop=ALL",
         "--security-opt=no-new-privileges", "--publish", "127.0.0.1::8080",
         "--env", "GATEWAY_TOKEN", image], env=env, text=True,
    ).strip()
    try:
        binding = subprocess.check_output(["docker", "port", container, "8080/tcp"], text=True).strip().splitlines()[0]
        base = "http://" + binding
        # These smoke tests target the local published port, never an external proxy.
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        session = ""

        def send(path, payload=None, authenticated=True, origin=None):
            headers = {}
            if authenticated:
                headers["Authorization"] = "Bearer " + token
            if origin:
                headers["Origin"] = origin
            data = None
            if payload is not None:
                data = json.dumps(payload).encode()
                headers.update({"Content-Type": "application/json", "Accept": "application/json, text/event-stream"})
                if session:
                    headers.update({"MCP-Session-Id": session, "MCP-Protocol-Version": "2025-06-18"})
            request = urllib.request.Request(base + path, data=data, headers=headers)
            try:
                response = opener.open(request, timeout=2)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                return response.status, response.headers, response.read()

        for _ in range(80):
            try:
                if send("/healthz")[0] == 200:
                    break
            except (urllib.error.URLError, TimeoutError):
                pass
            time.sleep(0.25)
        else:
            raise AssertionError("container did not become healthy")
        assert send("/readyz")[0] == 200
        assert send("/metrics", authenticated=False)[0] == 401
        assert send("/metrics", origin="https://untrusted.example")[0] == 403
        status, headers, raw = send("/mcp", {
            "jsonrpc": "2.0", "id": 1, "method": "initialize",
            "params": {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "container-check", "version": "1"}},
        })
        assert status == 200 and json.loads(raw)["result"]["protocolVersion"] == "2025-06-18"
        session = headers["MCP-Session-Id"]
        assert send("/mcp", {"jsonrpc": "2.0", "method": "notifications/initialized"})[0] == 202
        _, _, raw = send("/mcp", {"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
        assert [tool["name"] for tool in json.loads(raw)["result"]["tools"]] == ["health.check"]
        _, _, raw = send("/mcp", {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "health.check"}})
        result = json.loads(raw)["result"]
        assert not result.get("isError", False)
        assert json.loads(result["content"][0]["text"])["status"] == "ok"
        assert send("/metrics")[0] == 200
        user = subprocess.check_output(["docker", "inspect", "--format={{.Config.User}}", container], text=True).strip()
        assert user and user.split(":")[0] not in ("0", "root"), user
        print("PASS: non-root read-only container, liveness/readiness, authentication, Origin checks, MCP lifecycle, tool execution, metrics")
    except BaseException:
        subprocess.run(["docker", "logs", container], check=False)
        raise
    finally:
        subprocess.run(["docker", "stop", "--time", "15", container], check=False, stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--image", default="mcp-context-gateway:ci")
    check(parser.parse_args().image)
