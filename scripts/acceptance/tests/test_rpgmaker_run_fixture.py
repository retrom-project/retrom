from pathlib import Path
import tempfile
import unittest
from scripts.acceptance.rpgmaker_run_fixture import create, validate, MARKER


class RPGRunFixtureTests(unittest.TestCase):
    def test_run_copy_keeps_canonical_files_and_rejects_added_changed_or_missing_content(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            original, derived = root / "rpgxp", root / "derived"
            original.mkdir()
            (original / "Game.ini").write_bytes(b"owned content")
            run_id = "11111111-1111-4111-8111-111111111111"
            receipt = create(original, derived, run_id)
            self.assertEqual(receipt, validate(original, derived))
            self.assertFalse((original / MARKER).exists())
            with self.assertRaises(FileExistsError):
                create(original, derived, run_id)
            game = derived / "Game.ini"
            game.write_bytes(b"other content")
            with self.assertRaisesRegex(ValueError, "SOURCE_CHANGED"):
                validate(original, derived)
            game.write_bytes(b"owned content")
            extra = derived / "unexpected"
            extra.write_bytes(b"extra")
            with self.assertRaisesRegex(ValueError, "SOURCE_CHANGED"):
                validate(original, derived)
            extra.unlink()
            game.unlink()
            with self.assertRaisesRegex(ValueError, "SOURCE_CHANGED"):
                validate(original, derived)

    def test_copy_rejects_symlinks_and_invalid_run_markers(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            original, derived = root / "rpgvx", root / "derived"
            original.mkdir()
            (original / "Game.ini").write_bytes(b"owned")
            create(original, derived, "11111111-1111-4111-8111-111111111111")
            (derived / MARKER).write_bytes(b"arbitrary bytes")
            with self.assertRaises(ValueError):
                validate(original, derived)
            (derived / "link").symlink_to(original / "Game.ini")
            with self.assertRaisesRegex(ValueError, "SYMLINK"):
                validate(original, derived)


class RPGRunProvenanceTests(unittest.TestCase):
    def test_derived_provenance_is_explicit_and_rejects_forged_receipt(self):
        import copy
        from scripts.acceptance import rpgmaker_case as case
        spec = case.GENERATION_CASES["ACC-RPG-004"]
        original = case.ROOT / "testdata/public-roms/rpgmaker-smoke/rpgxp"
        with tempfile.TemporaryDirectory() as directory:
            derived = Path(directory) / "derived"
            create(original, derived, "11111111-1111-4111-8111-111111111111")
            digest, count, size = case.project_digest(derived)
            evidence = case.generation_input_provenance("ACC-RPG-004", spec, derived, digest, count, size)
            self.assertEqual(evidence["kind"], "RETROM_OWNED_RUN_FIXTURE")
            case.validate_input_provenance(evidence, spec, digest)
            mutations = [
                ("runInstance", {**evidence["runInstance"], "baseFilesSha256": "0" * 64}),
                ("fileCount", count + 1), ("totalBytes", size + 1),
                ("sourceSha256", "0" * 64), ("sourceVersion", "fixture-manifest-v1"),
                ("kind", "RETROM_OWNED_PUBLIC_FIXTURE"),
            ]
            for key, value in mutations:
                with self.subTest(key=key):
                    invalid = copy.deepcopy(evidence)
                    invalid[key] = value
                    with self.assertRaises(case.ContractError):
                        case.validate_input_provenance(invalid, spec, digest)
            with self.assertRaises(case.ContractError):
                case.validate_input_provenance(evidence, case.GENERATION_CASES["ACC-RPG-002"], digest)
