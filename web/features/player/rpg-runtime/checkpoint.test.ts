import { describe, expect, it } from "vitest";
import { decodeRpgCheckpoint, encodeRpgCheckpoint } from "./checkpoint";

describe("RPG Maker checkpoint codec", () => {
  it("round-trips the canonical EasyRPG bundle", async () => {
    const encoded = await encodeRpgCheckpoint({
      engine: "RPG2000",
      resumeSlot: 100,
      entries: [{ store: "FILESYSTEM", key: "Save/Save100.lsd", mediaType: "application/octet-stream", data: Uint8Array.of(3, 1, 4) }],
    });
    const decoded = await decodeRpgCheckpoint(encoded, "RPG2000");
    expect(decoded.resumeSlot).toBe(100);
    expect([...decoded.entries[0]!.data]).toEqual([3, 1, 4]);
  });

  it("writes the manifest in the Go decoder's RFC 8785 field order", async () => {
    const encoded = await encodeRpgCheckpoint({
      engine: "RPGMV",
      resumeSlot: 21,
      entries: [{ store: "RETROM_NATIVE", key: "save", mediaType: "application/octet-stream", data: Uint8Array.of(7) }],
    });
    const manifestSize = new DataView(encoded.buffer, encoded.byteOffset, encoded.byteLength).getUint32(8);
    const manifest = new TextDecoder().decode(encoded.subarray(12, 12 + manifestSize));
    expect(manifest).toBe([
      `{"engine":"RPGMV","entries":[{"key":"save","mediaType":"application/octet-stream",`,
      `"offset":0,"sha256":"ca358758f6d27e6cf45272937977a748fd88391db679ceda7dc7bf1f005ee879",`,
      `"sizeBytes":1,"store":"RETROM_NATIVE"}],"resumeSlot":21,"schemaVersion":1}`,
    ].join(""));
  });

  it("rejects a changed payload and an engine mismatch", async () => {
    const encoded = await encodeRpgCheckpoint({
      engine: "RPGMZ",
      resumeSlot: 21,
      entries: [{ store: "LOCALFORAGE", key: "file21", mediaType: "application/octet-stream", data: Uint8Array.of(8, 9) }],
    });
    encoded[encoded.length - 1] ^= 1;
    await expect(decodeRpgCheckpoint(encoded, "RPGMZ")).rejects.toThrow("RPG_CHECKPOINT_INVALID");
    await expect(decodeRpgCheckpoint(await encodeRpgCheckpoint({ engine: "RPGMZ", resumeSlot: 21, entries: [] }), "RPGMV"))
      .rejects.toThrow("RPG_CHECKPOINT_INVALID");
  });
});
