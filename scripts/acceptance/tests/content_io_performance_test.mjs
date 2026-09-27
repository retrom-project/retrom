import {test} from "node:test";
import assert from "node:assert/strict";
import {compareContentIOPerformance} from "../content_io_performance.mjs";
import {id, performanceFixture as fixture} from "./content_io_performance_fixture.mjs";
test("compares all five cold/warm pairs independently, retaining fractional timing and median thresholds", () => {
  const samples = fixture(); samples[10].metrics.inputReadyMs = 100000;
  const result = compareContentIOPerformance("ACC-TEST-001", samples);
  assert.equal(result.status, "PASS"); assert.equal(result.comparisons.length, 4);
  for (const sample of samples.filter(row => row.variant === "candidate" && row.cacheState === "warm")) sample.metrics.inputReadyMs = 2000;
  const failed = compareContentIOPerformance("ACC-TEST-001", samples);
  assert.equal(failed.status, "FAIL"); assert.deepEqual(failed.comparisons.filter(row => !row.passed).map(row => [row.cacheState, row.metric]), [["warm", "inputReadyMs"]]);
});
for (const [name, mutate] of [
  ["missing repetition", rows => rows.pop()], ["duplicate run", rows => {rows[1].runId = rows[0].runId;}],
  ["same baseline and candidate", rows => {for (const row of rows) {row.providerBundleSha256 = "a".repeat(64); row.providerModuleSha256 = "d".repeat(64);}}],
  ["changed loose module", rows => {rows[11].providerModuleSha256 = "f".repeat(64);}],
  ["different input", rows => {rows[1].sourceSha256 = "f".repeat(64);}], ["changed candidate", rows => {rows[11].providerBundleSha256 = "f".repeat(64);}],
  ["different Chrome", rows => {rows[1].browserSha256 = "f".repeat(64);}], ["new warm profile", rows => {rows[1].contextId = id(999);}],
  ["reused cold profile", rows => {rows[2].contextId = rows[0].contextId; rows[3].contextId = rows[0].contextId;}],
  ["fake memory zero", rows => {rows[0].metrics.processMemoryBytes = 0; rows[0].metrics.processMemoryUnavailableReason = null;}],
  ["missing memory reason", rows => {rows[0].metrics.processMemoryUnavailableReason = null;}],
  ["resource leak", rows => {rows[10].metrics.closed.leases = 1;}], ["budget exceeded", rows => {rows[10].metrics.publicPeak.temporaryBytes = 8388609;}],
  ["missing public metrics", rows => {rows[10].metrics.publicPeak = null;}], ["negative bytes", rows => {rows[0].metrics.networkBytes = -1;}],
  ["invalid timing", rows => {rows[0].metrics.firstFrameMs = Infinity;}], ["different endpoint", rows => {rows[1].observationId = "easier-scene";}],
]) test(`rejects ${name} without inventing measurements`, () => {
  const rows = fixture(); mutate(rows); assert.throws(() => compareContentIOPerformance("ACC-TEST-001", rows), /CONTENT_IO_/u);
});
test('compares real loose modules on one installed Bundle without accepting identical module bytes', () => {
  const samples = fixture();
  for (const sample of samples) sample.providerBundleSha256 = 'a'.repeat(64);
  for (const sample of samples.filter(row => row.variant === 'baseline')) {
    sample.metrics.publicPeak = {...samples[10].metrics.publicPeak};
    sample.metrics.closed = {...samples[10].metrics.closed};
  }
  assert.equal(compareContentIOPerformance('ACC-TEST-001', samples).status, 'PASS');
  for (const sample of samples) sample.providerModuleSha256 = 'd'.repeat(64);
  assert.throws(() => compareContentIOPerformance('ACC-TEST-001', samples), /BASELINE_IS_CANDIDATE/u);
});
