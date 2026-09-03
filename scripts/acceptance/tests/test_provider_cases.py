import importlib.util
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location("acceptance_runner", ROOT / "scripts/acceptance/run.py")
runner = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(runner)


class ProviderAcceptanceRegistrationTests(unittest.TestCase):
    def test_all_provider_cases_have_a_single_focused_runner(self):
        expected = {f"ACC-PROVIDER-{number:03d}" for number in range(1, 9)}
        self.assertEqual(expected, runner.PROVIDER_CASES)
        for case_id in expected:
            self.assertIn(case_id, runner.CASE_COMMANDS)
            self.assertIn(f"scripts/acceptance/provider-case.sh {case_id}", runner.CASE_COMMANDS[case_id][1])
        self.assertTrue(expected.issubset(runner.all_cases()))

    def test_runtime_loading_evidence_tracks_provider_bundle_urls(self):
        source = (ROOT / "scripts/acceptance/runtime_loading_evidence.mjs").read_text(encoding="utf-8")
        self.assertIn('pathname.startsWith("/runtime/providers/")', source)
        self.assertNotIn('/runtime/' + 'retrom-runtime/', source)

    def test_existing_acceptance_cases_do_not_call_retired_player_adapter_tests(self):
        sources = [(ROOT / "scripts/acceptance/run.py").read_text(encoding="utf-8")]
        sources.extend(path.read_text(encoding="utf-8") for path in (ROOT / "scripts/acceptance").glob("*.sh"))
        combined = "\n".join(sources)
        self.assertNotIn("features/player/adapters/", combined)
        self.assertNotIn("features/player/multi-disc-restore.test.ts", combined)


if __name__ == "__main__":
    unittest.main()
