import test from "node:test";
import assert from "node:assert/strict";
import {mkdtemp, rm, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {sourceReceiptDigest, readCaseProofInputs, proofScenario, rawProofArtifact} from "../content_io_case_proof.mjs";

const receipt = {id: "operator:owned", role: "owned-game", sha256: "a".repeat(64), sizeBytes: 193, files: 1};
test("[HP-07] UNIT/source receipts match Python canonical JSON including UTF-16 surrogate escaping", () => {
  assert.equal(sourceReceiptDigest([receipt]), "3e4ce9362eb1698ba35431e0dc23553a600b7b1460370e92b6635f76b76598ca");
  assert.equal(sourceReceiptDigest([{...receipt, id: "operator:测试😀"}]), "a62a4a060d0dc118c57cf72c083062f1d75aab603d29093789108f9b727dab64");
  assert.throws(() => sourceReceiptDigest([]), /RECEIPTS_MISSING/u);
});
test("[HP-07] UNIT/source receipts bind actual input bytes and reject a changed receipt", async () => {
  const directory = await mkdtemp(join(tmpdir(), "content-case-input-"));
  const source = {role: receipt.role, sha256: receipt.sha256, sizeBytes: receipt.sizeBytes};
  try {
    await writeFile(join(directory, "input-receipts.json"), JSON.stringify([receipt]));
    await writeFile(join(directory, "expected-identity.json"), JSON.stringify({sourceSha256: sourceReceiptDigest([receipt])}));
    assert.deepEqual((await readCaseProofInputs(directory, [source])).receipts, [receipt]);
    await assert.rejects(readCaseProofInputs(directory, [{...source, sha256: "b".repeat(64)}]), /ACTUAL_SOURCE_MISMATCH/u);
    await assert.rejects(readCaseProofInputs(directory, []), /RECEIPTS_COVERAGE/u);
    await writeFile(join(directory, "input-receipts.json"), JSON.stringify([{...receipt, sizeBytes: 194}]));
    await assert.rejects(readCaseProofInputs(directory, [source]), /RECEIPTS_CHANGED/u);
  } finally {await rm(directory, {recursive: true});}
});
test("[HP-07] UNIT/scenario emission preserves failed observations and rejects stale raw reports", async () => {
  const directory = await mkdtemp(join(tmpdir(), "content-case-report-"));
  try {
    await assert.rejects(proofScenario(directory, "ACC-TEST-001", "run", "cold", [], [["observed bytes", 4, 3]]), /SCENARIO_FAILED/u);
    await writeFile(join(directory, "raw.json"), JSON.stringify({schemaVersion: 1, caseId: "ACC-TEST-001", runId: "old", status: "PASS"}));
    await assert.rejects(rawProofArtifact(directory, "raw.json", "ACC-TEST-001", "current"));
  } finally {await rm(directory, {recursive: true});}
});
