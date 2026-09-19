import assert from "node:assert/strict";
import {readFile, realpath, lstat} from "node:fs/promises";
import {dirname, join} from "node:path";
import {proofDigest} from "./content_io_case_proof.mjs";

export function fantasyPerformanceAssets(spec, core) {
  return providerPerformanceAssets(spec, [["module", `assets/${core}/${core}-retrom.mjs`], ["wasm", `assets/${core}/${core}-retrom.wasm`]]);
}
export async function providerPerformanceAssets(spec, entries) {
  assert.equal(await realpath(spec.integrityPath), spec.integrityPath);
  assert.ok((await lstat(spec.integrityPath)).isFile());
  const root = dirname(spec.integrityPath), integrity = JSON.parse(await readFile(spec.integrityPath, "utf8"));
  assert.equal(proofDigest(await readFile(join(root, "client.mjs"))), spec.moduleSha256, "FANTASY_PERFORMANCE_MODULE_BASE_MISMATCH");
  const result = [];
  for (const [id, path] of entries) {
    assert.ok(!path.includes("..") && path.startsWith("assets/"));
    const entry = integrity.files.find(row => row.path === path); assert.ok(entry);
    const bytes = await readFile(join(root, path));
    assert.equal(bytes.length, entry.sizeBytes); assert.equal(proofDigest(bytes), entry.sha256);
    result.push({id, path, sizeBytes: bytes.length, sha256: entry.sha256});
  }
  return result;
}
