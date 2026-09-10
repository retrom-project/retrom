from __future__ import annotations

import importlib.util
import subprocess
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
        # Execute only the pure argument-selection block, never server/data setup.
        start = source.index('specification="e2e/acceptance.spec.ts"')
        end = source.index("core_expansion_results='[]'", start)
        selection = source[start:end]
        for number in range(1, 8):
            case_id = f"ACC-MOB-{number:03d}"
            with self.subTest(case_id=case_id):
                result = subprocess.run(
                    ["bash", "-eu", "-c", 'case_id="$1"\n' + selection +
                     '\nprintf "%s\\0" "${playwright_args[@]}"', "mobile-routing", case_id],
                    check=True, capture_output=True, text=True, timeout=5,
                )
                expected = ["playwright", "test", "e2e/mobile.spec.ts"]
                if number == 7:
                    expected += ["e2e/acceptance.spec.ts", "e2e/immersive.spec.ts", "--grep",
                                 "ACC-MOB-007|ACC-UI-005|ACC-UI-006|ACC-UI-007|ACC-IMM-007",
                                 "--workers=1"]
                else:
                    expected += ["--grep", case_id, "--project=chrome-mobile"]
                self.assertEqual(result.stdout.removesuffix("\0").split("\0"), expected)

    def test_mobile_accessibility_regressions_are_fixed_at_the_source(self) -> None:
        primitives = (ROOT / "web/styles/primitives.css").read_text(encoding="utf-8")
        imports_page = (ROOT / "web/app/admin/imports/page.tsx").read_text(encoding="utf-8")
        self.assertIn(".kpi p { margin: 24px 0 0; color: var(--muted);", primitives)
        self.assertIn('aria-labelledby="import-pipeline-title" tabIndex={0}', imports_page)
        self.assertIn('<h2 id="import-pipeline-title">', imports_page)


if __name__ == "__main__":
    unittest.main()
