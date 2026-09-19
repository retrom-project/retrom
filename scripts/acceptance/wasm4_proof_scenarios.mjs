import assert from "node:assert/strict";
import {proofScenario} from "./content_io_case_proof.mjs";
import {finalContentMetrics} from "./content_io_measurement.mjs";
import {selectedContentBackend} from "./content_store_events.mjs";
import {validateContentResources, compareContentIOPerformance} from "./content_io_performance.mjs";

export async function wasm4ProofScenarios(directory, runId, raw, expected) {
  const p = raw.product.report, storage = raw.storage.report, boundary = raw.boundary.report;
  const progress = raw.progress.report, performance = raw.performance.report;
  const runtimes = [...p.runtimes, ...storage.launches.map(row => row.runtime), boundary.maximum.runtime,
    boundary.rejected.runtime, progress.launch.runtime];
  for (const runtime of runtimes) {
    assert.equal(runtime.bundleSha256, expected.bundleSha256); assert.equal(runtime.moduleSha256, expected.moduleSha256);
  }
  const closed = p.contentIO.map(row => finalContentMetrics(row.sessions));
  for (const row of [...closed, ...storage.launches.map(row => row.metrics), boundary.maximum.metrics, progress.launch.metrics]) {
    validateContentResources(row.publicPeak, false); validateContentResources(row.closed, true);
  }
  const candidate = performance.samples.filter(row => row.variant === "candidate");
  const comparison = compareContentIOPerformance(p.caseId, performance.samples);
  const scenarios = [];
  const add = async (id, names, observations) => scenarios.push(await proofScenario(directory, p.caseId, runId, id,
    names.map(name => raw[name].reference), observations));
  await add("preview-publish", ["product"], [["preview square", {x: 20, y: 20}, p.previewPosition],
    ["published owned and operator games", 2, new Set(Object.values(p.games)).size]]);
  await add("cold", ["product", "performance"], [["preview cart requests", 1, p.coldCartRequests],
    ["preview bytes", p.ownedSource.outputSizeBytes, p.coldCartBytes],
    ["five cold cart bodies", Array(5).fill(performance.source.sizeBytes), candidate.filter(row => row.cacheState === "cold").map(row => row.metrics.networkBytes)]]);
  await add("warm-new-launch", ["product", "performance"], [["restore cart requests", 0, p.warmCartRequests],
    ["five warm cart bodies", [0, 0, 0, 0, 0], candidate.filter(row => row.cacheState === "warm").map(row => row.metrics.networkBytes)]]);
  await add("save-restore-input", ["product"], [["restored square", p.checkpoint.savedPosition, p.checkpoint.restoredPosition],
    ["different Launch", true, p.checkpoint.originalLaunchId !== p.checkpoint.restoredLaunchId],
    ["restored input moves left", true, p.checkpoint.restoredInputPosition.x < p.checkpoint.savedPosition.x],
    ["checkpoint contains bytes", true, p.checkpoint.sizeBytes > 0]]);
  await add("exit", ["product"], [["four formal exits", 4, closed.length],
    ["all resources released", true, closed.every(row => Object.values(row.closed).every(value => value === 0))]]);
  await add("cache-denied", ["storage"], [["two launches", 2, storage.launches.length],
    ["observed API denials", Array(2).fill({injected: true, opfs: "NotAllowedError", cache: "NotAllowedError"}), storage.launches.map(row => row.denial)],
    ["memory backend", ["MEMORY", "MEMORY"], storage.launches.map(row => selectedContentBackend(row.storeEvents))],
    ["both launches fetch cart", Array(2).fill(p.ownedSource.outputSizeBytes), storage.launches.map(row => row.metrics.networkBytes)]]);
  await add("size-boundaries", ["boundary"], [["valid Wasm input sizes", [65536, 65537], boundary.inputs.map(row => row.sizeBytes)],
    ["maximum materialized", 65536, boundary.maximum.metrics.materializedBytes],
    ["oversize rejection", "PROVIDER_LAUNCH_REQUEST_INVALID", boundary.rejected.errorCode],
    ["oversize creates no content or core", [0, 0, 0], [boundary.rejected.gameBodyRequests, boundary.rejected.workersCreated, boundary.rejected.canvasCount]]]);
  await add("eager-progress", ["progress"], [["verified actual Worker", expected.workerSha256, progress.commit.workerSha256],
    ["complete bytes before commit", p.ownedSource.outputSizeBytes, progress.commit.written],
    ["not yet complete progress", true, progress.commit.percentage < 100], ["loading visible", true, progress.commit.loadingVisible],
    ["core not started", 0, progress.commit.canvasCount]]);
  await add("performance-five-cold-warm", ["performance"], [["twenty comparable observations", 20, performance.samples.length],
    ["all median thresholds", "PASS", comparison.status]]);
  return scenarios;
}
