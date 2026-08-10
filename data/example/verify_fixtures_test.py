#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("verify-fixtures.py")
SPEC = importlib.util.spec_from_file_location("retrom_verify_fixtures", SCRIPT)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("unable to load fixture verifier")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class FixtureSelectorTests(unittest.TestCase):
    def test_selects_a_core_without_requiring_unrelated_multi_disc_bytes(self) -> None:
        manifest = {
            "fixtures": [{"core": "mgba"}, {"core": "yabause"}],
            "multiDiscFixtures": [{"id": "multidisc-saturn-2"}],
        }
        cores, multi_disc, unknown = MODULE.selected_fixtures(manifest, {"mgba"})
        self.assertEqual(cores, [{"core": "mgba"}])
        self.assertEqual(multi_disc, [])
        self.assertEqual(unknown, set())

    def test_reports_unknown_selectors_and_can_select_one_multi_disc_fixture(self) -> None:
        manifest = {
            "fixtures": [{"core": "yabause"}],
            "multiDiscFixtures": [
                {"id": "multidisc-saturn-2"},
                {"id": "multidisc-saturn-3"},
            ],
        }
        cores, multi_disc, unknown = MODULE.selected_fixtures(
            manifest, {"multidisc-saturn-3", "unknown"}
        )
        self.assertEqual(cores, [])
        self.assertEqual(multi_disc, [{"id": "multidisc-saturn-3"}])
        self.assertEqual(unknown, {"unknown"})


if __name__ == "__main__":
    unittest.main()
