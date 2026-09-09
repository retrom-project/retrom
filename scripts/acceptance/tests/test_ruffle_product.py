from __future__ import annotations

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]


class RuffleAcceptanceTests(unittest.TestCase):
    def test_registered_product_has_bounded_timeout(self):
        spec = importlib.util.spec_from_file_location("ruffle_acceptance", ROOT / "scripts/acceptance/run.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.assertIn("ACC-FLASH-001", module.all_cases())
        self.assertIn("ACC-FLASH-001", module.PRODUCT_CASES)
        self.assertEqual((300, ".cache/tools/node-v24.18.0-linux-x64/bin/node scripts/acceptance/ruffle_product.mjs"),
                         module.CASE_COMMANDS["ACC-FLASH-001"])

    def test_missing_inputs_block_before_browser_or_game_read(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = {key: value for key, value in os.environ.items()
                           if not key.startswith(("RETROM_ACCEPTANCE_", "RETROM_RUFFLE_"))}
            environment["RETROM_ACCEPTANCE_CASE_DIR"] = directory
            result = subprocess.run([ROOT / ".cache/tools/node-v24.18.0-linux-x64/bin/node",
                                     ROOT / "scripts/acceptance/ruffle_product.mjs"],
                                    cwd=ROOT, env=environment, capture_output=True, timeout=10)
            self.assertEqual(3, result.returncode, result.stderr)
            evidence = json.loads((Path(directory) / "ruffle-product.json").read_text())
            self.assertEqual("BLOCKED", evidence["status"])
            self.assertEqual("RUFFLE_ACCEPTANCE_INPUT_REQUIRED", evidence["errorCode"])
