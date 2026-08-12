export async function sha256Bytes(value) {
  const bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
  const digest = await globalThis.crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export function coreStateBytes(value) {
  const bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
  const signature = new TextDecoder().decode(bytes.subarray(0, 7));
  if (signature !== "RASTATE") return bytes;
  if (bytes[7] !== 1) throw new Error(`Unsupported RASTATE version ${bytes[7]}`);
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  for (let offset = 8; offset + 8 <= bytes.byteLength;) {
    const marker = new TextDecoder().decode(bytes.subarray(offset, offset + 4));
    const size = view.getUint32(offset + 4, true);
    const start = offset + 8;
    const end = start + size;
    if (end > bytes.byteLength) throw new Error("Truncated RASTATE block");
    if (marker === "MEM ") return bytes.subarray(start, end);
    if (marker === "END ") break;
    offset = start + ((size + 7) & ~7);
  }
  throw new Error("RASTATE has no core memory block");
}

export function sha256CoreState(value) {
  return sha256Bytes(coreStateBytes(value));
}
