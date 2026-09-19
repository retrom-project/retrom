import assert from "node:assert/strict";
import {validateFetchExtent, assertNoRepeatedBlocks, validateObservedFetchPolicy} from "./content_fetch_policy.mjs";
const snapshotKeys = ["declaredLargeFileCount", "declaredProjectBytes", "declaredProjectFileCount",
  "fullProjectFileResponseCount", "nativeProjectResponseCount", "projectContentIdentityCount",
  "rangeProjectFileResponseCount", "requestedLargeFileCount", "requestedProjectBytes",
  "requestedProjectFileCount", "runtimeAssetCacheHitCount", "runtimeAssetRequestCount", "runtimeAssetTransferredBytes"];
function exact(value, keys) {
  return value && typeof value === "object" && !Array.isArray(value) &&
    JSON.stringify(Object.keys(value).sort()) === JSON.stringify([...keys].sort());
}
export function assertIndexedLoading(value, range, errorCode) {
  try {
    assert.ok(exact(value, ["schemaVersion", "sameProjectContentIdentity", "sameCoreAssetIdentity",
      "coldVisible", "firstVisible", "restoreVisible", "coreAssetRequests", ...(range ? ["rangeRequests", "fetchPolicies"] : [])]));
    assert.equal(value.schemaVersion, range ? 4 : 2);
    assert.equal(value.sameProjectContentIdentity, true); assert.equal(value.sameCoreAssetIdentity, true);
    for (const snapshot of [value.coldVisible, value.firstVisible, value.restoreVisible]) {
      assert.ok(exact(snapshot, snapshotKeys));
      assert.ok(snapshotKeys.every(key => Number.isSafeInteger(snapshot[key]) && snapshot[key] >= 0));
      assert.equal(snapshot.projectContentIdentityCount, 1); assert.equal(snapshot.nativeProjectResponseCount, 0);
      assert.ok(snapshot.declaredLargeFileCount >= 1 && snapshot.declaredProjectBytes >= 4 * 1024 * 1024);
      assert.ok(snapshot.declaredProjectFileCount >= (range ? 1 : 2));
      assert.ok(snapshot.requestedProjectBytes < snapshot.declaredProjectBytes);
      assert.equal(snapshot.declaredProjectBytes, value.coldVisible.declaredProjectBytes);
      assert.equal(snapshot.declaredProjectFileCount, value.coldVisible.declaredProjectFileCount);
      if (!range) assert.ok(snapshot.requestedLargeFileCount < snapshot.declaredLargeFileCount);
    }
    const cold = value.coldVisible;
    assert.ok(cold.requestedProjectBytes > 0 && cold.requestedProjectFileCount > 0);
    if (range) assert.ok(cold.rangeProjectFileResponseCount > 0 && cold.requestedLargeFileCount > 0);
    else assert.ok(cold.requestedProjectFileCount < cold.declaredProjectFileCount);
    if (range) assertRangeReuse(value);
    else for (const snapshot of [value.firstVisible, value.restoreVisible]) {
      assert.equal(snapshot.requestedProjectBytes, 0); assert.equal(snapshot.requestedProjectFileCount, 0);
      assert.equal(snapshot.rangeProjectFileResponseCount, 0); assert.equal(snapshot.fullProjectFileResponseCount, 0);
      assert.equal(snapshot.requestedLargeFileCount, 0);
    }
    assert.deepEqual(value.coreAssetRequests, {cold: range ? 4 : 0, first: 0, restored: 0});
    if (!range) assert.ok(value.restoreVisible.runtimeAssetCacheHitCount >= 1);
  } catch (cause) {throw new Error(errorCode, {cause});}
}

function assertRangeReuse(value) {
  assert.ok(exact(value.rangeRequests, ["cold", "first", "restored"]));
  assert.ok(exact(value.fetchPolicies, ["cold", "first", "restored"]));
  const seen = new Set();
  for (const [phase, snapshot] of [["cold", value.coldVisible], ["first", value.firstVisible], ["restored", value.restoreVisible]]) {
    const requests = value.rangeRequests[phase];
    assert.ok(Array.isArray(requests));
    const policy = validateObservedFetchPolicy(value.fetchPolicies[phase]);
    assert.equal(requests.filter(item => item.range !== null).length, snapshot.rangeProjectFileResponseCount);
    assert.equal(requests.filter(item => item.range === null).length, snapshot.fullProjectFileResponseCount);
    let bytes = 0;
    for (const request of requests) {
      assert.ok(exact(request, ["sourceKey", "range", "sizeBytes", "sourceSizeBytes"]));
      assert.match(request.sourceKey, /^[0-9a-f]{64}$/u);
      const parts = request.range === null ? null : /^bytes=(\d+)-(\d+)$/u.exec(request.range);
      assert.ok(request.range === null || parts);
      const extent = validateFetchExtent({status: parts ? 206 : 200, range: request.range,
        sizeBytes: request.sizeBytes, contentRange: parts ? `bytes ${parts[1]}-${parts[2]}/${request.sourceSizeBytes}` : null},
      {sizeBytes: request.sourceSizeBytes}, policy);
      assertNoRepeatedBlocks([extent], request.sourceKey, seen);
      bytes += request.sizeBytes;
    }
    assert.equal(bytes, snapshot.requestedProjectBytes);
  }
}
