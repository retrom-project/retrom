from __future__ import annotations

import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest import mock


MODULE_SPEC = importlib.util.spec_from_file_location(
    "fbalpha2012_dat",
    Path(__file__).with_name("fbalpha2012_dat.py"),
)
if MODULE_SPEC is None or MODULE_SPEC.loader is None:
    raise RuntimeError("unable to load FBA2012 DAT generator")
fbalpha2012_dat = importlib.util.module_from_spec(MODULE_SPEC)
MODULE_SPEC.loader.exec_module(fbalpha2012_dat)


class FBA2012DATGeneratorTests(unittest.TestCase):
    CPS1_CONFIG = {
        "archive_root": "root",
        "archive_size_bytes": 1,
        "archive_sha256": "0" * 64,
        "expected_machine_count": 227,
        "expected_normalized_external_parents": [],
    }
    CPS2_CONFIG = {
        **CPS1_CONFIG,
        "expected_machine_count": 284,
        "expected_normalized_external_parents": ["mmancp2u->megaman"],
    }

    def test_stats_lock_exact_machine_and_parent_normalization(self) -> None:
        report = fbalpha2012_dat.parse_stats(
            'RETROM_FBA2012_DAT_STATS={"machineCount":284,'
            '"normalizedExternalParents":["mmancp2u->megaman"],'
            '"explicitBiosMachineCount":0,"baseDependencyTargetCount":0}',
            self.CPS2_CONFIG,
        )
        self.assertEqual(report["machineCount"], 284)

        with self.assertRaisesRegex(
            fbalpha2012_dat.GenerationError,
            "FBA2012_DAT_STATS_MISMATCH",
        ):
            fbalpha2012_dat.parse_stats(
                'RETROM_FBA2012_DAT_STATS={"machineCount":284,'
                '"normalizedExternalParents":["other->parent"],'
                '"explicitBiosMachineCount":0,"baseDependencyTargetCount":0}',
                self.CPS2_CONFIG,
            )

    def test_safe_extract_rejects_links_and_traversal(self) -> None:
        for name, member in (
            ("link", tarfile.TarInfo("root/link")),
            ("traversal", tarfile.TarInfo("root/../escape")),
        ):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                archive_path = Path(directory) / "source.tar.gz"
                if name == "link":
                    member.type = tarfile.SYMTYPE
                    member.linkname = "/etc/passwd"
                else:
                    member.size = 1
                with tarfile.open(archive_path, "w:gz") as archive:
                    archive.addfile(member, io.BytesIO(b"x") if member.size else None)
                with self.assertRaisesRegex(
                    fbalpha2012_dat.GenerationError,
                    "FBA2012_DAT_SOURCE_ARCHIVE_UNSAFE",
                ):
                    fbalpha2012_dat.safe_extract(archive_path, Path(directory) / "out", "root")

    def test_materialize_requires_two_identical_clean_generations(self) -> None:
        report = {
            "machineCount": 227,
            "normalizedExternalParents": [],
            "explicitBiosMachineCount": 0,
            "baseDependencyTargetCount": 0,
        }
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "out.dat"
            with (
                mock.patch.object(fbalpha2012_dat, "verify_archive"),
                mock.patch.object(
                    fbalpha2012_dat,
                    "generate_once",
                    side_effect=[(b"first", report), (b"second", report)],
                ),
                self.assertRaisesRegex(
                    fbalpha2012_dat.GenerationError,
                    "FBA2012_DAT_NONDETERMINISTIC",
                ),
            ):
                fbalpha2012_dat.materialize(
                    Path(directory) / "source.tar.gz",
                    "fbalpha2012_cps1",
                    output,
                    self.CPS1_CONFIG,
                )
            self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
