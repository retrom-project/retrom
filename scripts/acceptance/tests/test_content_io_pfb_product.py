"""Focused selection never silently drops a missing or repeated requested Case."""
import unittest

from scripts.acceptance.content_io_pfb_product_check import select_cases


class ContentIOPFBProductTests(unittest.TestCase):
    def test_selection_preserves_catalog_order(self):
        catalog = [{"caseId": "ACC-BBC-001"}, {"caseId": "ACC-SAMCOUPE-001"}, {"caseId": "ACC-DOSBOX-001"}]
        self.assertEqual(select_cases(catalog, ["ACC-DOSBOX-001", "ACC-BBC-001"]), [catalog[0], catalog[2]])

    def test_missing_duplicate_and_empty_selection_fail(self):
        catalog = [{"caseId": "ACC-BBC-001"}]
        for selected in [[], ["ACC-UNKNOWN-001"], ["ACC-BBC-001", "ACC-BBC-001"]]:
            with self.subTest(selected=selected), self.assertRaisesRegex(ValueError, "SELECTION_INVALID"):
                select_cases(catalog, selected)


if __name__ == "__main__":
    unittest.main()
