import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
MAIN = ROOT / "scripts" / "acceptance" / "rpgmaker_pack_provision.mjs"
PLAN = ROOT / "scripts" / "acceptance" / "rpgmaker_pack_provision_plan.mjs"
PRODUCT = ROOT / "scripts" / "acceptance" / "rpgmaker_pack_provision_product.mjs"


class RPGMakerPackProvisionTests(unittest.TestCase):
    def test_provisioner_preserves_existing_population_through_read_only_http(self) -> None:
        product = PRODUCT.read_text()
        population = (PRODUCT.parent / "rpgmaker_pack_population.mjs").read_text()
        for route in (
            "/api/v1/admin/runtime-asset-packs", "/api/v1/admin/games",
            "/api/v1/saves", "/api/v1/admin/reviews",
        ):
            self.assertIn(route, product + population)
        self.assertIn("RPG_009_PROVISION_PACK_CATALOG_NOT_EMPTY", product)
        self.assertIn("RPG_009_PROVISION_POPULATION_CHANGED", population)
        self.assertIn("RPG_009_PROVISION_FINAL_CARDINALITY_INVALID", population)
        self.assertIn("populationPreservation", MAIN.read_text())
        self.assertNotRegex(product + population, r"sqlite3|better-sqlite|retrom\.db|\.dev-data/")

    def test_protected_references_use_real_publish_product_save_and_restore(self) -> None:
        main = MAIN.read_text()
        product = PRODUCT.read_text()
        self.assertIn('client.upload(files, input.sourceType, "RUNTIME_ASSET_PACK")', product)
        self.assertIn("await trialReview(context, client, base, review, identity[4])", main)
        self.assertIn("const gameId = await approveReview(client, review.itemId)", main)
        self.assertIn("createProductSave(context, client, base, gameId, identity[3])", main)
        self.assertIn("const restore = await productLaunch(client, gameId, receipt.saveStateId)", product)
        self.assertIn("restore.playUrl", product)
        save_flow = product.split("export async function createProductSave", 1)[1].split(
            "export async function assertProvisionedState", 1,
        )[0]
        self.assertIn('exact(response.status(), 201', save_flow)
        self.assertIn('/api/v1/saves?gameId=', save_flow)
        self.assertIn('item.availability?.status === "AVAILABLE"', save_flow)
        self.assertNotIn("await response.json()", save_flow)

    def test_ready_reviews_use_ordinary_player_trials_without_approval_gates(self) -> None:
        main = MAIN.read_text()
        product = PRODUCT.read_text()
        matrix = main.split("async function createReviewMatrix", 1)[1].split(
            "function assertProtectedReview", 1,
        )[0]
        self.assertIn('identity[2] !== "ready"', matrix)
        self.assertIn("await trialReview(context, client, base, review, identity[1])", matrix)
        self.assertNotIn("approveReview", matrix)
        self.assertIn("current.canApprove !== ready", matrix)
        self.assertNotIn('current.state !== "REVIEW_PENDING"', matrix)
        self.assertIn('current.validation?.current', matrix)
        for retired in ("runtime-validations", "machineGates", "runtimeValidation", "core-artifacts"):
            self.assertNotIn(retired, product + main)
        for operation in ("/previews", "restoreFromPreviewId", "capturePreviewCheckpoint",
                          "observeFixturePosition", "observePreviewFrames", "finishPreview",
                          "RPG_009_PROVISION_RESTORE_FROZEN_PAYLOAD_MISMATCH"):
            self.assertIn(operation, product)
        self.assertIn('"/api/v1/admin/runtime-targets"', product)
        self.assertIn("localRpgAcceptanceProxy", main)

    def test_plan_is_written_last_with_schema_v2_and_exclusive_permissions(self) -> None:
        main = MAIN.read_text()
        plan = PLAN.read_text()
        self.assertLess(main.index("await assertProvisionedState"), main.index("writePlan(arguments_.plan, plan)"))
        self.assertIn("schemaVersion: 2", plan)
        self.assertIn('flag: "wx", mode: 0o600', plan)
        self.assertIn("validateIdentifierMap(reviewIds", plan)
        self.assertIn("validateReference(protectedReferences.restorableCheckpoint", plan)


if __name__ == "__main__":
    unittest.main()
