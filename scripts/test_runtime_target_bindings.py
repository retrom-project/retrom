import json
import unittest
from pathlib import Path

from runtime_target_bindings import load_runtime_target_bindings


ROOT = Path(__file__).resolve().parents[1]


class RuntimeTargetBindingsTest(unittest.TestCase):
    def test_catalog_is_closed_complete_and_maps_product_cores_without_defaults(self):
        catalog = load_runtime_target_bindings(
            ROOT / "data/runtime-target-bindings/v1/catalog.json"
        )
        self.assertNotIn("catalogVersion", catalog)
        self.assertEqual(len(catalog["bindings"]), 63)
        self.assertEqual(
            {item["providerId"] for item in catalog["bindings"]},
            {"emulatorjs", "retrom-runtime"},
        )
        for binding in catalog["bindings"]:
            self.assertEqual(
                set(binding),
                {
                    "id", "coreId", "providerId", "targetId", "platformIds",
                    "acceptedContentKinds", "detectorProfile", "launchPolicy",
                },
            )
        by_target = {(item["providerId"], item["targetId"]): item for item in catalog["bindings"]}
        self.assertEqual(by_target[("emulatorjs", "flycast")]["platformIds"], ["dreamcast"])
        self.assertEqual(by_target[("emulatorjs", "flycast")]["acceptedContentKinds"], ["SINGLE_FILE"])
        self.assertEqual(by_target[("emulatorjs", "gambatte")]["coreId"], "gambatte")
        self.assertEqual(by_target[("emulatorjs", "desmume2015")]["coreId"], "desmume2015")
        expected_single_file_targets = {
            "fuse": ("fuse", ["zxspectrum"]),
            "gearcoleco": ("gearcoleco", ["colecovision"]),
            "prboom": ("prboom", ["doom"]),
            "puae": ("puae", ["amiga"]),
            "vice-x128": ("vice_x128", ["c128"]),
            "vice-x64sc": ("vice_x64sc", ["c64"]),
            "vice-xvic": ("vice_xvic", ["vic20"]),
            "virtualjaguar": ("virtualjaguar", ["atarijaguar"]),
        }
        for target_id, (core_id, platform_ids) in expected_single_file_targets.items():
            binding = by_target[("emulatorjs", target_id)]
            self.assertEqual(binding["coreId"], core_id)
            self.assertEqual(binding["platformIds"], platform_ids)
            self.assertEqual(binding["acceptedContentKinds"], ["SINGLE_FILE"])
            self.assertEqual(binding["detectorProfile"], "EMULATORJS_SINGLE_FILE")
            self.assertEqual(binding["launchPolicy"], "SUPPORTED")
        self.assertEqual(by_target[("retrom-runtime", "j2me")]["detectorProfile"], "J2ME_JAR")
        self.assertEqual(by_target[("retrom-runtime", "scummvm")]["detectorProfile"], "SCUMMVM_PROJECT")
        self.assertEqual(by_target[("retrom-runtime", "scummvm")]["acceptedContentKinds"], ["SCUMMVM_PROJECT"])
        self.assertEqual(by_target[("retrom-runtime", "j2me")]["acceptedContentKinds"], ["SINGLE_FILE"])
        play = by_target[("retrom-runtime", "play-ps2")]
        self.assertEqual(play["coreId"], "play")
        self.assertEqual(play["platformIds"], ["ps2"])
        self.assertEqual(play["detectorProfile"], "OPTICAL_DISC")
        self.assertEqual(play["acceptedContentKinds"], ["SINGLE_FILE"])
        self.assertEqual(
            {item["coreId"] for item in catalog["bindings"] if item["providerId"] == "retrom-runtime" and item["targetId"].startswith("rpgmaker-")},
            {"rpgmaker"},
        )

    def test_catalog_contains_no_provider_implementation_facts(self):
        source = (ROOT / "data/runtime-target-bindings/v1/catalog.json").read_text(encoding="utf-8")
        value = json.loads(source)
        for forbidden in (
            "providerVersion", "adapterId", "adapterKind", "adapterAbi", "capabilities",
            "optionsKind", "targetOptionsSchema", "checkpoint", "assetPaths", "runtimeBaseUrl",
            "selectedForNewBindings", "priority", "deliveryProfile", "reviewPolicy",
        ):
            self.assertNotIn(forbidden, source)
        self.assertEqual(value["schemaVersion"], 1)


if __name__ == "__main__":
    unittest.main()
