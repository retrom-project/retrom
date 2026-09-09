from __future__ import annotations

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import sys
import unittest

ROOT = Path(__file__).resolve().parents[3]


class MSXAcceptanceTests(unittest.TestCase):
    def test_registered_product_has_bounded_timeout(self):
        spec = importlib.util.spec_from_file_location("msx_acceptance", ROOT / "scripts/acceptance/run.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.assertIn("ACC-MSX-001", module.all_cases())
        self.assertIn("ACC-MSX-001", module.PRODUCT_CASES)
        self.assertEqual((300, ".cache/tools/node-v24.18.0-linux-x64/bin/node scripts/acceptance/msx_product.mjs"),
                         module.CASE_COMMANDS["ACC-MSX-001"])

    def test_missing_inputs_block_before_browser_or_game_read(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = {key: value for key, value in os.environ.items()
                           if not key.startswith(("RETROM_ACCEPTANCE_", "RETROM_MSX_"))}
            environment["RETROM_ACCEPTANCE_CASE_DIR"] = directory
            result = subprocess.run([ROOT / ".cache/tools/node-v24.18.0-linux-x64/bin/node",
                                     ROOT / "scripts/acceptance/msx_product.mjs"],
                                    cwd=ROOT, env=environment, capture_output=True, timeout=10)
            self.assertEqual(3, result.returncode, result.stderr)
            evidence = json.loads((Path(directory) / "msx-product.json").read_text())
            self.assertEqual("BLOCKED", evidence["status"])
            self.assertEqual("MSX_ACCEPTANCE_INPUT_REQUIRED", evidence["errorCode"])


class MSXFixtureTests(unittest.TestCase):
    def test_owned_cartridge_is_deterministic_and_does_not_replace_another_game(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "Owned.rom"
            command = [sys.executable, ROOT / "scripts/acceptance/build_msx_fixture.py", output]
            subprocess.run(command, check=True, capture_output=True, timeout=10)
            payload = output.read_bytes()
            self.assertEqual(16384, len(payload))
            self.assertTrue(bytes.fromhex("af cd c3 00") in payload, "CLS requires the caller to set Z")
            self.assertEqual("1c871557b2ab21aa32e2f00e319abb39545e5f98314419830c0dc3d514b45407",
                             hashlib.sha256(payload).hexdigest())
            subprocess.run(command, check=True, capture_output=True, timeout=10)
            self.assertEqual(payload, output.read_bytes())
            output.write_bytes(b"different game")
            result = subprocess.run(command, capture_output=True, timeout=10)
            self.assertNotEqual(0, result.returncode)
            self.assertEqual(b"different game", output.read_bytes())
