import assert from "node:assert/strict";
import {execFileSync} from "node:child_process";
import {mkdirSync, mkdtempSync, rmSync} from "node:fs";
import {join} from "node:path";
import test from "node:test";
import {withScummvmRunArchive} from "./scummvm_product_source.mjs";

test("repeated imports keep game bytes but have distinct complete file sets and clean staging", async () => {
  mkdirSync(".cache", {recursive: true});
  const directory = mkdtempSync(join(".cache", "scummvm-source-test-"));
  try {
    const archive = join(directory, "owned.zip");
    execFileSync("python3", ["-c", "import sys,zipfile; z=zipfile.ZipFile(sys.argv[1],'w'); z.writestr('game/data.bin',b'owned game fixture'); z.close()", archive]);
    const inspect = (wrapped) => JSON.parse(execFileSync("python3", ["-c", `
import sys,zipfile,json
with zipfile.ZipFile(sys.argv[1]) as z:
    print(json.dumps({name:z.read(name).decode() for name in z.namelist()}))
`, wrapped], {encoding: "utf8"}));
    const first = await withScummvmRunArchive(archive, inspect);
    const second = await withScummvmRunArchive(archive, inspect);
    assert.equal(first['game/data.bin'], "owned game fixture");
    assert.equal(second['game/data.bin'], first['game/data.bin']);
    assert.notEqual(second['.retrom-acceptance-id.txt'], first['.retrom-acceptance-id.txt']);
    assert.deepEqual(Object.keys(first).sort(), ['.retrom-acceptance-id.txt', 'game/data.bin']);
  } finally {rmSync(directory, {recursive: true, force: true});}
});
