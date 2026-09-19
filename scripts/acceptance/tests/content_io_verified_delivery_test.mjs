import test from "node:test";
import assert from "node:assert/strict";
import {verifiedFetchMetrics} from "../content_io_verified_delivery.mjs";

function fixture() {
  const sources = ["game", "module", "wasm"].map((id, index) => ({id, url: `https://owned.invalid/${id}`,
    sha256: String(index + 1).repeat(64), sizeBytes: (index + 1) * 10}));
  const deliveries = sources.map(({id, sha256, sizeBytes}) => ({id, sha256, sizeBytes}));
  const requests = sources.map(row => ({path: new URL(row.url).pathname, resourceType: "fetch", failure: null,
    status: 200, method: "GET", range: null, sizeBytes: row.sizeBytes}));
  requests.push({...requests[1], resourceType: "script"}); return {sources, deliveries, requests};
}
test("baseline counts all three verified objects while retaining the distinct module import outside content totals", () => {
  const {sources, deliveries, requests} = fixture();
  assert.deepEqual(verifiedFetchMetrics(requests, deliveries, sources), {rangeRequests: 0, wholeRequests: 3, headRequests: 0,
    networkBytes: 60, materializedBytes: 60, publicPeak: null, closed: null});
});
for (const kind of ["missing-asset", "duplicate-fetch", "unknown-kind", "short-body", "failed-body", "wrong-digest"]) {
  test(`baseline observation rejects ${kind} rather than inventing consumed bytes`, () => {
    const {sources, deliveries, requests} = fixture();
    if (kind === "missing-asset") requests.splice(2, 1);
    if (kind === "duplicate-fetch") requests[2] = {...requests[0]};
    if (kind === "unknown-kind") requests[0].resourceType = null;
    if (kind === "short-body") requests[0].sizeBytes--;
    if (kind === "failed-body") requests[0].failure = "ERR_ABORTED";
    if (kind === "wrong-digest") deliveries[0].sha256 = "f".repeat(64);
    assert.throws(() => verifiedFetchMetrics(requests, deliveries, sources));
  });
}
test("explicit legacy cache hits need complete verified deliveries but no fabricated network body", () => {
  const {sources, deliveries} = fixture();
  assert.deepEqual(verifiedFetchMetrics([], deliveries, sources, {cacheHits: true}), {rangeRequests: 0, wholeRequests: 0, headRequests: 0,
    networkBytes: 0, materializedBytes: 60, publicPeak: null, closed: null});
  assert.throws(() => verifiedFetchMetrics([], deliveries.slice(1), sources, {cacheHits: true}));
  assert.throws(() => verifiedFetchMetrics([], deliveries, sources));
});
