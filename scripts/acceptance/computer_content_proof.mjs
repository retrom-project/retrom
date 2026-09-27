import assert from "node:assert/strict";
import {readFile, writeFile} from "node:fs/promises";
import {join} from "node:path";
import {contentCaseCommand} from "./content_io_case_command.mjs";
import {proofDigest, readCaseProofInputs, proofArtifact, proofScenario, rawProofArtifact} from "./content_io_case_proof.mjs";
import {validateContentResources, compareContentIOPerformance} from "./content_io_performance.mjs";
import {verifyProductEvidence} from "./content_io_product_evidence.mjs";

export async function completeComputerContentProof(directory, product, target) {
  const {caseId, runId} = product;
  const catalog = JSON.parse(await readFile("tests/fixtures/content-io/product-cases.json", "utf8"));
  const definition = catalog.cases.find(row => row.caseId === caseId);
  const {expected} = await readCaseProofInputs(directory, product.sources);
  const sources = target === "bbc-jsbeeb" ? product.sources : [product.sources[0], product.sources[2]];
  const inputPath = join(directory, "subcase-input.json");
  await writeFile(inputPath, JSON.stringify({gameId: product.gameId, sources,
    receiptsPath: join(directory, "input-receipts.json")}) + "\n", {flag: "wx"});
  for (const scenario of ["cache-denied", "eager-progress", "performance"]) {
    const timeout = definition.actions.find(row => row.scenario === (scenario === "performance" ? "performance-five-cold-warm" : scenario)).maxWaitMs;
    contentCaseCommand(directory, scenario, "scripts/acceptance/computer_content_subcases.mjs", timeout,
      {RETROM_CONTENT_IO_COMPUTER_INPUT: inputPath}, [target, scenario]);
  }
  contentCaseCommand(directory, "size-boundaries", "scripts/acceptance/computer_boundary_product.mjs", 120000,
    {RETROM_CONTENT_IO_COMPUTER_INPUT: inputPath}, [target]);
  const raw = {};
  for (const [name, path] of Object.entries({product: target === "bbc-jsbeeb" ? "bbc-product.json" : "samcoupe-product.json",
    storage: "cache-denied/cache-denied-product.json", progress: "eager-progress/eager-progress-product.json",
    boundary: "size-boundaries/boundary-product.json", performance: "performance/performance-product.json"})) {
    raw[name] = await rawProofArtifact(directory, path, caseId, runId);
  }
  const progress = raw.progress.report.launches[0], storage = raw.storage.report.launches;
  const performance = raw.performance.report, boundary = raw.boundary.report;
  const launches = [...product.launches, ...storage, progress];
  for (const row of launches) {
    assert.equal(row.runtime.bundleSha256, expected.bundleSha256); assert.equal(row.runtime.moduleSha256, expected.moduleSha256);
    validateContentResources(row.metrics.publicPeak, false); validateContentResources(row.metrics.closed, true);
    assert.ok(row.assets.some(asset => asset.path.endsWith("/client.mjs") && asset.sha256 === expected.moduleSha256));
    assert.ok(row.assets.some(asset => asset.path.endsWith("/assets/content-io/worker.mjs") && asset.sha256 === expected.workerSha256));
  }
  const scenarios = [], total = sources.reduce((sum, row) => sum + row.sizeBytes, 0);
  const add = async (id, names, observations) => scenarios.push(await proofScenario(directory, caseId, runId, id,
    names.map(name => raw[name].reference), observations));
  await add("preview-publish", ["product"], [["imported review approved", true, !!product.gameId],
    ["preview movement observed", true, target === "bbc-jsbeeb" ? product.preview.input.after.paddle > product.preview.input.before.paddle :
      product.preview.input.after.left > product.preview.input.before.left]]);
  const candidate = performance.samples.filter(row => row.variant === "candidate");
  await add("cold", ["product", "performance"], [["original game and firmware bytes", total, product.launches[0].metrics.networkBytes],
    ["five cold content bodies", Array(5).fill(total), candidate.filter(row => row.cacheState === "cold").map(row => row.metrics.networkBytes)]]);
  await add("warm-new-launch", ["product", "performance"], [["repeat launch bytes", 0, product.launches[1].metrics.networkBytes],
    ["five warm launches", [0, 0, 0, 0, 0], candidate.filter(row => row.cacheState === "warm").map(row => row.metrics.networkBytes)],
    ["distinct lifecycle launches", product.launches.length, new Set(product.launches.map(row => row.launchId)).size]]);
  const restore = target === "bbc-jsbeeb" ? [["native checkpoint and full RAM", product.nativeSave, product.nativeRestore],
    ["restored paddle responds", true, product.restoredInput.after.paddle < product.restoredInput.before.paddle]] :
    [["native saved disk", product.disk.after, product.restoredDisk], ["restored program responds to LOAD and RUN", product.output, product.restoredOutput],
      ["real game direction", true, product.game.input.after.left > product.game.input.before.left]];
  await add("save-restore-input", ["product"], restore);
  await add("exit", ["product", "storage", "progress"], [["every final Host CLOSE releases resources", true,
    launches.every(row => Object.values(row.metrics.closed).every(value => value === 0))]]);
  await add("cache-denied", ["storage"], [["two actual denied workers", Array(2).fill({injected: true, opfs: "NotAllowedError", cache: "NotAllowedError"}), storage.map(row => row.injection)],
    ["both launches refetch content", [total, total], storage.map(row => row.metrics.networkBytes)]]);
  await add("eager-progress", ["progress"], [["verified Worker", expected.workerSha256, progress.injection.workerSha256],
    ["downloaded full source", sources[0].sizeBytes, progress.injection.written],
    ["loading remains visible", true, progress.injection.loadingVisible], ["not yet complete", true, progress.injection.percentage < 100],
    ["core not started before commit", 0, progress.injection.canvasCount]]);
  const maximum = target === "bbc-jsbeeb" ? 33554432 : 16777216;
  await add("size-boundaries", ["boundary"], [["actual boundary sizes", [maximum, maximum + 1], boundary.inputs.map(row => row.sizeBytes)],
    ["maximum fully materialized", maximum, boundary.maximum.materialized.written],
    ["oversize stops before native canvas", 0, boundary.rejected.canvasCount],
    ["workers released", true, boundary.inputs.every(row => row.workersCreated === row.workersClosed)]]);
  await add("performance-five-cold-warm", ["performance"], [["twenty fresh comparable observations", 20, performance.samples.length],
    ["median thresholds", "PASS", compareContentIOPerformance(caseId, performance.samples).status]]);
  const identity = {bundleSha256: progress.runtime.bundleSha256, moduleSha256: progress.runtime.moduleSha256,
    workerSha256: progress.injection.workerSha256, browserSha256: proofDigest(await readFile(process.env.RETROM_CHROME_EXECUTABLE)),
    sourceSha256: performance.sourceReceiptSha256, networkSettingsSha256: performance.samples[0].networkSettingsSha256, observationId: performance.observationId};
  await proofArtifact(directory, "content-io-product.json", {schemaVersion: 1, caseId, runId, status: "PASS", providerId: "retrom-runtime", targetId: target,
    identity, observedIdentity: await proofArtifact(directory, "observed-identity.json", identity), scenarios,
    performance: await proofArtifact(directory, "performance-samples.json", performance.samples)});
  await verifyProductEvidence(directory, definition, runId);
}
