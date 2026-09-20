import test from "node:test";
import assert from "node:assert/strict";
import {mkdtemp, mkdir, writeFile, rm} from "node:fs/promises";
import {join} from "node:path";
import {tmpdir} from "node:os";
import {proofDigest} from "../content_io_case_proof.mjs";
import {fantasyPerformanceAssets} from "../fantasy_performance_assets.mjs";

test("performance asset inputs are bound to the exact installed module and every core byte", async () => {
  const root = await mkdtemp(join(tmpdir(), "fantasy-assets-"));
  try {
    await mkdir(join(root, "assets/tic80"), {recursive: true});
    const files = [];
    for (const extension of ["mjs", "wasm"]) {
      const path = `assets/tic80/tic80-retrom.${extension}`, bytes = Buffer.from(`owned ${extension} identity fixture`);
      await writeFile(join(root, path), bytes); files.push({path, sizeBytes: bytes.length, sha256: proofDigest(bytes)});
    }
    const spec = {integrityPath: join(root, "integrity.json"), moduleSha256: proofDigest("owned client")};
    await writeFile(join(root, "client.mjs"), "owned client");
    await writeFile(spec.integrityPath, JSON.stringify({schemaVersion: 1, files}));
    const result = await fantasyPerformanceAssets(spec, "tic80"); assert.equal(result.length, 2);
    await assert.rejects(fantasyPerformanceAssets({...spec, moduleSha256: "f".repeat(64)}, "tic80"), /MODULE_BASE_MISMATCH/u);
    await writeFile(join(root, files[0].path), Buffer.alloc(files[0].sizeBytes));
    await assert.rejects(fantasyPerformanceAssets(spec, "tic80"));
  } finally {await rm(root, {recursive: true});}
});
