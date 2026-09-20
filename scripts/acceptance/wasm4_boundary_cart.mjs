import assert from "node:assert/strict";

function unsignedLEB(value) {
  const bytes = [];
  do {const low = value & 127; value >>>= 7; bytes.push(low | (value ? 128 : 0));} while (value);
  return Buffer.from(bytes);
}

// Empty-name custom sections carry inert padding. Both boundary files remain
// valid executable cartridges; oversized admission must fail for size alone.
export function wasm4BoundaryCart(original, sizeBytes) {
  assert.ok([65536, 65537].includes(sizeBytes), "WASM4_BOUNDARY_SIZE_INVALID");
  assert.ok(WebAssembly.validate(original), "WASM4_BOUNDARY_SOURCE_INVALID");
  for (let lengthBytes = 1; lengthBytes <= 3; lengthBytes++) {
    const payloadLength = sizeBytes - original.length - 1 - lengthBytes;
    if (payloadLength < 1) continue;
    const length = unsignedLEB(payloadLength); if (length.length !== lengthBytes) continue;
    const result = Buffer.concat([original, Buffer.from([0]), length, Buffer.alloc(payloadLength)]);
    assert.equal(result.length, sizeBytes); assert.ok(WebAssembly.validate(result)); return result;
  }
  throw Error("WASM4_BOUNDARY_SOURCE_TOO_LARGE");
}
