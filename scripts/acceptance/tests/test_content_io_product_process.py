"""A Case's owned descendants must not survive a failed parent or its hard timeout."""
import json
import os
from pathlib import Path
import signal
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

from scripts.acceptance import content_io_product_check as check


class ContentIOProductProcessTests(unittest.TestCase):
    def test_completed_failed_case_kills_its_remaining_descendants(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            child_file = root / "processes.json"
            script = root / "case.py"
            script.write_text("import subprocess,sys,os,json\nfrom pathlib import Path\n"
                              "child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(60)'])\n"
                              f"Path({str(child_file)!r}).write_text(json.dumps([os.getpid(),child.pid]))\n"
                              "raise SystemExit(17)\n")
            case = {"caseId": "ACC-OWNED-001", "existingAcceptanceEntry": {
                "path": str(script), "arguments": [], "timeoutSeconds": 10}}
            environment = {"tools": {"python": {"path": sys.executable}, "chrome": {"path": "unused"}},
                           "pfb": {"hostOrigin": "http://localhost"}}
            inputs = {"authentication": {"username": "owned", "password": "owned"},
                      "cases": {case["caseId"]: {"environment": {}, "sources": []}}}
            try:
                with patch.object(check, "ROOT", root):
                    record = check.execute_case(case, environment, inputs, root / "output", "owned-run", [], {})
                self.assertEqual(record["exitCode"], 17)
                self.assertFalse(record["timedOut"])
                parent, child = json.loads(child_file.read_text())
                deadline = time.monotonic() + 1
                while time.monotonic() < deadline and running(child):
                    time.sleep(0.01)
                self.assertFalse(running(child), "CONTENT_IO_CASE_ORPHAN_SURVIVED")
            finally:
                if child_file.exists():
                    parent, _child = json.loads(child_file.read_text())
                    try:
                        os.killpg(parent, signal.SIGKILL)
                    except ProcessLookupError:
                        pass


def running(pid: int) -> bool:
    try:
        return Path(f"/proc/{pid}/stat").read_text().split(")", 1)[1].strip().split()[0] != "Z"
    except FileNotFoundError:
        return False


if __name__ == "__main__":
    unittest.main()
