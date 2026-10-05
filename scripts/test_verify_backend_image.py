"""Image verification must own an isolated database and clean up on failure."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ImageVerificationTests(unittest.TestCase):
    def run_verification(self, failure=""):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            docker = root / "docker"
            docker.write_text('''#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
with open(os.environ["IMAGE_CHECK_CALLS"], "a") as output:
    output.write(json.dumps(args) + "\\n")
if args[0] == "inspect": print("false")
if args[0] == "exec":
    failure = os.environ["IMAGE_CHECK_FAILURE"]
    if failure == "database" and "pg_isready" in args: sys.exit(1)
    if failure == "backend" and "wget" in args: sys.exit(1)
''')
            docker.chmod(0o755)
            calls = root / "calls.jsonl"
            result = subprocess.run(
                ["bash", str(ROOT / "scripts/verify-backend-image.sh"), "retrom:fixture"],
                env={**os.environ, "PATH": str(root) + ":" + os.environ["PATH"],
                     "IMAGE_CHECK_CALLS": str(calls), "IMAGE_CHECK_FAILURE": failure},
                capture_output=True, text=True, timeout=10,
            )
            return result, [json.loads(line) for line in calls.read_text().splitlines()]

    def assert_cleanup(self, calls):
        self.assertEqual("rm", calls[-2][0])
        self.assertEqual("-fv", calls[-2][1])
        self.assertEqual(["network", "rm"], calls[-1][:2])
        self.assertEqual(calls[0][-1], calls[-1][-1])

    def test_success_waits_for_private_database_and_injects_url(self):
        result, calls = self.run_verification()
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(["network", "create", "--internal"], calls[0][:3])
        pg = next(call for call in calls if call[0] == "run")
        self.assertIn("--tmpfs", pg)
        self.assertEqual("1000:1000", pg[pg.index("--user") + 1])
        ready = next(i for i, call in enumerate(calls) if "pg_isready" in call)
        create = next(i for i, call in enumerate(calls) if call[0] == "create")
        self.assertLess(ready, create)
        app = calls[create]
        self.assertEqual("1000:1000", app[app.index("--user") + 1])
        self.assertEqual(pg[pg.index("--network") + 1], app[app.index("--network") + 1])
        self.assertTrue(any(arg.startswith("RETROM_DATABASE_URL=postgres://") for arg in app))
        self.assertNotIn("-p", app + pg)
        self.assertNotIn("POSTGRES_PASSWORD", result.stdout + result.stderr)
        self.assert_cleanup(calls)

    def test_database_and_backend_failure_clean_all_owned_resources(self):
        for failure in ("database", "backend"):
            with self.subTest(failure=failure):
                result, calls = self.run_verification(failure)
                self.assertEqual(1, result.returncode, result.stderr)
                self.assert_cleanup(calls)
                if failure == "database":
                    self.assertFalse(any(call[0] == "create" for call in calls))


if __name__ == "__main__":
    unittest.main()
