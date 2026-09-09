"""Offline validation of branch-owned development dependencies."""
import copy
import json
import unittest
from pathlib import Path

from catalog import parse_manifest


ROOT = Path(__file__).resolve().parents[1]


class CatalogTests(unittest.TestCase):
    def setUp(self):
        self.data = json.loads((ROOT / "workspace/manifest.yaml").read_text())

    def parse(self):
        return parse_manifest(json.dumps(self.data))

    def test_runtime_release_repository_is_covered(self):
        repositories = self.parse()
        runtime = next(repo for repo in repositories if repo["id"] == "retrom-runtime")
        lock = json.loads((ROOT / "data/runtime-providers/retrom-runtime.lock.json").read_text())
        self.assertEqual(runtime["gitlink"].replace("git@github.com:", "https://github.com/").removesuffix(".git"), lock["repository"])

    def test_bootstrap_application_cannot_enter_dependency_catalog(self):
        self.data["repositories"][0]["role"] = "application"
        with self.assertRaisesRegex(ValueError, "bootstrap"):
            self.parse()

    def test_missing_dependency_and_cycle_are_rejected(self):
        self.data["repositories"][0]["dependsOn"].append("missing")
        with self.assertRaisesRegex(ValueError, "unknown dependencies"):
            self.parse()
        self.data["repositories"][0]["dependsOn"] = ["retrom-runtime"]
        with self.assertRaisesRegex(ValueError, "cycle"):
            self.parse()

    def test_duplicate_or_unconnected_repository_is_rejected(self):
        new = copy.deepcopy(self.data["repositories"][-1])
        self.data["repositories"].append(new)
        with self.assertRaisesRegex(ValueError, "duplicate"):
            self.parse()
        new.update(id="unused", role="core", path="project/retrom-core/unused", dependsOn=[])
        with self.assertRaisesRegex(ValueError, "not reachable"):
            self.parse()

    def test_path_escape_and_pfb_branch_are_rejected(self):
        runtime = self.data["repositories"][0]
        runtime["path"] = "project/../outside"
        with self.assertRaisesRegex(ValueError, "stay under"):
            self.parse()
        runtime["path"] = "project/retrom-runtime"
        runtime["defaultBranch"] = "codex/unfinished"
        with self.assertRaisesRegex(ValueError, "maintenance branch"):
            self.parse()

    def test_clone_options_are_typed(self):
        self.data["repositories"][0]["shallowClone"] = 1
        with self.assertRaisesRegex(ValueError, "shallowClone must be bool"):
            self.parse()


if __name__ == "__main__":
    unittest.main()
