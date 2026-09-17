"""BBKRPG admission must retain missing-input and product-evidence gates."""
import json
import os
import runpy
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


class BBKRPGProductTests(unittest.TestCase):
    def test_case_uses_product_evidence(self):
        runner = runpy.run_path(str(ROOT / "scripts/acceptance/run.py"))
        self.assertIn("ACC-BBKRPG-001", runner["all_cases"]())
        self.assertIn("ACC-BBKRPG-001", runner["PRODUCT_CASES"])
        self.assertEqual(runner["CASE_COMMANDS"]["ACC-BBKRPG-001"][0], 600)

    def test_missing_input_is_blocked_without_starting_a_browser(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = os.environ.copy()
            environment.pop("RETROM_BBKRPG_ROM", None)
            environment["RETROM_ACCEPTANCE_CASE_DIR"] = directory
            result = subprocess.run(
                [ROOT / ".cache/tools/node-v24.18.0-linux-x64/bin/node",
                 ROOT / "scripts/acceptance/bbkrpg_product.mjs"],
                env=environment, cwd=ROOT, capture_output=True, timeout=10, check=False,
            )
            self.assertEqual(result.returncode, 3, result.stderr.decode())
            evidence = json.loads((Path(directory) / "bbkrpg-product.json").read_text())
            self.assertEqual(evidence["status"], "BLOCKED")
            self.assertEqual(evidence["errorCode"], "BBKRPG_ACCEPTANCE_INPUT_REQUIRED")
