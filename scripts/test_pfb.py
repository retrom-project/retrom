#!/usr/bin/env python3
"""Focused, Docker-independent PFB contract tests."""

from __future__ import annotations

import json
import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from pfb.common import canonical_bytes
from pfb.errors import PFBError
from pfb.identity import app_origin, pfb_id, runtime_origin_template, validate_pfb_id, volume_name
from pfb.locks import _dependency_candidate_digest
from pfb.registry import empty_registry, locked_registry, register_spec, save_registry
from pfb.source_tree import source_tree_sha256, worktree_identity
from pfb.spec import HOST_MODE, validate_spec


class IdentityTests(unittest.TestCase):
    def test_vectors_are_stable(self) -> None:
        self.assertEqual(pfb_id("feat-ons-save"), "feat-ons-sa-d825862ff108")
        self.assertEqual(pfb_id("中文"), "pfb-72726d8818f6")
        self.assertEqual(pfb_id("___"), "pfb-bda251550bf0")
        identifier = pfb_id("hello")
        self.assertEqual(app_origin(identifier), "http://hello-2cf24dba5fb0.localhost:3000")
        self.assertEqual(runtime_origin_template(identifier), "http://{launchId}.hello-2cf24dba5fb0.rpg.localhost:3000")

    def test_invalid_names_and_ids_fail_closed(self) -> None:
        for name in ("", "with space", "\n", "x" * 129):
            with self.subTest(name=name), self.assertRaises(PFBError):
                pfb_id(name)
        for identifier in ("UPPER", "-bad", "bad-", "a" * 25, "a.b"):
            with self.subTest(identifier=identifier), self.assertRaises(PFBError):
                validate_pfb_id(identifier)

    def test_volume_requires_exact_digest(self) -> None:
        identifier = pfb_id("volume")
        self.assertTrue(volume_name(identifier, "data", "a" * 64).endswith("-data-aaaaaaaaaaaa"))
        with self.assertRaises(PFBError):
            volume_name(identifier, "data", "A" * 64)


class WorktreeTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(dir="/tmp")
        self.root = Path(self.temporary.name) / "repo"
        self.root.mkdir()
        self._git("init", "-b", "feat/test")
        self._git("config", "user.email", "test@example.invalid")
        self._git("config", "user.name", "PFB Test")
        (self.root / "tracked.txt").write_text("one\n", encoding="utf-8")
        self._git("add", "tracked.txt")
        self._git("commit", "-m", "initial")

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def _git(self, *arguments: str) -> None:
        subprocess.run(["git", "-C", str(self.root), *arguments], check=True, capture_output=True)

    def test_fingerprint_covers_untracked_bytes_and_modes(self) -> None:
        before = source_tree_sha256(self.root)
        (self.root / "untracked.txt").write_text("two\n", encoding="utf-8")
        after = source_tree_sha256(self.root)
        self.assertNotEqual(before, after)
        target = self.root / "untracked.txt"
        target.chmod(target.stat().st_mode | stat.S_IXUSR)
        self.assertNotEqual(after, source_tree_sha256(self.root))

    def test_detached_and_symlink_roots_are_rejected(self) -> None:
        commit = subprocess.run(["git", "-C", str(self.root), "rev-parse", "HEAD"], check=True, capture_output=True, text=True).stdout.strip()
        self._git("checkout", "--detach", commit)
        with self.assertRaisesRegex(PFBError, "PFB_WORKTREE_DETACHED"):
            worktree_identity(self.root)
        link = Path(self.temporary.name) / "link"
        link.symlink_to(self.root, target_is_directory=True)
        with self.assertRaisesRegex(PFBError, "PFB_WORKTREE_INVALID"):
            worktree_identity(link)

    def test_uninitialized_gitlink_has_a_finite_stable_fingerprint(self) -> None:
        nested = self.root / "vendor/core"
        nested.mkdir(parents=True)
        commit = subprocess.run(
            ["git", "-C", str(self.root), "rev-parse", "HEAD"],
            check=True, capture_output=True, text=True,
        ).stdout.strip()
        self._git("update-index", "--add", "--cacheinfo", f"160000,{commit},vendor/core")
        first = source_tree_sha256(self.root)
        second = source_tree_sha256(self.root)
        self.assertEqual(first, second)
        (nested / "untracked.txt").write_text("not an initialized worktree\n", encoding="utf-8")
        self.assertEqual(first, source_tree_sha256(self.root))


class SpecRegistryTests(unittest.TestCase):
    def test_unknown_spec_fields_are_rejected(self) -> None:
        value = {
            "schemaVersion": 1, "name": "test", "id": pfb_id("test"),
            "hostMode": HOST_MODE, "retrom": {}, "runtime": {"mode": "formal"},
            "cores": [], "unknown": True,
        }
        with self.assertRaises(PFBError):
            validate_spec(value)

    def test_registry_collision_never_overwrites(self) -> None:
        identifier = pfb_id("same")
        registry = empty_registry()
        register_spec(registry, {
            "id": identifier, "name": "same", "retrom": {"root": "/tmp/a"},
        })
        with self.assertRaisesRegex(PFBError, "PFB_ID_COLLISION"):
            register_spec(registry, {
                "id": identifier, "name": "different", "retrom": {"root": "/tmp/b"},
            })

    def test_registry_is_owner_only_and_canonical(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary, mock.patch.dict(os.environ, {"XDG_STATE_HOME": temporary}):
            with locked_registry() as (registry, path):
                save_registry(path, registry)
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            self.assertEqual(path.read_bytes(), canonical_bytes(empty_registry()) + b"\n")


class GatewayContractTests(unittest.TestCase):
    def test_gateway_has_only_loopback_publish_and_strict_hosts(self) -> None:
        root = Path(__file__).resolve().parent / "pfb/gateway"
        compose = (root / "compose.yaml").read_text(encoding="utf-8")
        app_compose = (root.parent / "compose.yaml").read_text(encoding="utf-8")
        nginx = (root / "nginx.conf").read_text(encoding="utf-8")
        self.assertIn('"127.0.0.1:3000:3000"', compose)
        self.assertNotIn('"3000:3000"', compose)
        self.assertIn("return 307", nginx)
        self.assertIn("return 409", nginx)
        self.assertIn("PFB_UPSTREAM_UNAVAILABLE", nginx)
        self.assertIn("rpg\\.localhost:3000", nginx)
        self.assertIn("log_format pfb", nginx)
        self.assertNotIn("access_log /dev/stdout combined", nginx)
        self.assertIn("proxy_read_timeout 28800s", nginx)
        self.assertNotIn("proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for", nginx)
        self.assertNotIn("container_name:", app_compose)
        self.assertIn("stop_grace_period: 45s", app_compose)


class DataGenerationTests(unittest.TestCase):
    def test_pure_application_source_changes_reuse_data_generation(self) -> None:
        base = {
            "formalDependencyManifestSha256": "a" * 64,
            "runtimeOverlaySha256": None,
            "candidateFilesSha256": "b" * 64,
            "retrom": {"sourceTreeSha256": "c" * 64},
        }
        changed = {**base, "retrom": {"sourceTreeSha256": "d" * 64}}
        self.assertEqual(_dependency_candidate_digest(base), _dependency_candidate_digest(changed))
        changed_candidate = {**base, "candidateFilesSha256": "e" * 64}
        self.assertNotEqual(_dependency_candidate_digest(base), _dependency_candidate_digest(changed_candidate))


if __name__ == "__main__":
    unittest.main()
