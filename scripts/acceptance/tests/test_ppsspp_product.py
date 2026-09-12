"""Keep PSP screenshot classification regressions in the acceptance test suite."""
import subprocess
import unittest
from pathlib import Path


class PPSSPPObservationTests(unittest.TestCase):
    def test_range_transfer_evidence_is_strict(self):
        test_path = Path(__file__).with_name("ppsspp_range_observation_test.mjs")
        result = subprocess.run(
            ["node", "--test", str(test_path)], capture_output=True,
            text=True, timeout=10, check=False,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_title_menu_is_distinguished_from_mode_selection(self):
        test_path = Path(__file__).with_name("ppsspp_observation_test.mjs")
        result = subprocess.run(
            ["node", "--test", str(test_path)], capture_output=True,
            text=True, timeout=10, check=False,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
