import assert from "node:assert/strict";
import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
  expandFixtureRuns,
  expandMultiDiscFixtures,
  resolveChromeBinary,
  resolveResultPath,
  resolveResultsDirectory,
  selectableFixtures
} from "./smoke-test.mjs";

test("smoke results can be isolated from the tracked historical baseline", () => {
  const isolated = path.join(os.tmpdir(), "retrom-smoke-results");
  assert.equal(resolveResultsDirectory({ RETROM_EXAMPLE_RESULTS_DIR: isolated }), isolated);
  assert.equal(
    resolveResultPath("mgba.png", { RETROM_EXAMPLE_RESULTS_DIR: isolated }),
    path.join(isolated, "mgba.png")
  );
  assert.equal(
    resolveResultsDirectory({ RETROM_EXAMPLE_RESULTS_DIR: "  " }),
    path.join(path.dirname(new URL(import.meta.url).pathname), "results")
  );
});

test("PPSSPP expands to independent CSO and ISO runs", () => {
  const fixtures = [{
    core: "ppsspp",
    game: { formatId: "cso" },
    formatVariants: [{ formatId: "iso" }]
  }, { core: "mgba" }];
  assert.deepEqual(
    expandFixtureRuns(fixtures, ["ppsspp"]).map(run => [run.runId, run.formatId, run.exampleQuery]),
    [
      ["ppsspp-cso", "cso", "?format=cso"],
      ["ppsspp-iso", "iso", "?format=iso"]
    ]
  );
  assert.equal(expandFixtureRuns(fixtures).length, 3);
  assert.throws(() => expandFixtureRuns(fixtures, ["unknown"]), /Unknown core/);
});

test("multi-disc fixtures reuse the pinned yabause runtime without joining the yabause selector", () => {
  const manifest = {
    fixtures: [{ core: "yabause", bios: [{ localPath: "bios.bin" }], coreArtifact: { path: "yabause.data" } }],
    multiDiscFixtures: [{
      id: "multidisc-saturn-2", kind: "RUNTIME_POSITIVE", core: "yabause",
      discs: [{ localPath: "disc-001.chd" }, { localPath: "disc-002.chd" }]
    }]
  };
  const runs = [...manifest.fixtures, ...expandMultiDiscFixtures(manifest)];
  assert.deepEqual(expandFixtureRuns(runs, ["yabause"]).map(run => run.runId || run.core), ["yabause"]);
  assert.deepEqual(expandFixtureRuns(runs, ["multidisc-saturn-2"]).map(run => run.runId), ["multidisc-saturn-2"]);
  assert.deepEqual(runs[1].expectedExternalFiles, ["disc-001.chd", "disc-002.chd"]);
});

test("all promoted core fixtures are selectable formal fixtures", () => {
  const manifest = {
    fixtures: [
      { core: "yabause", bios: [], coreArtifact: { path: "yabause.data" } },
      { core: "beetle_vb", bios: [], coreArtifact: { path: "beetle_vb.data" } }
    ],
    multiDiscFixtures: []
  };
  assert.deepEqual(selectableFixtures(manifest).map(fixture => fixture.core), ["yabause", "beetle_vb"]);
  assert.deepEqual(
    expandFixtureRuns(selectableFixtures(manifest), ["beetle_vb"]).map(fixture => fixture.core),
    ["beetle_vb"]
  );
});

test("explicit Chrome path is validated instead of silently falling back", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "retrom-chrome-resolution-"));
  try {
    const binary = path.join(directory, "chrome");
    await fs.writeFile(binary, "#!/bin/sh\nexit 0\n", { mode: 0o700 });
    assert.equal(await resolveChromeBinary({ RETROM_CHROME_BIN: binary }, directory), binary);
    await assert.rejects(resolveChromeBinary({ RETROM_CHROME_BIN: path.join(directory, "missing") }, directory), /not executable/);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});

test("Playwright Chromium cache is discovered deterministically", async () => {
  const home = await fs.mkdtemp(path.join(os.tmpdir(), "retrom-playwright-resolution-"));
  try {
    const older = path.join(home, ".cache", "ms-playwright", "chromium-999", "chrome-linux", "chrome");
    const newer = path.join(home, ".cache", "ms-playwright", "chromium-1000", "chrome-linux64", "chrome");
    await fs.mkdir(path.dirname(older), { recursive: true });
    await fs.mkdir(path.dirname(newer), { recursive: true });
    await fs.writeFile(older, "#!/bin/sh\n", { mode: 0o700 });
    await fs.writeFile(newer, "#!/bin/sh\n", { mode: 0o700 });
    assert.equal(await resolveChromeBinary({}, home), newer);
  } finally {
    await fs.rm(home, { recursive: true, force: true });
  }
});
