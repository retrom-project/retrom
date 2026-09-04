#!/usr/bin/env python3
"""Focused, Docker-independent PFB contract tests."""

from __future__ import annotations

import json
import inspect
import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from pfb.common import canonical_bytes
from pfb.docker import (
    _runtime_git_mount_arguments,
    app_restart,
    app_up,
    import_provider_base,
    migrate_legacy_storage,
    workspace_paths,
)
from pfb.errors import PFBError
from pfb.identity import app_origin, pfb_id, runtime_origin_template, validate_pfb_id, volume_name
from pfb.registry import empty_registry, locked_registry, register_spec, save_registry
from pfb.source_tree import git_common_dir, source_tree_sha256, worktree_identity
from pfb.spec import HOST_MODE, validate_spec


class IdentityTests(unittest.TestCase):
    def test_vectors_are_stable(self) -> None:
        self.assertEqual(pfb_id("feat-ons-save"), "feat-ons-sa-d825862ff108")
        self.assertEqual(pfb_id("中文"), "pfb-72726d8818f6")
        self.assertEqual(pfb_id("___"), "pfb-bda251550bf0")
        identifier = pfb_id("hello")
        self.assertEqual(app_origin(identifier), "http://hello-2cf24dba5fb0.localhost:3000")
        self.assertEqual(runtime_origin_template(identifier), "http://{launchId}.rpg.hello-2cf24dba5fb0.localhost:3000")

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

    def test_linked_worktree_resolves_a_narrow_shared_git_directory(self) -> None:
        linked = Path(self.temporary.name) / "linked"
        self._git("worktree", "add", "-b", "feat/linked", str(linked), "HEAD")
        expected = (self.root / ".git").resolve(strict=True)
        self.assertEqual(git_common_dir(self.root), expected)
        self.assertEqual(git_common_dir(linked), expected)

    def test_runtime_candidate_mounts_external_worktree_git_metadata_read_only(self) -> None:
        linked = Path(self.temporary.name) / "runtime-worktree"
        self._git("worktree", "add", "-b", "feat/runtime", str(linked))
        pfb_root = Path(self.temporary.name) / "pfb-retrom"
        pfb_root.mkdir()
        common = (self.root / ".git").resolve()
        self.assertEqual(
            _runtime_git_mount_arguments(pfb_root, linked),
            ["--volume", f"{common}:{common}:ro"],
        )
        self.assertEqual(_runtime_git_mount_arguments(pfb_root, self.root), [])


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
        entrypoint = (root.parent / "entrypoint.sh").read_text(encoding="utf-8")
        docker_controller = (root.parent / "docker.py").read_text(encoding="utf-8")
        nginx = (root / "nginx.conf").read_text(encoding="utf-8")
        proxy = (root / "proxy.inc").read_text(encoding="utf-8")
        self.assertIn('"127.0.0.1:3000:3000"', compose)
        self.assertNotIn('"3000:3000"', compose)
        self.assertIn("return 307", nginx)
        self.assertIn("return 409", nginx)
        self.assertIn("PFB_UPSTREAM_UNAVAILABLE", nginx)
        self.assertIn("\\.rpg\\.", nginx)
        self.assertIn("\\.localhost:3000", nginx)
        self.assertIn("log_format pfb", nginx)
        self.assertNotIn("access_log /dev/stdout combined", nginx)
        self.assertIn("proxy_read_timeout 28800s", nginx)
        self.assertNotIn("proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for", nginx)
        self.assertNotIn("add_header Cross-Origin-Resource-Policy", proxy)
        self.assertEqual(nginx.count("add_header Cross-Origin-Resource-Policy same-origin always;"), 2)
        self.assertEqual(nginx.count("add_header Cross-Origin-Resource-Policy cross-origin always;"), 2)
        self.assertNotIn("container_name:", app_compose)
        self.assertIn('user: "${PFB_UID:?}:${PFB_GID:?}"', app_compose)
        self.assertIn('user: "${PFB_UID:?}:${PFB_GID:?}"', compose)
        self.assertIn("source: ${PFB_RETROM_GIT_COMMON_DIR:?}", app_compose)
        self.assertIn("source: ${PFB_RUNTIME_GIT_COMMON_DIR:?}", app_compose)
        self.assertGreaterEqual(app_compose.count("read_only: true"), 2)
        self.assertIn('http://{launchId}.rpg.${PFB_ID}.localhost:3000', entrypoint)
        self.assertNotIn('http://{launchId}.${PFB_ID}.rpg.localhost:3000', entrypoint)
        self.assertIn('label=com.docker.compose.oneoff=False', docker_controller)
        self.assertIn("stop_grace_period: 45s", app_compose)


class LightweightDevelopmentContractTests(unittest.TestCase):
    def test_daily_lifecycle_never_builds_release_candidates(self) -> None:
        root = Path(__file__).resolve().parent
        controller = (root / "pfb/cli.py").read_text(encoding="utf-8")
        docker_controller = (root / "pfb/docker.py").read_text(encoding="utf-8")
        compose = (root / "pfb/compose.yaml").read_text(encoding="utf-8")
        entrypoint = (root / "pfb/entrypoint.sh").read_text(encoding="utf-8")
        self.assertNotIn("run_runtime_candidate_builder", controller)
        self.assertNotIn("_checked_current_locks", controller)
        self.assertNotIn('"--build"', docker_controller)
        self.assertIn('"--no-build"', docker_controller)
        self.assertNotIn("build:", compose)
        self.assertNotIn("entrypoint-check", entrypoint)
        self.assertNotIn("make dev", entrypoint)
        self.assertIn("scripts/dev.sh", entrypoint)
        self.assertIn("pfb-provider-watch.mjs", entrypoint)
        self.assertFalse((root / "pfb/locks.py").exists())
        for operation in (app_up, app_restart):
            source = inspect.getsource(operation)
            self.assertNotIn("npm", source)
            self.assertNotIn("candidate", source)
            self.assertNotIn("docker build", source)

    def test_workspace_is_stable_and_bind_mounted(self) -> None:
        root = Path("/tmp/retrom-pfb-fixture")
        paths = workspace_paths(root)
        self.assertEqual(paths["root"], root / ".pfb/workspace")
        self.assertEqual(paths["data"], root / ".pfb/workspace/data")
        self.assertEqual(paths["providerDev"], root / ".pfb/workspace/providers/dev")
        compose = (Path(__file__).resolve().parent / "pfb/compose.yaml").read_text(encoding="utf-8")
        self.assertIn("source: ${PFB_WORKSPACE_ROOT:?}", compose)
        self.assertNotIn("type: volume", compose)
        self.assertNotIn("volumes:\n  pfb-", compose)
        second = workspace_paths(Path("/tmp/retrom-pfb-other"))
        self.assertNotEqual(paths["root"], second["root"])
        self.assertNotEqual(paths["data"], second["data"])

    def test_expensive_core_build_and_storage_mutations_are_explicit(self) -> None:
        makefile = (Path(__file__).resolve().parents[1] / "Makefile").read_text(encoding="utf-8")
        self.assertIn("pfb-core-build", makefile)
        self.assertIn("pfb-migrate-storage", makefile)
        self.assertIn("pfb-data-reset", makefile)
        self.assertIn("CORE is required", makefile)
        self.assertIn("CONFIRM", makefile)

    def test_legacy_migration_is_staged_and_preserves_source_volumes(self) -> None:
        source = inspect.getsource(migrate_legacy_storage)
        self.assertIn(".workspace-migrating", source)
        self.assertIn("_tree_fingerprint", source)
        self.assertIn("staging.rename(paths[\"root\"])", source)
        self.assertNotIn("volume rm", source)

    def test_provider_base_import_is_staged_verified_and_idempotent(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary:
            root = Path(temporary) / "retrom"
            source = Path(temporary) / "source"
            bundle = source / "installed/retrom-runtime/a"
            bundle.mkdir(parents=True)
            (bundle / "client.mjs").write_text("export default 1;\n", encoding="utf-8")
            active = {
                "providers": [{"installationPath": "retrom-runtime/a"}],
                "release": None,
                "schemaVersion": 1,
                "source": "candidate",
                "sourceTreeSha256": "a" * 64,
            }
            (source / "active.json").write_text(json.dumps(active), encoding="utf-8")
            root.mkdir()
            validations: list[tuple[Path, Path]] = []

            def validate(active_path: Path, installed_root: Path) -> dict[str, object]:
                validations.append((active_path, installed_root))
                return json.loads(active_path.read_text(encoding="utf-8"))

            first = import_provider_base(root, source, validate, lambda _current, _incoming: None)
            second = import_provider_base(root, source, validate, lambda _current, _incoming: None)
            destination = workspace_paths(root)
            self.assertEqual(first, second)
            self.assertEqual((destination["providerInstalled"] / "retrom-runtime/a/client.mjs").read_text(),
                             "export default 1;\n")
            self.assertEqual(json.loads(destination["providerActive"].read_text()), active)
            self.assertGreaterEqual(len(validations), 4)
            self.assertFalse((root / ".pfb/.providers-importing").exists())

    def test_provider_import_can_use_a_separate_current_base_validator(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary:
            root = Path(temporary) / "retrom"
            legacy_source = Path(temporary) / "legacy"
            incoming_source = Path(temporary) / "incoming"
            root.mkdir()

            def write_source(source: Path, marker: str) -> None:
                bundle = source / f"installed/retrom-runtime/{marker}"
                bundle.mkdir(parents=True)
                (bundle / "client.mjs").write_text(f"export default '{marker}';\n", encoding="utf-8")
                active = {
                    "format": marker,
                    "providers": [{"installationPath": f"retrom-runtime/{marker}"}],
                    "source": "candidate",
                }
                (source / "active.json").write_text(json.dumps(active), encoding="utf-8")

            write_source(legacy_source, "legacy")
            write_source(incoming_source, "current")

            def load(active_path: Path, _installed_root: Path) -> dict[str, object]:
                return json.loads(active_path.read_text(encoding="utf-8"))

            import_provider_base(root, legacy_source, load, lambda _current, _incoming: None)

            def strict(active_path: Path, installed_root: Path) -> dict[str, object]:
                value = load(active_path, installed_root)
                if value.get("format") != "current":
                    raise ValueError("not-current")
                return value

            current_validations = 0

            def validate_current(active_path: Path, installed_root: Path) -> dict[str, object]:
                nonlocal current_validations
                current_validations += 1
                return load(active_path, installed_root)

            imported = import_provider_base(
                root,
                incoming_source,
                strict,
                lambda current, incoming: self.assertEqual((current["format"], incoming["format"]),
                                                            ("legacy", "current")),
                validate_current=validate_current,
            )
            self.assertEqual(imported["providerCount"], 1)
            self.assertEqual(current_validations, 1)


if __name__ == "__main__":
    unittest.main()
