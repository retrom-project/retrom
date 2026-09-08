"""PFB input acceptance consumes the previously published public fixture."""
from pathlib import Path
import json
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]


class InputDiagnosticsDriverTests(unittest.TestCase):
    def test_rerun_does_not_republish_an_already_finalized_review(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            scripts = root / "scripts/acceptance"
            scripts.mkdir(parents=True)
            shutil.copy2(ROOT / "scripts/acceptance/input-diagnostics.sh", scripts)
            spec = root / ".pfb/spec.json"
            spec.parent.mkdir()
            spec.write_text(json.dumps({"id": "fixture-0123456789ab"}))
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


if __name__ == "__main__":
    unittest.main()
