import { describe, expect, it, vi } from "vitest";
import { newUuid, sha256 } from "./crypto";

function hex(bytes: Uint8Array) {
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
}

describe("newUuid", () => {
  it("creates an RFC 4122 UUIDv4 when randomUUID is unavailable", () => {
    const provider = {
      getRandomValues<T extends ArrayBufferView>(array: T) {
        new Uint8Array(array.buffer, array.byteOffset, array.byteLength).set(Array.from({ length: 16 }, (_, index) => index));
        return array;
      },
    };
    expect(newUuid(provider)).toBe("00010203-0405-4607-8809-0a0b0c0d0e0f");
  });

  it("uses the native implementation when available", () => {
    const randomUUID = vi.fn(() => "123e4567-e89b-42d3-a456-426614174000");
    const provider = { getRandomValues: vi.fn(), randomUUID };
    expect(newUuid(provider)).toBe("123e4567-e89b-42d3-a456-426614174000");
    expect(randomUUID).toHaveBeenCalledOnce();
    expect(provider.getRandomValues).not.toHaveBeenCalled();
  });
});

describe("sha256", () => {
  it.each([
    ["", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"],
    ["abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"],
    ["The quick brown fox jumps over the lazy dog", "d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592"],
  ])("hashes with the insecure-context fallback: %s", async (value, expected) => {
    const provider = { getRandomValues: <T extends ArrayBufferView>(array: T) => array };
    expect(hex(await sha256(new TextEncoder().encode(value), provider))).toBe(expected);
  });

  it("hashes a multi-block upload payload with the standard million-a vector", async () => {
    const provider = { getRandomValues: <T extends ArrayBufferView>(array: T) => array };
    const payload = new Uint8Array(1_000_000).fill(0x61);
    expect(hex(await sha256(payload, provider))).toBe("cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0");
  });
});
