import assert from "node:assert/strict";
import {spawnSync} from "node:child_process";
import {closeSync, openSync} from "node:fs";
import {join} from "node:path";

// Children inherit the original Case process group and its unchanged hard timeout.
export function contentCaseCommand(directory, name, script, timeout, environment, args = []) {
  const stdout = openSync(join(directory, `${name}.stdout.log`), "wx");
  const stderr = openSync(join(directory, `${name}.stderr.log`), "wx");
  try {
    const result = spawnSync(process.execPath, [script, ...args], {timeout, stdio: ["ignore", stdout, stderr],
      env: {...process.env, ...environment, RETROM_ACCEPTANCE_CASE_DIR: join(directory, name)}});
    assert.ok(!result.error, `CONTENT_IO_CHILD_ERROR:${name}:${result.error?.code}`);
    assert.equal(result.status, 0, `CONTENT_IO_CHILD_FAILED:${name}`);
  } finally {closeSync(stdout); closeSync(stderr);}
}
