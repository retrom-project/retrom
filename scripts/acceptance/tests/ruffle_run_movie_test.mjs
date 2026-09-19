import assert from "node:assert/strict";
import {test} from "node:test";
import {deflateSync} from "node:zlib";
import {ruffleMovieTags, ruffleRunMovie} from "../ruffle_run_movie.mjs";
const identity = "11111111-2222-4333-8444-555555555555";
function movie(compressed) {
  // Owned parser bytes: RECT, frame rate/count, FileAttributes, opaque action, End.
  const body = Buffer.from([8, 0, 0, 30, 1, 0, 0x44, 0x11, 8, 0, 0, 0, 0x03, 0x03, 1, 2, 3, 0, 0]);
  const header = Buffer.alloc(8); header.write(compressed ? "CWS" : "FWS"); header[3] = 9; header.writeUInt32LE(body.length + 8, 4);
  return Buffer.concat([header, compressed ? deflateSync(body) : body]);
}
for (const compressed of [false, true]) test(`metadata preserves original frame and executable tags (${compressed ? "CWS" : "FWS"})`, () => {
  const input = movie(compressed), copy = Buffer.from(input), original = ruffleMovieTags(input);
  const bytes = ruffleRunMovie(input, identity), changed = ruffleMovieTags(bytes);
  assert.deepEqual(input, copy); assert.deepEqual(changed.frameHeader, original.frameHeader);
  assert.equal(changed.version, original.version); assert.deepEqual(changed.tags.map(row => row.code), [69, 12, 77, 0]);
  assert.deepEqual(changed.tags[1].bytes, original.tags[1].bytes); assert.equal(changed.tags[0].data[0], 24);
  assert.ok(changed.tags[2].data.toString().includes(identity)); assert.equal(changed.tags[2].data.at(-1), 0);
});
test("exact physical boundary sizes retain the same executable tags", () => {
  const input = movie(false), action = ruffleMovieTags(input).tags[1].bytes;
  for (const size of [64 * 1024 * 1024, 64 * 1024 * 1024 + 1]) {
    const bytes = ruffleRunMovie(input, identity, size); assert.equal(bytes.length, size);
    assert.deepEqual(ruffleMovieTags(bytes).tags[1].bytes, action);
  }
});
test("malformed lengths, truncated tags, missing End and unsupported compression fail", () => {
  const original = movie(false), short = Buffer.from(original); short.writeUInt32LE(original.length - 1, 4);
  assert.throws(() => ruffleRunMovie(short, identity), /LENGTH_INVALID/);
  const truncated = original.subarray(0, -3); truncated.writeUInt32LE(truncated.length, 4);
  assert.throws(() => ruffleRunMovie(truncated, identity), /TAG_TRUNCATED/);
  const unsupported = movie(false); unsupported.write("ZWS");
  assert.throws(() => ruffleRunMovie(unsupported, identity), /FORMAT_UNSUPPORTED/);
});
test("the ASC compiler's complete ShowFrame-at-EOF stream gains metadata and an explicit End", () => {
  const original = movie(false), eof = Buffer.from(original);
  eof.writeUInt16LE(1 << 6, eof.length - 2);
  const derived = ruffleRunMovie(eof, identity), tags = ruffleMovieTags(derived).tags;
  assert.deepEqual(tags.map(row => row.code), [69, 12, 1, 77, 0]);
  assert.deepEqual(tags[1].bytes, ruffleMovieTags(original).tags[1].bytes);
});
