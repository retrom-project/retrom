"""Run the normal product browser against an isolated database and managed files."""
from __future__ import annotations

import importlib.util
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
import uuid

import psycopg
from psycopg import sql

ROOT = Path(__file__).resolve().parents[2]
ARTIFACTS = ROOT / ".artifacts/acceptance"
STARTUP_SECONDS = 300
REDIS_IMAGE = "redis:8.2.2-bookworm@sha256:4521b581dbddea6e7d81f8fe95ede93f5648aaa66a9dacd581611bf6fe7527bd"


def port() -> int:
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def wait_ready(url: str, processes: list[subprocess.Popen], deadline: float) -> None:
    while time.monotonic() < deadline:
        if any(process.poll() is not None for process in processes):
            raise RuntimeError("acceptance service exited; inspect the private service logs")
        try:
            with urllib.request.urlopen(url, timeout=2) as response:
                if response.status == 200:
                    return
        except (OSError, TimeoutError):
            pass
        time.sleep(0.5)
    raise RuntimeError("acceptance service did not become ready within 300 seconds")


def prepare() -> dict[str, str]:
    required = ("RETROM_TEST_DATABASE_URL", "RETROM_RUNTIME_TOOL_INPUT", "RETROM_PROVIDER_INPUT")
    if any(not os.environ.get(key) for key in required):
        raise ValueError("acceptance requires " + ", ".join(required))
    tool = Path(os.environ["RETROM_RUNTIME_TOOL_INPUT"]).resolve()
    providers = Path(os.environ["RETROM_PROVIDER_INPUT"]).resolve()
    node = str(Path(os.environ.get("NODE_HOME", ROOT / ".cache/tools/node-v24.18.0-linux-x64")) / "bin/node")
    if not Path(node).is_file():
        raise ValueError("the pinned Node.js toolchain must be prepared")
    environment = {**os.environ, "RETROM_NODE": node}
    digest = subprocess.check_output(
        [sys.executable, str(ROOT / "scripts/image_input_digest.py"), "--check"],
        cwd=ROOT, env=environment, text=True,
    ).strip()
    return {"tool": str(tool), "providers": str(providers), "node": node, "inputDigest": digest}


def source_fixture(directory: Path, marker: str) -> str:
    title = "Browser acceptance NES " + marker
    source = directory / "browser"
    source.mkdir(parents=True)
    spec = importlib.util.spec_from_file_location("acceptance_rom", ROOT / "testdata/public-roms/nes-smoke/build.py")
    assert spec and spec.loader
    builder = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = builder
    spec.loader.exec_module(builder)
    (source / "browser-acceptance.nes").write_bytes(builder.build_rom(("RETROM MIT " + marker).encode()))
    (source / "metadata.pegasus.txt").write_text(
        f"collection: Browser acceptance NES\nshortname: nes\nextensions: nes\n\ngame: {title}\nfile: browser-acceptance.nes\n",
        encoding="utf-8",
    )
    return title


def stop(process: subprocess.Popen) -> None:
    if process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=10)


def run_browser() -> int:
    inputs = prepare()
    marker = uuid.uuid4().hex[:16]
    evidence = ARTIFACTS / marker / "cases/acc-rf-browser"
    evidence.mkdir(parents=True)
    database = "retrom_browser_" + marker
    redis = "retrom-browser-" + marker
    redis_port, backend_port, web_port = port(), port(), port()
    web_origin = f"http://127.0.0.1:{web_port}"
    backend_origin = f"http://127.0.0.1:{backend_port}"
    processes: list[subprocess.Popen] = []
    logs = []
    created = False
    redis_created = False
    started = time.monotonic()
    status = "failed"
    temporary = tempfile.TemporaryDirectory(prefix="retrom-browser-")
    try:
        data = Path(temporary.name)
        web = data / "web"
        shutil.copytree(ROOT / "web", web,
                        ignore=shutil.ignore_patterns("node_modules", ".next*", "test-results", "playwright-report"))
        (web / "node_modules").symlink_to(ROOT / "web/node_modules", target_is_directory=True)
        title = source_fixture(data / "sources", marker)
        with psycopg.connect(os.environ["RETROM_TEST_DATABASE_URL"], autocommit=True) as connection:
            connection.execute(sql.SQL("CREATE DATABASE {}").format(sql.Identifier(database)))
        created = True
        database_url = psycopg.conninfo.make_conninfo(os.environ["RETROM_TEST_DATABASE_URL"], dbname=database)
        subprocess.run(
            ["docker", "run", "-d", "--name", redis, "--cpus", "1", "--memory", "128m",
             "-p", f"127.0.0.1:{redis_port}:6379", REDIS_IMAGE,
             "redis-server", "--save", "", "--appendonly", "no"],
            check=True, stdout=subprocess.DEVNULL,
        )
        redis_created = True
        binary = data / "retrom"
        subprocess.run(["go", "build", "-o", str(binary), "./cmd/retrom"], cwd=ROOT, check=True)
        node_home = str(Path(inputs["node"]).parents[1])
        environment = {**os.environ, "PATH": node_home + "/bin:" + os.environ["PATH"],
            "RETROM_MODE": "release", "RETROM_DATABASE_URL": database_url,
            "RETROM_PFB_ID": "browser-" + marker, "RETROM_REDIS_ADDR": f"127.0.0.1:{redis_port}",
            "RETROM_HTTP_ADDR": f"127.0.0.1:{backend_port}", "RETROM_PUBLIC_ORIGIN": web_origin,
            "RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE": f"http://{{runId}}.rpg.localhost:{backend_port}",
            "RETROM_DATA_DIR": str(data / "data"), "RETROM_RUNTIME_ROOT": inputs["tool"],
            "RETROM_PROVIDER_ROOT": inputs["providers"], "RETROM_NODE": inputs["node"],
            "RETROM_DEPENDENCY_ROOT": os.environ.get("RETROM_DEPENDENCY_ROOT", str(ROOT / "data")),
            "NEXT_BACKEND_ORIGIN": backend_origin, "NEXT_WEB_E2E": "true", "NEXT_DIST_DIR": ".next",
            "RETROM_WEB_ORIGIN": web_origin, "RETROM_BROWSER_SOURCE_PATH": str(data / "sources" / "browser"),
            "RETROM_BROWSER_GAME_TITLE": title,
            "RETROM_BROWSER_ADMIN_USER": "browser-admin", "RETROM_BROWSER_ADMIN_PASSWORD": "Browser-" + uuid.uuid4().hex}
        for command, name, cwd in [
            ([str(binary)], "backend", ROOT),
            ([inputs["node"], "node_modules/next/dist/bin/next", "dev", "--hostname", "127.0.0.1", "--port", str(web_port), "--webpack"], "web", web),
        ]:
            log = (evidence / (name + ".log")).open("wb")
            logs.append(log)
            processes.append(subprocess.Popen(command, cwd=cwd, env=environment, stdout=log, stderr=log, start_new_session=True))
        deadline = time.monotonic() + STARTUP_SECONDS
        wait_ready(backend_origin + "/health/ready", processes, deadline)
        wait_ready(web_origin + "/login", processes, deadline)
        command = [node_home + "/bin/npm", "run", "test:e2e", "--", "clean-refactor.spec.ts", "library-mobile.spec.ts", "--output", str(evidence / "test-results")]
        if os.environ.get("RETROM_E2E_GREP"):
            command += ["--grep", os.environ["RETROM_E2E_GREP"]]
        browser_environment = {**environment, "TMPDIR": "/tmp", "PLAYWRIGHT_HTML_OUTPUT_DIR": str(evidence / "playwright-report")}
        browser_environment.pop("RETROM_E2E_GREP", None)
        with (evidence / "browser.log").open("wb") as log:
            result = subprocess.run(command, cwd=web, env=browser_environment, stdout=log, stderr=log, timeout=3600)
        if result.returncode:
            raise RuntimeError("product browser failed; inspect browser.log and Playwright evidence")
        status = "passed"
        return 0
    finally:
        for process in reversed(processes):
            stop(process)
        for log in logs:
            log.close()
        if redis_created:
            subprocess.run(["docker", "rm", "-fv", redis], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
        if created:
            with psycopg.connect(os.environ["RETROM_TEST_DATABASE_URL"], autocommit=True) as connection:
                connection.execute(sql.SQL("DROP DATABASE {} WITH (FORCE)").format(sql.Identifier(database)))
        temporary.cleanup()
        (evidence / "result.json").write_text(json.dumps({"case":"ACC-RF-BROWSER", "status":status,
            "inputDigest":inputs["inputDigest"], "elapsedSeconds":round(time.monotonic()-started,3)}, indent=2) + "\n")


def main() -> int:
    action = sys.argv[1] if len(sys.argv) > 1 else ""
    if action == "prepare":
        print(json.dumps(prepare()))
        return 0
    if action == "case" and sys.argv[2:] == ["ACC-RF-BROWSER"]:
        return run_browser()
    if action == "report":
        results = sorted(ARTIFACTS.glob("*/cases/*/result.json"))
        if not results:
            raise ValueError("no acceptance results exist")
        print(json.dumps([json.loads(path.read_text()) for path in results], indent=2))
        return 0
    raise ValueError("expected prepare, case ACC-RF-BROWSER, or report; unsupported cases cannot pass")


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (ValueError, RuntimeError, OSError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1) from error
