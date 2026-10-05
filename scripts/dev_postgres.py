#!/usr/bin/env python3
"""Own one native PostgreSQL cluster per local dev state directory."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time

from local_user import require_local_user

ROOT = Path(__file__).resolve().parents[1]
BIN = ROOT / ".cache/tools/postgresql-18.3/bin"


def write_json(path: Path, value: dict) -> None:
    temporary = path.with_suffix(".tmp")
    with temporary.open("w", encoding="utf-8") as output:
        os.chmod(temporary, 0o600)
        json.dump(value, output)
    temporary.replace(path)


def identity(pid: int) -> str | None:
    try:
        fields = Path(f"/proc/{pid}/stat").read_text().rsplit(") ", 1)[1].split()
        return None if fields[0] == "Z" else fields[19]
    except FileNotFoundError:
        return None


class Cluster:
    def __init__(self, state: Path, data: Path):
        self.root = state.resolve() / "postgres"
        self.data = self.root / "data"
        self.owner = {"repository": str(ROOT), "dataRoot": str(data.resolve())}
        self.info_path = self.root / "owner.json"
        self.process_path = self.root / "process.json"
        self.child: subprocess.Popen | None = None

    def info(self) -> dict:
        value = json.loads(self.info_path.read_text())
        if value["owner"] != self.owner:
            raise RuntimeError("local PostgreSQL owner does not match this repository/data root")
        return value

    def environment(self, info: dict) -> dict:
        return {**{k: v for k, v in os.environ.items() if not k.startswith("PG")},
                "PGHOST": "127.0.0.1", "PGPORT": str(info["port"]),
                "PGUSER": "retrom", "PGPASSWORD": info["password"],
                "PGDATABASE": "postgres", "PGCONNECT_TIMEOUT": "2"}

    def sql(self, info: dict, query: str) -> str:
        return subprocess.check_output(
            [str(BIN / "psql"), "-XAt", "-v", "ON_ERROR_STOP=1", "-c", query],
            env=self.environment(info), text=True, stderr=subprocess.DEVNULL, timeout=5,
        ).strip()

    def initialize(self) -> dict:
        self.root.mkdir(parents=True, exist_ok=True, mode=0o700)
        if self.root.is_symlink() or self.data.is_symlink():
            raise RuntimeError("local PostgreSQL directories must not be symlinks")
        if self.info_path.exists():
            info = self.info()
        else:
            if self.data.exists():
                raise RuntimeError("refusing to adopt an unowned PostgreSQL data directory")
            info = {"owner": self.owner, "password": secrets.token_hex(32), "port": 0}
            write_json(self.info_path, info)
        if not self.data.exists():
            staging = Path(tempfile.mkdtemp(prefix="init-", dir=self.root))
            try:
                password = staging / "password"
                password.write_text(info["password"] + "\n")
                password.chmod(0o600)
                with (self.root / "init.log").open("a") as log:
                    subprocess.run(
                        [str(BIN / "initdb"), "-D", str(staging / "data"),
                         "--username=retrom", "--auth=scram-sha-256", "--encoding=UTF8",
                         "--locale=C", f"--pwfile={password}"],
                        stdout=log, stderr=subprocess.STDOUT, check=True,
                    )
                (staging / "data").replace(self.data)
            finally:
                shutil.rmtree(staging)
        if (self.data / "PG_VERSION").read_text().strip() != "18":
            raise RuntimeError("local PostgreSQL cluster must use major version 18")
        return info

    def process(self) -> dict | None:
        if not self.process_path.exists():
            if (self.data / "postmaster.pid").exists():
                raise RuntimeError("PostgreSQL PID exists without an owned process registration")
            return None
        value = json.loads(self.process_path.read_text())
        pid = value["pid"]
        actual = identity(pid)
        if actual is None:
            return None
        proc = Path(f"/proc/{pid}")
        command = (proc / "cmdline").read_bytes().split(b"\0")
        if (actual != value["startTicks"] or proc.stat().st_uid != os.getuid()
                or (proc / "exe").resolve() != (BIN / "postgres").resolve()
                or command[:3] != [os.fsencode(BIN / "postgres"), b"-D", os.fsencode(self.data)]):
            raise RuntimeError("refusing to signal an unverified PostgreSQL process")
        return value

    def start(self) -> str:
        info = self.initialize()
        if self.process() is not None:
            raise RuntimeError("owned PostgreSQL is already running")
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            info["port"] = listener.getsockname()[1]
        write_json(self.info_path, info)
        with (self.root / "server.log").open("a") as log:
            process = subprocess.Popen(
                [str(BIN / "postgres"), "-D", str(self.data), "-h", "127.0.0.1",
                 "-p", str(info["port"]), "-c", "unix_socket_directories=",
                 "-c", "max_connections=200"],
                stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                env=self.environment(info), start_new_session=True,
            )
        self.child = process
        try:
            write_json(self.process_path, {"pid": process.pid, "startTicks": identity(process.pid)})
            deadline = time.monotonic() + 30
            while True:
                if process.poll() is not None:
                    raise RuntimeError("local PostgreSQL exited; inspect dev-state/postgres/server.log")
                try:
                    self.sql(info, "SELECT 1")
                    break
                except subprocess.SubprocessError:
                    if time.monotonic() >= deadline:
                        raise RuntimeError("local PostgreSQL did not become ready within 30 seconds")
                    time.sleep(0.1)
            if not self.sql(info, "SELECT 1 FROM pg_database WHERE datname = 'retrom'"):
                self.sql(info, "CREATE DATABASE retrom")
        except BaseException:
            if process.poll() is None:
                process.send_signal(signal.SIGINT)
            process.wait(timeout=35)
            raise
        return f"postgres://retrom:{info['password']}@127.0.0.1:{info['port']}/retrom?sslmode=disable"

    def stop(self) -> None:
        if not self.info_path.exists():
            return
        self.info()
        process = self.process()
        if process is None:
            if self.child is not None:
                self.child.wait(timeout=1)
            self.process_path.unlink(missing_ok=True)
            return
        # A pidfd pins the verified process even if its PID is subsequently reused.
        try:
            descriptor = os.pidfd_open(process["pid"])
        except ProcessLookupError:
            return
        try:
            if self.process() is not None:
                signal.pidfd_send_signal(descriptor, signal.SIGINT)
        finally:
            os.close(descriptor)
        deadline = time.monotonic() + 35
        while identity(process["pid"]) == process["startTicks"]:
            if time.monotonic() >= deadline:
                raise RuntimeError("local PostgreSQL did not stop within 35 seconds")
            time.sleep(0.1)
        if self.child is not None:
            self.child.wait(timeout=1)
        self.process_path.unlink(missing_ok=True)

    def watch(self) -> None:
        self.info()
        process = self.process()
        while process is not None and identity(process["pid"]) == process["startTicks"]:
            time.sleep(1)
        raise RuntimeError("managed PostgreSQL exited")


def main() -> None:
    require_local_user()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("start", "stop", "watch"))
    parser.add_argument("--state", required=True, type=Path)
    parser.add_argument("--data", required=True, type=Path)
    args = parser.parse_args()
    cluster = Cluster(args.state, args.data)
    if args.action == "start":
        def interrupted(_signum: int, _frame: object) -> None:
            raise KeyboardInterrupt
        signal.signal(signal.SIGTERM, interrupted)
        print(cluster.start())
    elif args.action == "stop":
        cluster.stop()
    else:
        cluster.watch()


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        raise SystemExit(130)
    except (RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as exc:
        print(f"LOCAL_POSTGRES_ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc
