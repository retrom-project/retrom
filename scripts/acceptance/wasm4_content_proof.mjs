import assert from "node:assert/strict";
import {readFile, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {contentCaseCommand} from "./content_io_case_command.mjs";
import {proofDigest, readCaseProofInputs, proofArtifact, rawProofArtifact} from "./content_io_case_proof.mjs";
import {wasm4ProofScenarios} from "./wasm4_proof_scenarios.mjs";
import {verifyProductEvidence} from "./content_io_product_evidence.mjs";

const caseId = "ACC-WASM4-001";
async function runSubcases(directory, product, expected) {
  const env = process.env, spec = JSON.parse(await readFile(env.RETROM_CONTENT_IO_PERFORMANCE_INPUT, "utf8"));
  assert.equal(spec.candidate.baseURL, env.RETROM_ACCEPTANCE_BASE_URL);
  assert.equal(spec.candidate.bundleSha256, expected.bundleSha256);
  assert.equal(spec.candidate.moduleSha256, expected.moduleSha256);
  assert.equal(spec.baseline.bundleSha256, expected.baselineBundleSha256);
  const input = {gameId: product.games.owned,
    source: {sha256: product.ownedSource.outputSha256, sizeBytes: product.ownedSource.outputSizeBytes},
    bundleSha256: expected.bundleSha256, moduleSha256: expected.moduleSha256, workerSha256: expected.workerSha256,
    workerPath: resolve(".pfb/workspace/providers/installed/retrom-runtime", expected.bundleSha256, "assets/content-io/worker.mjs")};
  const inputPath = join(directory, "subcase-input.json");
  await writeFile(inputPath, JSON.stringify(input) + "\n", {flag: "wx"});
  contentCaseCommand(directory, "cache-denied", "scripts/acceptance/wasm4_storage_product.mjs", 120000,
    {RETROM_CONTENT_IO_STORAGE_INPUT: inputPath});
  contentCaseCommand(directory, "size-boundaries", "scripts/acceptance/wasm4_boundary_product.mjs", 120000, {});
  contentCaseCommand(directory, "eager-progress", "scripts/acceptance/wasm4_progress_product.mjs", 120000,
    {RETROM_CONTENT_IO_PROGRESS_INPUT: inputPath});
  contentCaseCommand(directory, "performance", "scripts/acceptance/wasm4_performance.mjs", 300000,
    {RETROM_CONTENT_IO_SOURCE_RECEIPTS: join(directory, "input-receipts.json")});
}
export async function completeWasm4ContentProof(directory, product) {
  const runId = process.env.RETROM_CONTENT_IO_RUN_ID;
  assert.match(runId, /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/u);
  const {expected} = await readCaseProofInputs(directory, [
    {role: "owned-game", sha256: product.ownedSource.sourceSha256, sizeBytes: product.ownedSource.sourceSizeBytes},
    {role: "external-game", sha256: product.externalSource.sourceSha256, sizeBytes: product.externalSource.sourceSizeBytes},
  ]);
  for (const runtime of product.runtimes) {
    assert.equal(runtime.bundleSha256, expected.bundleSha256); assert.equal(runtime.moduleSha256, expected.moduleSha256);
  }
  await runSubcases(directory, product, expected);
  const raw = {};
  for (const [name, path] of Object.entries({product: "wasm4-product.json", storage: "cache-denied/storage-product.json",
    boundary: "size-boundaries/boundary-product.json", progress: "eager-progress/progress-product.json", performance: "performance/performance.json"})) {
    raw[name] = await rawProofArtifact(directory, path, caseId, runId);
  }
  const samples = raw.performance.report.samples, measured = raw.progress.report;
  const identity = {bundleSha256: measured.launch.runtime.bundleSha256, moduleSha256: measured.launch.runtime.moduleSha256,
    workerSha256: measured.commit.workerSha256, browserSha256: proofDigest(await readFile(process.env.RETROM_CHROME_EXECUTABLE)),
    sourceSha256: raw.performance.report.sourceReceiptSha256, networkSettingsSha256: samples[0].networkSettingsSha256,
    observationId: raw.performance.report.observationId};
  const proof = {schemaVersion: 1, caseId, runId, status: "PASS", providerId: "retrom-runtime", targetId: "wasm4", identity,
    observedIdentity: await proofArtifact(directory, "observed-identity.json", identity),
    scenarios: await wasm4ProofScenarios(directory, runId, raw, expected),
    performance: await proofArtifact(directory, "performance-samples.json", samples)};
  await proofArtifact(directory, "content-io-product.json", proof);
  const catalog = JSON.parse(await readFile(new URL("../../tests/fixtures/content-io/product-cases.json", import.meta.url), "utf8"));
  await verifyProductEvidence(directory, catalog.cases.find(row => row.caseId === caseId), runId);
}
