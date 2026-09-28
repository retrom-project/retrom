"""Lock the complete original fixture consumed by content_preload_product.mjs."""
import hashlib
import runpy
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "testdata/public-roms/dos-cache"


class DOSCacheFixtureTest(unittest.TestCase):
    def test_complete_fixture_is_deterministic_and_locked(self):
        generated = runpy.run_path(str(ROOT / "build.py"))["archive"]()
        self.assertEqual((ROOT / "dos-cache.zip").read_bytes(), generated)
        self.assertEqual(hashlib.sha256(generated).hexdigest(),
                         "43268e30c73ba9845f5cef7d52fdcb3d62e59f0b32a53834919e7dd9346769cc")


if __name__ == "__main__":
    unittest.main()
