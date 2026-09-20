import assert from "node:assert/strict";
const digest = value => typeof value === "string" && /^[a-f0-9]{64}$/u.test(value);
const uuid = value => typeof value === "string" && /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/u.test(value);
const count = value => Number.isSafeInteger(value) && value >= 0;
const peakKeys = ["cacheBytesL1", "cacheBytesL2", "temporaryBytes", "outputCreditBytes", "syncBufferBytes", "inflight", "queued", "waiters", "channels", "leases"];
function exact(value, keys, code) {
  assert.ok(value && typeof value === "object" && !Array.isArray(value), code);
  assert.deepEqual(Object.keys(value).sort(), [...keys].sort(), code);
}
export function validateContentResources(value, closed) {
  exact(value, peakKeys, "CONTENT_IO_PERFORMANCE_PUBLIC_METRICS");
  assert.ok(Object.values(value).every(count), "CONTENT_IO_PERFORMANCE_PUBLIC_METRICS");
  if (closed) assert.ok(Object.values(value).every(number => number === 0), "CONTENT_IO_PERFORMANCE_RESOURCE_LEAK");
  assert.ok(value.cacheBytesL1 + value.cacheBytesL2 <= 16777216 && value.temporaryBytes <= 8388608 &&
    value.outputCreditBytes <= 4194304 && value.outputCreditBytes <= value.temporaryBytes && value.inflight <= 4 &&
    value.queued <= 256 && value.channels <= 8 && value.syncBufferBytes <= 8 * 262208 && value.syncBufferBytes % 262208 === 0,
  "CONTENT_IO_PERFORMANCE_BUDGET_EXCEEDED");
}
function validateMetrics(metrics, variant) {
  exact(metrics, ["firstFrameMs", "inputReadyMs", "exitMs", "rangeRequests", "wholeRequests", "headRequests", "networkBytes", "serverSentBytes",
    "processMemoryBytes", "processMemoryUnavailableReason", "publicPeak", "closed", "materializedBytes", "wasmHeapBytes"], "CONTENT_IO_PERFORMANCE_METRICS");
  for (const key of ["firstFrameMs", "inputReadyMs", "exitMs"]) assert.ok(typeof metrics[key] === "number" && Number.isFinite(metrics[key]) && metrics[key] >= 0, "CONTENT_IO_PERFORMANCE_TIME");
  assert.ok(metrics.inputReadyMs >= metrics.firstFrameMs, "CONTENT_IO_PERFORMANCE_OBSERVATION_ORDER");
  for (const key of ["rangeRequests", "wholeRequests", "headRequests", "networkBytes", "materializedBytes", "wasmHeapBytes"]) assert.ok(count(metrics[key]), "CONTENT_IO_PERFORMANCE_COUNT");
  assert.ok(metrics.serverSentBytes === null || count(metrics.serverSentBytes), "CONTENT_IO_PERFORMANCE_SERVER_BYTES");
  if (metrics.processMemoryBytes === null) assert.ok(typeof metrics.processMemoryUnavailableReason === "string" && metrics.processMemoryUnavailableReason.trim(), "CONTENT_IO_PERFORMANCE_MEMORY_REASON");
  else assert.ok(count(metrics.processMemoryBytes) && metrics.processMemoryBytes > 0 && metrics.processMemoryUnavailableReason === null, "CONTENT_IO_PERFORMANCE_MEMORY");
  if (variant === "candidate") {validateContentResources(metrics.publicPeak, false); validateContentResources(metrics.closed, true);}
  else assert.ok(metrics.publicPeak === null && metrics.closed === null, "CONTENT_IO_BASELINE_HAS_NO_PUBLIC_LAYER");
}
function validateSample(sample, caseId) {
  exact(sample, ["caseId", "runId", "variant", "cacheState", "repetition", "launchId", "contextId", "sourceSha256", "browserSha256", "networkSettingsSha256", "providerBundleSha256", "observationId", "metrics"], "CONTENT_IO_PERFORMANCE_SAMPLE");
  assert.equal(sample.caseId, caseId, "CONTENT_IO_PERFORMANCE_CASE");
  assert.ok(["baseline", "candidate"].includes(sample.variant) && ["cold", "warm"].includes(sample.cacheState), "CONTENT_IO_PERFORMANCE_VARIANT");
  assert.ok(Number.isInteger(sample.repetition) && sample.repetition >= 0 && sample.repetition < 5, "CONTENT_IO_PERFORMANCE_REPETITION");
  assert.ok([sample.runId, sample.launchId, sample.contextId].every(uuid), "CONTENT_IO_PERFORMANCE_ID");
  assert.ok([sample.sourceSha256, sample.browserSha256, sample.networkSettingsSha256, sample.providerBundleSha256].every(digest), "CONTENT_IO_PERFORMANCE_IDENTITY");
  assert.ok(typeof sample.observationId === "string" && sample.observationId.length > 0, "CONTENT_IO_PERFORMANCE_OBSERVATION");
  validateMetrics(sample.metrics, sample.variant);
}
const median = values => [...values].sort((a, b) => a - b)[2];
export function compareContentIOPerformance(caseId, samples) {
  assert.ok(Array.isArray(samples) && samples.length === 20, "CONTENT_IO_PERFORMANCE_REPETITIONS_MISSING");
  for (const sample of samples) validateSample(sample, caseId);
  assert.notEqual(samples.find(sample => sample.variant === "baseline")?.providerBundleSha256,
    samples.find(sample => sample.variant === "candidate")?.providerBundleSha256, "CONTENT_IO_PERFORMANCE_BASELINE_IS_CANDIDATE");
  for (const key of ["runId", "launchId"]) assert.equal(new Set(samples.map(sample => sample[key])).size, 20, "CONTENT_IO_PERFORMANCE_REUSED_RUN");
  for (const key of ["sourceSha256", "browserSha256", "networkSettingsSha256", "observationId"]) {
    assert.equal(new Set(samples.map(sample => sample[key])).size, 1, "CONTENT_IO_PERFORMANCE_INCOMPARABLE");
  }
  const groups = new Map(); const contexts = new Set();
  for (const variant of ["baseline", "candidate"]) {
    const selected = samples.filter(sample => sample.variant === variant);
    assert.equal(selected.length, 10, "CONTENT_IO_PERFORMANCE_REPETITIONS_MISSING");
    assert.equal(new Set(selected.map(sample => sample.providerBundleSha256)).size, 1, "CONTENT_IO_PERFORMANCE_CANDIDATE_CHANGED");
    for (const state of ["cold", "warm"]) {
      const rows = selected.filter(sample => sample.cacheState === state).sort((a, b) => a.repetition - b.repetition);
      assert.deepEqual(rows.map(row => row.repetition), [0, 1, 2, 3, 4], "CONTENT_IO_PERFORMANCE_REPETITIONS_MISSING"); groups.set(`${variant}:${state}`, rows);
    }
    for (let repetition = 0; repetition < 5; repetition++) {
      const cold = groups.get(`${variant}:cold`)[repetition], warm = groups.get(`${variant}:warm`)[repetition];
      assert.equal(cold.contextId, warm.contextId, "CONTENT_IO_PERFORMANCE_WARM_CONTEXT");
      assert.ok(!contexts.has(cold.contextId), "CONTENT_IO_PERFORMANCE_COLD_CONTEXT_REUSED"); contexts.add(cold.contextId);
    }
  }
  const comparisons = [];
  for (const cacheState of ["cold", "warm"]) for (const metric of ["firstFrameMs", "inputReadyMs"]) {
    const baselineMedian = median(groups.get(`baseline:${cacheState}`).map(sample => sample.metrics[metric]));
    const candidateMedian = median(groups.get(`candidate:${cacheState}`).map(sample => sample.metrics[metric]));
    const maximum = baselineMedian * 1.15 + 100;
    comparisons.push({cacheState, metric, baselineMedian, candidateMedian, maximum, passed: candidateMedian <= maximum});
  }
  return {caseId, status: comparisons.every(value => value.passed) ? "PASS" : "FAIL", repetitions: 5, comparisons};
}
