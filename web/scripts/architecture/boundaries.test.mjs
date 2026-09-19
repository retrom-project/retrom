import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { inspectWebBoundaries } from "./boundaries.mjs";

function source(file, dependencies = [], options = {}) {
  return {
    file, typed: true, directives: [], exports: [], ...options,
    imports: dependencies.map((dependency) => ({
      kind: "import", typeOnly: false, exportAll: false, line: 3,
      ...(typeof dependency === "string" ? { resolved: dependency, specifier: dependency } : dependency),
    })),
  };
}

function fixture(files, kinds = {}) {
  const packages = new Map();
  for (const file of files) {
    const directory = path.posix.dirname(file.file);
    if (!packages.has(directory)) {
      packages.set(directory, { path: directory, layer: "feature", module: "example", owner: "RF20", files: [] });
    }
    packages.get(directory).files.push({ path: file.file, kind: kinds[file.file] ?? "production" });
  }
  return {
    schemaVersion: 1, baseline: "82834bade1648da3067ebda1b1fc89c18577fd6a",
    packages: [...packages.values()],
  };
}

function check(files, kinds = {}) {
  return inspectWebBoundaries(files, fixture(files, kinds));
}

function feature(name, dependencies = []) {
  return [
    source("web/features/" + name + "/api.ts", dependencies),
    source("web/features/" + name + "/contracts.ts"),
    source("web/features/" + name + "/private.ts"),
  ];
}

test("public Feature entries and their own implementation form a legal acyclic graph", () => {
  const files = [
    source("web/app/page.ts", ["web/features/a/api.ts"]),
    ...feature("a", ["web/features/a/private.ts", "web/features/b/contracts.ts"]),
    ...feature("b"),
  ];
  assert.deepEqual(check(files), []);
});

test("private imports, nested entry names and export-star cannot widen the public boundary", () => {
  const files = [
    source("web/app/page.ts", ["web/features/a/private.ts", "web/features/a/nested/api.ts"]),
    ...feature("a", [{ resolved: "web/features/a/private.ts", specifier: "./private", exportAll: true }]),
    source("web/features/a/nested/api.ts"),
  ];
  const findings = check(files).filter((item) => item.rule === "WEB01");
  assert.equal(findings.length, 3);
  assert.ok(findings.every((item) => item.line === 3 && item.dependencyChain.length === 2 && item.symbol));
});

test("type-only imports still participate in Feature cycles and reverse layer direction", () => {
  const files = [
    ...feature("a", [{ resolved: "web/features/b/contracts.ts", specifier: "../b/contracts", typeOnly: true }]),
    ...feature("b", ["web/features/a/contracts.ts"]),
    source("web/lib/shared.ts", [{ resolved: "web/features/a/contracts.ts", specifier: "../features/a/contracts", typeOnly: true }]),
  ];
  const findings = check(files);
  assert.equal(findings.filter((item) => item.message === "Feature dependency cycle").length, 2);
  assert.ok(findings.some((item) => item.file === "web/lib/shared.ts" && item.message.includes("layer direction")));
});

test("a neutral helper cannot hide transitive application and test dependencies", () => {
  const files = [
    source("web/lib/helper.ts", ["web/shared/bridge.ts"]),
    source("web/shared/bridge.ts", ["web/app/page.ts", "web/testing/helper.ts"]),
    source("web/app/page.ts"), source("web/testing/helper.ts"),
  ];
  const findings = check(files, { "web/testing/helper.ts": "test" });
  assert.ok(findings.some((item) => item.rule === "WEB01" && item.dependencyChain.length === 3));
  assert.ok(findings.some((item) => item.rule === "GOV01" && item.dependencyChain.length === 3));
});

test("contracts reject runtime exports and each production Feature needs both public entries", () => {
  const files = [source("web/features/a/contracts.ts", [], { exports: [{ name: "runtime", runtimeValue: true }] })];
  const findings = check(files);
  assert.ok(findings.some((item) => item.file.endsWith("/api.ts") && item.message.includes("absent")));
  assert.ok(findings.some((item) => item.message.includes("only compiler-resolved types")));
});

test("unknown compiled inputs and variable module loads remain visible failures", () => {
  const files = [source("web/lib/dynamic.ts", [{ resolved: null, specifier: "<expression>", kind: "dynamic-expression" }])];
  const registry = fixture([source("web/lib/different.ts")]);
  const findings = inspectWebBoundaries(files, registry);
  assert.ok(findings.some((item) => item.rule === "GOV01"));
  assert.ok(findings.some((item) => item.rule === "WEB01" && item.dependencyChain[1] === "<expression>"));
});

test("client execution cannot acquire server-only capabilities through a helper", () => {
  const files = [
    source("web/components/client.ts", ["web/lib/helper.ts"], { directives: ["use client"] }),
    source("web/lib/helper.ts", ["web/lib/secret.ts"]),
    source("web/lib/secret.ts", [{ resolved: "framework:server-only", specifier: "server-only" }]),
  ];
  const findings = check(files).filter((item) => item.rule === "WEB02");
  assert.equal(findings.length, 1);
  assert.deepEqual(findings[0].dependencyChain, files.map((file) => file.file));
});

test("server rendering permits a Client Component boundary but rejects a browser singleton helper", () => {
  const browser = source("web/lib/api/browser-client.ts", [{ resolved: "framework:client-only", specifier: "client-only" }]);
  const safe = [
    source("web/app/page.ts", ["web/components/widget.ts"]),
    source("web/components/widget.ts", [browser.file], { directives: ["use client"] }),
    browser,
  ];
  assert.deepEqual(check(safe), []);
  const unsafe = [safe[0], source("web/components/widget.ts", [browser.file]), browser];
  assert.equal(check(unsafe).filter((item) => item.rule === "WEB02").length, 1);
});

test("erased type imports do not transfer runtime state", () => {
  const files = [
    source("web/app/page.ts", [{ resolved: "web/lib/api/browser-client.ts", specifier: "../lib/api/browser-client", typeOnly: true }]),
    source("web/lib/api/browser-client.ts"),
  ];
  assert.deepEqual(check(files), []);
});

test("test-only private imports are legal but cannot become production dependencies", () => {
  const files = [
    ...feature("a"),
    source("web/testing/check.ts", ["web/features/a/private.ts"]),
  ];
  assert.deepEqual(check(files, { "web/testing/check.ts": "test" }), []);
  files.push(source("web/app/page.ts", ["web/testing/check.ts"]));
  assert.ok(check(files, { "web/testing/check.ts": "test" }).some((item) => item.rule === "GOV01"));
});

test("empty scans and ambiguous ownership are analysis errors", () => {
  const files = [source("web/lib/value.ts")];
  const registry = fixture(files);
  assert.throws(() => inspectWebBoundaries([], registry), /empty/);
  registry.packages[0].files.push({ ...registry.packages[0].files[0] });
  assert.throws(() => inspectWebBoundaries(files, registry), /duplicated/);
});
