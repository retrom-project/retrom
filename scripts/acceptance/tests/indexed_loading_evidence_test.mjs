import assert from "node:assert/strict";
import {policyContext} from "./content_policy_fixture.mjs";
import test from "node:test";
import {indexedLoading, indexedLoadingEvidence} from "../indexed_loading_evidence.mjs";
const base = "http://localhost", root = `${base}/runtime/content/project/${"a".repeat(64)}/`;
function contextFixture() {
  const context = policyContext();
  context.request = {get: async url => ({status: () => 200, json: async () => url.endsWith("/config") ? {
    resources: [{kind: "FILE_TREE", indexUrl: root + "index.json"}],
    runtime: {runtimeBaseUrl: base + "/runtime/providers/retrom-runtime/bundle/", bundleSha256: "b".repeat(64)},
  } : {files: [{url: root + "game.xp3", sizeBytes: 8388608}]}})};
  return context;
}
function emit(context, status) {
  const request = {url: () => root + "game.xp3", method: () => "GET", allHeaders: async () => ({range: "bytes=0-262143"}),
    sizes: async () => ({responseBodySize: 262144})};
  context.emit("response", {request: () => request, url: request.url, status: () => status,
    allHeaders: async () => ({"content-length": "262144", "content-range": "bytes 0-262143/8388608", etag: '"strong"'}), finished: async () => null});
}
test("indexed launch observes cold Worker Range and preserves current index identity on zero-network restore", async () => {
  const context = contextFixture(), page = {context: () => context, frames: () => []};
  const cold = await indexedLoading(context, "preview", base, "kirikiri"), first = cold.track(page);
  emit(context, 206); const read = await first.snapshot(); first.stop();
  assert.equal(read.evidence.rangeProjectFileResponseCount, 1);
  const restored = await indexedLoading(context, "new-launch", base, "kirikiri"), second = restored.track(page);
  const warm = await second.snapshot(); second.stop();
  assert.equal(warm.evidence.requestedProjectBytes, 0);
  assert.equal(indexedLoadingEvidence(read, warm, warm, [cold.assetIdentity, restored.assetIdentity]).sameProjectContentIdentity, true);
  assert.equal(context.listenerCount("response"), 0);
});
test("indexed Range observer rejects accidental whole GET and changed core identity", async () => {
  const context = contextFixture(), page = {context: () => context, frames: () => []};
  const descriptor = await indexedLoading(context, "preview", base, "kirikiri"), probe = descriptor.track(page);
  emit(context, 200); await assert.rejects(probe.snapshot(), /CONTENT_IO_WHOLE_RESPONSE/); probe.stop();
  const snapshot = {projectContentIdentity: "a", evidence: {}, coreAssetRequests: 0};
  assert.equal(indexedLoadingEvidence(snapshot, snapshot, snapshot, ["a", "b"]).sameCoreAssetIdentity, false);
});
