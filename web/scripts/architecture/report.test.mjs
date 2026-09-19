import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { readOwnership } from "./ownership.mjs";
import { inspectWebReport, repositorySnapshot, webReportExitCode } from "./report.mjs";

function write(root, name, body) {
  mkdirSync(path.dirname(path.join(root, name)), { recursive: true });
  writeFileSync(path.join(root, name), typeof body === "string" ? body : JSON.stringify(body));
}

function fixture(t) {
  const root = mkdtempSync(path.join(os.tmpdir(), "retrom-web-report-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  execFileSync("git", ["init", "-q"], { cwd: root });
  write(root, "web/tsconfig.json", {
    compilerOptions: { target: "es2022", module: "esnext", moduleResolution: "bundler", paths: { "@/*": ["./*"] } },
    include: ["app/**/*.ts", "features/**/*.ts"],
  });
  write(root, "web/package.json", {});
  write(root, "web/package-lock.json", {});
  write(root, "web/features/example/contracts.ts", "export type Value = string;");
  write(root, "web/features/example/private.ts", "export const value = 1;");
  write(root, "web/features/example/api.ts", 'export { value } from "./private";');
  write(root, "web/app/page.ts", 'import { value } from "@/features/example/api"; export default value;');
  const registry = {
    schemaVersion: 1, baseline: "82834bade1648da3067ebda1b1fc89c18577fd6a",
    packages: [
      { path: "web/app", layer: "app", module: "routing", owner: "RF20",
        files: [{ path: "web/app/page.ts", kind: "production" }] },
      { path: "web/features/example", layer: "feature", module: "example", owner: "RF20",
        files: ["api", "contracts", "private"].map((name) => ({ path: "web/features/example/" + name + ".ts", kind: "production" })) },
    ],
  };
  write(root, "quality/architecture/package-ownership.json", registry);
  execFileSync("git", ["add", "."], { cwd: root });
  execFileSync("git", ["-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture"], { cwd: root });
  return root;
}

test("real TypeScript aliases feed boundaries and source-bound reports without partial success", (t) => {
  const root = fixture(t);
  const initial = inspectWebReport(root);
  assert.deepEqual(initial.violations, []);
  assert.equal(initial.status, "NOT_READY");
  assert.equal(webReportExitCode(initial), 1);
  assert.ok(initial.startedAtMS <= initial.finishedAtMS);
  assert.match(initial.commit, /^[a-f0-9]{40}$/);
  write(root, "web/app/page.ts", 'import { value } from "@/features/example/private"; export default value;');
  const changed = inspectWebReport(root);
  assert.equal(changed.commit, initial.commit);
  assert.notEqual(changed.sourceSha256, initial.sourceSha256);
  assert.equal(changed.violations.filter((item) => item.message.includes("private Feature")).length, 1);
});

test("ownership rejects duplicate JSON keys and stale registry versions", (t) => {
  const root = fixture(t);
  write(root, "quality/architecture/package-ownership.json",
    '{"schemaVersion":1,"schemaVersion":1,"baseline":"82834bade1648da3067ebda1b1fc89c18577fd6a","packages":[]}');
  assert.throws(() => readOwnership(root), /duplicate JSON key/);
  write(root, "quality/architecture/package-ownership.json", { schemaVersion: 2, packages: [] });
  assert.throws(() => readOwnership(root), /invalid ownership/);
});

test("binary and non-code source changes invalidate execution evidence", (t) => {
  const root = fixture(t);
  writeFileSync(path.join(root, "fixture.bin"), Buffer.from([0, 128, 255]));
  const before = repositorySnapshot(root);
  writeFileSync(path.join(root, "fixture.bin"), Buffer.from([0, 129, 255]));
  assert.notEqual(repositorySnapshot(root).sourceSha256, before.sourceSha256);
  write(root, "web/package.json", { changed: true });
  assert.notEqual(repositorySnapshot(root).sourceSha256, before.sourceSha256);
});

test("exit semantics reject absent evidence, violations and unfinished checks", () => {
  assert.equal(webReportExitCode({}), 2);
  const report = { commit: "a".repeat(40), sourceSha256: "b".repeat(64), files: [{}], implementedChecks: ["boundary"],
    status: "VERIFIED", violations: [], pendingChecks: [] };
  assert.equal(webReportExitCode(report), 0);
  assert.equal(webReportExitCode({ ...report, violations: [{}] }), 1);
  assert.equal(webReportExitCode({ ...report, pendingChecks: ["behavior"] }), 1);
});
