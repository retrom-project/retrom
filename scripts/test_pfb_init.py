"""PFB core extension keeps the established runtime and data identity."""

import argparse
import contextlib
import copy
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts.pfb import cli
from scripts.pfb.errors import PFBError


class PFBInitTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.spec = {
            "schemaVersion": 1, "name": "fixture", "id": "fixture-000000000000",
            "hostMode": "LOCALHOST_SHARED_GATEWAY_V1",
            "retrom": {"root": "/fixture/retrom", "branch": "feat/fixture"},
            "runtime": {"root": "/fixture/runtime", "branch": "feat/fixture", "mode": "branch"},
            "cores": [{"id": "easyrpg", "root": "/fixture/Player", "branch": "fix/fixture", "mode": "branch"}],
        }
        (self.root / ".pfb/workspace/data").mkdir(parents=True)
        (self.root / ".pfb/spec.json").write_text(json.dumps(self.spec), encoding="utf-8")
        (self.root / ".pfb/workspace/data/sentinel").write_text("keep", encoding="utf-8")
        self.args = argparse.Namespace(pfb="fixture", runtime_root=None, core_roots=None)

    def invoke(self, requested, running):
        with mock.patch.object(cli, "create_spec", return_value=requested), \
             mock.patch.object(cli, "app_container_running", return_value=running), \
             mock.patch.object(cli, "locked_registry", return_value=contextlib.nullcontext(({}, self.root / "registry"))), \
             mock.patch.object(cli, "register_spec"), mock.patch.object(cli, "save_registry"), \
             contextlib.redirect_stdout(io.StringIO()):
            return cli.command_init(self.root, self.args)

    def test_adds_an_explicit_core_only_when_stopped_without_touching_data(self):
        requested = copy.deepcopy(self.spec)
        requested["cores"].append({"id": "mkxp", "root": "/fixture/mkxp", "branch": "fix/fixture", "mode": "branch"})
        self.assertEqual(self.invoke(requested, False), 0)
        self.assertEqual(json.loads((self.root / ".pfb/spec.json").read_text()), requested)
        self.assertEqual((self.root / ".pfb/workspace/data/sentinel").read_text(), "keep")

    def test_rejects_running_extension_or_changes_to_existing_source_identity(self):
        candidates = []
        for field in ("retrom", "runtime"):
            value = copy.deepcopy(self.spec)
            value[field]["branch"] = "feat/other"
            candidates.append(value)
        value = copy.deepcopy(self.spec)
        value["cores"] = []
        candidates.append(value)
        value = copy.deepcopy(self.spec)
        value["cores"][0]["root"] = "/elsewhere/Player"
        candidates.append(value)
        for requested in candidates:
            with self.subTest(requested=requested), self.assertRaises(PFBError):
                self.invoke(requested, False)
        requested = copy.deepcopy(self.spec)
        requested["cores"].append({"id": "mkxp", "root": "/fixture/mkxp", "branch": "fix/fixture", "mode": "branch"})
        with self.assertRaises(PFBError):
            self.invoke(requested, True)
        self.assertEqual(json.loads((self.root / ".pfb/spec.json").read_text()), self.spec)


if __name__ == "__main__":
    unittest.main()
