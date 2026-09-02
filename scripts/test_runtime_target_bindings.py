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
        self.assertEqual(catalog["catalogVersion"], 1)
        self.assertEqual(len(catalog["bindings"]), 47)
        self.assertEqual(
            {item["providerId"] for item in catalog["bindings"]},
            {"emulatorjs", "retrom-runtime"},
        )
        for binding in catalog["bindings"]:
            self.assertEqual(
                set(binding),
                {
                    "id", "coreId", "providerId", "targetId", "platformIds",
                    "acceptedContentKinds", "detectorProfile", "deliveryProfile",
                    "launchPolicy", "reviewPolicy",
                },
            )
        by_target = {(item["providerId"], item["targetId"]): item for item in catalog["bindings"]}
        self.assertEqual(by_target[("emulatorjs", "gambatte")]["coreId"], "gambatte")
        self.assertEqual(by_target[("emulatorjs", "desmume2015")]["coreId"], "desmume2015")
        self.assertEqual(
            {item["coreId"] for item in catalog["bindings"] if item["providerId"] == "retrom-runtime" and item["targetId"].startswith("rpgmaker-")},
            {"rpgmaker"},
        )

    def test_catalog_contains_no_provider_implementation_facts(self):
        source = (ROOT / "data/runtime-target-bindings/v1/catalog.json").read_text(encoding="utf-8")
        value = json.loads(source)
        for forbidden in (
            "providerVersion", "adapterId", "adapterKind", "adapterAbi", "capabilities",
            "optionsKind", "checkpoint", "assetPaths", "runtimeBaseUrl",
            "selectedForNewBindings", "priority",
        ):
            self.assertNotIn(forbidden, source)
        self.assertEqual(value["schemaVersion"], 1)


if __name__ == "__main__":
    unittest.main()
