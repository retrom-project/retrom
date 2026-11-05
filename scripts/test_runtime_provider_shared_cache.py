"""Shared downloads must remain isolated from each consumer's installation."""
import json
import multiprocessing
import tempfile
import unittest
from pathlib import Path

from runtime_inputs import prepare, verify_prepared
from test_runtime_inputs import paired_fixture, serve_release


def prepare_in_process(root, consumer, base, barrier):
    root = Path(root)
    barrier.wait(timeout=10)
    prepare(root / "runtime-inputs.json", root / consumer, root / "shared", base_url=base)


class SharedProviderCacheTests(unittest.TestCase):
    def test_concurrent_consumers_download_each_chunk_once(self):
        self.assert_concurrent_downloads()

    def test_concurrent_preparation_of_one_destination_publishes_one_complete_set(self):
        self.assert_concurrent_downloads(consumers=("same", "same"))

    def assert_concurrent_downloads(self, consumers=("first", "second")):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, archives, manifest = paired_fixture(root)
            # Identical chunks of different Providers share one content identity and one download.
            chunks = len({chunk["chunkId"] for provider in manifest["providers"]
                          for chunk in json.loads((archives / provider["chunkManifest"]["asset"]).read_text())["chunks"]})
            context = multiprocessing.get_context("spawn")
            barrier = context.Barrier(len(consumers))
            with serve_release(archives) as (base, counts):
                processes = [context.Process(target=prepare_in_process, args=(root, consumer, base, barrier))
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
            for consumer in set(consumers):
                prepared = next(path for path in (root / consumer).iterdir() if path.is_dir())
                verify_prepared(prepared, manifest)
            self.assertEqual(len([path for path in (root / "shared/chunks").iterdir() if len(path.name) == 64]), chunks)


if __name__ == "__main__":
    unittest.main()
