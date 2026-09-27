import assert from "node:assert/strict";
import {validateContentResources} from "./content_io_performance.mjs";

export function validateDOSForcedCleanup(cleanup) {
  assert.equal(cleanup.mode, "FORCED_WORKER_TERMINATION");
  assert.equal(cleanup.worker.workerClosed, true, "DOS_WORKER_CLOSE_NOT_OBSERVED");
  assert.match(cleanup.worker.workerSha256, /^[a-f0-9]{64}$/u);
  const names = ["active", "queued", "pending", "waiters", "lruBytes", "channels", "files", "managementPending", "jobs", "syncChannels"];
  assert.deepEqual(Object.keys(cleanup.clientResources).sort(), names.sort());
  for (const count of Object.values(cleanup.clientResources)) assert.equal(count, 0, "DOS_CLIENT_RESOURCE_LEAK");
}

export async function collectDOSFailure(opened, collector, worker, retainedResources) {
  // Force termination cannot emit the Worker's normal final CLOSE diagnostic.
  // Prove its actual browser close and inspect surviving client allocations;
  // keep last diagnostics as observations, never manufacture zero Worker counts.
  const clientResources = retainedResources ?? await opened.page.evaluate(() => globalThis.__dosContentAcceptance.resources());
  const cleanup = {mode: "FORCED_WORKER_TERMINATION", worker, clientResources};
  validateDOSForcedCleanup(cleanup);
  const sessions = collector.snapshot(opened.page), rows = Object.values(sessions);
  assert.equal(rows.length, 1); assert.equal(rows[0].source, "HOST");
  const names = ["cacheBytesL1", "cacheBytesL2", "temporaryBytes", "outputCreditBytes", "syncBufferBytes", "inflight", "queued", "waiters", "channels", "leases"];
  const publicPeak = Object.fromEntries(names.map(name => [name, rows[0].observedMax[`peak${name[0].toUpperCase()}${name.slice(1)}`]]));
  validateContentResources(publicPeak, false);
  const metrics = {publicPeak, observation: "LAST_DIAGNOSTICS_BEFORE_FORCED_WORKER_TERMINATION"};
  const requests = structuredClone(opened.network.requests).map(row => ({...row,
    ...(row.transferredBytes === null ? {transferUnavailableReason: "WORKER_CLOSED_BEFORE_BROWSER_COMPLETION"} : {})}));
  const result = {launchId: opened.launch.launchId, runtime: {
    bundleSha256: opened.config.runtime.bundleSha256, moduleSha256: opened.config.runtime.moduleSha256},
  assets: opened.assets, requests, sessions, metrics, cleanup};
  await opened.dispose(); await opened.page.close(); return result;
}
