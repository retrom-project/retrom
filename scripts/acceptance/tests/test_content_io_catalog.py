import copy
import json
from pathlib import Path
import tempfile
import unittest
from scripts.acceptance.content_io_catalog import CATALOG, ROOT, load_catalog
from scripts.acceptance.content_io_cases import TARGET_DECLARATIONS, TARGET_MODES


class ContentIOCatalogTests(unittest.TestCase):
    def test_fixed_managed_target_matrix_and_real_entrypoints(self):
        cases = load_catalog()
        self.assertEqual({case["targetId"] for case in cases}, set(TARGET_DECLARATIONS))
        self.assertEqual(TARGET_MODES["flycast"], "RANGE")
        self.assertEqual(TARGET_DECLARATIONS["daphne"]["checkpointSemantics"], "NO_SAVE")
        self.assertNotIn("save-restore-input", next(case for case in cases if case["targetId"] == "daphne")["requiredScenarios"])
        for target in ["rpgmaker-mv", "rpgmaker-mz", "tyranoscript"]:
            self.assertEqual(TARGET_MODES[target], "RANGE")
            self.assertNotIn("retired-rtp-boundary", next(case for case in cases if case["targetId"] == target)["requiredScenarios"])
        ranges = [case for case in cases if case["networkPolicy"]["mode"] == "RANGE"]
        self.assertEqual(len(ranges), sum(mode == "RANGE" for mode in TARGET_MODES.values()))
        self.assertTrue(all("trace-10000" in case["requiredScenarios"] for case in ranges))
        rpg = [case for case in ranges if case["targetId"] in {"rpgmaker-xp", "rpgmaker-vx", "rpgmaker-vx-ace"}]
        self.assertTrue(all("retired-rtp-boundary" in case["requiredScenarios"] for case in rpg))
        self.assertTrue(all(case["inputRoles"] == ["game"] for case in rpg))
        self.assertTrue(all(not any(ref.startswith("operator:") for ref in case["fixtureRef"]) for case in rpg))

    def test_rejects_missing_duplicate_private_or_weakened_cases(self):
        original = json.loads(CATALOG.read_text())
        mutations = [
            lambda value: value["cases"].pop(),
            lambda value: value["cases"].append(value["cases"][0]),
            lambda value: value["cases"][0].update(fixtureRef=["/private/game.iso"]),
            lambda value: value["cases"][0]["performance"].update(repetitions=1),
            lambda value: value["cases"][0]["networkPolicy"].update(observeEntireBrowserContext=False),
            lambda value: value["cases"][0]["requiredScenarios"].remove("warm-new-launch"),
            lambda value: value["cases"][0]["requiredScenarios"].remove("fault-range-200"),
            lambda value: value["cases"][0].update(targetId="unknown-target"),
            lambda value: value["cases"][0]["networkPolicy"].update(mode="EAGER"),
            lambda value: value["cases"][0].update(providerId="retrom-runtime"),
            lambda value: value["cases"][4]["requiredScenarios"].remove("retired-rtp-boundary"),
            lambda value: value["cases"][4]["inputRoles"].append("rtp"),
            lambda value: value["cases"][4]["fixtureRef"].append("operator:rpgmaker-xp-rtp"),
            lambda value: value["cases"][0]["actions"].pop(),
            lambda value: value["cases"][0]["existingAcceptanceEntry"].update(path="scripts/acceptance/absent.mjs"),
            lambda value: value["cases"][0]["existingAcceptanceEntry"].update(arguments=["$(unsafe)"]),
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "catalog.json"
            for index, mutate in enumerate(mutations):
                with self.subTest(index=index):
                    value = copy.deepcopy(original); mutate(value)
                    path.write_text(json.dumps(value))
                    with self.assertRaisesRegex(ValueError, "CONTENT_IO_CATALOG_"):
                        load_catalog(path, ROOT)
