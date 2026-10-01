"""Shared downloads must remain isolated from each consumer's installation."""
import multiprocessing
import tempfile
import unittest
from pathlib import Path

from runtime_providers import check_active_providers, prepare_production_providers
from test_runtime_provider_release import release_fixture


def prepare_in_process(root, consumer, downloads, barrier, release, started, extra, calls):
    root = Path(root)

    def fetch(url, maximum):
        with calls.get_lock():
            calls.value += 1
            if calls.value > 1:
                extra.set()
        with (root / "downloads.log").open("a", encoding="utf-8") as output:
            output.write(url + "\n")
        started.set()
        if not release.wait(10):
            raise AssertionError("download was not released")
        return downloads[url]

    barrier.wait(timeout=10)
    prepare_production_providers(
        root / "release.json", root / "shared", root / consumer / "installed",
        root / consumer / "active.json", fetch,
    )


class SharedProviderCacheTests(unittest.TestCase):
    def test_concurrent_consumers_download_each_asset_once(self):
        self.assert_concurrent_downloads(metadata_cached=False)

    def test_concurrent_consumers_download_archives_once_with_warm_metadata(self):
        self.assert_concurrent_downloads(metadata_cached=True)

    def assert_concurrent_downloads(self, metadata_cached):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            metadata, downloads = release_fixture(root)
            expected_downloads = dict(downloads)
            if metadata_cached:
                descriptor = root / "shared/releases/v1.0.0/provider-release.json"
                descriptor.parent.mkdir(parents=True)
                url = next(url for url in downloads if url.endswith("provider-release.json"))
                descriptor.write_bytes(expected_downloads.pop(url))
            (root / "release.json").write_text('{"tag":"v1.0.0"}', encoding="utf-8")
            context = multiprocessing.get_context("spawn")
            barrier, release = context.Barrier(2), context.Event()
            started, extra, calls = context.Event(), context.Event(), context.Value("i", 0)
            processes = [context.Process(
                target=prepare_in_process,
                args=(root, consumer, downloads, barrier, release, started, extra, calls),
            ) for consumer in ("first", "second")]
            try:
                for process in processes:
                    process.start()
                self.assertTrue(started.wait(10), "neither consumer started downloading")
                extra.wait(1)
                release.set()
                for process in processes:
                    process.join(10)
                    self.assertEqual(process.exitcode, 0)
                requested = (root / "downloads.log").read_text(encoding="utf-8").splitlines()
                self.assertCountEqual(requested, expected_downloads)
                for consumer in ("first", "second"):
                    active = check_active_providers(
                        root / consumer / "active.json", root / consumer / "installed", "production",
                    )
                    self.assertEqual(active["release"], metadata["release"])
                self.assertEqual(len(list((root / "shared").glob("*/*.tar.gz"))), 2)
            finally:
                release.set()
                for process in processes:
                    if process.pid is not None:
                        if process.is_alive():
                            process.terminate()
                        process.join(10)


if __name__ == "__main__":
    unittest.main()
