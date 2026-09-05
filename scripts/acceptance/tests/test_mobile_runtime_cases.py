from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location(
    "acceptance_runner_mobile_runtime", ROOT / "scripts/acceptance/run.py"
)
runner = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(runner)


class MobileRuntimeAcceptanceRegistrationTests(unittest.TestCase):
    def test_all_mobile_runtime_cases_have_focused_runners(self) -> None:
        expected = {"ACC-MOB-005", "ACC-MOB-006", "ACC-MOB-007"}
        for case_id in expected:
            self.assertIn(case_id, runner.CASE_COMMANDS)
            self.assertIn(f"scripts/acceptance/ui-case.sh {case_id}", runner.CASE_COMMANDS[case_id][1])
        self.assertIn("features/player/orientation.test.ts", runner.CASE_COMMANDS["ACC-MOB-005"][1])
        self.assertIn("features/player/player-chrome.test.tsx", runner.CASE_COMMANDS["ACC-MOB-006"][1])
        self.assertIn("scripts/acceptance/provider-case.sh ACC-PROVIDER-007", runner.CASE_COMMANDS["ACC-MOB-006"][1])

    def test_ui_driver_routes_mobile_runtime_cases_to_the_mobile_matrix(self) -> None:
        source = (ROOT / "scripts/acceptance/ui-case.sh").read_text(encoding="utf-8")
        self.assertIn("ACC-MOB-00[5-7]", source)
        self.assertIn('specification="e2e/mobile.spec.ts"', source)
        self.assertIn('case_id" =~ ^ACC-MOB-00[56]$', source)
        self.assertIn('case_id" == "ACC-MOB-007"', source)

    def test_mobile_accessibility_regressions_are_fixed_at_the_source(self) -> None:
        primitives = (ROOT / "web/styles/primitives.css").read_text(encoding="utf-8")
        imports_page = (ROOT / "web/app/admin/imports/page.tsx").read_text(encoding="utf-8")
        self.assertIn(".kpi p { margin: 24px 0 0; color: var(--muted);", primitives)
        self.assertIn('aria-labelledby="import-pipeline-title" tabIndex={0}', imports_page)
        self.assertIn('<h2 id="import-pipeline-title">', imports_page)


if __name__ == "__main__":
    unittest.main()
