"""Published Provider files must be readable by a different runtime UID."""
import os
import stat
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock

from runtime_inputs import prepare
from test_runtime_inputs import paired_fixture


class RuntimeProviderPermissionsTest(unittest.TestCase):
    def test_fresh_and_cached_installations_allow_a_different_runtime_uid(self):
        previous_umask = os.umask(0o022)
        try:
            with tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                pin, _, _, downloads = paired_fixture(root)
                fetch = Mock(side_effect=lambda url, maximum: downloads[url])
                first = prepare(pin, root / "first", root / "cache", fetch=fetch)
                self.assertEqual(first.stat().st_mode & 0o777, 0o755)
                self.assert_public_readable(first / "providers/active.json", first / "providers/installed")
                offline = Mock(side_effect=AssertionError("must reuse verified cache"))
                second = prepare(pin, root / "second", root / "cache", fetch=offline)
                self.assertEqual((second / "providers/active.json").read_bytes(), (first / "providers/active.json").read_bytes())
                self.assert_public_readable(second / "providers/active.json", second / "providers/installed")
                offline.assert_not_called()
        finally:
            os.umask(previous_umask)

    def assert_public_readable(self, active, installed):
        for path in (active, installed, *installed.rglob("*")):
            with self.subTest(path=path.relative_to(active.parent)):
                mode = path.stat().st_mode
                self.assertTrue(mode & stat.S_IROTH, "runtime UID cannot read Provider input")
                if path.is_dir():
                    self.assertTrue(mode & stat.S_IXOTH, "runtime UID cannot traverse Provider directory")


if __name__ == "__main__":
    unittest.main()
