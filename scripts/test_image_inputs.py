"""Reject archive escape and link attacks before materializing executable inputs."""
import io
from pathlib import Path
import tarfile
import tempfile
import unittest

from runtime_input_archive import unpack_tool


class ImageInputTests(unittest.TestCase):
    def test_cli_uses_one_repository_pin_and_transport_only_overrides(self):
        source = Path(__file__).with_name("prepare_image_inputs.py").read_text()
        self.assertIn('ROOT / "data/runtime-inputs.json"', source)
        self.assertNotIn('add_argument("--manifest"', source)
        self.assertIn('add_argument("--archive-root"', source)
        self.assertIn('add_argument("--base-url"', source)

    def archive(self, directory: Path, kind: str, name: str) -> Path:
        path = directory / "input.tar.gz"
        with tarfile.open(path, "w:gz") as output:
            root = tarfile.TarInfo("package")
            root.type = tarfile.DIRTYPE
            output.addfile(root)
            entry = tarfile.TarInfo(name)
            entry.mode = 0o755
            if kind == "link":
                entry.type = tarfile.SYMTYPE
                entry.linkname = "/tmp/outside"
                output.addfile(entry)
            else:
                entry.size = 4
                output.addfile(entry, io.BytesIO(b"test"))
        return path

    def test_executable_bytes_are_preserved_inside_one_root(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            target = root / "output"
            target.mkdir()
            unpack_tool(self.archive(root, "file", "package/scripts/detector"), target)
            file = target / "scripts/detector"
            self.assertEqual(file.read_bytes(), b"test")
            self.assertEqual(file.stat().st_mode & 0o777, 0o755)

    def test_traversal_absolute_paths_second_roots_and_symlinks_are_rejected(self):
        for kind, name in (("file", "package/../outside"), ("file", "/outside"),
                           ("file", "other/file"), ("link", "package/scripts/detector")):
            with self.subTest(kind=kind, name=name), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                target = root / "output"
                target.mkdir()
                with self.assertRaises(ValueError):
                    unpack_tool(self.archive(root, kind, name), target)


if __name__ == "__main__":
    unittest.main()
