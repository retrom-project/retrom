"""PFB host and toolchain preflight checks."""

from __future__ import annotations

import json
import os
import platform
import shutil
import subprocess
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from .errors import PFBError


def validate_host(root: Path, *, require_browser: bool = True) -> dict[str, str]:
    if platform.system() != "Linux" or platform.machine() not in {"x86_64", "amd64"}:
        raise PFBError("PFB_PLATFORM_UNSUPPORTED")
    try:
        version = Path("/proc/version").read_text(encoding="utf-8").lower()
    except OSError:
        version = ""
    is_wsl2 = "microsoft" in version or "wsl" in version
    pfb_root = root / ".pfb"
    if not os.access(root, os.R_OK | os.W_OK | os.X_OK) or not os.access(pfb_root, os.R_OK | os.W_OK | os.X_OK):
        raise PFBError("PFB_WORKTREE_INVALID", "not-writable")
    for executable in ("git", "make", "python3", "docker"):
        if shutil.which(executable) is None:
            raise PFBError("PFB_TOOLCHAIN_MISSING", executable)
    _run(["docker", "info", "--format", "{{.ID}}"], "PFB_TOOLCHAIN_MISSING")
    compose = _run(["docker", "compose", "version", "--short"], "PFB_TOOLCHAIN_MISSING")
    context = _run(["docker", "context", "show"], "PFB_PLATFORM_UNSUPPORTED")
    if context.strip() != "default":
        raise PFBError("PFB_PLATFORM_UNSUPPORTED", "docker-context")
    if require_browser:
        chrome = Path(os.environ.get(
            "RETROM_CHROME_EXECUTABLE",
            root / ".cache/tools/retrom-chrome-for-testing",
        ))
        if not chrome.is_file() or not os.access(chrome, os.X_OK):
            raise PFBError("PFB_TOOLCHAIN_MISSING", "chrome")
        _validate_localhost_browser(chrome)
    return {
        "platform": "linux-x86_64-wsl2" if is_wsl2 else "linux-x86_64",
        "dockerContext": context.strip(),
        "compose": compose.strip(),
    }


def _run(arguments: list[str], code: str) -> str:
    result = subprocess.run(arguments, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise PFBError(code, arguments[0])
    return result.stdout


class _BrowserProbeHandler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:  # noqa: N802
        body = b"<!doctype html><title>Retrom PFB localhost probe</title>"
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cross-Origin-Opener-Policy", "same-origin")
        self.send_header("Cross-Origin-Embedder-Policy", "require-corp")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, _format: str, *_arguments: object) -> None:
        return


def _validate_localhost_browser(chrome: Path) -> None:
    server = ThreadingHTTPServer(("127.0.0.1", 0), _BrowserProbeHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        port = server.server_address[1]
        hosts = (
            "pfb-validation.localhost",
            "0198abcd-1234-7123-8abc-1234567890ab.rpg.pfb-validation.localhost",
        )
        node = chrome.parent / "node-v24.18.0-linux-x64/bin/node"
        probe_script = Path(__file__).with_name("browser-probe.mjs")
        if not node.is_file() or not os.access(node, os.X_OK):
            raise PFBError("PFB_TOOLCHAIN_MISSING", "node")
        result = subprocess.run([
            str(node), str(probe_script), str(chrome), str(port), *hosts,
        ], capture_output=True, text=True, check=False, timeout=45)
        try:
            probes = json.loads(result.stdout)
        except json.JSONDecodeError as exc:
            raise PFBError("PFB_LOCALHOST_NOT_TRUSTWORTHY") from exc
        expected = [
            {"host": host, "secure": True, "isolated": True, "sab": True}
            for host in hosts
        ]
        if result.returncode != 0 or probes != expected:
            raise PFBError("PFB_LOCALHOST_NOT_TRUSTWORTHY")
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise PFBError("PFB_LOCALHOST_NOT_TRUSTWORTHY") from exc
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
