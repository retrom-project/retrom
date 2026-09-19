import test from "node:test";
import assert from "node:assert/strict";
import {finalContentMetrics, startContentMeasurement} from "../content_io_measurement.mjs";

function sessions() {
  const counts = {rangeRequests: 2, wholeRequests: 1, headRequests: 0, networkBytes: 524300, materializedBytes: 12};
  for (const key of ["cacheBytesL1", "cacheBytesL2", "temporaryBytes", "outputCreditBytes", "syncBufferBytes",
    "inflight", "queued", "waiters", "channels", "leases"]) {
    counts[key] = 0; counts[`peak${key[0].toUpperCase()}${key.slice(1)}`] = 0;
  }
  counts.peakTemporaryBytes = 524288; counts.peakInflight = 1;
  return {owned: {source: "HOST", lastOperation: "CLOSE", lastCodeNumber: 0, closeCounts: counts}};
}
const measurements = () => ({sessions: sessions(), wasmHeapBytes: 65536, processMemoryBytes: null,
  processMemoryUnavailableReason: "Browser process accounting unavailable", serverSentBytes: null});
test("[HP-03] baseline timing remains measurable without inventing public counters", () => {
  let now = 10;
  const sample = startContentMeasurement({observationId: "scene", now: () => now});
  assert.throws(() => sample.timings(), /EXIT_MISSING/u);
  now = 20; sample.firstFrame(); now = 25; sample.inputReady();
  now = 40; sample.beginExit(); now = 42.5; sample.exited();
  assert.deepEqual(sample.timings(), {firstFrameMs: 10, inputReadyMs: 15, exitMs: 2.5});
});
test("[HP-03] actual clock marks preserve fractional durations and exact final allocation peaks", () => {
  let clock = 50;
  const sample = startContentMeasurement({observationId: "owned-square-direction-confirm-v1", now: () => clock});
  clock = 151.25; sample.firstFrame(); clock = 250.5; sample.inputReady();
  clock = 1000; sample.beginExit(); clock = 1030.125; sample.exited();
  const result = sample.result(measurements());
  assert.equal(result.firstFrameMs, 101.25); assert.equal(result.inputReadyMs, 200.5); assert.equal(result.exitMs, 30.125);
  assert.equal(result.publicPeak.temporaryBytes, 524288); assert.equal(result.closed.temporaryBytes, 0);
  assert.equal(result.networkBytes, 524300); assert.equal(result.processMemoryBytes, null);
});
for (const [name, mutate] of [
  ["missing session", rows => {delete rows.owned;}], ["extra session", rows => {rows.extra = rows.owned;}],
  ["transport-only", rows => {rows.owned.source = "TRANSPORT";}],
  ["missing close", rows => {rows.owned.lastOperation = "READ";}],
  ["failed close", rows => {rows.owned.lastCodeNumber = 4;}],
  ["missing exact peak", rows => {delete rows.owned.closeCounts.peakInflight; rows.owned.observedMax = {inflight: 1};}],
  ["partial final snapshot", rows => {delete rows.owned.closeCounts.channels; rows.owned.latest = {channels: 0};}],
]) test(`[HP-03] rejects ${name} without filling absent measurements`, () => {
  const rows = sessions(); mutate(rows); assert.throws(() => finalContentMetrics(rows), /CONTENT_IO_MEASUREMENT_/u);
});
test("[HP-03] input and exit marks require preceding verified observations", () => {
  const sample = startContentMeasurement({observationId: "scene"});
  assert.throws(() => sample.inputReady(), /INPUT_ORDER/u); assert.throws(() => sample.beginExit(), /EXIT_ORDER/u);
  assert.throws(() => sample.result(measurements()), /EXIT_MISSING/u);
  sample.firstFrame(); assert.throws(() => sample.firstFrame(), /FRAME_REPEATED/u);
});
