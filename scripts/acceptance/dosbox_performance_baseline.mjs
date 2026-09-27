import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import {join} from "node:path";
import {proofDigest} from "./content_io_case_proof.mjs";

export async function selectDOSPerformanceBaseline(provider, directory) {
  const changed = JSON.stringify(provider.nativeAssets) !== JSON.stringify(provider.installedNativeAssets);
  if (!changed) return null;
  assert.ok(directory, "DOS_SHARED_CORE_BASELINE_REQUIRED");
  const proof = JSON.parse(await readFile(join(directory, "baseline.json")));
  assert.equal(proof.kind, "RELEASED_ADAPTER_SHARED_CANDIDATE_CORE");
  assert.equal(proof.developmentSha256, provider.developmentSha256);
  assert.equal(proof.installedModuleSha256, provider.identities.baseline.moduleSha256);
  for (const core of proof.coreFiles) {
    const actual = provider.nativeAssets.find(row => row.path === core.path);
    if (core.path.endsWith("-wasm.data")) assert.deepEqual(actual, core);
  }
  const bytes = await readFile(join(directory, "shared-core/client.mjs"));
  assert.equal(proofDigest(bytes), proof.moduleSha256);
  provider.files.baseline.set("client.mjs", bytes);
  provider.identities.baseline.moduleSha256 = proof.moduleSha256;
  return proof;
}
