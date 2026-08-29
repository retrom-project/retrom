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
DRIVER_PATH = ROOT / "scripts/acceptance/ons_product.mjs"
NODE_PATH = ROOT / ".cache/tools/node-v24.18.0-linux-x64/bin/node"


class ONSProductAcceptanceTests(unittest.TestCase):
    def test_formal_case_is_registered_and_documented(self) -> None:
        spec = importlib.util.spec_from_file_location("acceptance_run_ons", RUNNER_PATH)
        assert spec and spec.loader
        runner = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = runner
        spec.loader.exec_module(runner)
        self.assertEqual({"ACC-ONS-001"}, runner.ONS_CASES)
        self.assertIn("ACC-ONS-001", runner.CASE_COMMANDS)
        self.assertIn("ACC-ONS-001", runner.all_cases())

    def test_missing_private_input_is_blocked_before_browser_launch(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            environment = os.environ.copy()
            for name in (
                "RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME",
                "RETROM_ACCEPTANCE_PASSWORD", "RETROM_CHROME_EXECUTABLE",
                "RETROM_ONS_SMOKE_ARCHIVE",
            ):
                environment.pop(name, None)
            environment["RETROM_ACCEPTANCE_CASE_DIR"] = directory
            completed = subprocess.run(
                [NODE_PATH, DRIVER_PATH], cwd=ROOT, env=environment,
                check=False, capture_output=True, text=True, timeout=10,
            )
            self.assertEqual(3, completed.returncode)
            evidence = json.loads((Path(directory) / "ons-product.json").read_text())
            self.assertEqual("BLOCKED", evidence["status"])
            self.assertEqual("ONS_ACCEPTANCE_INPUT_REQUIRED", evidence["errorCode"])

    def test_driver_never_embeds_an_operator_game_path(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertNotIn("/data/game", contents)
        self.assertIn("RETROM_ONS_SMOKE_ARCHIVE", contents)

    def test_driver_accepts_http_only_for_local_acceptance_hosts(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn('import { isLocalAcceptanceHostname } from "./rpgmaker_url.mjs";', contents)
        self.assertIn('url.protocol !== "http:" || !isLocalAcceptanceHostname(url.hostname)', contents)

    def test_driver_requires_the_sdl_backing_buffer_and_centered_focused_surface(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        for contract in (
            "backingWidth", "backingHeight", "centerOffsetXPx", "centerOffsetYPx", "focused",
            "ONS_ACCEPTANCE_CANVAS_LAYOUT_INVALID", "data-ons-runtime-surface",
        ):
            self.assertIn(contract, contents)

    def test_terminal_duplicate_import_does_not_wait_until_timeout(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn('["REVIEW_PENDING", "COMPLETE", "COMPLETED"].includes(job.state)', contents)


if __name__ == "__main__":
    unittest.main()
