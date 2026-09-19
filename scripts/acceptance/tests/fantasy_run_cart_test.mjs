import test from "node:test";
import assert from "node:assert/strict";
import {crc32, deflateSync} from "node:zlib";
import {fantasyControls} from "../../../testdata/public-roms/fantasy-controls/build.mjs";
import {fantasyRunCart, fantasyBoundaryCart} from "../fantasy_run_cart.mjs";

const identity = "01234567-1234-4123-8123-0123456789ab";
for (const core of ["tic80", "fake08"]) {
  test(`${core} import identity preserves every original byte and boundary construction has exact sizes`, () => {
    const original = fantasyControls(core), derived = fantasyRunCart(core, original, identity);
    assert.deepEqual(derived.subarray(0, original.length), original); assert.ok(derived.includes(identity));
    for (const size of [4194304, 4194305]) {
      const bounded = fantasyBoundaryCart(core, derived, size);
      assert.equal(bounded.length, size); assert.deepEqual(bounded.subarray(0, derived.length), derived);
    }
    assert.throws(() => fantasyBoundaryCart(core, original, 4194303), /BOUNDARY_SIZE/u);
  });
}
function pngChunk(type, data) {
  const body = Buffer.concat([Buffer.from(type), data]), size = Buffer.alloc(4), crc = Buffer.alloc(4);
  size.writeUInt32BE(data.length); crc.writeUInt32BE(crc32(body)); return Buffer.concat([size, body, crc]);
}
function ownedPng() {
  const header = Buffer.alloc(13); header.writeUInt32BE(1); header.writeUInt32BE(1, 4); header[8] = 8; header[9] = 6;
  // One owned RGBA pixel. This is a format test, not a PICO-8 game.
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), pngChunk("IHDR", header),
    pngChunk("IDAT", deflateSync(Buffer.from([0, 12, 34, 56, 255]))), pngChunk("IEND", Buffer.alloc(0))]);
}
test("PNG metadata preserves original encoded pixel bytes and inserts one valid ancillary chunk before IEND", () => {
  const original = ownedPng(), output = fantasyRunCart("fake08", original, identity), boundary = original.length - 12;
  assert.deepEqual(output.subarray(0, boundary), original.subarray(0, boundary));
  assert.deepEqual(output.subarray(-12), original.subarray(-12));
  const size = output.readUInt32BE(boundary), body = output.subarray(boundary + 4, boundary + 8 + size);
  assert.equal(body.toString(), `tEXtRetromAcceptance\0${identity}`);
  assert.equal(output.readUInt32BE(boundary + 8 + size), crc32(body));
  assert.equal(output.length, original.length + 12 + size);
});
test("metadata transformation refuses malformed original carts and invalid identities", () => {
  const corrupt = ownedPng(); corrupt[corrupt.length - 1] ^= 1;
  assert.throws(() => fantasyRunCart("fake08", corrupt, identity), /PNG_CRC/u);
  assert.throws(() => fantasyRunCart("fake08", ownedPng().subarray(0, 15), identity), /PNG_TRUNCATED/u);
  assert.throws(() => fantasyRunCart("tic80", Buffer.from([5, 10, 0, 0]), identity), /TIC_TRUNCATED/u);
  assert.throws(() => fantasyRunCart("fake08", Buffer.from("bad cart"), identity), /P8_HEADER/u);
  assert.throws(() => fantasyRunCart("tic80", fantasyControls("tic80"), "invalid"));
});
