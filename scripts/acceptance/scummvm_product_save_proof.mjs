import {gunzipSync} from "node:zlib";
import assert from "node:assert/strict";
import {createHash} from "node:crypto";

const sha256 = (bytes) => createHash("sha256").update(bytes).digest("hex");

// Acceptance inspects its own downloaded checkpoint. Product code keeps these bytes opaque.
export async function scummvmSaveProof(context, launchId, automatic) {
  const response = await context.request.get(`/runtime/launches/${launchId}/config`);
  assert.equal(response.status(), 200);
  const config = await response.json();
  const restore = config.restore;
  assert.ok(["scummvm-save-bundle-v1", "scummvm-save-bundle-v1-storage-v1"].includes(restore?.format));
  assert(restore.sizeBytes > 12 && restore.sizeBytes <= 64 * 1024 * 1024);
  const downloaded = await context.request.get(restore.url);
  assert.equal(downloaded.status(), 200);
  const stored = await downloaded.body();
  assert.equal(stored.length, restore.sizeBytes);
  assert.equal(sha256(stored), restore.sha256);
  const bytes = restore.format.endsWith("-storage-v1") ? gunzipSync(stored, {maxOutputLength: 64 * 1024 * 1024}) : stored;
  assert.equal(bytes.subarray(0, 8).toString(), "RTSCUMV1");
  const start = 12 + bytes.readUInt32BE(8);
  assert(start > 12 && start < bytes.length);
  const manifest = JSON.parse(bytes.subarray(12, start).toString());
  assert.equal(manifest.schemaVersion, 1);
  assert(/^[0-9a-f]{64}$/u.test(manifest.identity));
  assert(automatic ? Number.isInteger(manifest.resumeSlot) && manifest.resumeSlot >= 0 : manifest.resumeSlot === null);
  assert(manifest.files.length > 0);
  for (const file of manifest.files) {
    const data = bytes.subarray(start + file.offset, start + file.offset + file.size);
    assert.equal(data.length, file.size); assert.equal(sha256(data), file.sha256);
  }
  return {format: restore.format, sizeBytes: stored.length, sha256: restore.sha256,
    resumeSlot: manifest.resumeSlot, fileCount: manifest.files.length};
}
