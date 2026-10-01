"""Verify cache ownership, explicit overrides, and offline cache consolidation."""
import copy
import json
import shutil
import shlex
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock

from runtime_provider_cache import default_cache_root
from runtime_provider_cache_import import import_provider_cache
from runtime_providers import prepare_production_providers
from test_runtime_provider_release import release_fixture


REPOSITORY_ROOT = Path(__file__).resolve().parent.parent


def git(root, *arguments):
    subprocess.run(["git", "-C", str(root), *arguments], check=True, capture_output=True)


def create_checkout(root):
    (root / "scripts").mkdir(parents=True)
    for name in ("runtime_provider_cache.py", "runtime_provider_io.py"):
        shutil.copyfile(REPOSITORY_ROOT / "scripts" / name, root / "scripts" / name)
    shutil.copyfile(REPOSITORY_ROOT / "Makefile", root / "Makefile")
    (root / "go.mod").write_text("module cache-test\n\ngo 1.26.5\n", encoding="utf-8")
    git(root, "init", "--quiet")
    git(root, "add", ".")
    git(root, "-c", "user.name=Cache Test", "-c", "user.email=cache@example.invalid",
        "commit", "--quiet", "-m", "cache fixture")


def prepared_cache(root):
    metadata, downloads = release_fixture(root)
    (root / "release.json").write_text('{"tag":"v1.0.0"}', encoding="utf-8")
    prepare_production_providers(
        root / "release.json", root / "source", root / "installed", root / "active.json",
        lambda url, maximum: downloads[url],
    )
    return metadata


class ProviderCacheLocationTests(unittest.TestCase):
    def assert_make_cache(self, checkout, expected, *arguments):
        output = subprocess.run(
            ["make", "--no-print-directory", "--dry-run", "runtime-provider-prepare", *arguments],
            cwd=checkout, capture_output=True, text=True, check=True,
        ).stdout
        command = next(line for line in output.splitlines() if "runtime_providers.py prepare" in line)
        tokens = shlex.split(command)
        self.assertEqual(tokens[tokens.index("--cache-root") + 1], str(expected))

    def test_baseline_and_linked_worktree_share_workspace_cache(self):
        with tempfile.TemporaryDirectory(prefix="provider cache ") as temporary:
            workspace = Path(temporary)
            (workspace / "manifest.yaml").write_text("{}", encoding="utf-8")
            baseline = workspace / "project/retrom"
            create_checkout(baseline)
            linked = workspace / ".worktree/feature/project/retrom"
            git(baseline, "worktree", "add", "--quiet", "--detach", str(linked))
            expected = workspace / ".cache/runtime-providers"
            for checkout in (baseline, linked):
                with self.subTest(checkout=checkout):
                    self.assertEqual(default_cache_root(checkout), expected)
                    self.assert_make_cache(checkout, expected)
                    custom = workspace / "custom cache"
                    self.assert_make_cache(checkout, custom, f"RETROM_PROVIDER_CACHE_ROOT={custom}")
            self.assertFalse(expected.exists(), "path discovery must not create cache state")

    def test_standalone_checkout_and_source_archive_use_local_cache(self):
        with tempfile.TemporaryDirectory() as temporary:
            checkout = Path(temporary) / "standalone"
            create_checkout(checkout)
            self.assertEqual(default_cache_root(checkout), checkout / ".cache/runtime-providers")
            self.assert_make_cache(checkout, checkout / ".cache/runtime-providers")
            archive = Path(temporary) / "archive"
            archive.mkdir()
            self.assertEqual(default_cache_root(archive), archive / ".cache/runtime-providers")


class ProviderCacheImportTests(unittest.TestCase):
    def test_docker_provider_stage_can_prepare_from_its_copied_scripts(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            prepared_cache(root)
            stage = root / "stage"
            provider_stage = (REPOSITORY_ROOT / "Dockerfile").read_text().split("AS providers\n", 1)[1]
            for line in provider_stage.split("\nFROM ", 1)[0].splitlines():
                if not line.startswith("COPY scripts/"):
                    continue
                tokens = shlex.split(line)
                if len(tokens) == 3 and tokens[0] == "COPY" and tokens[1].startswith("scripts/"):
                    destination = stage / tokens[2]
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(REPOSITORY_ROOT / tokens[1], destination)
            subprocess.run([
                sys.executable, str(stage / "scripts/runtime_providers.py"), "prepare",
                "--release-path", str(root / "release.json"), "--cache-root", str(root / "source"),
                "--installed-root", str(stage / "installed"), "--active-path", str(stage / "active.json"),
            ], cwd=stage, check=True, capture_output=True)
            self.assertTrue((stage / "active.json").is_file())

    def test_import_is_idempotent_and_supports_an_offline_consumer(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            metadata = prepared_cache(root)
            original = {path.relative_to(root / "source"): path.read_bytes()
                        for path in (root / "source").rglob("*") if path.is_file()}
            for _ in range(2):
                result = import_provider_cache(root / "source", root / "shared")
                self.assertEqual((result["releases"], result["archives"]), (1, 2))
            offline = Mock(side_effect=AssertionError("must use imported downloads"))
            active = prepare_production_providers(
                root / "release.json", root / "shared", root / "second/installed",
                root / "second/active.json", offline,
            )
            self.assertEqual(active["release"], metadata["release"])
            offline.assert_not_called()
            for relative, contents in original.items():
                self.assertEqual((root / "source" / relative).read_bytes(), contents)

    def test_invalid_source_archive_is_not_published(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            prepared_cache(root)
            for archive in (root / "source").glob("*/*.tar.gz"):
                archive.write_bytes(b"corrupt")
            with self.assertRaisesRegex(ValueError, "PROVIDER_BUNDLE_DIGEST_INVALID"):
                import_provider_cache(root / "source", root / "shared")
            self.assertEqual(list((root / "shared").glob("*/*.tar.gz")), [])

    def test_conflicting_release_metadata_is_not_replaced(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            metadata = prepared_cache(root)
            import_provider_cache(root / "source", root / "shared")
            descriptor = root / "shared/releases/v1.0.0/provider-release.json"
            original = descriptor.read_bytes()
            modified = copy.deepcopy(metadata)
            modified["release"]["commit"] = "b" * 40
            (root / "source/releases/v1.0.0/provider-release.json").write_text(json.dumps(modified))
            with self.assertRaisesRegex(ValueError, "PROVIDER_CACHE_METADATA_CONFLICT"):
                import_provider_cache(root / "source", root / "shared")
            self.assertEqual(descriptor.read_bytes(), original)


if __name__ == "__main__":
    unittest.main()
