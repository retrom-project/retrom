import copy
from pathlib import Path
import tempfile
import unittest

from scripts.acceptance import rpgmaker_policy_fixture as fixture
from scripts.acceptance.rpgmaker_run_fixture import inventory, MARKER

ROOT = Path(__file__).resolve().parents[3] / "testdata/public-roms/rpgmaker-smoke"
RUN = "11111111-1111-4111-8111-111111111111"


class PolicyFixtureTests(unittest.TestCase):
    def test_each_run_preserves_all_five_owned_projects_and_gets_distinct_content(self):
        originals = {name: inventory(ROOT / name) for name in fixture.GENERATIONS}
        with tempfile.TemporaryDirectory() as directory:
            first, second = Path(directory) / "first", Path(directory) / "second"
            receipts = fixture.create(ROOT, first, RUN)
            fixture.validate_receipts(ROOT, receipts)
            fixture.create(ROOT, second, "22222222-2222-4222-8222-222222222222")
            for name, base in originals.items():
                actual = inventory(first / name)
                self.assertEqual({key: value for key, value in actual.items() if key != MARKER}, base)
                self.assertNotEqual(actual[MARKER], inventory(second / name)[MARKER])
                self.assertEqual(inventory(ROOT / name), base)
            with self.assertRaises(FileExistsError):
                fixture.create(ROOT, first, RUN)

    def test_receipt_rejects_missing_generation_changed_source_and_forged_marker(self):
        with tempfile.TemporaryDirectory() as directory:
            receipts = fixture.create(ROOT, Path(directory) / "input", RUN)
            for key in ("baseFilesSha256", "addedFile", "runId"):
                invalid = copy.deepcopy(receipts)
                invalid[0][key] = "forged"
                with self.assertRaises(ValueError):
                    fixture.validate_receipts(ROOT, invalid)
            with self.assertRaises(ValueError):
                fixture.validate_receipts(ROOT, receipts[:-1])
            with self.assertRaises(ValueError):
                fixture.validate_receipts(ROOT, [receipts[0]] * 5)
