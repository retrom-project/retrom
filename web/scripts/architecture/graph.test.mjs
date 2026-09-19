import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import ts from "typescript";
import { inspectImports, readProject, sourcePaths } from "./graph.mjs";

function fixture(t) {
  const root = mkdtempSync(path.join(os.tmpdir(), "retrom-architecture-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(path.join(root, "web"), { recursive: true });
  execFileSync("git", ["init", "-q"], { cwd: root });
  return root;
}

function write(root, name, content) {
  const target = path.join(root, name);
  mkdirSync(path.dirname(target), { recursive: true });
  writeFileSync(target, content);
}

test("resolves alias, relative, type, export-star and dynamic literal edges", (t) => {
  const root = fixture(t);
  write(root, "web/tsconfig.json", JSON.stringify({
    compilerOptions: { moduleResolution: "bundler", module: "esnext", paths: { "@/*": ["./*"] } },
    include: ["**/*.ts"],
  }));
  write(root, "web/features/example/api.ts", "export type Value = string; export const value = 1;");
  const filename = path.join(root, "web/app/page.ts");
  const text = [
    'import type { Value } from "@/features/example/api";',
    'export * from "../features/example/api";',
    'export const load = () => import("@/features/example/api");',
    'export type Reference = import("../features/example/api").Value;',
  ].join("\n");
  write(root, "web/app/page.ts", text);
  const source = ts.createSourceFile(filename, text, ts.ScriptTarget.Latest, true);
  const edges = inspectImports(root, source, readProject(root).options);
  assert.deepEqual(edges.map((edge) => edge.kind), ["import", "export", "dynamic", "type"]);
  assert.ok(edges.every((edge) => edge.resolved === "web/features/example/api.ts"));
});

test("fails on unresolved imports and malformed project configuration", (t) => {
  const root = fixture(t);
  write(root, "web/tsconfig.json", "{ invalid");
  assert.throws(() => readProject(root));
  const filename = path.join(root, "web/missing.ts");
  const source = ts.createSourceFile(filename, 'import "./missing";', ts.ScriptTarget.Latest, true);
  assert.throws(() => inspectImports(root, source, {}), /unresolved import/);
});

test("discovers nonignored untracked sources without admitting ignored caches", (t) => {
  const root = fixture(t);
  write(root, ".gitignore", "/web/cache/\n");
  write(root, "web/app/page.ts", "export const value = 1;");
  write(root, "web/cache/hidden.ts", "export const hidden = 1;");
  assert.deepEqual(sourcePaths(root), ["web/app/page.ts"]);
});
