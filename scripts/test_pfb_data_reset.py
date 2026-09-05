"""Destructive development reset preserves recoverable state and immutable caches."""

import argparse
import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts.pfb import cli
from scripts.pfb.docker import ensure_workspace
from scripts.pfb.errors import PFBError


class PFBDataResetTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / "retrom"
        self.paths = ensure_workspace(self.root)
        self.spec = {"id": "fixture-000000000000"}
        self.args = argparse.Namespace(pfb="fixture", confirm=self.spec["id"], source_root=None)
        self.sentinels = {
            "data": "database", "providerDev": "dev-provider.json", "home": "npm-cache",
            "devState": "state", "providerInstalled": "cached.wasm", "webNode": "dependency",
            "runtimeNode": "dependency", "next": "cache", "go": "cache",
        }
        for key, name in self.sentinels.items():
            (self.paths[key] / name).write_text(key, encoding="utf-8")
        self.paths["providerActive"].write_text("old incompatible manifest", encoding="utf-8")
        self.source = Path(self.temporary.name) / "source"
        bundle = self.source / "installed/retrom-runtime/new"
        bundle.mkdir(parents=True)
        (bundle / "client.mjs").write_text("new module", encoding="utf-8")
        self.active = {"source": "candidate", "providers": [{"installationPath": "retrom-runtime/new"}]}
        (self.source / "active.json").write_text(json.dumps(self.active), encoding="utf-8")

    def validate(self, active_path, installed_root):
        value = json.loads(active_path.read_text(encoding="utf-8"))
        self.assertEqual((installed_root / "retrom-runtime/new/client.mjs").read_text(), "new module")
        return value

    def invoke(self, *, running=False, validator=None):
        output = io.StringIO()
        with mock.patch.object(cli, "_named_spec", return_value=self.spec), \
             mock.patch.object(cli, "app_container_running", return_value=running), \
             mock.patch("scripts.runtime_providers.check_active_providers", side_effect=validator or
                        (lambda active, installed, _source: self.validate(active, installed))), \
             contextlib.redirect_stdout(output):
            cli.command_data_reset(self.root, self.args)
        return json.loads(output.getvalue())

    def assert_original(self):
        for key, name in self.sentinels.items():
            self.assertEqual((self.paths[key] / name).read_text(), key)
        self.assertEqual(self.paths["providerActive"].read_text(), "old incompatible manifest")

    def test_plain_reset_preserves_provider_and_all_caches(self):
        result = self.invoke()
        self.assertEqual(list(self.paths["data"].iterdir()), [])
        self.assertEqual((Path(result["backup"]) / "data/database").read_text(), "data")
        for key, name in self.sentinels.items():
            if key != "data":
                self.assertEqual((self.paths[key] / name).read_text(), key)
        self.assertEqual(self.paths["providerActive"].read_text(), "old incompatible manifest")

    def test_explicit_base_replacement_archives_old_state_without_reading_old_schema(self):
        self.args.source_root = self.source
        result = self.invoke()
        backup = Path(result["backup"])
        self.assertEqual((backup / "data/database").read_text(), "data")
        self.assertEqual((backup / "providers/active.json").read_text(), "old incompatible manifest")
        self.assertEqual((backup / "providers/dev/dev-provider.json").read_text(), "providerDev")
        self.assertEqual(json.loads(self.paths["providerActive"].read_text()), self.active)
        self.assertEqual(list(self.paths["data"].iterdir()), [])
        self.assertEqual(list(self.paths["providerDev"].iterdir()), [])
        for key, name in self.sentinels.items():
            if key not in {"data", "providerDev"}:
                self.assertEqual((self.paths[key] / name).read_text(), key)

    def test_invalid_source_fails_before_archiving_anything(self):
        self.args.source_root = self.source
        with self.assertRaisesRegex(PFBError, "PFB_PROVIDER_BASE_INVALID"):
            self.invoke(validator=mock.Mock(side_effect=ValueError("invalid fixture manifest")))
        self.assert_original()
        self.assertFalse((self.paths["root"] / "reset-backups").exists())

    def test_failure_after_copy_restores_database_and_provider_selection(self):
        self.args.source_root = self.source

        def fail_after_publish(active, installed, _source):
            if active == self.paths["providerActive"]:
                raise ValueError("final validation failed")
            return self.validate(active, installed)

        with self.assertRaisesRegex(PFBError, "PFB_PROVIDER_BASE_INVALID"):
            self.invoke(validator=fail_after_publish)
        self.assert_original()

    def test_running_or_wrong_confirmation_cannot_reset(self):
        for running, confirmation in ((True, self.spec["id"]), (False, "another-pfb")):
            with self.subTest(running=running, confirmation=confirmation), self.assertRaises(PFBError):
                self.args.confirm = confirmation
                self.invoke(running=running)
            self.assert_original()

    def test_in_workspace_source_or_symlinked_data_is_rejected(self):
        self.args.source_root = self.paths["providers"]
        with self.assertRaises(PFBError):
            self.invoke()
        self.assert_original()
        self.args.source_root = None
        displaced = self.paths["data"].with_name("external-data")
        self.paths["data"].rename(displaced)
        self.paths["data"].symlink_to(displaced, target_is_directory=True)
        with self.assertRaises(PFBError):
            self.invoke()
        self.assertEqual((displaced / "database").read_text(), "data")

    def test_parser_accepts_optional_base_only_for_explicit_reset(self):
        args = cli.parser().parse_args([
            "data-reset", "--root", str(self.root), "--pfb", "fixture",
            "--confirm", self.spec["id"], "--source-root", str(self.source),
        ])
        self.assertEqual(args.source_root, self.source)


if __name__ == "__main__":
    unittest.main()
