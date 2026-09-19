import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import ts from "typescript";
import { inspectImports, inspectWeb, readProject, sourcePaths } from "./graph.mjs";

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

test("only leading string directives create a client boundary", (t) => {
  const root = fixture(t);
  write(root, "web/tsconfig.json", JSON.stringify({ compilerOptions: { target: "es2022" } }));
  write(root, "web/app/page.ts", 'export const value = 1; "use client";');
  const files = inspectWeb(root);
  assert.deepEqual(files.find((file) => file.file === "web/app/page.ts").directives, []);
});

test("the actual compiler closure includes ignored local dependencies", (t) => {
  const root = fixture(t);
  write(root, ".gitignore", "/web/hidden/\n");
  write(root, "web/tsconfig.json", JSON.stringify({
    compilerOptions: { target: "es2022", moduleResolution: "bundler", module: "esnext" },
    include: ["app/**/*.ts"],
  }));
  write(root, "web/hidden/value.ts", "export const hidden = 1;");
  write(root, "web/app/page.ts", 'export { hidden } from "../hidden/value";');
  const files = inspectWeb(root);
  const hidden = files.find((file) => file.file === "web/hidden/value.ts");
  assert.ok(hidden, "compiled ignored source must be represented for ownership validation");
  assert.equal(hidden.discovered, false);
  assert.equal(hidden.typed, true);
});

test("variable module loading cannot disappear from the import graph", (t) => {
  const root = fixture(t);
  const source = ts.createSourceFile(path.join(root, "web/app/page.ts"),
    "export const load = (name: string) => import(name);", ts.ScriptTarget.Latest, true);
  const edges = inspectImports(root, source, {});
  assert.equal(edges.length, 1);
  assert.equal(edges[0].kind, "dynamic-expression");
  assert.equal(edges[0].resolved, null);
});

test("records type-only and wildcard export intent", (t) => {
  const root = fixture(t);
  write(root, "web/value.ts", "export type Value = string; export const value = 1;");
  const source = ts.createSourceFile(path.join(root, "web/use.ts"), [
    'import { type Value } from "./value";',
    'export type { Value } from "./value";',
    'export * from "./value";',
    'export const load = () => import(`./value`);',
  ].join("\n"), ts.ScriptTarget.Latest, true);
  const edges = inspectImports(root, source, {});
  assert.deepEqual(edges.map((edge) => edge.typeOnly), [true, true, false, false]);
  assert.deepEqual(edges.map((edge) => edge.exportAll), [false, false, true, false]);
  assert.ok(edges.every((edge) => edge.resolved === "web/value.ts"));
});

test("rejects symlinks instead of hashing their external targets", (t) => {
  const root = fixture(t);
  write(root, "web/tsconfig.json", JSON.stringify({ compilerOptions: { target: "es2022" } }));
  write(root, "web/source.ts", "export const value = 1;");
  symlinkSync("source.ts", path.join(root, "web/alias.ts"));
  assert.throws(() => inspectWeb(root), /symlinked source/);
});

test("type-only class reexports cannot be confused with runtime value exports", (t) => {
  const root = fixture(t);
  write(root, "web/tsconfig.json", JSON.stringify({ compilerOptions: { target: "es2022" } }));
  write(root, "web/value.ts", "export class Value {}");
  write(root, "web/contracts.ts", 'export type { Value } from "./value";');
  write(root, "web/api.ts", 'export { Value } from "./value";');
  const files = inspectWeb(root);
  assert.equal(files.find((file) => file.file === "web/contracts.ts").exports[0].runtimeValue, false);
  assert.equal(files.find((file) => file.file === "web/api.ts").exports[0].runtimeValue, true);
});
