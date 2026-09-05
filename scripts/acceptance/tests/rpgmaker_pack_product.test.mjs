import assert from "node:assert/strict";
import test from "node:test";
import {approveReview, rpgPlatformInstances} from "../rpgmaker_pack_provision_product.mjs";
import {reviewRoles} from "../rpgmaker_pack_provision_plan.mjs";

const targetIds = [...new Set(Object.values(reviewRoles).map((identity) => identity[0]))];

test("resource-pack provision resolves five targets through one virtual RPG Maker directory", async () => {
  assert.deepEqual(targetIds, ["rpgmaker-2000", "rpgmaker-2003", "rpgmaker-xp", "rpgmaker-vx", "rpgmaker-vx-ace"]);
  const client = {writeHeaders: () => ({}), async json(method, path) {
    if (method === "POST") {return {};}
    if (path.includes("platform-instances")) {return {items: [{id: "directory", enabled: true, defaultCoreId: "rpgmaker"}]};}
    assert.equal(path, "/api/v1/admin/runtime-targets");
    return {items: targetIds.map((targetId) => ({coreId: "rpgmaker", providerId: "retrom-runtime", targetId, bundleSha256: "a".repeat(64)}))};
  }};
  assert.deepEqual([...await rpgPlatformInstances(client, targetIds)], targetIds.map((id) => [id, "directory"]));
});

test("ordinary ready review publishes without any runtime trial proof", async () => {
  const calls = [];
  const client = {writeHeaders: () => ({}), async json(method, path) {
    calls.push(path);
    return path.includes("/reviews/") ? {canApprove: true, version: 1, validation: {current: true, status: "READY"}}
      : {status: "PUBLISHED"};
  }, async raw(method, path) {
    calls.push(path);
    return {status: () => 201, json: async () => ({gameId: "published-game"})};
  }};
  assert.equal(await approveReview(client, "review"), "published-game");
  assert.deepEqual(calls, ["/api/v1/admin/reviews/review", "/api/v1/admin/reviews/review/approve", "/api/v1/admin/games/published-game"]);
});

test("blocked or stale dependency never reaches publication", async () => {
  for (const validation of [{current: false, status: "READY"}, {current: true, status: "BLOCKED"}]) {
    const client = {async json() {return {canApprove: true, validation};}, raw() {assert.fail("must not publish");}};
    await assert.rejects(approveReview(client, "review"), /APPROVAL_VALIDATION_INVALID/);
  }
});
