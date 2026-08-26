import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
MAIN = ROOT / "scripts" / "acceptance" / "rpgmaker_pack_provision.mjs"
PLAN = ROOT / "scripts" / "acceptance" / "rpgmaker_pack_provision_plan.mjs"
PRODUCT = ROOT / "scripts" / "acceptance" / "rpgmaker_pack_provision_product.mjs"


class RPGMakerPackProvisionTests(unittest.TestCase):
    def test_provisioner_has_a_fail_closed_fresh_http_boundary(self) -> None:
        product = PRODUCT.read_text()
        for route in (
            "/api/v1/admin/runtime-asset-packs", "/api/v1/admin/games?limit=1",
            "/api/v1/saves?limit=1", "/api/v1/admin/reviews?limit=1",
            "/api/v1/admin/imports/summary",
        ):
            self.assertIn(route, product)
        self.assertIn("RPG_009_PROVISION_DATABASE_NOT_FRESH", product)
        self.assertNotRegex(product, r"sqlite3|better-sqlite|retrom\.db|\.dev-data/")

    def test_protected_references_use_real_publish_product_save_and_restore(self) -> None:
        main = MAIN.read_text()
        product = PRODUCT.read_text()
        self.assertIn('client.upload(files, input.sourceType, "RUNTIME_ASSET_PACK")', product)
        self.assertIn("await validateReview(context, client, base, review, identity[4])", main)
        self.assertIn("const gameId = await approveReview(client, review.itemId)", main)
        self.assertIn("createProductSave(context, client, base, gameId, identity[3])", main)
        self.assertIn("const restore = await productLaunch(client, gameId, coreId, receipt.saveStateId)", product)
        self.assertIn("restore.playUrl", product)
        save_flow = product.split("export async function createProductSave", 1)[1].split(
            "export async function assertProvisionedState", 1,
        )[0]
        self.assertIn('exact(response.status(), 201', save_flow)
        self.assertIn('/api/v1/saves?gameId=', save_flow)
        self.assertIn('item.availability?.status === "AVAILABLE"', save_flow)
        self.assertNotIn("await response.json()", save_flow)

    def test_five_ready_reviews_complete_fourteen_gates_without_approval(self) -> None:
        main = MAIN.read_text()
        product = PRODUCT.read_text()
        matrix = main.split("async function createReviewMatrix", 1)[1].split(
            "function assertProtectedReview", 1,
        )[0]
        self.assertIn('identity[2] !== "ready"', matrix)
        self.assertIn("await validateReview(context, client, base, review, identity[1])", matrix)
        self.assertNotIn("approveReview", matrix)
        gate_block = product.split("const gates = [", 1)[1].split("];", 1)[0]
        self.assertEqual(14, len(re.findall(r'"[A-Z0-9_]+"', gate_block)))
        self.assertIn("current.canApprove !== ready", matrix)
        self.assertNotIn('current.state !== "REVIEW_PENDING"', matrix)
        self.assertIn('current.rpgMaker.runtimeValidation?.state !== "PASSED"', matrix)

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
