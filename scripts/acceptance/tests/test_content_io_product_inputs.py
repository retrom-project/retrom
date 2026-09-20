"""Operator-only product inputs are explicit, complete and fingerprinted without path disclosure."""
import json
from pathlib import Path
import tempfile
import unittest

from scripts.acceptance.content_io_product_inputs import read_operator_inputs, source_receipt, validate_case_inputs


class ContentIOProductInputsTests(unittest.TestCase):
    def test_receipts_detect_same_size_changes_and_hide_private_names(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            game = root / "private-game-name"
            game.write_bytes(b"one")
            source = {"id": "operator:game", "role": "game", "path": str(root)}
            first = source_receipt(source)
            game.write_bytes(b"two")
            second = source_receipt(source)
            self.assertEqual(first["sizeBytes"], second["sizeBytes"])
            self.assertNotEqual(first["sha256"], second["sha256"])
            self.assertNotIn("private", json.dumps(first))
            (root / "link").symlink_to(game)
            with self.assertRaisesRegex(ValueError, "SOURCE_LINK"):
                source_receipt(source)

    def test_missing_case_input_and_protected_environment_fail_before_execution(self) -> None:
        case = {"caseId": "ACC-OWNED-001", "fixtureRef": ["owned:game"], "inputRoles": ["game"]}
        selected = {"environment": {"RETROM_ACCEPTANCE_BASE_URL": "http://other"}, "sources": [{"id": "owned:game", "role": "game", "path": "/owned/game"}]}
        with self.assertRaisesRegex(ValueError, "ENVIRONMENT_INVALID"):
            validate_case_inputs(selected, case)
        selected["environment"] = {"NODE_OPTIONS": "--import=other"}
        with self.assertRaisesRegex(ValueError, "ENVIRONMENT_INVALID"):
            validate_case_inputs(selected, case)
        selected["environment"] = {"RETROM_OWNED_INPUT": "/owned/game"}
        selected["sources"].append(selected["sources"][0])
        with self.assertRaisesRegex(ValueError, "SOURCE_COVERAGE"):
            validate_case_inputs(selected, case)
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary).resolve() / "inputs.json"
            path.write_text(json.dumps({"schemaVersion": 1, "pfbId": "owned", "authentication": {"username": "owned", "password": "owned"}, "cases": {}}))
            with self.assertRaisesRegex(ValueError, "INPUT_COVERAGE"):
                read_operator_inputs(path, "owned", [case])

    def test_explicit_complete_inputs_retain_only_the_requested_fields(self) -> None:
        case = {"caseId": "ACC-OWNED-001", "fixtureRef": ["owned:game"], "inputRoles": ["game"]}
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary).resolve() / "inputs.json"
            value = {"schemaVersion": 1, "pfbId": "owned", "authentication": {"username": "owned", "password": "owned"},
                     "cases": {case["caseId"]: {"environment": {"RETROM_OWNED_GAME": "/owned/game"}, "sources": [{"id": "owned:game", "role": "game", "path": "/owned/game"}]}}}
            path.write_text(json.dumps(value))
            self.assertEqual(read_operator_inputs(path, "owned", [case]), value)
            with self.assertRaisesRegex(ValueError, "INPUT_SCHEMA"):
                read_operator_inputs(path, "another-pfb", [case])


if __name__ == "__main__":
    unittest.main()
