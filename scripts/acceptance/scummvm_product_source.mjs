import {execFileSync} from "node:child_process";
import {randomUUID} from "node:crypto";
import {mkdirSync, mkdtempSync, rmSync} from "node:fs";
import {join} from "node:path";

export async function withScummvmRunArchive(archive, consume) {
  // Retrom intentionally discards already published file sets. Give this run its
  // own harmless metadata member while preserving every original game byte.
  mkdirSync(".cache", {recursive: true});
  const directory = mkdtempSync(join(".cache", "scummvm-acceptance-"));
  const wrapped = join(directory, "scummvm-public-game.zip");
  try {
    execFileSync("python3", ["-c", `
import sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as source, zipfile.ZipFile(sys.argv[2], 'w', zipfile.ZIP_DEFLATED) as target:
    for member in source.infolist():
        target.writestr(member, source.read(member))
    target.writestr('.retrom-acceptance-id.txt', sys.argv[3])
`, archive, wrapped, randomUUID()], {stdio: "pipe", timeout: 30_000});
    return await consume(wrapped);
  } finally {rmSync(directory, {recursive: true, force: true});}
}
