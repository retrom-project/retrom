import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {readFile, realpath, lstat} from "node:fs/promises";
import {resolve, relative, isAbsolute} from "node:path";
import {parseArgs} from "node:util";
import {fileURLToPath} from "node:url";
import {compareContentIOPerformance} from "./content_io_performance.mjs";

const sha = bytes => createHash("sha256").update(bytes).digest("hex");
const digest = value => typeof value === "string" && /^[a-f0-9]{64}$/u.test(value);
function exact(value, fields) {
  assert.ok(value && typeof value === "object" && !Array.isArray(value), "CONTENT_IO_PRODUCT_PROOF_SCHEMA");
  assert.deepEqual(Object.keys(value).sort(), [...fields].sort(), "CONTENT_IO_PRODUCT_PROOF_SCHEMA");
}
async function regularJSON(path) {
  assert.equal(await realpath(path), path, "CONTENT_IO_PRODUCT_ARTIFACT_LINK");
  assert.ok((await lstat(path)).isFile(), "CONTENT_IO_PRODUCT_ARTIFACT_INVALID");
  return JSON.parse(await readFile(path, "utf8"));
}
async function artifact(directory, reference) {
  exact(reference, ["path", "sha256"]);
  assert.ok(typeof reference.path === "string" && !isAbsolute(reference.path) && digest(reference.sha256), "CONTENT_IO_PRODUCT_ARTIFACT_INVALID");
  const path = resolve(directory, reference.path), local = relative(directory, path);
  assert.ok(local && !local.startsWith("..") && !isAbsolute(local), "CONTENT_IO_PRODUCT_ARTIFACT_ESCAPE");
  assert.equal(await realpath(path), path, "CONTENT_IO_PRODUCT_ARTIFACT_LINK");
  assert.ok((await lstat(path)).isFile(), "CONTENT_IO_PRODUCT_ARTIFACT_INVALID");
  const bytes = await readFile(path);
  assert.equal(sha(bytes), reference.sha256, "CONTENT_IO_PRODUCT_ARTIFACT_CHANGED");
  return JSON.parse(bytes);
}
async function scenarioReport(directory, report, scenario, caseId, runId) {
  exact(report, ["schemaVersion", "caseId", "runId", "scenario", "status", "assertions", "evidence"]);
  assert.equal(report.schemaVersion, 1); assert.equal(report.caseId, caseId); assert.equal(report.runId, runId);
  assert.equal(report.scenario, scenario); assert.equal(report.status, "PASS");
  assert.ok(Array.isArray(report.evidence) && report.evidence.length > 0, "CONTENT_IO_PRODUCT_RAW_MISSING");
  for (const reference of report.evidence) {
    const raw = await artifact(directory, reference);
    assert.equal(raw.schemaVersion, 1); assert.equal(raw.caseId, caseId); assert.equal(raw.runId, runId);
    assert.equal(raw.status, "PASS", "CONTENT_IO_PRODUCT_RAW_FAILED");
  }
  assert.ok(Array.isArray(report.assertions) && report.assertions.length > 0, "CONTENT_IO_PRODUCT_ASSERTIONS_MISSING");
  for (const assertion of report.assertions) {
    exact(assertion, ["name", "expected", "observed"]);
    assert.ok(typeof assertion.name === "string" && assertion.name.length > 0, "CONTENT_IO_PRODUCT_ASSERTION_INVALID");
    assert.deepEqual(assertion.observed, assertion.expected, "CONTENT_IO_PRODUCT_ASSERTION_FAILED");
  }
}
export async function verifyProductEvidence(directory, definition, runId) {
  assert.equal(await realpath(directory), directory, "CONTENT_IO_PRODUCT_DIRECTORY_INVALID");
  assert.match(runId, /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/u, "CONTENT_IO_PRODUCT_RUN_INVALID");
  const proof = await regularJSON(resolve(directory, "content-io-product.json"));
  exact(proof, ["schemaVersion", "caseId", "runId", "status", "providerId", "targetId", "identity", "observedIdentity", "scenarios", "performance"]);
  assert.equal(proof.schemaVersion, 1); assert.equal(proof.caseId, definition.caseId); assert.equal(proof.runId, runId);
  assert.equal(proof.status, "PASS"); assert.equal(proof.providerId, definition.providerId); assert.equal(proof.targetId, definition.targetId);
  exact(proof.identity, ["bundleSha256", "moduleSha256", "workerSha256", "browserSha256", "sourceSha256", "networkSettingsSha256", "observationId"]);
  assert.ok(Object.entries(proof.identity).every(([key, value]) => key === "observationId" ? typeof value === "string" && value.length > 0 : digest(value)), "CONTENT_IO_PRODUCT_IDENTITY_INVALID");
  const observed = await artifact(directory, proof.observedIdentity);
  assert.deepEqual(observed, proof.identity, "CONTENT_IO_PRODUCT_IDENTITY_MISMATCH");
  const expected = await regularJSON(resolve(directory, "expected-identity.json"));
  exact(expected, ["bundleSha256", "baselineBundleSha256", "moduleSha256", "workerSha256", "sourceSha256", "browserSha256"]);
  for (const key of Object.keys(expected).filter(key => key !== "baselineBundleSha256")) assert.equal(proof.identity[key], expected[key], "CONTENT_IO_PRODUCT_IDENTITY_STALE");
  assert.ok(Array.isArray(proof.scenarios), "CONTENT_IO_PRODUCT_SCENARIOS_MISSING");
  assert.deepEqual(proof.scenarios.map(row => row.id).sort(), [...definition.requiredScenarios].sort(), "CONTENT_IO_PRODUCT_SCENARIOS_MISSING");
  for (const row of proof.scenarios) {
    exact(row, ["id", "report"]); await scenarioReport(directory, await artifact(directory, row.report), row.id, proof.caseId, runId);
  }
  const samples = await artifact(directory, proof.performance);
  for (const sample of samples) {
    for (const key of ["browserSha256", "sourceSha256", "networkSettingsSha256", "observationId"]) assert.equal(sample[key], proof.identity[key], "CONTENT_IO_PRODUCT_PERFORMANCE_IDENTITY");
    if (sample.variant === "candidate") assert.equal(sample.providerBundleSha256, proof.identity.bundleSha256, "CONTENT_IO_PRODUCT_PERFORMANCE_IDENTITY");
    if (sample.variant === "baseline") assert.equal(sample.providerBundleSha256, expected.baselineBundleSha256, "CONTENT_IO_PRODUCT_PERFORMANCE_BASELINE_STALE");
  }
  const comparison = compareContentIOPerformance(proof.caseId, samples);
  assert.equal(comparison.status, "PASS", "CONTENT_IO_PRODUCT_PERFORMANCE_REGRESSION");
  return {caseId: proof.caseId, status: "PASS", identity: proof.identity, scenarios: proof.scenarios, comparison};
}
async function main() {
  const {values} = parseArgs({options: {case: {type: "string"}, run: {type: "string"}, directory: {type: "string"}, help: {type: "boolean"}}});
  if (values.help) {console.log("content_io_product_evidence --case ACC-... --run <run UUID> --directory <absolute case output>"); return;}
  assert.ok(isAbsolute(values.directory ?? "") && values.run, "CONTENT_IO_PRODUCT_ARGUMENT_REQUIRED");
  const catalog = JSON.parse(await readFile(new URL("../../tests/fixtures/content-io/product-cases.json", import.meta.url), "utf8"));
  const definition = catalog.cases.find(row => row.caseId === values.case); assert.ok(definition, "CONTENT_IO_PRODUCT_CASE_INVALID");
  console.log(JSON.stringify(await verifyProductEvidence(values.directory, definition, values.run)));
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(error => {console.error(error.message); process.exitCode = 1;});
}
