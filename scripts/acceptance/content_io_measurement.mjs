import assert from "node:assert/strict";

const resources = ["cacheBytesL1", "cacheBytesL2", "temporaryBytes", "outputCreditBytes", "syncBufferBytes",
  "inflight", "queued", "waiters", "channels", "leases"];
const counters = ["rangeRequests", "wholeRequests", "headRequests", "networkBytes", "materializedBytes"];
const count = value => Number.isSafeInteger(value) && value >= 0;

// Use the actual complete final Host message. Merged historical counters cannot prove cleanup.
export function finalContentMetrics(sessions) {
  const rows = Object.values(sessions);
  assert.equal(rows.length, 1, "CONTENT_IO_MEASUREMENT_SESSION_COUNT");
  const session = rows[0];
  assert.equal(session.source, "HOST", "CONTENT_IO_MEASUREMENT_HOST_MISSING");
  assert.equal(session.lastOperation, "CLOSE", "CONTENT_IO_MEASUREMENT_CLOSE_MISSING");
  assert.equal(session.lastCodeNumber, 0, "CONTENT_IO_MEASUREMENT_CLOSE_FAILED");
  const counts = session.closeCounts;
  assert.ok(counts && typeof counts === "object", "CONTENT_IO_MEASUREMENT_CLOSE_MISSING");
  const peak = {}, closed = {}, totals = {};
  for (const key of resources) {
    const peakKey = `peak${key[0].toUpperCase()}${key.slice(1)}`;
    assert.ok(count(counts[key]) && count(counts[peakKey]), `CONTENT_IO_MEASUREMENT_MISSING:${key}`);
    closed[key] = counts[key]; peak[key] = counts[peakKey];
  }
  for (const key of counters) {
    assert.ok(count(counts[key]), `CONTENT_IO_MEASUREMENT_MISSING:${key}`); totals[key] = counts[key];
  }
  return {...totals, publicPeak: peak, closed};
}

// Start immediately before navigating to the Launch. The case calls these marks only after
// its fixed scene and direction+confirm assertions succeed, and after the actual exit finishes.
export function startContentMeasurement({observationId, now = () => performance.now()}) {
  assert.ok(typeof observationId === "string" && observationId.length > 0, "CONTENT_IO_MEASUREMENT_OBSERVATION_REQUIRED");
  const started = now(); let firstFrameMs, inputReadyMs, exitStarted, exitMs;
  const elapsed = origin => {
    const value = now() - origin;
    assert.ok(Number.isFinite(value) && value >= 0, "CONTENT_IO_MEASUREMENT_CLOCK_INVALID"); return value;
  };
  const timings = () => {
    assert.ok(exitMs !== undefined, "CONTENT_IO_MEASUREMENT_EXIT_MISSING");
    return {firstFrameMs, inputReadyMs, exitMs};
  };
  return {
    timings,
    firstFrame() {
      assert.equal(firstFrameMs, undefined, "CONTENT_IO_MEASUREMENT_FRAME_REPEATED"); firstFrameMs = elapsed(started);
    },
    inputReady() {
      assert.ok(firstFrameMs !== undefined && inputReadyMs === undefined, "CONTENT_IO_MEASUREMENT_INPUT_ORDER"); inputReadyMs = elapsed(started);
    },
    beginExit() {
      assert.ok(inputReadyMs !== undefined && exitStarted === undefined, "CONTENT_IO_MEASUREMENT_EXIT_ORDER"); exitStarted = now();
    },
    exited() {
      assert.ok(exitStarted !== undefined && exitMs === undefined, "CONTENT_IO_MEASUREMENT_EXIT_ORDER"); exitMs = elapsed(exitStarted);
    },
    result({sessions, wasmHeapBytes, processMemoryBytes, processMemoryUnavailableReason, serverSentBytes}) {
      assert.ok(exitMs !== undefined, "CONTENT_IO_MEASUREMENT_EXIT_MISSING");
      assert.ok(count(wasmHeapBytes), "CONTENT_IO_MEASUREMENT_HEAP_MISSING");
      assert.ok(serverSentBytes === null || count(serverSentBytes), "CONTENT_IO_MEASUREMENT_SERVER_BYTES");
      if (processMemoryBytes === null) assert.ok(typeof processMemoryUnavailableReason === "string" && processMemoryUnavailableReason.trim(), "CONTENT_IO_MEASUREMENT_MEMORY_REASON");
      else assert.ok(count(processMemoryBytes) && processMemoryBytes > 0 && processMemoryUnavailableReason === null, "CONTENT_IO_MEASUREMENT_MEMORY_INVALID");
      return {...timings(), ...finalContentMetrics(sessions), wasmHeapBytes,
        processMemoryBytes, processMemoryUnavailableReason, serverSentBytes};
    },
  };
}
