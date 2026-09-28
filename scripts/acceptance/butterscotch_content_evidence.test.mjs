import assert from "node:assert/strict";
import test from "node:test";
import {projectReadEvidence} from "./butterscotch_content_evidence.mjs";

const base = `https://example.test/runtime/content/project/${"a".repeat(64)}/`;
const range = {url: base + "data.win", status: 206, bytes: 524288,
  range: "bytes 0-524287/8388608", requested: "bytes=0-524287"};
test("counts actual bounded responses and retains large-file whole-download evidence", () => {
  const first = {entries: [range]}, restored = {entries: [{url: base + "index.json", status: 200, bytes: 300}]};
  assert.deepEqual(projectReadEvidence(first, restored), {
    dataWinSizeBytes: 8388608, firstDataWinBytes: 524288, firstDataWinResponseCount: 1,
    firstRangeResponseCount: 1, largestRangeBytes: 524288, largeWholeResponseCount: 0,
    restoreDataWinResponseCount: 0, restoreIndexResponseCount: 1,
    restoreRepeatedBytes: 0,
  });
  assert.equal(projectReadEvidence({entries: [{...range, status: 200, bytes: 8388608, range: null}]}, restored).largeWholeResponseCount, 1);
  assert.equal(projectReadEvidence(first, {entries: [{...restored.entries[0], bytes: 2 * 1024 * 1024}]}).largeWholeResponseCount, 0);
});
test("distinguishes new scene reads from downloading cached ranges again", () => {
  const next = {...range, range: "bytes 524288-1048575/8388608", requested: "bytes=524288-1048575"};
  assert.equal(projectReadEvidence({entries: [range]}, {entries: [next]}).restoreRepeatedBytes, 0);
  assert.equal(projectReadEvidence({entries: [range]}, {entries: [range]}).restoreRepeatedBytes, 524288);
  assert.equal(projectReadEvidence({entries: [range]}, {entries: [range]}, {entries: [range, range]}).restoreRepeatedBytes, 524288);
});
test("rejects malformed, truncated and mismatched Range evidence", () => {
  for (const entry of [{...range, bytes: 10}, {...range, range: null}, {...range, requested: "bytes=1-524288"}]) {
    assert.throws(() => projectReadEvidence({entries: [entry]}, {entries: []}), /RANGE_INVALID/u);
  }
});
