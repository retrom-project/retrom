import assert from "node:assert/strict";
import {readFile, writeFile} from "node:fs/promises";
import {join} from "node:path";
import {contentCaseCommand} from "./content_io_case_command.mjs";
import {proofDigest, readCaseProofInputs, proofArtifact, proofScenario, rawProofArtifact} from "./content_io_case_proof.mjs";
import {validateContentResources, compareContentIOPerformance} from "./content_io_performance.mjs";
import {verifyProductEvidence} from "./content_io_product_evidence.mjs";

export async function completeDOSContentProof(directory, product) {
  const {caseId, runId} = product, {expected} = await readCaseProofInputs(directory, product.sources);
  const catalog = JSON.parse(await readFile("tests/fixtures/content-io/product-cases.json", "utf8"));
  const definition = catalog.cases.find(row => row.caseId === caseId), inputPath = join(directory, "subcase-input.json");
  await writeFile(inputPath, JSON.stringify({gameId: product.gameId, content: product.content,
    receiptsPath: join(directory, "input-receipts.json")}) + "\n", {flag: "wx"});
  const environment = {RETROM_CONTENT_IO_DOS_INPUT: inputPath}, raw = {};
  for (const name of ["trace", "cache-denied", "fault-range-200", "fault-identity-412", "fault-short-body", "read-exit", "worker-termination"]) {
    const scenario = name === "trace" ? "trace-10000" : name;
    // Startup is separately bounded by the cold case; fault timing is measured
    // only after the game is running, inside each scenario's original deadline.
    const timeout = definition.actions.find(row => row.scenario === scenario).maxWaitMs + (name.startsWith("fault-") || ["read-exit", "worker-termination"].includes(name) ? 120000 : 0);
    contentCaseCommand(directory, name, "scripts/acceptance/dosbox_content_subcases.mjs", timeout, environment, [name]);
    raw[name] = await rawProofArtifact(directory, `${name}/${name}-product.json`, caseId, runId);
  }
  contentCaseCommand(directory, "cache-corruption", "scripts/acceptance/dosbox_cache_corruption.mjs", 120000, environment);
  raw.corruption = await rawProofArtifact(directory, "cache-corruption/cache-corruption-product.json", caseId, runId);
  contentCaseCommand(directory, "performance", "scripts/acceptance/dosbox_performance.mjs",
    definition.actions.find(row => row.scenario === "performance-five-cold-warm").maxWaitMs, environment);
  raw.performance = await rawProofArtifact(directory, "performance/performance-product.json", caseId, runId);
  raw.product = await rawProofArtifact(directory, "dosbox-product.json", caseId, runId);
  const launches = [raw.product, raw.corruption, ...Object.values(raw).filter(row => row !== raw.product && row !== raw.corruption && row !== raw.performance)]
    .flatMap(row => row.report.launches);
  for (const row of launches) {
    assert.equal(row.runtime.bundleSha256, expected.bundleSha256); assert.equal(row.runtime.moduleSha256, expected.moduleSha256);
    assert.ok(row.assets.some(asset => asset.path.endsWith("/client.mjs") && asset.sha256 === expected.moduleSha256));
    assert.ok(row.assets.some(asset => asset.path.endsWith("/assets/content-io/worker.mjs") && asset.sha256 === expected.workerSha256));
    validateContentResources(row.metrics.publicPeak, false); validateContentResources(row.metrics.closed, true);
  }
  const scenarios = [], performance = raw.performance.report;
  const add = async (id, names, observations) => scenarios.push(await proofScenario(directory, caseId, runId, id,
    names.map(name => raw[name].reference), observations));
  await add("preview-publish", ["product"], [["real review and game", true, !!product.import.itemId && !!product.gameId],
    ["native preview shooting", true, product.preview.input.moved.ammo.sha256 !== product.preview.input.fired.ammo.sha256]]);
  await add("cold", ["product"], [["range loading observed", true, product.cold.requests > 0],
    ["no full body", 0, product.launches[0].metrics.wholeRequests]]);
  await add("warm-new-launch", ["product", "performance"], [["new restore Launch", true, product.launches[1].launchId !== product.launches[2].launchId],
    ["restore content body requests", 0, product.launches[2].requests.length],
    ["all candidate warm bodies", [0, 0, 0, 0, 0], performance.samples.filter(row => row.variant === "candidate" && row.cacheState === "warm").map(row => row.metrics.networkBytes)]]);
  await add("save-restore-input", ["product"], [["native checkpoint bytes", product.nativeSave, product.nativeRestore],
    ["restored ammunition", product.savedScene.ammo, product.restoredScene.ammo],
    ["restored view", product.savedScene.wall, product.restoredScene.wall],
    ["restored input fires", true, product.restoredInput.moved.ammo.sha256 !== product.restoredInput.fired.ammo.sha256]]);
  await add("exit", ["product"], [["resources released", true, product.launches.every(row => Object.values(row.metrics.closed).every(value => value === 0))]]);
  await add("metadata-no-body", ["trace"], [["native stat size", product.content.sizeBytes, raw.trace.report.metadata.sizeBytes],
    ["metadata content bodies", 0, raw.trace.report.metadata.bodyRequests]]);
  await add("trace-10000", ["trace"], [["native operations", 10000, raw.trace.report.trace.operations],
    ["boundary and empty reads", true, raw.trace.report.trace.boundaryReads > 0 && raw.trace.report.trace.emptyReads > 0]]);
  await add("concurrent-read", ["trace"], [["overlapping transport", 1, raw.trace.report.concurrent.requests],
    ["only cancelled caller rejects", "CONTENT_IO_ABORTED", raw.trace.report.concurrent.cancelled],
    ["actual native callers", 2, raw.trace.report.concurrent.nativeCallers]]);
  await add("cache-denied", ["cache-denied"], [["two independent new launches", 2, raw["cache-denied"].report.launches.length],
    ["both require network", true, raw["cache-denied"].report.launches.every(row => row.metrics.networkBytes > 0 && row.injection.injected)]]);
  await add("cache-corruption", ["corruption"], [["partial corruption found", true, raw.corruption.report.replay.corruptBlocks > 0],
    ["published generation quarantined", "QUARANTINED", raw.corruption.report.quarantine.state],
    ["leased bytes not repaired in place", raw.corruption.report.published.corruptedSha256, raw.corruption.report.quarantine.sha256]]);
  for (const name of ["fault-range-200", "fault-identity-412", "fault-short-body", "read-exit", "worker-termination"]) {
    const fault = raw[name].report.fault;
    await add(name, [name], [["native read settles unsuccessfully", true, fault.settled.value.code === 29 || fault.settled.value.error === "CONTENT_IO_ABORTED"],
      ["bounded settlement", true, fault.elapsedMs < 30000], ["no partial native success", 0, fault.settled.value.copied ?? 0],
      ...name === "fault-identity-412" ? [["cached empty and later reads revoked", Array(3).fill("CONTENT_IO_IDENTITY_CHANGED"), raw[name].report.revocation.failures],
        ["other object survives before Player teardown", false, raw[name].report.revocation.unrelatedRevoked]] : []]);
  }
  await add("performance-five-cold-warm", ["performance"], [["all observations", 20, performance.samples.length],
    ["original thresholds", "PASS", compareContentIOPerformance(caseId, performance.samples).status]]);
  const identity = {bundleSha256: product.launches[0].runtime.bundleSha256, moduleSha256: product.launches[0].runtime.moduleSha256,
    workerSha256: expected.workerSha256, browserSha256: proofDigest(await readFile(process.env.RETROM_CHROME_EXECUTABLE)),
    sourceSha256: performance.sourceReceiptSha256, networkSettingsSha256: performance.samples[0].networkSettingsSha256, observationId: performance.observationId};
  await proofArtifact(directory, "content-io-product.json", {schemaVersion: 1, caseId, runId, status: "PASS", providerId: "emulatorjs", targetId: "dosbox-pure",
    identity, observedIdentity: await proofArtifact(directory, "observed-identity.json", identity), scenarios,
    performance: await proofArtifact(directory, "performance-samples.json", performance.samples)});
  await verifyProductEvidence(directory, definition, runId);
}
