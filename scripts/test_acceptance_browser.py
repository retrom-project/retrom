"""Keep browser acceptance isolated, bounded and on current product interfaces."""
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]


class BrowserAcceptanceBoundaryTests(unittest.TestCase):
    def test_product_fixture_uses_normal_import_and_current_browser_suite(self):
        source = (ROOT / "scripts/acceptance/browser.py").read_text()
        self.assertIn("builder.build_rom", source)
        self.assertIn("metadata.pegasus.txt", source)
        self.assertIn("clean-refactor.spec.ts", source)
        self.assertIn("library-mobile.spec.ts", source)
        self.assertNotIn("INSERT INTO", source)
        self.assertNotIn("seed-", source)
        self.assertIn('sys.argv[2:] == ["ACC-RF-BROWSER"]', source)

    def test_owned_database_and_processes_are_cleaned_after_failure(self):
        source = (ROOT / "scripts/acceptance/browser.py").read_text()
        self.assertIn("sql.Identifier(database)", source)
        self.assertIn("DROP DATABASE {} WITH (FORCE)", source)
        self.assertIn("finally:", source)
        self.assertIn("stop(process)", source)
        self.assertIn("STARTUP_SECONDS = 300", source)
        self.assertIn('"TMPDIR": "/tmp"', source)
        self.assertIn('browser_environment.pop("RETROM_E2E_GREP", None)', source)
        self.assertNotIn("--project", source)

    def test_tool_and_provider_inputs_have_no_old_release_default(self):
        source = (ROOT / "scripts/acceptance/browser.py").read_text()
        self.assertIn("RETROM_RUNTIME_TOOL_INPUT", source)
        self.assertIn("RETROM_PROVIDER_INPUT", source)
        self.assertIn("image_input_digest.py", source)
        self.assertNotIn("runtime-provider-prepare-auto", source)
        self.assertNotIn("release.json", source)


if __name__ == "__main__":
    unittest.main()
