import test from "node:test";
import assert from "node:assert/strict";
import {mkdtemp, mkdir, writeFile, rm} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {proofDigest} from "../content_io_case_proof.mjs";
import {selectDOSPerformanceBaseline} from "../dosbox_performance_baseline.mjs";

test("Shared-core performance requires a frozen rebuilt baseline matching the current candidate", async t => {
  const directory = await mkdtemp(join(tmpdir(), "dos-baseline-")); t.after(() => rm(directory, {recursive: true}));
  const native = {path: "assets/dosbox_pure-thread-wasm.data", sha256: proofDigest("new core"), sizeBytes: 8};
  const provider = () => ({nativeAssets: [native], installedNativeAssets: [{...native, sha256: proofDigest("old core")}],
    developmentSha256: proofDigest("candidate"), files: {baseline: new Map()}, identities: {baseline: {moduleSha256: proofDigest("released")}}});
  await assert.rejects(selectDOSPerformanceBaseline(provider()), /BASELINE_REQUIRED/u);
  await mkdir(join(directory, "shared-core")); await writeFile(join(directory, "shared-core/client.mjs"), "rebuilt");
  const proof = {kind: "RELEASED_ADAPTER_SHARED_CANDIDATE_CORE", developmentSha256: proofDigest("candidate"),
    installedModuleSha256: proofDigest("released"), moduleSha256: proofDigest("rebuilt"), coreFiles: [native]};
  const publish = () => writeFile(join(directory, "baseline.json"), JSON.stringify(proof)); await publish();
  const selected = provider(); await selectDOSPerformanceBaseline(selected, directory);
  assert.equal(selected.identities.baseline.moduleSha256, proofDigest("rebuilt"));
  assert.equal(selected.files.baseline.get("client.mjs").toString(), "rebuilt");
  proof.developmentSha256 = proofDigest("another candidate"); await publish();
  await assert.rejects(selectDOSPerformanceBaseline(provider(), directory));
  proof.developmentSha256 = proofDigest("candidate"); await publish();
  await writeFile(join(directory, "shared-core/client.mjs"), "different adapter");
  await assert.rejects(selectDOSPerformanceBaseline(provider(), directory));
});
