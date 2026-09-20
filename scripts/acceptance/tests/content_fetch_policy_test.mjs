import assert from "node:assert/strict";
import test from "node:test";
import {runInNewContext} from "node:vm";
import {observeFetchPolicy, validateFetchExtent, assertNoRepeatedBlocks} from "../content_fetch_policy.mjs";
const B = 262144;
function response(start, end, size) {
  return {status: 206, range: `bytes=${start}-${end}`, contentRange: `bytes ${start}-${end}/${size}`, sizeBytes: end - start + 1};
}
test("network extent validation follows observed thresholds and windows, including cached holes and clipped EOF", () => {
  for (const window of [B, 3 * B, 5 * B, 8 * B]) {
    const policy = {smallFileThresholdBytes: B + 17, networkWindowBytes: window};
    for (const size of [B + 16, B + 17]) {
      assert.deepEqual(validateFetchExtent({status: 200, range: null, contentRange: null, sizeBytes: size}, {sizeBytes: size}, policy), {start: 0, end: size - 1});
    }
    const size = window * 2 + 17;
    assert.deepEqual(validateFetchExtent(response(window, window * 2 - 1, size), {sizeBytes: size}, policy), {start: window, end: window * 2 - 1});
    validateFetchExtent(response(window * 2, size - 1, size), {sizeBytes: size}, policy);
    validateFetchExtent(response(window, window + B - 1, size), {sizeBytes: size}, policy);
    assert.throws(() => validateFetchExtent(response(window - B, window + B - 1, size), {sizeBytes: size}, policy));
    assert.throws(() => validateFetchExtent({status: 200, range: null, contentRange: null, sizeBytes: B + 18}, {sizeBytes: B + 18}, policy));
    assert.throws(() => validateFetchExtent(response(1, B, size), {sizeBytes: size}, policy));
  }
});
test("overlapping windows cannot disguise re-downloaded cached blocks", () => {
  const seen = new Set();
  assertNoRepeatedBlocks([{start: B, end: 2 * B - 1}], "same", seen);
  assert.throws(() => assertNoRepeatedBlocks([{start: 0, end: 3 * B - 1}], "same", seen), /CACHED_BLOCK_REDOWNLOADED/u);
});
test("BOOT probe preserves native call and records only actual numeric policy, surviving page closure", async () => {
  const calls = [], context = {}, policy = {smallFileThresholdBytes: B + 17, networkWindowBytes: 3 * B};
  const realm = {Worker: class {postMessage(...args) {calls.push(args); return 42;}}};
  context.exposeBinding = async (name, handler) => {realm[name] = value => handler({}, value);};
  context.addInitScript = async script => {runInNewContext(`(${script.toString()})()`, realm);};
  const probe = observeFetchPolicy(context); await probe.ready;
  assert.throws(() => probe.read(), /UNOBSERVED/u);
  const worker = new realm.Worker(), transfer = [], message = {v: 1, type: "BOOT", fetchPolicy: policy, secret: "hidden"};
  assert.equal(worker.postMessage(message, transfer), 42);
  assert.equal(calls[0][0], message); assert.equal(calls[0][1], transfer);
  assert.deepEqual(probe.read(), policy); assert.ok(!JSON.stringify(probe.read()).includes("hidden"));
  policy.networkWindowBytes = B; assert.equal(probe.read().networkWindowBytes, 3 * B);
  assert.equal(observeFetchPolicy(context), probe);
  worker.postMessage(message); assert.throws(() => probe.read(), /CHANGED/u);
});
