from __future__ import annotations

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]


class ScummvmAcceptanceTests(unittest.TestCase):
    def test_formal_case_is_registered_with_bounded_timeout(self):
        spec = importlib.util.spec_from_file_location("scummvm_acceptance", ROOT / "scripts/acceptance/run.py")
        module = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = module
        spec.loader.exec_module(module)
        self.assertEqual({"ACC-SCUMMVM-001"}, module.SCUMMVM_CASES)
        self.assertIn("ACC-SCUMMVM-001", module.all_cases())
        self.assertEqual(600, module.CASE_COMMANDS["ACC-SCUMMVM-001"][0])

    def test_retry_archives_the_scummvm_product_evidence(self):
        spec = importlib.util.spec_from_file_location("scummvm_archive", ROOT / "scripts/acceptance/run.py")
        module = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = module
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as directory:
            run = Path(directory)
            case = run / "cases/acc-scummvm-001"
            case.mkdir(parents=True)
            (run / "defects.json").write_text("[]")
            (case / "result.json").write_text(json.dumps({"caseId": "ACC-SCUMMVM-001", "status": "FAIL"}))
            payload = '{"caseId":"ACC-SCUMMVM-001","status":"FAIL","errorCode":"example"}'
            (case / "scummvm-product.json").write_text(payload)
            module.archive_previous(case)
            self.assertFalse((case / "scummvm-product.json").exists())
            self.assertEqual(payload, (case / "attempts/001/scummvm-product.json").read_text())

    def test_missing_inputs_block_before_browser_or_game_read(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = {key: value for key, value in os.environ.items()
                           if not key.startswith("RETROM_ACCEPTANCE_") and not key.startswith("RETROM_SCUMMVM_")}
            environment["RETROM_ACCEPTANCE_CASE_DIR"] = directory
            result = subprocess.run([ROOT / ".cache/tools/node-v24.18.0-linux-x64/bin/node",
                                     ROOT / "scripts/acceptance/scummvm_product.mjs"],
                                    cwd=ROOT, env=environment, capture_output=True, timeout=10)
            self.assertEqual(3, result.returncode, result.stderr)
            evidence = json.loads((Path(directory) / "scummvm-product.json").read_text())
            self.assertEqual("BLOCKED", evidence["status"])
            self.assertEqual("SCUMMVM_ACCEPTANCE_INPUT_REQUIRED", evidence["errorCode"])


if __name__ == "__main__":
    unittest.main()
