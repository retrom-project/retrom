"""PFB input acceptance consumes the previously published public fixture."""
from pathlib import Path
import json
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]


class InputDiagnosticsDriverTests(unittest.TestCase):
    def test_acceptance_case_identifiers_are_unique_after_integration(self):
        document = (ROOT / "docs/project-acceptance.md").read_text()
        identifiers = re.findall(r"^### (ACC-[A-Z]+-[0-9]{3})[：:]", document, re.MULTILINE)
        self.assertTrue(identifiers)
        self.assertEqual(len(identifiers), len(set(identifiers)), "Acceptance cases must have unique IDs")

    def test_rerun_does_not_republish_an_already_finalized_review(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            scripts = prepare_driver(root)
            fixture_flow = scripts / "http-flow.sh"
            fixture_flow.write_text("#!/bin/sh\nexit 44\n")
            fixture_flow.chmod(0o755)
            browser = root / "web/node_modules/.bin/playwright"
            browser.parent.mkdir(parents=True)
            browser.write_text(
                '#!/bin/sh\n'
                'test "$RETROM_WEB_ORIGIN" = "http://fixture-0123456789ab.localhost:3000" || exit 45\n'
                'test "$RETROM_PFB_DIAGNOSTICS_REQUIRED" = 1 || exit 46\n'
            )
            browser.chmod(0o755)
            result = subprocess.run(["bash", str(scripts / "input-diagnostics.sh")], cwd=root,
                                    capture_output=True, text=True, check=False)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


    def test_mismatched_provider_version_fails_before_starting_browser(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            scripts = prepare_driver(root, base_version="2.4.1")
            browser = root / "web/node_modules/.bin/playwright"
            browser.parent.mkdir(parents=True)
            browser.write_text("#!/bin/sh\ntouch browser-started\n")
            browser.chmod(0o755)
            result = subprocess.run(["bash", str(scripts / "input-diagnostics.sh")], cwd=root,
                                    capture_output=True, text=True, check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("PFB_INPUT_PROVIDER_VERSION_MISMATCH", result.stderr)
            self.assertFalse((root / "browser-started").exists())


def prepare_driver(root: Path, base_version: str = "2.5.0") -> Path:
    scripts = root / "scripts/acceptance"
    scripts.mkdir(parents=True)
    shutil.copy2(ROOT / "scripts/acceptance/input-diagnostics.sh", scripts)
    runtime = root / "runtime"
    catalog = runtime / "src/providers/emulatorjs/catalog.ts"
    catalog.parent.mkdir(parents=True)
    catalog.write_text('providerVersion: "2.5.0",\n')
    spec = root / ".pfb/spec.json"
    spec.parent.mkdir()
    spec.write_text(json.dumps({"id": "fixture-0123456789ab", "runtime": {"root": str(runtime)}}))
    active = root / ".pfb/workspace/providers/active.json"
    active.parent.mkdir(parents=True)
    active.write_text(json.dumps({"providers": [{"providerId": "emulatorjs", "providerVersion": base_version}]}))
    return scripts


if __name__ == "__main__":
    unittest.main()
