import assert from "node:assert/strict";
import {mkdtempSync, readFileSync, rmSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {spawnSync} from "node:child_process";
import {test} from "node:test";

test("PS2 acceptance records BLOCKED without private inputs and makes no launches", () => {
  const directory = mkdtempSync(join(tmpdir(), "play-acceptance-contract-"));
  const env = {...process.env, RETROM_ACCEPTANCE_CASE_DIR: directory};
  for (const key of Object.keys(env)) {
    if (key.startsWith("RETROM_PLAY_") || key === "RETROM_ACCEPTANCE_BASE_URL") {delete env[key];}
  }
  try {
    const result = spawnSync(process.execPath, ["scripts/acceptance/play_product.mjs"], {env, timeout: 15000});
    assert.equal(result.status, 3, result.stderr?.toString());
    const evidence = JSON.parse(readFileSync(join(directory, "play-product.json")));
    assert.equal(evidence.caseId, "ACC-PS2-001");
    assert.equal(evidence.status, "BLOCKED");
    assert.equal(evidence.errorCode, "PLAY_ACCEPTANCE_INPUT_REQUIRED");
    assert.deepEqual(evidence.launches, []);
    assert.equal(evidence.rangeBytes, 0);
  } finally {rmSync(directory, {recursive: true, force: true});}
});
