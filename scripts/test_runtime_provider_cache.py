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
from runtime_inputs import prepare, verify_prepared
from test_runtime_inputs import paired_fixture


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
    pin, _, manifest, downloads = paired_fixture(root)
    prepared = prepare(pin, root / "source", root / "cache", fetch=lambda url, maximum: downloads[url])
    return prepared, manifest


class ProviderCacheLocationTests(unittest.TestCase):
    def assert_make_cache(self, checkout, expected, *arguments):
        output = subprocess.run(
            ["make", "--no-print-directory", "--dry-run", "runtime-provider-prepare", *arguments],
            cwd=checkout, capture_output=True, text=True, check=True,
        ).stdout
        command = next(line for line in output.splitlines() if "prepare_image_inputs.py" in line)
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
    def test_docker_consumes_explicit_verified_provider_context(self):
        dockerfile = (REPOSITORY_ROOT / "Dockerfile").read_text()
        self.assertIn("COPY --from=providers /active.json", dockerfile)
        self.assertIn("COPY --from=providers /installed", dockerfile)
        self.assertIn("COPY --from=runtime-tool / /opt/retrom/runtime-tool/", dockerfile)
        self.assertNotIn("prepare_image_inputs.py", dockerfile)
        self.assertNotIn("COPY data/runtime-providers/release.json", dockerfile)

    def test_import_is_idempotent_and_supports_an_offline_consumer(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source, manifest = prepared_cache(root)
            original = {path.relative_to(source): path.read_bytes() for path in source.rglob("*") if path.is_file()}
            for _ in range(2):
                result = import_provider_cache(source, root / "shared")
                self.assertEqual((result["inputSets"], result["archives"]), (1, 3))
            offline = Mock(side_effect=AssertionError("must use imported downloads"))
            prepared = prepare(root / "runtime-inputs.json", root / "second", root / "shared", fetch=offline)
            verify_prepared(prepared, manifest)
            offline.assert_not_called()
            for relative, contents in original.items():
                self.assertEqual((source / relative).read_bytes(), contents)

    def test_invalid_source_archive_is_not_published(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source, _ = prepared_cache(root)
            next((source / "archives").glob("*.tar.gz")).write_bytes(b"corrupt")
            with self.assertRaisesRegex(ValueError, "ARTIFACT_DIGEST_INVALID"):
                import_provider_cache(source, root / "shared")
            self.assertEqual(list((root / "shared").rglob("*.tar.gz")), [])

    def test_cross_source_descriptor_is_not_published(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source, manifest = prepared_cache(root)
            manifest["sourceTreeSha256"] = "b" * 64
            (source / "runtime-inputs.json").write_text(json.dumps(manifest))
            with self.assertRaisesRegex(ValueError, "SOURCE_MISMATCH"):
                import_provider_cache(source, root / "shared")
            self.assertFalse((root / "shared").exists())


if __name__ == "__main__":
    unittest.main()
