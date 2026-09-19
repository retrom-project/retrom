import { execFileSync } from "node:child_process";
import path from "node:path";
import { inspectWeb, readProject } from "./graph.mjs";
import { inspectWebBoundaries } from "./boundaries.mjs";
import { readOwnership } from "./ownership.mjs";
import { checkedBytes, contentDigest } from "./source-inputs.mjs";

function git(root, ...args) {
  return execFileSync("git", args, { cwd: root, encoding: "utf8", maxBuffer: 16 * 1024 * 1024 });
}

export function repositorySnapshot(root) {
  const names = [...new Set(git(root, "ls-files", "--cached", "--others", "--exclude-standard", "-z").split("\0").filter(Boolean))].sort();
  if (names.length === 0) {
    throw new Error("architecture: empty repository snapshot");
  }
  const files = names.map((name) => ({ path: name, sha256: contentDigest(checkedBytes(root, name)) }));
  return {
    commit: git(root, "rev-parse", "HEAD").trim(),
    tree: git(root, "rev-parse", "HEAD^{tree}").trim(),
    sourceSha256: contentDigest(JSON.stringify(files)),
    fileCount: files.length,
  };
}

function projectRoots(root) {
  return readProject(root).fileNames.map((file) => path.relative(root, file).split(path.sep).join("/")).sort();
}

export function inspectWebReport(root) {
  const startedAtMS = Date.now();
  const snapshot = repositorySnapshot(root);
  const roots = projectRoots(root);
  const ownership = readOwnership(root);
  const files = inspectWeb(root);
  const violations = inspectWebBoundaries(files, ownership);
  for (const file of files) {
    if (file.sha256 !== contentDigest(checkedBytes(root, file.file))) {
      throw new Error("architecture: compiled input changed during inspection");
    }
  }
  if (JSON.stringify(roots) !== JSON.stringify(projectRoots(root)) ||
    JSON.stringify(snapshot) !== JSON.stringify(repositorySnapshot(root))) {
    throw new Error("architecture: repository or compiler roots changed during inspection");
  }
  return {
    schemaVersion: 1, status: "NOT_READY", ...snapshot,
    compiledSha256: contentDigest(JSON.stringify(files.map(({ file, sha256 }) => ({ file, sha256 })))),
    configurationSha256: contentDigest(JSON.stringify([
      "web/tsconfig.json", "web/package.json", "web/package-lock.json", "quality/architecture/package-ownership.json",
    ].map((name) => ({ path: name, sha256: contentDigest(checkedBytes(root, name)) })))),
    startedAtMS, finishedAtMS: Date.now(), files,
    implementedChecks: [
      "resolved static, type, reexport and literal dynamic imports",
      "actual compiler input closure and source ownership",
      "Feature public entries, dependency cycles and transitive layer direction",
      "production dependencies on test/tool sources",
      "client and server module execution boundaries",
    ],
    pendingChecks: [
      "generated-source provenance and public export consumer registration",
      "Provider private ABI and unique dispatcher registration",
      "request-scoped authentication state and behavioral isolation evidence",
      "complete permanent gate wiring",
    ],
    violations,
  };
}

export function webReportExitCode(report) {
  if (!report?.sourceSha256 || !report.commit || !report.files?.length || !report.implementedChecks?.length) {
    return 2;
  }
  return report.status === "VERIFIED" && report.pendingChecks?.length === 0 && report.violations?.length === 0 ? 0 : 1;
}
