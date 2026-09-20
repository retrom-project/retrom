import assert from "node:assert/strict";
import {proofScenario} from "./content_io_case_proof.mjs";
import {finalContentMetrics} from "./content_io_measurement.mjs";
import {selectedContentBackend} from "./content_store_events.mjs";
import {validateContentResources, compareContentIOPerformance} from "./content_io_performance.mjs";

export async function fantasyProofScenarios(directory, runId, raw, expected) {
  const p = raw.product.report, storage = raw.storage.report, boundary = raw.boundary.report;
  const progress = raw.progress.report.launches[0], performance = raw.performance.report;
  const runtimes = [...p.runtimes, ...storage.launches.map(row => row.runtime), boundary.maximum.runtime,
    boundary.rejected.runtime, progress.runtime];
  for (const runtime of runtimes) {
    assert.equal(runtime.bundleSha256, expected.bundleSha256); assert.equal(runtime.moduleSha256, expected.moduleSha256);
  }
  const closed = p.contentIO.map(row => finalContentMetrics(row.sessions));
  for (const row of [...closed, ...storage.launches.map(row => row.metrics), boundary.maximum.metrics, progress.metrics]) {
    validateContentResources(row.publicPeak, false); validateContentResources(row.closed, true);
  }
  const candidate = performance.samples.filter(row => row.variant === "candidate");
  const assetBytes = performance.assets.candidate.reduce((sum, row) => sum + row.sizeBytes, 0);
  const comparison = compareContentIOPerformance(p.caseId, performance.samples), scenarios = [];
  const add = async (id, names, observations) => scenarios.push(await proofScenario(directory, p.caseId, runId, id,
    names.map(name => raw[name].reference), observations));
  await add("preview-publish", ["product"], [["preview matches initial scene", p.owned.initialState, p.previewState],
    ["published owned and operator games", 2, new Set([p.owned.gameId, p.external.gameId]).size]]);
  await add("cold", ["product", "performance"], [["preview cart requests", 1, p.coldCartRequests],
    ["preview bytes", p.ownedSource.outputSizeBytes, p.coldCartBytes],
    ["five cold game and asset bodies", Array(5).fill(performance.source.sizeBytes + assetBytes),
      candidate.filter(row => row.cacheState === "cold").map(row => row.metrics.networkBytes)]]);
  await add("warm-new-launch", ["product", "performance"], [["restore cart requests", 0, p.warmCartRequests],
    ["five warm game and asset bodies", [0, 0, 0, 0, 0], candidate.filter(row => row.cacheState === "warm").map(row => row.metrics.networkBytes)]]);
  await add("save-restore-input", ["product"], [["restored position and confirm colour", p.owned.savedState, p.owned.restoredState],
    ["different Launch", true, p.owned.originalLaunchId !== p.owned.restoredLaunchId],
    ["confirm changes colour", false, JSON.stringify(p.owned.initialState.color) === JSON.stringify(p.owned.savedState.color)],
    ["restored input moves left", true, p.owned.restoredInputPosition < p.owned.savedPosition]]);
  await add("exit", ["product"], [["formal exits", p.core === "tic80" ? 4 : 5, closed.length],
    ["all resources released", true, closed.every(row => Object.values(row.closed).every(value => value === 0))]]);
  await add("cache-denied", ["storage"], [["two launches", 2, storage.launches.length],
    ["observed API denials", Array(2).fill({injected: true, opfs: "NotAllowedError", cache: "NotAllowedError"}), storage.launches.map(row => row.injection)],
    ["memory backend", ["MEMORY", "MEMORY"], storage.launches.map(row => selectedContentBackend(row.storeEvents))],
    ["both launches fetch game and assets", Array(2).fill(p.ownedSource.outputSizeBytes + assetBytes), storage.launches.map(row => row.metrics.networkBytes)]]);
  await add("size-boundaries", ["boundary"], [["native cart input sizes", [4194304, 4194305], boundary.inputs.map(row => row.sizeBytes)],
    ["maximum and assets materialized", 4194304 + assetBytes, boundary.maximum.metrics.materializedBytes],
    ["oversize rejection", "FANTASY_RUNTIME_CONFIG_INVALID", boundary.rejected.errorCode],
    ["admission failure terminates the Worker", {kind: "FORCED_WORKER_TERMINATION", workersClosed: 1,
      closeMetrics: null, closeMetricsUnavailableReason: "ADMISSION_FAILURE_FORCED_SESSION"}, boundary.rejected.cleanup],
    ["oversize reads no game and releases owner", [0, 0, 0], [boundary.rejected.gameBodyRequests, boundary.rejected.workersRemaining, boundary.rejected.canvasCount]]]);
  await add("eager-progress", ["progress"], [["verified actual Worker", expected.workerSha256, progress.injection.workerSha256],
    ["complete bytes before commit", p.ownedSource.outputSizeBytes, progress.injection.written],
    ["not yet complete progress", true, progress.injection.percentage < 100], ["loading visible", true, progress.injection.loadingVisible],
    ["core not started", 0, progress.injection.canvasCount]]);
  await add("performance-five-cold-warm", ["performance"], [["twenty comparable observations", 20, performance.samples.length],
    ["all median thresholds", "PASS", comparison.status]]);
  return scenarios;
}
