import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import path from "node:path";
import {expect, type Page, type TestInfo} from "@playwright/test";
import type {SourceImportSummary} from "../features/server-import/source-import-model";
import {formatTime} from "../lib/backend";
import {evidencePath} from "./acceptance-support";
import {exitRuntimePlayer, runtimeCheckpoint, runtimeFrameCount, runtimeResource, type RuntimeEnvelope} from "./runtime-provider-support";
import {serverSourcePath} from "./server-directory-support";
import {persistentContentProfile} from "./persistent-content-profile";
import {createOrdinaryImport, login, test} from "./issue-regression-support";

async function createScan(page: Page, directory: string, requestHeaders?: Record<string, string>) {
  const headers = requestHeaders ?? await login(page);
  const roots = await (await page.request.get("/api/v1/admin/server-import-roots")).json() as {items: {id: string; status: string}[]};
  const response = await page.request.post("/api/v1/admin/source-imports", {
    headers: {...headers, "Idempotency-Key": crypto.randomUUID()},
    data: {rootId: roots.items.find(root => root.status === "AVAILABLE")!.id,
      format: "PEGASUS", extensionFilter: "", sourceRelativePath: serverSourcePath(directory)},
  });
  expect(response.status(), await response.text()).toBe(202);
  const plan = await response.json() as SourceImportSummary;
  return plan;
}

async function scan(page: Page, directory: string) {
  return waitScan(page, (await createScan(page, directory)).id);
}

async function waitScan(page: Page, id: string) {
  let summary!: SourceImportSummary;
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/admin/source-imports/${id}`);
    expect(response.ok()).toBe(true);
    summary = await response.json() as SourceImportSummary;
    return summary.state;
  }, {timeout: 30_000}).not.toBe("SCANNING");
  return summary;
}

test("ACC-PEG-007 rejected scans retain diagnostics, recover, distinguish empty input and cancel", async ({page, sourceDrafts}, testInfo) => {
  test.setTimeout(150_000);
  const source = process.env.RETROM_E2E_SERVER_SOURCE;
  expect(source).toBeTruthy();
  const name = `Diagnostics-${testInfo.project.name}`, root = path.join(source!, name);
  mkdirSync(root, {recursive: true});
  writeFileSync(path.join(root, "game.nes"), readFileSync(path.join(process.cwd(), "../testdata/public-roms/nes-smoke/nes-smoke.nes")));
  writeFileSync(path.join(root, "metadata.pegasus.txt"), "collection: NES\nbroken syntax\n");
  const invalid = await scan(page, name);
  expect(invalid).toMatchObject({state: "FAILED", scanOutcome: "INVALID", counts: {metadata: 1, invalidMetadata: 1, collections: 0, games: 0},
    scanDiagnostics: [{relativePath: "metadata.pegasus.txt", line: 2, code: "PEGASUS_METADATA_SYNTAX_INVALID", message: "field must contain a non-empty key"}]});
  await page.goto(`/admin/imports/server/source/${invalid.id}`);
  await expect(page.getByRole("region", {name: "扫描诊断"})).toContainText("第 2 行");
  await page.getByRole("button", {name: "修正后重新扫描"}).click();
  const drawer = page.getByRole("dialog", {name: "从目录准备审核事项"});
  await expect(drawer.getByRole("region", {name: "扫描诊断"})).toBeVisible();
  await page.screenshot({path: evidencePath(testInfo, "scan-invalid-desktop.png"), fullPage: true});
  await page.keyboard.press("Escape");
  await page.getByRole("button", {name: "修正后重新扫描"}).click();
  await expect(drawer).toContainText("第 2 行");
  await drawer.getByRole("button", {name: "返回修改输入"}).click();
  await expect(drawer.getByRole("combobox", {name: "文件组织格式"})).toHaveValue("PEGASUS");
  await page.keyboard.press("Escape");
  await page.getByRole("button", {name: "修正后重新扫描"}).click();
  writeFileSync(path.join(root, "metadata.pegasus.txt"), "collection: NES\ngame: Diagnostics ROM\nfile: game.nes\n");
  const created = page.waitForResponse(response => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/v1/admin/source-imports");
  await drawer.getByRole("button", {name: "修正后重新扫描"}).click();
  const rescanned = await (await created).json() as SourceImportSummary;
  const corrected = await waitScan(page, rescanned.id);
  sourceDrafts.push(corrected.id);
  expect(corrected.id).not.toBe(invalid.id);
  expect(corrected).toMatchObject({state: "AWAITING_MAPPING", scanOutcome: "READY", counts: {collections: 1, games: 1, invalidMetadata: 0}, scanDiagnostics: []});
  await expect(drawer.getByRole("region", {name: "扫描诊断"})).toHaveCount(0);
  await page.keyboard.press("Escape");
  mkdirSync(path.join(root, "bad"));
  writeFileSync(path.join(root, "bad/metadata.pegasus.txt"), "bad syntax\n");
  const partial = await scan(page, name);
  sourceDrafts.push(partial.id);
  expect(partial).toMatchObject({state: "AWAITING_MAPPING", scanOutcome: "PARTIAL", counts: {metadata: 2, invalidMetadata: 1, collections: 1, games: 1}});
  await page.goto(`/admin/imports/server/source/${partial.id}`);
  await page.getByRole("button", {name: "继续映射"}).click();
  await expect(drawer).toContainText("部分 metadata 被拒绝，合法集合可以继续");
  await expect(drawer.getByRole("button", {name: "确认映射"})).toBeDisabled();
  await page.setViewportSize({width: 390, height: 844});
  await expect(page.getByText("请在电脑上管理游戏库", {exact: true})).toBeVisible();
  await expect(drawer).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({path: evidencePath(testInfo, "scan-mobile-desktop-guide.png"), fullPage: true});
  await page.setViewportSize(testInfo.project.use.viewport!);
  await page.keyboard.press("Escape");
  const headers = await login(page);
  const cancelName = `${name}-Cancel`, cancelRoot = path.join(source!, cancelName);
  mkdirSync(cancelRoot);
  const gamesToScan = "game: Cancellation fixture\nfile: game.nes\n".repeat(7000);
  for (let index = 0; index < 10; index++) {
    const directory = path.join(cancelRoot, String(index)); mkdirSync(directory);
    writeFileSync(path.join(directory, "metadata.pegasus.txt"), `collection: Cancel ${index}\n${gamesToScan}`);
  }
  const scanning = await createScan(page, cancelName, headers);
  expect(scanning.state).toBe("SCANNING");
  await expect.poll(async () => {
    const current = await (await page.request.get(`/api/v1/admin/source-imports/${scanning.id}`)).json() as SourceImportSummary;
    expect(current.state).toBe("SCANNING");
    const cancelled = await page.request.post(`/api/v1/admin/source-imports/${scanning.id}/cancel`, {
      headers: {...headers, "If-Match": `"v${current.version}"`, "Idempotency-Key": crypto.randomUUID()}, data: {reason: "Acceptance cancellation"},
    });
    if (!cancelled.ok()) {expect(cancelled.status(), await cancelled.text()).toBe(409);}
    return cancelled.ok();
  }, {timeout: 10_000, intervals: [50]}).toBe(true);
  await expect.poll(async () => (await (await page.request.get(`/api/v1/admin/source-imports/${scanning.id}`)).json() as SourceImportSummary).state,
    {timeout: 30_000}).toBe("CANCELLED");
  for (const [suffix, outcome, contents] of [["Empty", "EMPTY", false], ["NoMetadata", "NO_METADATA", true]] as const) {
    mkdirSync(path.join(source!, `${name}-${suffix}`));
    if (contents) {writeFileSync(path.join(source!, `${name}-${suffix}/game.nes`), "input without metadata");}
    const result = await scan(page, `${name}-${suffix}`);
    expect(result).toMatchObject({state: "FAILED", scanOutcome: outcome, counts: {metadata: 0, collections: 0, games: 0}, scanDiagnostics: []});
    await page.goto(`/admin/imports/server/source/${result.id}`);
    await expect(page.getByRole("region", {name: "扫描诊断"})).toBeVisible();
    await expect(page.getByRole("button", {name: "修正后重新扫描"})).toBeVisible();
  }
  const games = await (await page.request.get("/api/v1/games?limit=100")).json() as {items: {title: string}[]};
  expect(games.items.some(game => game.title === "Diagnostics ROM")).toBe(false);
});

type FixtureGame = {fixtureId: string; gameId: string};
async function launch(page: Page, headers: Record<string, string>, gameId: string, coreId: string, saveStateId: string | null = null) {
  let value!: {status?: string; playUrl: string; launchId: string; jobId?: string};
  await expect.poll(async () => {
    const response = await page.request.post("/api/v1/launches", {headers: {...headers, "Idempotency-Key": crypto.randomUUID()},
      data: {gameId, coreId, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`,
        clientCapabilities: {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true}}});
    expect(response.ok(), await response.text()).toBe(true);
    value = await response.json() as typeof value;
    return value.status ?? "READY";
  }, {timeout: 30_000, intervals: [500]}).toBe("READY");
  return value;
}

async function playable(page: Page) {
  await expect(page.locator(".player-loading")).toBeHidden({timeout: 60_000});
  await expect.poll(() => runtimeFrameCount(page), {timeout: 30_000}).toBeGreaterThan(30);
  const canvas = page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas");
  await expect(canvas).toBeVisible();
  const before = await canvas.screenshot(), checkpoint = await runtimeCheckpoint(page), frame = await runtimeFrameCount(page);
  await canvas.click({position: {x: 64, y: 64}});
  await page.keyboard.down("ArrowLeft");
  try {
    await expect.poll(() => runtimeFrameCount(page)).toBeGreaterThan(frame + 30);
    await expect.poll(async () => (await canvas.screenshot()).equals(before), {intervals: [50, 100, 125]}).toBe(false);
  }
  finally {await page.keyboard.up("ArrowLeft");}
  expect((await runtimeCheckpoint(page)).sha256).not.toBe(checkpoint.sha256);
  return canvas;
}

test("ACC-RUN-019 immutable ROM bytes survive same-core, cross-core and restore launches", async ({}, testInfo) => {
  test.setTimeout(240_000);
  const fixtures = JSON.parse(process.env.RETROM_CORE_EXPANSION_RESULTS ?? "[]") as FixtureGame[];
  const game = fixtures.find(item => item.fixtureId === "fceumm")!, changed = fixtures.find(item => item.fixtureId === "nestopia")!;
  expect(game).toBeTruthy(); expect(changed).toBeTruthy();
  const profile = persistentContentProfile(testInfo), errors: string[] = [], requests: string[] = [];
  try {
    const interruptedContext = await profile.reopen(), interruptedPage = await interruptedContext.newPage();
    const interruptedHeaders = await login(interruptedPage);
    let truncated = 0;
    await interruptedContext.route("**/runtime/content/game/**", async route => {
      const response = await route.fetch(), body = await response.body();
      truncated++;
      const partial = body.subarray(0, Math.floor(body.length / 2));
      await route.fulfill({response, body: partial, headers: {...response.headers(), "content-length": String(partial.length)}});
    });
    await interruptedPage.goto((await launch(interruptedPage, interruptedHeaders, game.gameId, "fceumm")).playUrl);
    await expect(interruptedPage.locator(".player-loading").getByRole("link", {name: "返回游戏库"})).toBeVisible({timeout: 60_000});
    expect(truncated).toBeGreaterThan(0);
    const evidence: object[] = [], resources: unknown[] = [];
    let save: string | null = null;
    for (const [core, gameId, expectedGets, restore] of [["fceumm", game.gameId, 1, false], ["fceumm", game.gameId, 0, false],
      ["nestopia", game.gameId, 0, false], ["nestopia", game.gameId, 0, true], ["nestopia", changed.gameId, 1, false]] as const) {
      const context = await profile.reopen(), page = await context.newPage(), headers = await login(page);
      page.on("pageerror", error => errors.push(error.message));
      await page.addInitScript(() => {Object.defineProperty(Element.prototype, "requestFullscreen", {configurable: true, value: () => Promise.resolve()});});
      requests.length = 0;
      await context.route("**/runtime/content/game/**", async route => {
        requests.push(route.request().method());
        if (expectedGets === 0) {await route.abort("failed");} else {await route.continue();}
      });
      const current = await launch(page, headers, gameId, core, restore ? save : null);
      const config = page.waitForResponse(response => /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.ok());
      await page.goto(current.playUrl);
      const envelope = await (await config).json() as RuntimeEnvelope;
      const resource = runtimeResource(envelope, "game");
      resources.push(resource);
      expect(envelope.runtime.targetId).toBe(core);
      expect(envelope.runtime.capabilities.contentLoading).toBe("PRELOAD_ONLY");
      const canvas = await playable(page);
      expect(requests).toHaveLength(expectedGets);
      evidence.push({core, launchId: current.launchId, gameId, romGets: requests.length, resource, restore});
      await canvas.screenshot({path: evidencePath(testInfo, `cache-${evidence.length}-${core}.png`)});
      if (core === "nestopia" && !restore && gameId === game.gameId) {
        await page.mouse.move(640, 1);
        const response = page.waitForResponse(value => /\/save-states$/.test(value.url()) && value.request().method() === "POST");
        await page.locator(".player-save-button").click();
        const saved = await response; expect(saved.status()).toBe(201);
        save = (await saved.json() as {saveStateId: string}).saveStateId;
      }
      await exitRuntimePlayer(page);
      await page.goto("about:blank");
    }
    expect(resources[1]).toEqual(resources[0]); expect(resources[2]).toEqual(resources[0]); expect(resources[3]).toEqual(resources[0]);
    expect(resources[4]).not.toEqual(resources[0]);
    expect(errors).toEqual([]);
    await testInfo.attach("rom-cache", {contentType: "application/json", body: JSON.stringify({httpCache: "disabled", warmNetwork: "blocked", browserRestarted: true, truncatedGets: truncated, launches: evidence})});
  } finally {await profile.close();}
});

test("ACC-UI-012 import timestamps use the browser timezone across overview, tasks and server details", async ({page, browser, sourceDrafts}, testInfo: TestInfo) => {
  test.setTimeout(180_000);
  const source = await scan(page, "Games");
  sourceDrafts.push(source.id);
  const headers = await login(page);
  const biosResponse = await page.request.post("/api/v1/admin/server-imports", {
    headers: {...headers, "Idempotency-Key": crypto.randomUUID()}, data: {kind: "BIOS_DIRECTORY", rootId: source.root.id, sourceRelativePath: serverSourcePath("BIOS"), replaceIfBetter: false},
  });
  expect(biosResponse.ok(), await biosResponse.text()).toBe(true);
  const bios = await biosResponse.json() as {id: string; createdAtMs: number};
  const ordinary = await createOrdinaryImport(page, headers);
  for (const zone of ["Asia/Shanghai", "UTC", "America/Los_Angeles"]) {
    const context = await browser.newContext({storageState: await page.context().storageState(), timezoneId: zone,
      viewport: {width: 2560, height: 1440}, deviceScaleFactor: 1.5});
    const local = await context.newPage(), errors: string[] = [];
    local.on("pageerror", error => errors.push(error.message));
    local.on("console", message => {if (message.type() === "error") {errors.push(message.text());}});
    for (const [url, selector, time] of [["/admin/imports", ".import-recent-list", ordinary.createdAtMs],
      ["/admin/imports/tasks", ".import-task-list", ordinary.createdAtMs], ["/admin/imports/server", ".server-import-history", source.createdAtMs],
      [`/admin/imports/server/source/${source.id}`, ".server-import-detail-head", source.createdAtMs],
      [`/admin/imports/server/${bios.id}`, ".server-import-detail-head", bios.createdAtMs]] as const) {
      await local.goto(url);
      await expect(local.locator(selector).locator("time").filter({hasText: formatTime(time, zone)}).first()).toBeVisible();
      expect(await local.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }
    expect(errors).toEqual([]);
    await local.screenshot({path: evidencePath(testInfo, `timezone-${zone.replaceAll("/", "-")}.png`), fullPage: true});
    await context.close();
  }
});
