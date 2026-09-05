import importlib.util
import re
import shlex
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location("acceptance_runner", ROOT / "scripts/acceptance/run.py")
runner = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(runner)


class ProviderAcceptanceRegistrationTests(unittest.TestCase):
    def test_quality_sentinel_count_tracks_the_authoritative_case_catalog(self):
        source = (ROOT / "scripts/acceptance/quality-sentinels.sh").read_text(encoding="utf-8")
        self.assertIn(f"if len(catalog) != {len(runner.all_cases())}:", source)

    def test_go_case_selectors_cannot_succeed_without_current_tests(self):
        for case_id, (_, command) in runner.CASE_COMMANDS.items():
            for part in command.split("&&"):
                if not part.strip().startswith("go test "):
                    continue
                tokens = shlex.split(part)
                if tokens[:2] != ["go", "test"] or "-run" not in tokens:
                    continue
                pattern = tokens[tokens.index("-run") + 1]
                sources = [
                    path.read_text(encoding="utf-8")
                    for package in tokens if package.startswith("./internal/")
                    for path in (ROOT / package).glob("*_test.go")
                ]
                names = [
                    name for source in sources
                    if "//go:build integration" not in source or "-tags=integration" in tokens
                    for name in re.findall(r"func (Test\w+)\(", source)
                ]
                with self.subTest(case_id=case_id, pattern=pattern):
                    self.assertTrue(any(re.search(pattern, name) for name in names), "selector runs no tests")
                    for prefix in re.findall(r"Test\w+", pattern):
                        self.assertTrue(any(name.startswith(prefix) for name in names), f"retired test: {prefix}")

    def test_database_cases_select_existing_tests(self):
        source = "\n".join(path.read_text(encoding="utf-8") for path in (ROOT / "internal/store").glob("*_test.go"))
        names = re.findall(r"func (Test\w+)\(", source)
        for case_id in ("ACC-DB-001", "ACC-DB-002"):
            command = runner.CASE_COMMANDS[case_id][1]
            pattern = re.search(r"-run '([^']+)'", command).group(1)
            self.assertTrue(any(re.search(pattern, name) for name in names), case_id)

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
