"""Shared downloads must remain isolated from each consumer's installation."""
import json
import multiprocessing
import tempfile
import unittest
from pathlib import Path

from runtime_inputs import prepare, verify_prepared
from test_runtime_inputs import paired_fixture, serve_release


def prepare_in_process(root, consumer, base, tool, barrier):
    root = Path(root)

    def fetch(url, maximum):
        with (root / "downloads.log").open("a", encoding="utf-8") as output:
            output.write(url + "\n")
        return (root / "inputs" / tool).read_bytes()

    barrier.wait(timeout=10)
    prepare(root / "runtime-inputs.json", root / consumer, root / "shared", base_url=base, fetch=fetch)


class SharedProviderCacheTests(unittest.TestCase):
    def test_concurrent_consumers_download_each_asset_once(self):
        self.assert_concurrent_downloads(tool_cached=False)

    def test_concurrent_consumers_download_chunks_once_with_warm_tool(self):
        self.assert_concurrent_downloads(tool_cached=True)

    def test_concurrent_preparation_of_one_destination_publishes_one_complete_set(self):
        self.assert_concurrent_downloads(tool_cached=False, consumers=("same", "same"))

    def assert_concurrent_downloads(self, tool_cached, consumers=("first", "second")):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, archives, manifest, _ = paired_fixture(root)
            record = manifest["tool"]
            if tool_cached:
                archive = root / "shared/runtime-inputs" / record["sha256"] / record["archive"]
                archive.parent.mkdir(parents=True)
                archive.write_bytes((archives / record["archive"]).read_bytes())
            # Identical chunks of different Providers share one content identity and one download.
            chunks = len({chunk["chunkId"] for provider in manifest["providers"]
                          for chunk in json.loads((archives / provider["chunkManifest"]["asset"]).read_text())["chunks"]})
            context = multiprocessing.get_context("spawn")
            barrier = context.Barrier(len(consumers))
            with serve_release(archives) as (base, counts):
                processes = [context.Process(target=prepare_in_process,
                                             args=(root, consumer, base, record["archive"], barrier))
                             for consumer in consumers]
                try:
                    for process in processes:
                        process.start()
                    for process in processes:
                        process.join(60)
                        self.assertEqual(process.exitcode, 0)
                    # Chunks are content addressed in one locked cache: each is downloaded once.
                    self.assertEqual(counts["chunks"], chunks)
                finally:
                    for process in processes:
                        if process.is_alive():
                            process.terminate()
                            process.join(10)
            log = root / "downloads.log"
            requested = log.read_text(encoding="utf-8").splitlines() if log.exists() else []
            self.assertEqual(len(requested), 0 if tool_cached else 1)
            for consumer in set(consumers):
                prepared = next(path for path in (root / consumer).iterdir() if path.is_dir())
                verify_prepared(prepared, manifest)
            self.assertEqual(len([path for path in (root / "shared/chunks").iterdir() if len(path.name) == 64]), chunks)


if __name__ == "__main__":
    unittest.main()
