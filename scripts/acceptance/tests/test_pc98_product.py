"""Numeric platform names must survive acceptance catalog discovery."""
import runpy
import unittest
from pathlib import Path


class PC98AcceptanceTests(unittest.TestCase):
    def test_numbered_platform_cases_are_discovered_and_runnable(self):
        runner = runpy.run_path(str(Path(__file__).resolve().parents[1] / "run.py"))
        cases = runner["all_cases"]()
        for case in ("ACC-PC98-001", "ACC-PS2-001"):
            self.assertIn(case, cases)
            self.assertIn(case, runner["PRODUCT_CASES"])
            self.assertIn(case, runner["CASE_COMMANDS"])
