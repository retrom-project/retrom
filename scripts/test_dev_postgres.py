"""Ownership checks must never signal an unrelated process."""
import json
import os
from pathlib import Path
import tempfile
import unittest

from dev_postgres import Cluster, identity, write_json


class OwnershipTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.cluster = Cluster(self.root / "state", self.root / "files")
        self.cluster.root.mkdir(parents=True)

    def test_unowned_directory_is_not_adopted(self):
        self.cluster.data.mkdir()
        with self.assertRaisesRegex(RuntimeError, "unowned"):
            self.cluster.initialize()

    def test_foreign_repository_or_data_root_cannot_be_stopped(self):
        for owner in ({"repository": "foreign", "dataRoot": "foreign"},
                      {**self.cluster.owner, "dataRoot": "different"}):
            write_json(self.cluster.info_path, {"owner": owner})
            with self.assertRaisesRegex(RuntimeError, "owner"):
                self.cluster.stop()

    def test_forged_pid_and_matching_start_ticks_do_not_authorize_signal(self):
        write_json(self.cluster.info_path, {"owner": self.cluster.owner})
        write_json(self.cluster.process_path, {"pid": os.getpid(), "startTicks": identity(os.getpid())})
        with self.assertRaisesRegex(RuntimeError, "unverified"):
            self.cluster.stop()
        os.kill(os.getpid(), 0)

    def test_unregistered_postmaster_is_not_adopted(self):
        write_json(self.cluster.info_path, {"owner": self.cluster.owner})
        self.cluster.data.mkdir()
        (self.cluster.data / "postmaster.pid").write_text(str(os.getpid()))
        with self.assertRaisesRegex(RuntimeError, "without an owned"):
            self.cluster.stop()

    def test_credentials_and_registration_are_owner_only(self):
        write_json(self.cluster.info_path, {"secret": "private"})
        self.assertEqual(0o600, self.cluster.info_path.stat().st_mode & 0o777)
        self.assertEqual({"secret": "private"}, json.loads(self.cluster.info_path.read_text()))


class PreparationTests(unittest.TestCase):
    def setUp(self):
        import shutil
        from dev_postgres import ROOT
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "scripts").mkdir()
        for name in ("prepare-postgres.sh", "local_user.py"):
            shutil.copy2(ROOT / "scripts" / name, self.root / "scripts" / name)
        self.target = self.root / ".cache/tools/postgresql-18.3"
        self.fake_bin = self.root / "fake-bin"
        self.fake_bin.mkdir()
        fake_curl = self.fake_bin / "curl"
        fake_curl.write_text("#!/bin/sh\nexit 42\n")
        fake_curl.chmod(0o755)

    def run_prepare(self):
        import subprocess
        return subprocess.run(["bash", "scripts/prepare-postgres.sh"], cwd=self.root,
                              env={**os.environ, "PATH": str(self.fake_bin) + ":" + os.environ["PATH"]},
                              capture_output=True, text=True, timeout=10)

    def test_native_build_clears_outer_make_recursion_and_overrides(self):
        import subprocess
        commands = {
            "curl": "#!/bin/sh\nexit 0\n",
            "sha256sum": "#!/bin/sh\ncat >/dev/null\nexit 0\n",
            "tar": """#!/usr/bin/env python3
import pathlib, sys
root = pathlib.Path(sys.argv[sys.argv.index('-C') + 1]) / 'postgresql-18.3'
root.mkdir()
configure = root / 'configure'
configure.write_text('#!/bin/sh\\nexit 0\\n')
configure.chmod(0o755)
""",
            "make": """#!/usr/bin/env python3
import os, pathlib, sys
for key in ('MAKELEVEL', 'MAKEFLAGS', 'MFLAGS', 'MAKEOVERRIDES'):
    if key in os.environ: raise SystemExit('inherited ' + key)
if 'install' in sys.argv:
    stage = pathlib.Path(next(arg[8:] for arg in sys.argv if arg.startswith('DESTDIR=')))
    target = stage / os.environ['NATIVE_TEST_TARGET'].lstrip('/') / 'bin'
    target.mkdir(parents=True)
    for name in ('postgres', 'initdb', 'psql', 'pg_isready'):
        tool = target / name
        tool.write_text(f'#!/bin/sh\\necho "{name} (PostgreSQL) 18.3"\\n')
        tool.chmod(0o755)
""",
        }
        for name, source in commands.items():
            path = self.fake_bin / name
            path.write_text(source)
            path.chmod(0o755)
        result = subprocess.run(
            ["bash", "scripts/prepare-postgres.sh"], cwd=self.root,
            env={**os.environ, "PATH": str(self.fake_bin) + ":" + os.environ["PATH"],
                 "MAKELEVEL": "1", "MAKEFLAGS": "inherited", "MFLAGS": "inherited",
                 "MAKEOVERRIDES": "inherited", "NATIVE_TEST_TARGET": str(self.target)},
            capture_output=True, text=True, timeout=10,
        )
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertTrue((self.target / "bin/postgres").is_file())

    def test_verified_cache_never_downloads(self):
        (self.target / "bin").mkdir(parents=True)
        for name in ("postgres", "initdb", "psql", "pg_isready"):
            path = self.target / "bin" / name
            path.write_text(f"#!/bin/sh\necho '{name} (PostgreSQL) 18.3'\n")
            path.chmod(0o755)
        result = self.run_prepare()
        self.assertEqual(0, result.returncode, result.stderr)

    def test_failed_rebuild_preserves_existing_cache(self):
        self.target.mkdir(parents=True)
        marker = self.target / "marker"
        marker.write_text("preserved")
        result = self.run_prepare()
        self.assertEqual(42, result.returncode, result.stderr)
        self.assertEqual("preserved", marker.read_text())
        self.assertEqual([], list(self.target.parent.glob(".postgres-build-*")))


if __name__ == "__main__":
    unittest.main()
