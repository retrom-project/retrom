import assert from "node:assert/strict";
import {randomUUID} from "node:crypto";
import {inflateSync} from "node:zlib";
import {mkdir, mkdtemp, readFile, rm, writeFile} from "node:fs/promises";
import {basename, join} from "node:path";
import {proofDigest} from "./content_io_case_proof.mjs";

const maximum = 64 * 1024 * 1024;
export function ruffleMovieTags(bytes) {
  assert.ok(bytes.length >= 14 && bytes.length <= maximum + 1, "RUFFLE_MOVIE_SIZE_INVALID");
  const signature = bytes.toString("ascii", 0, 3), size = bytes.readUInt32LE(4);
  assert.ok(["FWS", "CWS"].includes(signature) && bytes[3] >= 8, "RUFFLE_MOVIE_FORMAT_UNSUPPORTED");
  assert.ok(size >= 14 && size <= maximum + 1);
  const body = signature === "CWS" ? inflateSync(bytes.subarray(8), {maxOutputLength: maximum - 7}) : bytes.subarray(8);
  assert.equal(body.length + 8, size, "RUFFLE_MOVIE_LENGTH_INVALID");
  let cursor = Math.ceil((5 + 4 * (body[0] >> 3)) / 8) + 4;
  assert.ok(cursor < body.length, "RUFFLE_MOVIE_FRAME_HEADER_INVALID");
  const frameHeader = body.subarray(0, cursor), tags = [];
  while (cursor < body.length) {
    const start = cursor; assert.ok(cursor + 2 <= body.length);
    const header = body.readUInt16LE(cursor), code = header >> 6; cursor += 2;
    let length = header & 63;
    if (length === 63) {assert.ok(cursor + 4 <= body.length); length = body.readUInt32LE(cursor); cursor += 4;}
    assert.ok(cursor + length <= body.length, "RUFFLE_MOVIE_TAG_TRUNCATED");
    const data = body.subarray(cursor, cursor + length); cursor += length;
    tags.push({code, data, bytes: body.subarray(start, cursor)});
    if (code === 0) {assert.equal(length, 0); assert.equal(cursor, body.length); return {version: bytes[3], frameHeader, tags};}
  }
  if (tags.at(-1)?.code === 1 && tags.at(-1).data.length === 0) return {version: bytes[3], frameHeader, tags};
  throw Error("RUFFLE_MOVIE_END_MISSING");
}
export function ruffleRunMovie(original, identity, sizeBytes) {
  assert.match(identity, /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/u);
  const movie = ruffleMovieTags(original);
  if (movie.tags.at(-1).code !== 0) movie.tags.push({code: 0, data: Buffer.alloc(0), bytes: Buffer.alloc(2)});
  const tags = movie.tags.map(tag => {
    const bytes = Buffer.from(tag.bytes);
    if (tag.code === 69) {assert.equal(tag.data.length, 4); bytes[bytes.length - 4] |= 16;}
    return bytes;
  });
  const metadata = Buffer.from(`<retrom-acceptance id="${identity}"/>`);
  const originalSize = 8 + movie.frameHeader.length + tags.reduce((sum, tag) => sum + tag.length, 0);
  const targetSize = sizeBytes ?? originalSize + 6 + metadata.length + 1;
  assert.ok(Number.isSafeInteger(targetSize) && targetSize >= originalSize + 6 + metadata.length + 1 && targetSize <= maximum + 1);
  const payload = Buffer.alloc(targetSize - originalSize - 6, 32); metadata.copy(payload); payload[payload.length - 1] = 0;
  const tagHeader = Buffer.alloc(6); tagHeader.writeUInt16LE((77 << 6) | 63); tagHeader.writeUInt32LE(payload.length, 2);
  const header = Buffer.alloc(8); header.write("FWS"); header[3] = movie.version; header.writeUInt32LE(targetSize, 4);
  return Buffer.concat([header, movie.frameHeader, ...tags.slice(0, -1), tagHeader, payload, tags.at(-1)]);
}
export async function withRuffleRunMovie(filename, consume) {
  await mkdir(".cache", {recursive: true}); const directory = await mkdtemp(".cache/ruffle-acceptance-");
  try {
    const original = await readFile(filename), bytes = ruffleRunMovie(original, randomUUID()), path = join(directory, basename(filename));
    const receipt = {recipe: "swf-native-metadata-v1", sourceSha256: proofDigest(original), sourceSizeBytes: original.length,
      outputSha256: proofDigest(bytes), outputSizeBytes: bytes.length};
    await writeFile(path, bytes); return await consume(path, receipt);
  } finally {await rm(directory, {recursive: true});}
}
