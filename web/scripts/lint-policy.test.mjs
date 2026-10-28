import assert from "node:assert/strict";
import { test } from "node:test";
import { ESLint } from "eslint";
import configuration from "../eslint.config.mjs";

test("Web lint exclusions stay within the reviewed generated and build paths", () => {
  const ignores = configuration.filter((entry) => entry.ignores)
    .map(({ files, ignores: paths }) => ({ files, paths }));
  assert.deepEqual(ignores, [
    { files: undefined, paths: [".next/**", "out/**", "build/**", "next-env.d.ts"] },
    { files: undefined, paths: [".next/**", "out/**", "build/**", "next-env.d.ts"] },
    { files: ["**/*.{ts,tsx,js,jsx,mjs}"], paths: ["**/*.test.{ts,tsx,js,jsx,mjs}", "**/*.spec.{ts,tsx,js,jsx,mjs}", "e2e/**"] },
    { files: undefined, paths: [".next/**", ".next-build/**", ".next-e2e/**", ".next-pfb-*/**", ".next-runtime-dev/**",
      "lib/api/generated/**", "playwright-report/**", "test-results/**"] },
  ]);
});

test("Web lint keeps production, tests and new feature paths under the required limits", async () => {
  const eslint = new ESLint();
  for (const [path, lines, functionLines] of [
    ["features/new-feature/view.tsx", 600, 250],
    ["features/new-feature/view.test.tsx", 800, 350],
    ["e2e/new-feature.spec.ts", 800, 350],
  ]) {
    const config = await eslint.calculateConfigForFile(path);
    assert.ok(config, `${path} must be linted`);
    assert.equal(config.linterOptions.noInlineConfig, true);
    assert.deepEqual(config.rules.complexity, [2, 15]);
    assert.deepEqual(config.rules["max-depth"], [2, 4]);
    assert.equal(config.rules["max-lines"][1].max, lines);
    assert.equal(config.rules["max-lines-per-function"][1].max, functionLines);
    for (const rule of ["no-explicit-any", "no-floating-promises", "no-misused-promises"]) {
      assert.equal(config.rules[`@typescript-eslint/${rule}`][0], 2);
    }
  }
});
