"""A proposed fixed upstream baseline can be tested before promotion."""

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

from scripts.pfb.cli import _validate_branch_policy
from scripts.pfb.errors import PFBError


class UpstreamSyncTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.git("init", "-b", "upstream")
        self.git("config", "user.name", "Test")
        self.git("config", "user.email", "test@example.invalid")
        self.git("commit", "--allow-empty", "-m", "upstream")
        self.baseline = self.git("rev-parse", "HEAD")
        self.name = "g" + self.baseline[:12]
        self.git("switch", "-c", "sync/upstream-" + self.name)
        self.fork = {"schemaVersion": 1, "defaultBranch": "retrom/" + self.name,
                     "upstreams": [{"refType": "COMMIT", "ref": self.baseline, "commit": self.baseline}]}
        self.spec = {"retrom": {"root": str(self.root)}, "runtime": {"mode": "release"},
                     "cores": [{"id": "test", "root": str(self.root)}]}

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.root), *args], stderr=subprocess.DEVNULL,
                                       text=True).strip()

    def validate(self):
        (self.root / "retrom-fork.json").write_text(json.dumps(self.fork))
        _validate_branch_policy(self.spec)

    def test_pinned_sync_can_run_before_maintenance_branch_exists(self):
        self.git("commit", "--allow-empty", "-m", "downstream patch")
        self.validate()

    def test_feature_branch_still_requires_maintenance_ancestry(self):
        self.git("switch", "-c", "feat/unreviewed")
        with self.assertRaises(PFBError):
            self.validate()

    def test_sync_rejects_floating_or_mismatched_baseline(self):
        for changes in ({"ref": "master"}, {"commit": "a" * 40}, {"refType": "BRANCH"}):
            with self.subTest(changes=changes):
                original = self.fork["upstreams"][0].copy()
                self.fork["upstreams"][0].update(changes)
                with self.assertRaises(PFBError):
                    self.validate()
                self.fork["upstreams"][0] = original

    def test_sync_rejects_merge_after_fixed_baseline(self):
        self.git("switch", "-c", "side")
        self.git("commit", "--allow-empty", "-m", "side")
        self.git("switch", "sync/upstream-" + self.name)
        self.git("commit", "--allow-empty", "-m", "patch")
        self.git("merge", "--no-ff", "side", "-m", "merge")
        with self.assertRaises(PFBError):
            self.validate()


if __name__ == "__main__":
    unittest.main()
