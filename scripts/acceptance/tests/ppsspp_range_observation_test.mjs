import {test} from "node:test";
import assert from "node:assert/strict";
import {rangeSummary, assertPartialStartup, isPSPDiscRequest} from "../ppsspp_range_observation.mjs";
const source = {url: "/runtime/content/game/frozen/disc.iso", sizeBytes: 1000000, sha256: "a".repeat(64)};
const response = {path: source.url, status: 206, failure: null, range: "bytes=0-262143", contentRange: "bytes 0-262143/1000000",
  sizeBytes: 262144, etag: `"sha256-${source.sha256}"`};
test("host thumbnails and checkpoints are not counted as disc downloads", () => {
  assert.equal(isPSPDiscRequest("http://localhost/runtime/content/game/frozen/disc.iso"), true);
  for (const path of ["/api/v1/content/blobs/screenshot.png", "/runtime/content/project/frozen/index.json",
    "/runtime/providers/core/assets/ppsspp.wasm", "/runtime/launches/session/save-states/state"]) {
    assert.equal(isPSPDiscRequest("http://localhost" + path), false);
  }
});
test("range evidence rejects full downloads, wrong identities and failed transfers", () => {
  assert.equal(assertPartialStartup(rangeSummary([response], source)).fraction, .262144);
  for (const fields of [{status: 200}, {range: null}, {sizeBytes: 262143}, {etag: 'wrong'}, {failure: 'closed'},
    {range: 'bytes=0-999999', sizeBytes: 1000000, contentRange: 'bytes 0-999999/1000000'}]) {
    assert.throws(() => rangeSummary([{...response, ...fields}], source));
  }
  assert.throws(() => assertPartialStartup({requests: 0, downloadedBytes: 0, discBytes: 1000000}));
  assert.throws(() => assertPartialStartup({requests: 4, downloadedBytes: 1000000, discBytes: 1000000}));
});
