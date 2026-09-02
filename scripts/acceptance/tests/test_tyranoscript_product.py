from __future__ import annotations

import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
RUNNER_PATH = ROOT / "scripts/acceptance/run.py"
DRIVER_PATH = ROOT / "scripts/acceptance/tyranoscript_product.mjs"
NODE_PATH = ROOT / ".cache/tools/node-v24.18.0-linux-x64/bin/node"


class TyranoScriptProductAcceptanceTests(unittest.TestCase):
    def test_formal_case_is_registered(self) -> None:
        spec = importlib.util.spec_from_file_location("acceptance_run_tyranoscript", RUNNER_PATH)
        assert spec and spec.loader
        runner = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = runner
        spec.loader.exec_module(runner)
        self.assertEqual({"ACC-TYRANOSCRIPT-001"}, runner.TYRANOSCRIPT_CASES)
        self.assertIn("ACC-TYRANOSCRIPT-001", runner.CASE_COMMANDS)
        self.assertIn("ACC-TYRANOSCRIPT-001", runner.all_cases())

    def test_missing_operator_input_blocks_before_browser_launch(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            environment = os.environ.copy()
            for name in (
                "RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME",
                "RETROM_ACCEPTANCE_PASSWORD", "RETROM_CHROME_EXECUTABLE",
                "RETROM_TYRANOSCRIPT_SMOKE_ARCHIVE",
            ):
                environment.pop(name, None)
            environment["RETROM_ACCEPTANCE_CASE_DIR"] = directory
            completed = subprocess.run(
                [NODE_PATH, DRIVER_PATH], cwd=ROOT, env=environment,
                check=False, capture_output=True, text=True, timeout=10,
            )
            self.assertEqual(3, completed.returncode)
            evidence = json.loads((Path(directory) / "tyranoscript-product.json").read_text())
            self.assertEqual("BLOCKED", evidence["status"])
            self.assertEqual("TYRANOSCRIPT_ACCEPTANCE_INPUT_REQUIRED", evidence["errorCode"])

    def test_driver_keeps_operator_input_private_and_checks_product_boundaries(self) -> None:
        contract_path = DRIVER_PATH.with_name("tyranoscript_product_contract.mjs")
        contents = DRIVER_PATH.read_text(encoding="utf-8") + contract_path.read_text(encoding="utf-8")
        self.assertNotIn("/data/game", contents)
        for contract in (
            "RETROM_TYRANOSCRIPT_SMOKE_ARCHIVE", "TYRANOSCRIPT_PROJECT_V1",
            "/__retrom/tyranoscript/project/data/", "installVirtualStandardGamepad",
            "TYRANOSCRIPT_ACCEPTANCE_GAMEPAD_INPUT_UNOBSERVED", "checkpoint-b-created",
            "different-launch-restored-b", "post-restore-gamepad-input",
            "resumeAfterCheckpoint", 'name: "继续游戏"',
            'surface.screenshot({animations: "disabled"',
        ):
            self.assertIn(contract, contents)

    def test_runner_consumes_and_archives_the_tyranoscript_product_evidence(self) -> None:
        contents = RUNNER_PATH.read_text(encoding="utf-8")
        self.assertIn('elif case_id in TYRANOSCRIPT_CASES:', contents)
        self.assertIn('product_filename = "tyranoscript-product.json"', contents)
        self.assertIn('"tyranoscript-product.json",', contents)


if __name__ == "__main__":
    unittest.main()
