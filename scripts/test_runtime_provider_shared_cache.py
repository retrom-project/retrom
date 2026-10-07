"""Shared downloads must remain isolated from each consumer's installation."""
import multiprocessing
import tempfile
import unittest
from pathlib import Path

from runtime_inputs import prepare, verify_prepared
from test_runtime_inputs import paired_fixture


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
    prepare(root / "runtime-inputs.json", root / consumer, root / "shared", fetch=fetch)


class SharedProviderCacheTests(unittest.TestCase):
    def test_concurrent_consumers_download_each_asset_once(self):
        self.assert_concurrent_downloads(tool_cached=False)

    def test_concurrent_consumers_download_archives_once_with_warm_tool(self):
        self.assert_concurrent_downloads(tool_cached=True)

    def test_concurrent_preparation_of_one_destination_publishes_one_complete_set(self):
        self.assert_concurrent_downloads(tool_cached=False, consumers=("same", "same"))

    def assert_concurrent_downloads(self, tool_cached, consumers=("first", "second")):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, _, manifest, downloads = paired_fixture(root)
            expected_downloads = dict(downloads)
            if tool_cached:
                record = manifest["tool"]
                archive = root / "shared/runtime-inputs" / record["sha256"] / record["archive"]
                archive.parent.mkdir(parents=True)
                url = next(url for url in downloads if "host-tool" in url)
                archive.write_bytes(expected_downloads.pop(url))
            context = multiprocessing.get_context("spawn")
            barrier, release = context.Barrier(2), context.Event()
            started, extra, calls = context.Event(), context.Event(), context.Value("i", 0)
            processes = [context.Process(
                target=prepare_in_process,
                args=(root, consumer, downloads, barrier, release, started, extra, calls),
            ) for consumer in consumers]
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
                for consumer in consumers:
                    prepared = next(path for path in (root / consumer).iterdir() if path.is_dir())
                    verify_prepared(prepared, manifest)
                self.assertEqual(len(list((root / "shared").rglob("*.tar.gz"))), 3)
            finally:
                release.set()
                for process in processes:
                    if process.pid is not None:
                        if process.is_alive():
                            process.terminate()
                        process.join(10)


if __name__ == "__main__":
    unittest.main()
