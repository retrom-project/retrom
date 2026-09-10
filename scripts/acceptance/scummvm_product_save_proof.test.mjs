import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {gzipSync} from "node:zlib";
import test from "node:test";
import {scummvmSaveProof} from "./scummvm_product_save_proof.mjs";

for (const format of ["scummvm-save-bundle-v1", "scummvm-save-bundle-v1-storage-v1"]) {
  test(`ScummVM semantic proof inspects ${format} after verifying stored bytes`, async () => {
    const data = Buffer.from([1, 2, 3]);
    const sha = bytes => createHash("sha256").update(bytes).digest("hex");
    const manifest = Buffer.from(JSON.stringify({schemaVersion: 1, identity: "a".repeat(64), resumeSlot: null,
      files: [{offset: 0, size: data.length, sha256: sha(data)}]}));
    const header = Buffer.alloc(12); header.write("RTSCUMV1"); header.writeUInt32BE(manifest.length, 8);
    const native = Buffer.concat([header, manifest, data]);
    const stored = format.endsWith("-storage-v1") ? gzipSync(native) : native;
    const restore = {format, sizeBytes: stored.length, sha256: sha(stored), url: "/state"};
    const context = {request: {get: async url => ({status: () => 200,
      json: async () => ({restore}), body: async () => {assert.equal(url, "/state"); return stored;}})}};
    assert.deepEqual(await scummvmSaveProof(context, "launch", false),
      {format, sizeBytes: stored.length, sha256: sha(stored), resumeSlot: null, fileCount: 1});
  });
}
