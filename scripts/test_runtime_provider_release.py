import copy
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock

from runtime_provider_release import (
    METADATA_MAX_BYTES, REPOSITORY, load_release_config, pin_provider_release,
    resolve_provider_release,
)
from runtime_providers import check_active_providers, prepare_production_providers
from test_runtime_providers import fixture_bundle


def release_fixture(root):
    providers, downloads = [], {}
    for provider_id in ("emulatorjs", "retrom-runtime"):
        archive, lock = fixture_bundle(root, provider_id=provider_id)
        providers.append({
            **{key: lock[key] for key in (
                "providerId", "providerVersion", "bundleSha256", "bundleSizeBytes",
                "manifestSha256", "fileCount", "unpackedSizeBytes",
            )},
            "archive": f"{provider_id}/{archive.name}",
            "bundleDirectory": f"{provider_id}/{provider_id}-1.0.0",
        })
        downloads[f"{REPOSITORY}/releases/download/v1.0.0/{archive.name}"] = archive.read_bytes()
    metadata = {
        "schemaVersion": 1, "providers": providers,
        "release": {"repository": REPOSITORY, "tag": "v1.0.0", "commit": "a" * 40},
    }
    downloads[f"{REPOSITORY}/releases/download/v1.0.0/provider-release.json"] = json.dumps(metadata).encode()
    return metadata, downloads


class RuntimeReleaseTests(unittest.TestCase):
    def test_tag_resolves_both_providers_and_supports_offline_installation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            metadata, downloads = release_fixture(root)
            fetch = Mock(side_effect=lambda url, maximum: downloads[url])
            config, cache = root / "release.json", root / "cache"
            pin_provider_release("v1.0.0", config, cache, fetch)
            self.assertEqual(json.loads(config.read_text()), {"tag": "v1.0.0"})
            self.assertEqual(fetch.call_count, 1)  # Pin fetches only the descriptor.
            active_path = root / "active.json"
            active = prepare_production_providers(config, cache, root / "installed", active_path, fetch)
            self.assertEqual(fetch.call_count, 3)
            self.assertEqual(active["release"], metadata["release"])
            self.assertEqual([item["providerId"] for item in active["providers"]], ["emulatorjs", "retrom-runtime"])
            check_active_providers(active_path, root / "installed", "production")
            changed = copy.deepcopy(active)
            changed["release"]["tag"] = "v0.46.0"
            active_path.write_text(json.dumps(changed))
            with self.assertRaisesRegex(ValueError, "RUNTIME_PROVIDER_ACTIVE_INVALID"):
                check_active_providers(active_path, root / "installed", "production")
            offline = Mock(side_effect=AssertionError("cache must support offline preparation"))
            second = prepare_production_providers(config, cache, root / "installed-b", root / "active-b.json", offline)
            self.assertEqual(active, second)
            offline.assert_not_called()

    def test_config_accepts_only_a_formal_tag(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "release.json"
            for value in (None, [], {}, {"tag": 123}, {"tag": "v1.0.0", "bundleSha256": "a" * 64},
                          {"tag": "../candidate"}, {"tag": "master"}, {"tag": "v1.0.0-dev"}, {"tag": "v01.0.0"}):
                with self.subTest(value=value):
                    path.write_text(json.dumps(value))
                    with self.assertRaisesRegex(ValueError, "PROVIDER_RELEASE_CONFIG_INVALID"):
                        load_release_config(path)

    def test_invalid_metadata_never_changes_the_pin_or_publishes_cache(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            original, _ = release_fixture(root)
            invalid = []
            for key, value in (("tag", "v2.0.0"), ("repository", "https://example.invalid/runtime"), ("commit", "dirty")):
                changed = copy.deepcopy(original)
                changed["release"][key] = value
                invalid.append(changed)
            for key, value in (("providerVersion", "2.0.0"), ("archive", "../escape.tar.gz"),
                               ("bundleSizeBytes", True), ("fileCount", 100001), ("bundleSha256", "bad")):
                changed = copy.deepcopy(original)
                changed["providers"][0][key] = value
                invalid.append(changed)
            invalid.extend([
                {**original, "providers": original["providers"][:1]},
                {**original, "providers": original["providers"] * 2},
                {**original, "sourceTreeSha256": "a" * 64},
                {**original, "schemaVersion": True},
            ])
            config = root / "release.json"
            config.write_text('{"tag":"v0.9.0"}\n')
            for value in invalid:
                with self.subTest(value=value):
                    with self.assertRaisesRegex(ValueError, "PROVIDER_RELEASE_METADATA_INVALID"):
                        pin_provider_release("v1.0.0", config, root / "cache", lambda *args: json.dumps(value).encode())
                    self.assertEqual(config.read_text(), '{"tag":"v0.9.0"}\n')
                    self.assertFalse((root / "cache/releases/v1.0.0/provider-release.json").exists())

    def test_bad_or_oversized_descriptor_fails_closed(self):
        with tempfile.TemporaryDirectory() as temporary:
            for contents in (b"broken", b"\xff", b"[]", b" " * (METADATA_MAX_BYTES + 1)):
                with self.subTest(contents=contents[:8]):
                    with self.assertRaisesRegex(ValueError, "PROVIDER_RELEASE_METADATA_INVALID"):
                        resolve_provider_release("v1.0.0", Path(temporary), lambda *args: contents)

    def test_download_failure_preserves_pin(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "release.json"
            config.write_text('{"tag":"v0.9.0"}')
            with self.assertRaisesRegex(ValueError, "PROVIDER_DOWNLOAD_INVALID"):
                pin_provider_release("v1.0.0", config, root / "cache", Mock(side_effect=ValueError("PROVIDER_DOWNLOAD_INVALID")))
            self.assertEqual(json.loads(config.read_text()), {"tag": "v0.9.0"})

    def test_tampered_descriptor_or_archive_cache_does_not_activate(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            metadata, downloads = release_fixture(root)
            config, cache = root / "release.json", root / "cache"
            fetch = lambda url, maximum: downloads[url]
            pin_provider_release("v1.0.0", config, cache, fetch)
            descriptor = cache / "releases/v1.0.0/provider-release.json"
            descriptor.write_text("{}")
            with self.assertRaisesRegex(ValueError, "PROVIDER_RELEASE_METADATA_INVALID"):
                prepare_production_providers(config, cache, root / "installed", root / "active.json", fetch)
            descriptor.write_text(json.dumps(metadata))
            first = metadata["providers"][0]
            archive = cache / first["providerId"] / (first["bundleSha256"] + ".tar.gz")
            archive.parent.mkdir(parents=True)
            archive.write_bytes(b"tampered")
            with self.assertRaisesRegex(ValueError, "PROVIDER_BUNDLE_DIGEST_INVALID"):
                prepare_production_providers(config, cache, root / "installed", root / "active.json", fetch)
            self.assertFalse((root / "active.json").exists())

    def test_failed_prepare_preserves_existing_production_activation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            metadata, downloads = release_fixture(root)
            config, cache, active_path = root / "release.json", root / "cache", root / "active.json"
            config.write_text('{"tag":"v1.0.0"}')
            fetch = lambda url, maximum: downloads[url]
            prepare_production_providers(config, cache, root / "installed", active_path, fetch)
            original_active = active_path.read_bytes()
            first = metadata["providers"][0]
            archive = cache / first["providerId"] / (first["bundleSha256"] + ".tar.gz")
            archive.write_bytes(b"corrupt")
            with self.assertRaisesRegex(ValueError, "PROVIDER_BUNDLE_DIGEST_INVALID"):
                prepare_production_providers(config, cache, root / "installed", active_path, fetch)
            self.assertEqual(active_path.read_bytes(), original_active)

    def test_corrupt_archive_download_does_not_publish_cache_or_active(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, downloads = release_fixture(root)
            config = root / "release.json"
            config.write_text('{"tag":"v1.0.0"}')
            def fetch(url, maximum):
                return downloads[url] if url.endswith(".json") else b"corrupt"
            with self.assertRaisesRegex(ValueError, "PROVIDER_BUNDLE_DIGEST_INVALID"):
                prepare_production_providers(config, root / "cache", root / "installed", root / "active.json", fetch)
            self.assertFalse((root / "active.json").exists())
            self.assertEqual(list((root / "cache").rglob("*.tar.gz")), [])


if __name__ == "__main__":
    unittest.main()
