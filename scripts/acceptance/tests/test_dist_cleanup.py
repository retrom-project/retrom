"""Exercise the actual UI driver cleanup with a late Next cache writer."""
import os
from pathlib import Path
import re
import shlex
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]


class DistCleanupTests(unittest.TestCase):
    def run_cleanup(self, root, stubborn=False):
        web, state, tools = root / "web", root / "state", root / "tools"
        for directory in (web, state, tools):
            directory.mkdir()
        for name in ("next-env.d.ts", "tsconfig.json"):
            (web / name).write_text("modified")
            (state / name).write_text("original")
        (state / "server.log").write_text("fixture server stopped\n")
        dist = web / ".next-acceptance-fixture"
        (dist / "dev/cache").mkdir(parents=True)
        (dist / "dev/cache/original").write_text("cache")
        counter = root / "removals"
        counter.write_text("0")
        remover = tools / "rm"
        remover.write_text('''#!/usr/bin/env bash
for argument; do target="$argument"; done
if [[ "$target" != "$CLEANUP_TEST_DIST" ]]; then exec /bin/rm "$@"; fi
count=$(cat "$CLEANUP_TEST_COUNTER")
count=$((count + 1))
printf '%s' "$count" > "$CLEANUP_TEST_COUNTER"
if [[ "$CLEANUP_TEST_STUBBORN" == 1 ]]; then exit 1; fi
/bin/rm "$@"
if (( count == 1 )); then
  mkdir -p "$target/dev/cache"
  printf 'late cache flush' > "$target/dev/cache/late"
  printf 'rm: Directory not empty\n' >&2
  exit 1
fi
''')
        remover.chmod(0o755)
        script = (ROOT / "scripts/acceptance/ui-case.sh").read_text()
        cleanup = re.search(r"(?ms)^cleanup\(\) \{.*?^\}", script).group(0)
        command = "\n".join([
            "set -euo pipefail",
            "source " + shlex.quote(str(ROOT / "scripts/acceptance/dev-dist-cleanup.sh")),
            "repository_root=" + shlex.quote(str(root)),
            "temporary_root=" + shlex.quote(str(state)),
            'process_id=""', 'acceptance_dist_dir=".next-acceptance-fixture"',
            cleanup, "cleanup",
        ])
        environment = {**os.environ, "PATH": str(tools) + os.pathsep + os.environ["PATH"],
                       "CLEANUP_TEST_DIST": str(dist), "CLEANUP_TEST_COUNTER": str(counter),
                       "CLEANUP_TEST_STUBBORN": "1" if stubborn else "0"}
        result = subprocess.run(["bash", "-c", command], env=environment,
                                capture_output=True, text=True, timeout=12, check=False)
        return result, dist, counter

    def test_late_cache_flush_is_removed_and_generated_files_are_restored(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            result, dist, counter = self.run_cleanup(root)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertFalse(dist.exists())
            self.assertEqual(counter.read_text(), "2")
            self.assertFalse((root / "state").exists())
            for name in ("next-env.d.ts", "tsconfig.json"):
                self.assertEqual((root / "web" / name).read_text(), "original")

    def test_persistent_cleanup_failure_keeps_case_failed_and_preserves_logs(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            result, dist, counter = self.run_cleanup(root, stubborn=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertTrue(dist.exists())
            self.assertGreater(int(counter.read_text()), 1)
            self.assertIn("failed to remove acceptance build directory", result.stderr)
            logs = list((root / ".cache/retrom/acceptance").glob("*/server.log"))
            self.assertEqual(len(logs), 1)
            self.assertEqual(logs[0].read_text(), "fixture server stopped\n")


if __name__ == "__main__":
    unittest.main()
