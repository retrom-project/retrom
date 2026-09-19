import assert from "node:assert/strict";
import {execFileSync} from "node:child_process";
import {existsSync, mkdtempSync, readFileSync, rmSync} from "node:fs";
import {join} from "node:path";
import test from "node:test";
import {withProjectRunArchive} from "../project_run_archive.mjs";
test("repeat project import changes only owned metadata at the detected project root and cleans its copy", async () => {
  const directory = mkdtempSync(".cache/project-source-test-");
  const archive = join(directory, "owned.zip"); let temporary;
  try {
    execFileSync("python3", ["-c", "import sys,zipfile; z=zipfile.ZipFile(sys.argv[1],'w'); z.writestr('game/data.win',b'owned bytes'); z.close()", archive]);
    const original = readFileSync(archive);
    const inspect = async wrapped => {
      temporary = wrapped;
      return JSON.parse(execFileSync("python3", ["-c", "import sys,zipfile,json; z=zipfile.ZipFile(sys.argv[1]); print(json.dumps({n:z.read(n).decode() for n in z.namelist()}))", wrapped], {encoding: "utf8"}));
    };
    const first = await withProjectRunArchive(archive, "data.win", inspect);
    const second = await withProjectRunArchive(archive, "data.win", inspect);
    assert.equal(first['game/data.win'], "owned bytes");
    assert.equal(second['game/data.win'], first['game/data.win']);
    const marker = Object.keys(first).find(key => key !== "game/data.win");
    assert.match(marker, /^game\/\.retrom-acceptance-.+\.txt$/u);
    assert.equal(Object.keys(first).length, 2); assert.notDeepEqual(first, second);
    assert.deepEqual(readFileSync(archive), original); assert.equal(existsSync(temporary), false);
    await assert.rejects(withProjectRunArchive(archive, "missing", inspect));
  } finally {rmSync(directory, {recursive: true, force: true});}
});
