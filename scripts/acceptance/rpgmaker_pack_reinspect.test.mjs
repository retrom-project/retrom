import {test} from "node:test";
import assert from "node:assert/strict";
import {validateCatalog} from "./rpgmaker_pack_reinspect.mjs";

test("read-only reinspection preserves the exact completed catalog including its deleted zero-reference row", () => {
  const plan = {protectedReferences: {xp: {installationId: "protected"}}};
  const rows = [{installationId: "installed", definitionId: "standard", filesDigest: "a", fileCount: 1,
    totalBytes: 10, sourceNote: "owned", status: "READY"}, {installationId: "deleted", definitionId: "custom",
    filesDigest: "b", fileCount: 1, totalBytes: 11, sourceNote: "owned", status: "READY"}];
  const observed = {installations: {standard: rows[0], zeroReference: rows[1]}};
  const catalog = {installations: [{installationId: "protected"}, rows[0], {...rows[1], status: "DELETED"}]};
  validateCatalog(catalog, plan, observed);
  for (const mutate of [
    (c) => c.installations.pop(),
    (c) => {c.installations[1].filesDigest = "changed";},
    (c) => {c.installations[2].status = "READY";},
    (c) => c.installations.push({installationId: "foreign"}),
  ]) {
    const changed = structuredClone(catalog); mutate(changed);
    assert.throws(() => validateCatalog(changed, plan, observed), /REINSPECT_CATALOG_INVALID/);
  }
});
