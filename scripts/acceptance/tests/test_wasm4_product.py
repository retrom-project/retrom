import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]


class Wasm4ProductTests(unittest.TestCase):
    def test_missing_input_blocks_before_game_or_browser_use(self):
        with tempfile.TemporaryDirectory() as directory:
            env = {key: value for key, value in os.environ.items()
                   if not key.startswith(("RETROM_ACCEPTANCE_", "RETROM_WASM4_"))}
            env["RETROM_ACCEPTANCE_CASE_DIR"] = directory
            result = subprocess.run([ROOT / ".cache/tools/node-v24.18.0-linux-x64/bin/node",
                                     ROOT / "scripts/acceptance/wasm4_product.mjs"], cwd=ROOT, env=env,
                                    capture_output=True, timeout=10)
            self.assertEqual(result.returncode, 3, result.stderr)
            value = json.loads((Path(directory) / "wasm4-product.json").read_text())
            self.assertEqual(value["status"], "BLOCKED")

    def test_case_is_registered_with_bounded_timeout(self):
        spec = importlib.util.spec_from_file_location("acceptance", ROOT / "scripts/acceptance/run.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.assertIn("ACC-WASM4-001", module.all_cases())
        self.assertIn("ACC-WASM4-001", module.PRODUCT_CASES)
        self.assertEqual(module.CASE_COMMANDS["ACC-WASM4-001"][0], 300)
