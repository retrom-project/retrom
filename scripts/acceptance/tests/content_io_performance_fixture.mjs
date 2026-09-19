// Synthetic measurements for validator unit tests only; never product evidence.
export const id = value => `00000000-0000-4000-8000-${value.toString(16).padStart(12, "0")}`;
export function performanceFixture() {
  let sequence = 0;
  const zero = {cacheBytesL1: 0, cacheBytesL2: 0, temporaryBytes: 0, outputCreditBytes: 0, syncBufferBytes: 0, inflight: 0, queued: 0, waiters: 0, channels: 0, leases: 0};
  return ["baseline", "candidate"].flatMap((variant, vi) => Array.from({length: 5}, (_, repetition) => ["cold", "warm"].map(cacheState => ({
    caseId: "ACC-TEST-001", runId: id(++sequence), variant, cacheState, repetition, launchId: id(sequence + 100), contextId: id(vi * 5 + repetition + 200),
    sourceSha256: "a".repeat(64), browserSha256: "b".repeat(64), networkSettingsSha256: "c".repeat(64), providerBundleSha256: String(vi).repeat(64), observationId: "fixed-game-scene",
    metrics: {firstFrameMs: 1000.5, inputReadyMs: 1200.5, exitMs: 100.1, rangeRequests: cacheState === "cold" ? 3 : 0, wholeRequests: 0, headRequests: 0, networkBytes: cacheState === "cold" ? 524411 : 0,
      serverSentBytes: null, processMemoryBytes: null, processMemoryUnavailableReason: "Browser process accounting unavailable", publicPeak: variant === "candidate" ? {...zero, temporaryBytes: 262144, inflight: 1} : null,
      closed: variant === "candidate" ? {...zero} : null, materializedBytes: 0, wasmHeapBytes: 65536},
  })))).flat();
}
