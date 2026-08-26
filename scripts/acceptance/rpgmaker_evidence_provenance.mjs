import { createHash } from "node:crypto";
import { existsSync, lstatSync, realpathSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, resolve } from "node:path";
import { spawnSync } from "node:child_process";


export function gitProvenance() {
  const commit = git(["rev-parse", "HEAD"]).trim() || "UNBORN";
  const entries = git(["status", "--porcelain=v1", "--untracked-files=all"])
    .split("\n").filter(Boolean).map((line) => {
      if (line.length < 4 || line[2] !== " ") { throw new Error("RPG_PROVENANCE_GIT_STATUS_INVALID"); }
      return { status: line.slice(0, 2), path: line.slice(3) };
    }).sort((left, right) => `${left.path}\0${left.status}`.localeCompare(`${right.path}\0${right.status}`));
  const encoded = JSON.stringify(entries);
  return {
    gitCommit: commit, gitDirty: entries.length > 0,
    gitDirtySummary: {
      fileCount: entries.length,
      sha256: createHash("sha256").update(encoded).digest("hex"), entries,
    },
  };
}


export function writeExclusiveJson(path, payload, code) {
  if (!isAbsolute(path) || existsSync(path)) { throw new Error(code); }
  const parent = resolve(dirname(path));
  if (!lstatSync(parent).isDirectory() || realpathSync(parent) !== parent) { throw new Error(code); }
  writeFileSync(path, `${JSON.stringify(payload, null, 2)}\n`, { encoding: "utf8", flag: "wx", mode: 0o600 });
}


function git(arguments_) {
  const result = spawnSync("git", arguments_, { cwd: resolve("."), encoding: "utf8" });
  if (result.status !== 0 || result.error) { throw new Error("RPG_PROVENANCE_GIT_FAILED"); }
  return result.stdout;
}
