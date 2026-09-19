import assert from "node:assert/strict";
import {policyContext} from "./content_policy_fixture.mjs";
import test from "node:test";
import {observePlayLoading} from "../play_loading_evidence.mjs";
const digest = 'a'.repeat(64), url = 'http://localhost/disc';
async function fixture() {
  const context = policyContext(), source = {role: "game", kind: "SEEKABLE_BLOB", url, sha256: digest, sizeBytes: 1048576};
  context.request = {get: async () => ({status: () => 200, json: async () => ({runtime: {targetId: "play-ps2"}, resources: [source]})})};
  const evidence = {rangeRequests: 0, rangeBytes: 0};
  return {context, evidence, observer: await observePlayLoading(context, "http://localhost", {launchId: 'launch'}, evidence)};
}
function response(context, status) {
  const request = {url: () => url, method: () => "GET", allHeaders: async () => ({range: "bytes=0-262143", "if-match": `"sha256-${digest}"`}), sizes: async () => ({responseBodySize: 262144})};
  context.emit("response", {request: () => request, status: () => status, allHeaders: async () => ({"content-length": "262144", "content-range": "bytes 0-262143/1048576", etag: `"sha256-${digest}"`}), finished: async () => null});
}
test("Play loading includes Worker responses, counts each once and rejects accidental whole-body requests", async () => {
  const {context, evidence, observer} = await fixture();
  response(context, 206); await observer.snapshot(); await observer.snapshot();
  assert.deepEqual(evidence, {rangeRequests: 1, rangeBytes: 262144}); observer.close();
  assert.equal(context.listenerCount('response'), 0);
  const bad = await fixture(); response(bad.context, 200);
  await assert.rejects(bad.observer.snapshot(), /WHOLE_RESPONSE/); bad.observer.close();
});
