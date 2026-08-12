import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { readManifest } from "../tools/asset-lib.mjs";

test("asset manifest has unique relative sources and targets", async () => {
  const manifest = await readManifest();
  const targets = new Set();
  const kinds = new Set();
  for (const asset of manifest.assets) {
    assert.equal(path.isAbsolute(asset.source), false);
    assert.equal(path.isAbsolute(asset.target), false);
    assert.equal(asset.target.includes(".."), false);
    assert.match(asset.sha256, /^[0-9a-f]{64}$/);
    assert.equal(targets.has(asset.target), false);
    targets.add(asset.target);
    kinds.add(asset.kind);
  }
  assert.deepEqual([...kinds].sort(), ["rom", "runtime"]);
});
