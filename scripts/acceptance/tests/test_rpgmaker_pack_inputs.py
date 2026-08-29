import importlib.util
import json
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
MODULE_PATH = ROOT / "scripts" / "acceptance" / "rpgmaker_pack_inputs.py"
SPEC = importlib.util.spec_from_file_location("rpgmaker_pack_inputs", MODULE_PATH)
assert SPEC and SPEC.loader
pack_inputs = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(pack_inputs)


class RPGMakerPackInputTests(unittest.TestCase):
    def test_generation_is_repeatable_and_covers_the_formal_matrix(self) -> None:
        seven_zip = shutil.which("7z") or shutil.which("7zz")
        self.assertIsNotNone(seven_zip)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = pack_inputs.generate(root / "first", str(seven_zip))
            second = pack_inputs.generate(root / "second", str(seven_zip))

            first_rows = first["inputs"]
            second_rows = second["inputs"]
            self.assertEqual("MIT", first["license"])
            self.assertEqual("testdata/public-roms/rpgmaker-smoke/LICENSE", first["licenseSource"])
            self.assertEqual(12, len(first_rows))
            self.assertEqual(
                {
                    "RPG2000_RTP", "RPG2003_RTP", "RGSS1_RTP_STANDARD",
                    "RGSS2_RTP_RPGVX", "RGSS3_RTP_RPGVXAce", "RGSS_CUSTOM_RTP",
                },
                {row["kind"] for row in first_rows.values()},
            )
            self.assertEqual({"DIRECTORY", "FILES"}, {row["sourceType"] for row in first_rows.values()})
            self.assertTrue({".zip", ".7z"} <= {
                Path(row["sourcePath"]).suffix for row in first_rows.values() if row["sourceType"] == "FILES"
            })
            canonical = lambda rows: {
                role: {key: value for key, value in row.items() if key != "sourcePath"}
                for role, row in rows.items()
            }
            self.assertEqual(canonical(first_rows), canonical(second_rows))
            self.assertNotEqual(
                first_rows["rgss1StandardV1"]["sourceSha256"],
                first_rows["rgss1StandardV2"]["sourceSha256"],
            )
            self.assertEqual(13, len(first["reviewProjects"]))
            self.assertEqual({"publishedVariant", "restorableCheckpoint"}, set(first["protectedPackInputs"]))
            self.assertEqual({"publishedVariant", "restorableCheckpoint"}, set(first["protectedProjects"]))
            self.assertEqual(
                {"RGSS1_RTP_STANDARD", "RGSS2_RTP_RPGVX"},
                {row["kind"] for row in first["protectedPackInputs"].values()},
            )
            for role in first["protectedPackInputs"]:
                first_row = first["protectedPackInputs"][role]
                second_row = second["protectedPackInputs"][role]
                self.assertEqual(
                    {key: value for key, value in first_row.items() if key != "sourcePath"},
                    {key: value for key, value in second_row.items() if key != "sourcePath"},
                )

            persisted = json.loads((root / "first" / "inputs.json").read_text())
            self.assertEqual(first, persisted)
            for row in first_rows.values():
                source = Path(row["sourcePath"])
                self.assertTrue(source.is_dir() if row["sourceType"] == "DIRECTORY" else source.is_file())
                self.assertEqual(pack_inputs.SOURCE_NOTE, row["sourceNote"])
            for review in first["reviewProjects"].values():
                self.assertTrue(Path(review["sourcePath"]).is_dir())
            for review in first["protectedProjects"].values():
                self.assertTrue(Path(review["sourcePath"]).is_dir())

            plan_path = root / "plan.json"
            evidence_path = root / "provision-evidence.json"
            module = "./scripts/acceptance/rpgmaker_pack_provision_plan.mjs"
            script = f"""
              import {{buildPlan, buildProvisionEvidence, loadGeneratorInputs, reviewRoles,
                writePlan, writeProvisionEvidence}} from {json.dumps(module)};
              const inputs = loadGeneratorInputs({json.dumps(str(root / 'first' / 'inputs.json'))});
              const uuid = (value) => `${{String(value).padStart(8, "0")}}-1111-4111-8111-111111111111`;
              const reviews = Object.fromEntries(Object.keys(reviewRoles).map((role, index) => [role, uuid(index + 1)]));
              const references = {{
                publishedVariant: {{installationId: uuid(20), gameId: uuid(21)}},
                restorableCheckpoint: {{installationId: uuid(22), gameId: uuid(23), saveStateId: uuid(24)}},
              }};
              const plan = buildPlan(inputs, reviews, references);
              const repository = {{gitCommit: "1".repeat(40), gitDirty: false,
                gitDirtySummary: {{fileCount: 0, sha256: "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945", entries: []}}}};
              writePlan({json.dumps(str(plan_path))}, plan);
              writeProvisionEvidence({json.dumps(str(evidence_path))}, buildProvisionEvidence(inputs, plan, repository));
            """
            completed = subprocess.run(
                ["node", "--input-type=module", "-e", script], cwd=ROOT,
                check=False, text=True, capture_output=True,
            )
            self.assertEqual(0, completed.returncode, completed.stderr)
            plan = json.loads(plan_path.read_text())
            self.assertEqual(2, plan["schemaVersion"])
            self.assertEqual(set(first["inputs"]), set(plan["uploads"]))
            self.assertEqual(set(first["reviewProjects"]), set(plan["reviewIds"]))
            self.assertEqual({"publishedVariant", "restorableCheckpoint"}, set(plan["protectedReferences"]))
            self.assertEqual(0o600, stat.S_IMODE(plan_path.stat().st_mode))
            provision_evidence = json.loads(evidence_path.read_text())
            self.assertEqual("PROVISIONED", provision_evidence["status"])
            self.assertEqual(plan["reviewIds"], provision_evidence["planIdentity"]["reviewIds"])
            self.assertNotIn("sourcePath", json.dumps(provision_evidence))
            self.assertEqual(0o600, stat.S_IMODE(evidence_path.stat().st_mode))

            source = Path(first["inputs"]["rpg2000Rtp"]["sourcePath"]) / "RTP" / "Backdrop" / "Bridge.png"
            source.write_bytes(source.read_bytes() + b"tampered")
            tamper_check = subprocess.run(
                ["node", "--input-type=module", "-e",
                 f'import {{loadGeneratorInputs}} from {json.dumps(module)}; '
                 f'loadGeneratorInputs({json.dumps(str(root / "first" / "inputs.json"))});'],
                cwd=ROOT, check=False, text=True, capture_output=True,
            )
            self.assertNotEqual(0, tamper_check.returncode)
            self.assertIn("RPG_009_PROVISION_INPUT_IDENTITY_INVALID", tamper_check.stderr)

    def test_generation_refuses_relative_or_existing_output(self) -> None:
        with self.assertRaisesRegex(ValueError, "new absolute path"):
            pack_inputs.generate(Path("relative"), "7z")
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, "new absolute path"):
                pack_inputs.generate(Path(directory), "7z")


if __name__ == "__main__":
    unittest.main()
