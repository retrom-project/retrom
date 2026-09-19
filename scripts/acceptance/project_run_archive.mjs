import {execFileSync} from "node:child_process";
import {randomUUID} from "node:crypto";
import {mkdirSync, mkdtempSync, rmSync} from "node:fs";
import {join} from "node:path";

export async function withProjectRunArchive(archive, entryName, consume) {
  mkdirSync(".cache", {recursive: true});
  const directory = mkdtempSync(join(".cache", "project-acceptance-"));
  const wrapped = join(directory, "project.zip");
  try {
    // Append to a copy: even large compressed game members remain byte-identical.
    execFileSync("python3", ["-c", `
import sys, shutil, zipfile, pathlib
shutil.copyfile(sys.argv[1], sys.argv[2])
with zipfile.ZipFile(sys.argv[2], 'a') as target:
    roots = [pathlib.PurePosixPath(name).parent for name in target.namelist()
             if pathlib.PurePosixPath(name).name.lower() == sys.argv[3].lower()]
    if len(roots) != 1:
        raise ValueError('ACCEPTANCE_PROJECT_ROOT_AMBIGUOUS')
    target.writestr(str(roots[0] / ('.retrom-acceptance-' + sys.argv[4] + '.txt')), sys.argv[4])
`, archive, wrapped, entryName, randomUUID()], {stdio: "pipe", timeout: 60_000});
    return await consume(wrapped);
  } finally {rmSync(directory, {recursive: true, force: true});}
}
