"""Native PostgreSQL and dev supervisor lifecycle integration tests (no Docker)."""
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "scripts"))
from dev_postgres import BIN, Cluster, identity


def wait_for(predicate, timeout=40):
    deadline = time.monotonic() + timeout
    while not predicate():
        if time.monotonic() > deadline:
            raise AssertionError("condition did not become true")
        time.sleep(0.05)


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="retrom-native-postgres-")
        self.root = Path(self.temporary.name)
        self.state = self.root / "state"
        self.data = self.root / "files"
        self.cluster = Cluster(self.state, self.data)
        self.children = []
        self.log = (self.root / "dev.log").open("w+")
        self.fake = self.root / "bin"
        self.fake.mkdir()
        for name in ("go", "npm"):
            path = self.fake / name
            path.write_text('''#!/usr/bin/env python3
import os, signal, sys, time
signal.signal(signal.SIGTERM, lambda *args: sys.exit(0))
while True: time.sleep(0.1)
''')
            path.chmod(0o755)
        self.env = {**os.environ, "PATH": str(self.fake) + ":" + os.environ["PATH"],
                    "RETROM_DEV_STATE_DIR": str(self.state), "RETROM_DATA_DIR": str(self.data),
                    "RETROM_DATABASE_URL": "", "RETROM_MODE": "test"}

    def tearDown(self):
        subprocess.run(["bash", "scripts/dev.sh", "--stop"], cwd=ROOT, env=self.env,
                       stdout=self.log, stderr=self.log, timeout=80, check=True)
        for child in self.children:
            if child.poll() is None:
                child.terminate()
            child.wait(timeout=40)
        self.cluster.stop()
        self.log.close()
        self.temporary.cleanup()

    def start_dev(self, external=""):
        previous = (self.state / "dev.pid").read_text() if (self.state / "dev.pid").exists() else ""
        process = subprocess.Popen(["bash", "scripts/dev.sh"], cwd=ROOT,
                                   env={**self.env, "RETROM_DATABASE_URL": external},
                                   stdout=self.log, stderr=self.log, start_new_session=True)
        self.children.append(process)
        def ready():
            if process.poll() is not None:
                raise AssertionError((self.root / "dev.log").read_text())
            path = self.state / "dev.pid"
            return path.exists() and path.read_text() != previous
        wait_for(ready)
        return process

    def registered_children(self):
        values = (self.state / "dev.pid").read_text().split()
        return [(int(values[3]), values[4]), (int(values[5]), values[6])]

    def assert_children_stopped(self, children):
        wait_for(lambda: all(identity(pid) != ticks for pid, ticks in children))

    def test_persistence_and_two_isolated_clusters(self):
        self.cluster.start()
        info = self.cluster.info()
        self.cluster.sql(info, "CREATE TABLE lifecycle_probe (value text)")
        self.cluster.sql(info, "INSERT INTO lifecycle_probe VALUES ('retained')")
        other = Cluster(self.root / "other-state", self.root / "other-files")
        try:
            other.start()
            self.assertNotEqual(info["port"], other.info()["port"])
            self.cluster.stop()
            self.assertEqual("1", other.sql(other.info(), "SELECT 1"))
            self.cluster.start()
            self.assertEqual("retained", self.cluster.sql(self.cluster.info(), "SELECT value FROM lifecycle_probe"))
        finally:
            other.stop()

    def test_bind_failure_leaves_no_postgres_process(self):
        import socket
        from unittest.mock import patch
        with socket.socket() as occupied:
            occupied.bind(("127.0.0.1", 0))
            occupied.listen()
            with patch("dev_postgres.socket.socket") as reserve:
                reserve.return_value.__enter__.return_value.getsockname.return_value = occupied.getsockname()
                with self.assertRaisesRegex(RuntimeError, "PostgreSQL exited"):
                    self.cluster.start()
            self.assertIsNone(self.cluster.process())

    def test_term_and_int_stop_database_and_apps(self):
        for sig in (signal.SIGTERM, signal.SIGINT):
            with self.subTest(sig=sig):
                process = self.start_dev()
                children = self.registered_children()
                self.assertIsNotNone(self.cluster.process())
                process.send_signal(sig)
                process.wait(timeout=40)
                self.assert_children_stopped(children)
                self.assertIsNone(self.cluster.process())
                self.assertTrue((self.cluster.data / "PG_VERSION").exists())

    def test_replacement_and_sigkill_recover_owned_database(self):
        original = self.start_dev()
        first_pg = self.cluster.process()
        replacement = self.start_dev()
        original.wait(timeout=40)
        self.assertNotEqual(first_pg["pid"], self.cluster.process()["pid"])
        children = self.registered_children()
        orphan_pg = self.cluster.process()
        replacement.kill()
        replacement.wait(timeout=5)
        self.assertEqual(orphan_pg, self.cluster.process())
        self.start_dev()
        self.assert_children_stopped(children)
        self.assertNotEqual(orphan_pg["pid"], self.cluster.process()["pid"])

    def test_children_do_not_inherit_takeover_lock(self):
        process = self.start_dev()
        # Wait for the supervisor to finish registration and close its own lock.
        wait_for(lambda: not Path(f"/proc/{process.pid}/fd/9").exists())
        children = Path(f"/proc/{process.pid}/task/{process.pid}/children").read_text().split()
        self.assertEqual(3, len(children))  # Go, Web and PostgreSQL watcher.
        lock = str(self.state / "dev-takeover.lock")
        for pid in children:
            for descriptor in Path(f"/proc/{pid}/fd").iterdir():
                self.assertNotEqual(lock, os.readlink(descriptor), f"child {pid} inherited the takeover lock")

    def test_database_failure_stops_application(self):
        process = self.start_dev()
        children = self.registered_children()
        self.cluster.stop()
        self.assertNotEqual(0, process.wait(timeout=40))
        self.assert_children_stopped(children)

    def test_make_dev_stop_stops_managed_database_and_apps(self):
        process = self.start_dev()
        children = self.registered_children()
        subprocess.run(
            ["make", "dev-stop", "RETROM_DEV_CONFIG=/dev/null",
             f"RETROM_DEV_STATE_DIR={self.state}", f"RETROM_DATA_DIR={self.data}"],
            cwd=ROOT, env=self.env, stdout=self.log, stderr=self.log, timeout=80, check=True,
        )
        process.wait(timeout=5)
        self.assert_children_stopped(children)
        self.assertIsNone(self.cluster.process())

    def test_application_failure_stops_database(self):
        process = self.start_dev()
        children = self.registered_children()
        os.kill(children[0][0], signal.SIGKILL)
        self.assertNotEqual(0, process.wait(timeout=40))
        self.assert_children_stopped(children)
        self.assertIsNone(self.cluster.process())

    def test_external_url_never_owns_or_stops_external_database(self):
        external = Cluster(self.root / "external-state", self.root / "external-files")
        try:
            url = external.start()
            process = self.start_dev(external=url)
            self.assertFalse(self.cluster.root.exists())
            process.terminate()
            process.wait(timeout=40)
            self.assertEqual("1", external.sql(external.info(), "SELECT 1"))
        finally:
            external.stop()


if __name__ == "__main__":
    unittest.main()
