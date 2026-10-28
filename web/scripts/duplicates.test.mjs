import assert from "node:assert/strict";
import {test} from "node:test";
import {mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync} from "node:fs";
import {tmpdir} from "node:os";
import {dirname, join} from "node:path";
import {fileURLToPath} from "node:url";
import {spawnSync} from "node:child_process";

const configuration = JSON.parse(readFileSync(new URL("../.jscpd.json", import.meta.url), "utf8"));
const cli = fileURLToPath(new URL("../node_modules/jscpd/run-jscpd.js", import.meta.url));
const snippet = [
  "export function summarize(records) {",
  "  const counts = new Map();",
  "  let total = 0;",
  "  for (const record of records) {",
  "    const key = record.kind + ':' + record.name.trim().toLowerCase();",
  "    const previous = counts.get(key) ?? 0;",
  "    counts.set(key, previous + 1);",
  "    total += record.amount;",
  "  }",
  "  const ranked = [...counts].sort((left, right) => right[1] - left[1]);",
  "  const valid = Number.isFinite(total) && records.every(item => item.amount >= 0);",
  "  if (!valid) throw new Error('Invalid record amount');",
  "  return {total, count: records.length, labels: ranked.map(item => item[0]),",
  "    average: records.length ? total / records.length : null};",
  "}",
].join("\n");

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "retrom-duplicates-"));
  t.after(() => rmSync(directory, {recursive: true, force: true}));
  const write = (path, contents = snippet) => {
    const target = join(directory, path);
    mkdirSync(dirname(target), {recursive: true});
    writeFileSync(target, contents);
  };
  write(".jscpd.json", JSON.stringify({...configuration, output: "report"}));
  write(".jscpd-baseline.json", JSON.stringify({version: 1, fingerprints: {}}));
  const scan = (...flags) => {
    const result = spawnSync(process.execPath, [cli, "--config", ".jscpd.json", "--no-colors", ...flags],
      {cwd: directory, encoding: "utf8", timeout: 10000});
    assert.ifError(result.error);
    return result;
  };
  const expect = (status, ...flags) => {
    const result = scan(...flags);
    assert.equal(result.status, status, result.stdout + result.stderr);
    return JSON.parse(readFileSync(join(directory, "report/jscpd-report.json"), "utf8")).statistics.total;
  };
  return {write, scan, expect, remove: (path) => rmSync(join(directory, path))};
}

test("duplicate policy scans new paths, keeps the minimum clone size and fails on new clones", () => {
  assert.deepEqual(configuration.path, ["."]);
  assert.equal(configuration.minTokens, 120);
  assert.equal(configuration.minLines, 10);
  assert.equal(configuration.mode, "weak");
  assert.equal(configuration.ignoreIdentifiers, "consistent");
  assert.equal(configuration.crossFormats, "js-ts");
  assert.deepEqual(configuration.format, ["javascript", "jsx", "typescript", "tsx"]);
  assert.equal(configuration.baseline, ".jscpd-baseline.json");
  assert.equal(configuration.failOnNewClones, 0);
  assert.equal(configuration.failOnEmpty, true);
  assert.equal(configuration.ignorePattern, undefined);
});

test("duplicates in new files, renamed copies and the same file fail; test and generated copies are exempt", (t) => {
  const f = fixture(t);
  f.write("features/new-feature/original.ts");
  const ignored = [
    "types/schema.d.ts",
    "types/schema.d.mts",
    "types/schema.d.cts",
    "new-feature/example.test.ts",
    "new-feature/example.spec.tsx",
    "e2e/browser.ts",
    "lib/api/generated/schema.ts",
    ".next/types/page.ts",
  ];
  for (const path of ignored) {f.write(path);}
  assert.equal(f.expect(0).newClones, 0);
  const renamed = snippet.replaceAll("records", "entries").replace("summarize", "aggregate");
  f.write("features/another-feature/copied.tsx", renamed);
  assert.ok(f.expect(1).newClones > 0);
  f.remove("features/another-feature/copied.tsx");
  f.write("features/new-feature/original.ts", snippet + "\n" + renamed);
  assert.ok(f.expect(1).newClones > 0);
});

test("the baseline accepts existing copies after line shifts and rejects an additional copy of the same code", (t) => {
  const f = fixture(t);
  f.write("new-feature/original.ts");
  f.write("another-feature/existing.tsx");
  f.expect(0, "--update-baseline");
  assert.equal(f.expect(0).newClones, 0);
  f.write("new-feature/original.ts", "// unrelated line shift\n\n" + snippet);
  assert.equal(f.expect(0).newClones, 0);
  f.write("new-third-feature/copied.mjs");
  assert.ok(f.expect(1).newClones > 0);
});

test("a scan that ignores every source file fails", (t) => {
  const f = fixture(t);
  f.write("ignored.test.ts");
  assert.equal(f.scan().status, 1);
});
