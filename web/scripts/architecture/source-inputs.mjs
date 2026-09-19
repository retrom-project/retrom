import { createHash } from "node:crypto";
import { lstatSync, readFileSync } from "node:fs";
import path from "node:path";

export function checkedText(root, name) {
  const absolute = path.resolve(root, name);
  const relative = path.relative(root, absolute);
  if (relative === ".." || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    throw new Error("architecture: source outside repository");
  }
  let current = root;
  for (const part of relative.split(path.sep)) {
    current = path.join(current, part);
    if (lstatSync(current).isSymbolicLink()) {
      throw new Error(`architecture: symlinked source ${name}`);
    }
  }
  return readFileSync(absolute, "utf8");
}

export function contentDigest(text) {
  return createHash("sha256").update(text).digest("hex");
}

export function compiledSourcePaths(root, program) {
  return program.getSourceFiles().map((source) => path.relative(root, source.fileName).split(path.sep).join("/"))
    .filter((name) => name !== ".." && !name.startsWith("../") && !path.isAbsolute(name) &&
      !name.split("/").includes("node_modules") && /\.(?:ts|tsx|js|jsx|mjs|cjs)$/.test(name));
}
