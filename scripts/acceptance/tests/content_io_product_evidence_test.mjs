import test from "node:test";
import assert from "node:assert/strict";
import {createHash, randomUUID} from "node:crypto";
import {mkdtemp, readFile, writeFile, rm} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {verifyProductEvidence} from "../content_io_product_evidence.mjs";
import {performanceFixture} from "./content_io_performance_fixture.mjs";

async function fixture(directory) {
  const runId = randomUUID(), samples = performanceFixture();
  const definition = {caseId: "ACC-TEST-001", providerId: "retrom-runtime", targetId: "owned", requiredScenarios: ["cold"]};
  const identity = {bundleSha256: "1".repeat(64), moduleSha256: "d".repeat(64), workerSha256: "e".repeat(64),
    browserSha256: "b".repeat(64), sourceSha256: "a".repeat(64), networkSettingsSha256: "c".repeat(64), observationId: "fixed-game-scene"};
  const artifact = async (path, value) => {
    const bytes = JSON.stringify(value); await writeFile(join(directory, path), bytes);
    return {path, sha256: createHash("sha256").update(bytes).digest("hex")};
  };
  await artifact("expected-identity.json", {...Object.fromEntries(Object.entries(identity).filter(([key]) => !["networkSettingsSha256", "observationId"].includes(key))), baselineBundleSha256: "0".repeat(64)});
  const scenario = {schemaVersion: 1, runId, caseId: definition.caseId, scenario: "cold", status: "PASS",
    evidence: [await artifact("raw.json", {schemaVersion: 1, runId, caseId: definition.caseId, status: "PASS", observed: 7})],
    assertions: [{name: "synthetic validator assertion, not a game observation", expected: 7, observed: 7}]};
  const proof = {schemaVersion: 1, runId, caseId: definition.caseId, status: "PASS", providerId: definition.providerId,
    targetId: definition.targetId, identity, observedIdentity: await artifact("observed-identity.json", identity),
    scenarios: [{id: "cold", report: await artifact("cold.json", scenario)}], performance: await artifact("performance.json", samples)};
  await artifact("content-io-product.json", proof);
  return {definition, runId, proof, artifact, scenario};
}
test("[HP-07] UNIT/product-evidence validates synthetic complete proof without claiming a real product run", async () => {
  const directory = await mkdtemp(join(tmpdir(), "content-product-validator-"));
  try {
    const {definition, runId} = await fixture(directory);
    const result = await verifyProductEvidence(directory, definition, runId);
    assert.equal(result.status, "PASS"); assert.equal(result.comparison.comparisons.length, 4);
  } finally {await rm(directory, {recursive: true});}
});
for (const corruption of ["old-run", "missing-scenario", "no-assertions", "failed-assertion", "tampered-report", "stale-module", "missing-performance", "wrong-baseline", "missing-raw", "tampered-raw", "old-raw-run"]) {
  test(`[HP-07] UNIT/product-evidence rejects ${corruption} despite a top-level PASS`, async () => {
    const directory = await mkdtemp(join(tmpdir(), "content-product-validator-"));
    try {
      const {definition, runId, proof, artifact, scenario} = await fixture(directory);
      if (corruption === "old-run") proof.runId = randomUUID();
      if (corruption === "missing-scenario") proof.scenarios = [];
      if (corruption === "no-assertions" || corruption === "failed-assertion") {
        if (corruption === "no-assertions") scenario.assertions = [];
        else scenario.assertions[0].observed = 9;
        proof.scenarios[0].report = await artifact("cold.json", scenario);
      }
      if (corruption === "missing-raw" || corruption === "old-raw-run") {
        scenario.evidence = corruption === "missing-raw" ? [] : [await artifact("raw.json", {
          schemaVersion: 1, runId: randomUUID(), caseId: definition.caseId, status: "PASS", observed: 7})];
        proof.scenarios[0].report = await artifact("cold.json", scenario);
      }
      if (corruption === "tampered-raw") await writeFile(join(directory, "raw.json"), "{}");
      if (corruption === "tampered-report") await writeFile(join(directory, "cold.json"), "{}");
      if (corruption === "stale-module") {
        const expected = JSON.parse(await readFile(join(directory, "expected-identity.json"), "utf8"));
        expected.moduleSha256 = "f".repeat(64); await artifact("expected-identity.json", expected);
      }
      if (corruption === "missing-performance" || corruption === "wrong-baseline") {
        const samples = performanceFixture();
        if (corruption === "missing-performance") samples.pop();
        else for (const sample of samples.filter(row => row.variant === "baseline")) sample.providerBundleSha256 = "f".repeat(64);
        proof.performance = await artifact("performance.json", samples);
      }
      await artifact("content-io-product.json", proof);
      await assert.rejects(verifyProductEvidence(directory, definition, runId));
    } finally {await rm(directory, {recursive: true});}
  });
}
