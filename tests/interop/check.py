"""Exercise Python SDK client -> Go gateway -> Python SDK JSON and SSE servers."""
import argparse
import asyncio
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def serve(port, use_json):
    from mcp.server.fastmcp import FastMCP
    from pydantic import BaseModel

    class AddResult(BaseModel):
        sum: int

    server = FastMCP("interop-fixture", host="127.0.0.1", port=port, json_response=use_json)

    @server.tool()
    def add(a: int, b: int) -> AddResult:
        """Add two integers and return structured output."""
        return AddResult(sum=a + b)

    @server.tool()
    def fail() -> str:
        """Return a deliberate tool execution error."""
        raise ValueError("intentional fixture failure")

    server.run(transport="streamable-http")


def wait_ready(url, process, success_only=False):
    for _ in range(200):
        if process.poll() is not None:
            raise RuntimeError(f"process exited with {process.returncode}")
        try:
            with urllib.request.urlopen(url, timeout=0.2) as response:
                if not success_only or response.status == 200:
                    return
        except urllib.error.HTTPError:
            if not success_only:
                return
        except (urllib.error.URLError, TimeoutError):
            pass
        time.sleep(0.05)
    raise RuntimeError(f"timed out waiting for {url}")


async def check(url):
    from mcp import ClientSession
    from mcp.client.streamable_http import streamablehttp_client
    import httpx

    def local_client(**kwargs):
        # Fixtures are exclusively loopback; do not route them through environment proxies.
        return httpx.AsyncClient(**kwargs, trust_env=False)

    async with streamablehttp_client(url, httpx_client_factory=local_client) as (read, write, _):
        async with ClientSession(read, write) as client:
            initialized = await client.initialize()
            assert initialized.protocolVersion == "2025-06-18"
            tools = await client.list_tools()
            names = {tool.name for tool in tools.tools}
            assert names == {"health.check", "json.add", "json.fail", "sse.add", "sse.fail"}, names
            for name in ("json.add", "sse.add"):
                result = await client.call_tool(name, {"a": 6, "b": 8})
                assert not result.isError, result
                assert result.structuredContent == {"sum": 14}, result
            for name in ("json.fail", "sse.fail"):
                result = await client.call_tool(name)
                assert result.isError, result
                assert "intentional fixture failure" in result.content[0].text
            invalid = await client.call_tool("json.add", {"a": "bad", "b": 8})
            assert invalid.isError, invalid
            await client.send_ping()
    print("PASS: official Python SDK initialization, discovery, JSON/SSE calls, structured output, tool errors, validation, ping, session cleanup")


def main(binary):
    processes = []
    with tempfile.TemporaryDirectory(prefix="gateway-interop-") as directory:
        root = Path(directory)
        logs = []
        try:
            ports = [free_port() for _ in range(3)]
            for index, (name, json_response) in enumerate((("json", True), ("sse", False))):
                log = (root / f"{name}.log").open("w+")
                logs.append(log)
                command = [sys.executable, __file__, "--server", str(ports[index])]
                if json_response:
                    command.append("--json")
                process = subprocess.Popen(command, stdout=log, stderr=log)
                processes.append(process)
                wait_ready(f"http://127.0.0.1:{ports[index]}/mcp", process)
            config = {
                "listen": f"127.0.0.1:{ports[2]}",
                "servers": [{"name": name, "url": f"http://127.0.0.1:{port}/mcp"} for name, port in zip(("json", "sse"), ports)],
            }
            configuration = root / "config.json"
            configuration.write_text(json.dumps(config))
            log = (root / "gateway.log").open("w+")
            logs.append(log)
            process = subprocess.Popen([os.path.abspath(binary), "-config", str(configuration)], stdout=log, stderr=log)
            processes.append(process)
            wait_ready(f"http://127.0.0.1:{ports[2]}/readyz", process, success_only=True)
            asyncio.run(check(f"http://127.0.0.1:{ports[2]}/mcp"))
        except BaseException:
            for log in logs:
                log.flush()
                log.seek(0)
                print(log.read(), file=sys.stderr)
            raise
        finally:
            for process in reversed(processes):
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            for log in logs:
                log.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", default="./gateway")
    parser.add_argument("--server", type=int)
    parser.add_argument("--json", action="store_true")
    options = parser.parse_args()
    if options.server:
        serve(options.server, options.json)
    else:
        main(options.binary)
