#!/usr/bin/env python3
"""Docker-backed routing checks for the PFB gateway contract."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
IMAGE = (ROOT / "scripts/pfb/gateway/image.lock").read_text(encoding="utf-8").strip()
NODE_IMAGE = "node:24.18.0-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d"
PFB_ID = "gatewaytest-aaaaaaaaaaaa"
LAUNCH_ID = "0198abcd-1234-7123-8abc-1234567890ab"


def run(*arguments: str, capture: bool = True) -> str:
    result = subprocess.run(arguments, capture_output=capture, text=True, check=False)
    if result.returncode != 0:
        raise RuntimeError(f"PFB_GATEWAY_TEST_COMMAND_FAILED:{arguments[1]}:{result.stderr}")
    return result.stdout


def request(port: int, host: str, path: str, method: str = "GET") -> tuple[int, bytes, dict[str, str]]:
    value = urllib.request.Request(
        f"http://127.0.0.1:{port}{path}", method=method,
        headers={"Host": host, "X-Forwarded-Host": "spoof.example"},
    )
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):  # type: ignore[no-untyped-def]
            return None

    opener = urllib.request.build_opener(urllib.request.HTTPHandler(), NoRedirect())
    try:
        response = opener.open(value, timeout=5)
    except urllib.error.HTTPError as error:
        response = error
    return response.status, response.read(), {key.lower(): item for key, item in response.headers.items()}


def main() -> None:
    suffix = str(os.getpid())
    network = f"retrom-pfb-gateway-test-{suffix}"
    upstream = f"retrom-pfb-upstream-test-{suffix}"
    gateway = f"retrom-pfb-nginx-test-{suffix}"
    with tempfile.TemporaryDirectory(prefix="retrom-pfb-gateway-", dir="/tmp") as temporary:
        config = Path(temporary)
        shutil.copy2(ROOT / "scripts/pfb/gateway/nginx.conf", config / "nginx.conf")
        shutil.copy2(ROOT / "scripts/pfb/gateway/proxy.inc", config / "proxy.inc")
        (config / "selected.conf").write_text(
            f'set $pfb_selected_origin "http://{PFB_ID}.localhost:3000";\n', encoding="utf-8",
        )
        try:
            run("docker", "network", "create", network)
            script = (
                "const h=require('http');"
                "for(const [p,n] of [[3000,'next'],[8080,'go']])"
                "h.createServer((q,s)=>{s.setHeader('content-type','text/plain');s.end(n+':'+q.headers.host+':'+q.url)}).listen(p,'0.0.0.0');"
            )
            run(
                "docker", "run", "--detach", "--name", upstream, "--network", network,
                "--network-alias", f"retrom-pfb-{PFB_ID}",
                "--user", f"{os.getuid()}:{os.getgid()}", NODE_IMAGE, "node", "-e", script,
            )
            run(
                "docker", "run", "--detach", "--name", gateway, "--network", network,
                "--publish", "127.0.0.1::3000", "--volume", f"{config}:/etc/nginx/pfb:ro",
                "--user", f"{os.getuid()}:{os.getgid()}",
                "--tmpfs", f"/var/cache/nginx:uid={os.getuid()},gid={os.getgid()},mode=0700",
                "--tmpfs", f"/var/run:uid={os.getuid()},gid={os.getgid()},mode=0700",
                IMAGE, "nginx", "-c", "/etc/nginx/pfb/nginx.conf", "-g", "daemon off;",
            )
            inspected = json.loads(run("docker", "inspect", gateway))[0]
            assert inspected["Config"]["User"] == f"{os.getuid()}:{os.getgid()}"
            port = int(inspected["NetworkSettings"]["Ports"]["3000/tcp"][0]["HostPort"])
            deadline = time.monotonic() + 10
            while True:
                try:
                    status, _, _ = request(port, "localhost:3000", "/ready")
                    if status == 307:
                        break
                except OSError:
                    pass
                if time.monotonic() >= deadline:
                    raise RuntimeError("PFB_GATEWAY_TEST_NOT_READY")
                time.sleep(0.1)

            status, _, headers = request(port, "localhost:3000", "/path?value=1")
            assert status == 307 and headers["location"] == f"http://{PFB_ID}.localhost:3000/path?value=1"
            assert request(port, "localhost:3000", "/write", "POST")[0] == 409
            status, body, headers = request(port, f"{PFB_ID}.localhost:3000", "/")
            assert status == 200 and body.startswith(f"next:{PFB_ID}.localhost:3000".encode())
            assert headers["cross-origin-opener-policy"] == "same-origin"
            assert request(port, f"{PFB_ID}.localhost:3000", "/api/v1/home")[1].startswith(b"go:")
            runtime_host = f"{LAUNCH_ID}.rpg.{PFB_ID}.localhost:3000"
            status, body, headers = request(port, runtime_host, "/__retrom/entry")
            assert status == 200 and body.startswith(b"go:")
            assert headers["cross-origin-resource-policy"] == "cross-origin"
            assert request(port, runtime_host, "/not-runtime")[0] == 404
            assert request(port, runtime_host.replace("-7123-", "-7123-7"), "/__retrom/entry")[0] == 404
            legacy_runtime_host = f"{LAUNCH_ID}.{PFB_ID}.rpg.localhost:3000"
            status, body, headers = request(port, legacy_runtime_host, "/__retrom/entry")
            assert status == 200 and body.startswith(b"go:")
            assert headers["cross-origin-resource-policy"] == "cross-origin"
            assert request(port, legacy_runtime_host, "/not-runtime")[0] == 404
            assert request(port, f"{PFB_ID.upper()}.localhost:3000", "/")[0] == 404
            assert request(port, "unknown-aaaaaaaaaaaa.localhost:3000", "/")[0] == 503
        finally:
            subprocess.run(["docker", "rm", "--force", gateway, upstream], capture_output=True, check=False)
            subprocess.run(["docker", "network", "rm", network], capture_output=True, check=False)
    print("PFB gateway integration: ok")


if __name__ == "__main__":
    main()
