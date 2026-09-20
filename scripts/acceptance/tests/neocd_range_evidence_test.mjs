import assert from "node:assert/strict";
import {policyContext, blockPolicy} from "./content_policy_fixture.mjs";
import test from "node:test";
import {observeNeoCDRanges} from "../neocd_range_evidence.mjs";
const url = 'http://localhost/disc', sha256 = 'a'.repeat(64);
function emit(context, status, range) {
  const request = {url: () => url, method: () => "GET", allHeaders: async () => ({range, "if-match": `"sha256-${sha256}"`}), sizes: async () => ({responseBodySize: 262144})};
  context.emit("response", {request: () => request, status: () => status, allHeaders: async () => ({"content-length": "262144", "content-range": "bytes 0-262143/1048576", etag: `"sha256-${sha256}"`}), finished: async () => null});
}
test("NeoCD observes the declared disc even when a Worker accidentally omits Range", async () => {
  const context = policyContext(), observer = await observeNeoCDRanges(context);
  observer.register({url, sha256, sizeBytes: 1048576}, 'http://localhost');
  emit(context, 206, 'bytes=0-262143'); assert.deepEqual(await observer.snapshot(url), {requests: 1, bytes: 262144, fetchPolicy: blockPolicy});
  emit(context, 200, undefined); await assert.rejects(observer.verify(url, 1048576), /WHOLE_RESPONSE/);
  observer.close(); assert.equal(context.listenerCount('response'), 0);
});
