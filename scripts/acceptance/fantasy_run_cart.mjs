import assert from "node:assert/strict";
import {createHash, randomUUID} from "node:crypto";
import {mkdir, mkdtemp, readFile, rm, writeFile} from "node:fs/promises";
import {join, basename} from "node:path";
import {crc32} from "node:zlib";

const pngSignature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
function ticChunk(bytes) {
  assert.ok(bytes.length <= 65535);
  const header = Buffer.alloc(4); header.writeUInt16LE(bytes.length, 1);
  return Buffer.concat([header, bytes]); // CHUNK_DUMMY, ignored by the native cart loader.
}
function validateTic(bytes) {
  let cursor = 0;
  while (cursor < bytes.length) {
    assert.ok(cursor + 4 <= bytes.length, "FANTASY_TIC_TRUNCATED");
    const type = bytes[cursor] & 31, size = bytes.readUInt16LE(cursor + 1);
    cursor += 4 + (size === 0 && [5, 19].includes(type) ? 65536 : size);
    assert.ok(cursor <= bytes.length, "FANTASY_TIC_TRUNCATED");
  }
}
function pngMetadata(bytes, identity) {
  let cursor = 8;
  while (cursor < bytes.length) {
    assert.ok(cursor + 12 <= bytes.length, "FANTASY_PNG_TRUNCATED");
    const size = bytes.readUInt32BE(cursor), end = cursor + size + 12;
    assert.ok(end <= bytes.length, "FANTASY_PNG_TRUNCATED");
    assert.equal(bytes.readUInt32BE(end - 4), crc32(bytes.subarray(cursor + 4, end - 4)), "FANTASY_PNG_CRC");
    if (bytes.toString("ascii", cursor + 4, cursor + 8) === "IEND") {
      assert.equal(size, 0); assert.equal(end, bytes.length);
      const body = Buffer.from(`tEXtRetromAcceptance\0${identity}`), length = Buffer.alloc(4), checksum = Buffer.alloc(4);
      length.writeUInt32BE(body.length - 4); checksum.writeUInt32BE(crc32(body));
      return Buffer.concat([bytes.subarray(0, cursor), length, body, checksum, bytes.subarray(cursor)]);
    }
    cursor = end;
  }
  throw new Error("FANTASY_PNG_IEND_MISSING");
}
export function fantasyRunCart(core, bytes, identity) {
  assert.match(identity, /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/u);
  if (core === "tic80") {validateTic(bytes); return Buffer.concat([bytes, ticChunk(Buffer.from(`retrom:${identity}`))]);}
  assert.equal(core, "fake08");
  if (bytes.subarray(0, 8).equals(pngSignature)) return pngMetadata(bytes, identity);
  assert.ok(bytes.toString("ascii", 0, 16).startsWith("pico-8 cartridge"), "FANTASY_P8_HEADER");
  return Buffer.concat([bytes, Buffer.from(`\n__retrom_acceptance__\n${identity}\n`)]);
}
export function fantasyBoundaryCart(core, bytes, sizeBytes) {
  assert.ok([4194304, 4194305].includes(sizeBytes), "FANTASY_BOUNDARY_SIZE");
  if (core === "fake08") {
    assert.ok(bytes.toString("ascii", 0, 16).startsWith("pico-8 cartridge"), "FANTASY_P8_HEADER");
    const prefix = Buffer.concat([bytes, Buffer.from("\n__retrom_padding__\n")]);
    assert.ok(prefix.length < sizeBytes); return Buffer.concat([prefix, Buffer.alloc(sizeBytes - prefix.length, 32)]);
  }
  assert.equal(core, "tic80"); validateTic(bytes);
  const parts = [bytes]; let remaining = sizeBytes - bytes.length;
  assert.ok(remaining >= 4);
  while (remaining) {
    let payload = Math.min(65535, remaining - 4), rest = remaining - payload - 4;
    if (rest > 0 && rest < 4) payload -= 4 - rest;
    const chunk = ticChunk(Buffer.alloc(payload)); parts.push(chunk); remaining -= chunk.length;
  }
  return Buffer.concat(parts);
}
export async function withFantasyRunCart(core, filename, consume) {
  await mkdir(".cache", {recursive: true});
  const directory = await mkdtemp(".cache/fantasy-acceptance-"), path = join(directory, basename(filename));
  try {
    const original = await readFile(filename), bytes = fantasyRunCart(core, original, randomUUID());
    const sha = value => createHash("sha256").update(value).digest("hex");
    const receipt = {recipe: core === "tic80" ? "tic-dummy-chunk-v1" : "pico-ignored-metadata-v1",
      sourceSha256: sha(original), sourceSizeBytes: original.length, outputSha256: sha(bytes), outputSizeBytes: bytes.length};
    await writeFile(path, bytes); return await consume(path, receipt);
  } finally {await rm(directory, {recursive: true});}
}
