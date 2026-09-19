import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {readFile, writeFile} from "node:fs/promises";
import {join} from "node:path";

export const proofDigest = bytes => createHash("sha256").update(bytes).digest("hex");
export function sourceReceiptDigest(receipts) {
  assert.ok(Array.isArray(receipts) && receipts.length > 0, "CONTENT_IO_SOURCE_RECEIPTS_MISSING");
  const canonical = JSON.stringify(receipts.map(row => Object.fromEntries(Object.entries(row).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0))));
  return proofDigest(canonical.replace(/[\u007f-\uffff]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`));
}
export async function readCaseProofInputs(directory, sources) {
  const receipts = JSON.parse(await readFile(join(directory, "input-receipts.json"), "utf8"));
  const expected = JSON.parse(await readFile(join(directory, "expected-identity.json"), "utf8"));
  assert.equal(sourceReceiptDigest(receipts), expected.sourceSha256, "CONTENT_IO_SOURCE_RECEIPTS_CHANGED");
  assert.equal(receipts.length, sources.length, "CONTENT_IO_SOURCE_RECEIPTS_COVERAGE");
  for (const source of sources) {
    const matches = receipts.filter(row => row.role === source.role && row.sha256 === source.sha256 &&
      row.sizeBytes === source.sizeBytes && row.files === 1);
    assert.equal(matches.length, 1, "CONTENT_IO_ACTUAL_SOURCE_MISMATCH");
  }
  return {receipts, expected};
}
export async function proofArtifact(directory, path, value) {
  const bytes = JSON.stringify(value, null, 2) + "\n";
  await writeFile(join(directory, path), bytes, {flag: "wx"});
  return {path, sha256: proofDigest(bytes)};
}
export async function rawProofArtifact(directory, path, caseId, runId) {
  const bytes = await readFile(join(directory, path)), report = JSON.parse(bytes);
  assert.equal(report.schemaVersion, 1); assert.equal(report.caseId, caseId); assert.equal(report.runId, runId);
  assert.equal(report.status, "PASS", "CONTENT_IO_RAW_CASE_FAILED");
  return {report, reference: {path, sha256: proofDigest(bytes)}};
}
export async function proofScenario(directory, caseId, runId, id, evidence, observations) {
  assert.ok(observations.length > 0);
  const assertions = observations.map(([name, expected, observed]) => {
    assert.deepEqual(observed, expected, `CONTENT_IO_SCENARIO_FAILED:${id}:${name}`);
    return {name, expected, observed};
  });
  const report = await proofArtifact(directory, `scenario-${id}.json`, {
    schemaVersion: 1, caseId, runId, scenario: id, status: "PASS", evidence, assertions,
  });
  return {id, report};
}
