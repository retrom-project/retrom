import {createHash} from "node:crypto";
import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { currentEmulatorBrightRatio, evidencePath } from "./acceptance-support";
import {
  exitRuntimePlayer, runtimeCheckpoint, runtimeFrameCount, runtimeResource, runtimeResourceURLs,
  type RuntimeEnvelope,
} from "./runtime-provider-support";

type ExpansionResult = {
  coreId: string;
  fixtureId: string;
  fixtureSha256: string;
  gameId: string;
  platformInstanceId: string;
};

type ExpansionCase = {
  caseId: string;
  coreId: string;
  targetId: string;
  expectsBios: boolean;
  expectsParent: boolean;
  fixtureId: string;
};

const cases: ExpansionCase[] = [
  { caseId: "ACC-RUN-008", fixtureId: "snes9x", coreId: "snes9x", targetId: "snes9x", expectsBios: false, expectsParent: false },
  { caseId: "ACC-RUN-009", fixtureId: "nestopia", coreId: "nestopia", targetId: "nestopia", expectsBios: false, expectsParent: false },
  { caseId: "ACC-RUN-010", fixtureId: "mame2003_plus", coreId: "mame2003_plus", targetId: "mame2003-plus", expectsBios: true, expectsParent: true },
  { caseId: "ACC-RUN-011", fixtureId: "fbalpha2012_cps1", coreId: "fbalpha2012_cps1", targetId: "fbalpha2012-cps1", expectsBios: false, expectsParent: false },
  { caseId: "ACC-RUN-012", fixtureId: "fbalpha2012_cps2", coreId: "fbalpha2012_cps2", targetId: "fbalpha2012-cps2", expectsBios: false, expectsParent: true },
];

function expansionResults() {
  const encoded = process.env.RETROM_CORE_EXPANSION_RESULTS;
  expect(encoded, "core expansion product-flow results").toBeTruthy();
  return JSON.parse(encoded!) as ExpansionResult[];
}

function resultFor(expectation: ExpansionCase) {
  const result = expansionResults().find((item) => item.fixtureId === expectation.fixtureId);
  expect(result, `${expectation.fixtureId} product-flow result`).toBeTruthy();
  expect(result).toMatchObject({ coreId: expectation.coreId });
  expect(result?.fixtureSha256).toMatch(/^[0-9a-f]{64}$/);
  return result!;
}

async function createLaunch(page: Page, csrfToken: string, gameId: string, coreId: string, saveStateId: string | null) {
  const response = await page.request.post("/api/v1/launches", {
    data: {
      gameId, coreId, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`,
      clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true },
    },
    headers: {
      Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000",
      "X-Retrom-Csrf": csrfToken,
      "Idempotency-Key": crypto.randomUUID(),
    },
  });
  expect(response.ok(), await response.text()).toBe(true);
  const launch = await response.json() as { launchId: string; playUrl: string; status?: string };
  expect(launch.status ?? "READY").toBe("READY");
  expect(launch.playUrl).toMatch(/\/play\/[0-9a-f-]+$/);
  return launch;
}

async function captureRuntimeState(page: Page) {
  return {...await runtimeCheckpoint(page), frame: await runtimeFrameCount(page)};
}

async function waitForRuntime(page: Page) {
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
  const canvas = page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas");
  await expect(canvas).toBeVisible({ timeout: 15_000 });
  const frame = page.frames().find((candidate) => candidate !== page.mainFrame());
  expect(frame, "EmulatorJS iframe").toBeTruthy();
  await expect.poll(() => runtimeFrameCount(page), {
    timeout: 30_000,
  }).toBeGreaterThan(30);
  return {canvas};
}

async function verifyCore(page: Page, testInfo: TestInfo, expectation: ExpansionCase) {
  test.setTimeout(180_000);
  await page.addInitScript(() => {
    Object.defineProperty(Element.prototype, "requestFullscreen", { configurable: true, value: () => Promise.resolve() });
  });
  const result = resultFor(expectation);
  const errors: string[] = [];
  const runtimeRequests: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.startsWith("/runtime/")) {runtimeRequests.push(request.url());}
  });
  page.on("requestfailed", (request) => {
    const pathname = new URL(request.url()).pathname;
    if (pathname.startsWith("/runtime/") && request.failure()?.errorText !== "net::ERR_ABORTED") {
      errors.push(`${pathname}: ${request.failure()?.errorText ?? "unknown"}`);
    }
  });
  const loginResponse = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" },
    headers: { Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000" },
  });
  expect(loginResponse.ok()).toBe(true);
  const csrfToken = (await loginResponse.json() as { csrfToken: string }).csrfToken;
  const initialLaunch = await createLaunch(page, csrfToken, result.gameId, expectation.coreId, null);
  const initialConfigResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await page.goto(initialLaunch.playUrl);
  const initialConfiguration = await (await initialConfigResponse).json() as RuntimeEnvelope;
  expect(initialConfiguration).toMatchObject({
    schemaVersion: 1,
    session: {purpose: "PRODUCT", mode: "SINGLE"},
    runtime: {providerId: "emulatorjs", providerApiVersion: 1, targetId: expectation.targetId},
    restore: null,
  });
  const gameURLs = runtimeResourceURLs(runtimeResource(initialConfiguration, "game"));
  expect(gameURLs).toHaveLength(1);
  expect(runtimeResource(initialConfiguration, "parent") !== null).toBe(expectation.expectsParent);
  expect(runtimeResource(initialConfiguration, "bios") !== null).toBe(expectation.expectsBios);
  const initialRuntime = await waitForRuntime(page);
  const initialState = await captureRuntimeState(page);
  expect(initialState.sizeBytes).toBeGreaterThan(0);
  expect(initialState.sizeBytes).toBeLessThanOrEqual(initialConfiguration.runtime.checkpoint!.maxBytes);
  expect(initialState.format).toBe(initialConfiguration.runtime.checkpoint!.writeFormat);
  expect(initialState.sha256).toMatch(/^[0-9a-f]{64}$/);
  const firstFrame = await initialRuntime.canvas.screenshot();
  await initialRuntime.canvas.click({ position: { x: 64, y: 64 } });
  await page.keyboard.down("a");
  await page.keyboard.down("ArrowLeft");
  try {
    await expect.poll(() => runtimeFrameCount(page), {
      timeout: 15_000,
    }).toBeGreaterThan(initialState.frame + 30);
  } finally {
    await page.keyboard.up("ArrowLeft");
    await page.keyboard.up("a");
  }
  const afterInputState = await captureRuntimeState(page);
  expect(afterInputState.sha256).not.toBe(initialState.sha256);
  const secondFrame = await initialRuntime.canvas.screenshot();
  expect(firstFrame.equals(secondFrame)).toBe(false);
  // The NES fixture intentionally changes three 8x8 indicator tiles rather
  // than painting a full-screen scene. A 64x64 downsample still has to retain
  // multiple bright samples, while the independent frame comparison above
  // proves that the visible input indicators changed.
  await expect.poll(() => currentEmulatorBrightRatio(page), { timeout: 15_000, intervals: [500] }).toBeGreaterThan(0.001);

  await page.mouse.move(640, 1);
  const saveResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/save-states$/.test(response.url()) && response.request().method() === "POST", {timeout: 30_000});
  await page.locator(".player-save-button").click();
  const savedResponse = await saveResponse;
  expect(savedResponse.status()).toBe(201);
  const saveStateId = (await savedResponse.json() as { saveStateId: string }).saveStateId;
  const resumeLaunch = await createLaunch(page, csrfToken, result.gameId, expectation.coreId, saveStateId);
  const resumeConfigResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await exitRuntimePlayer(page);
  await page.goto(resumeLaunch.playUrl);
  const resumeConfiguration = await (await resumeConfigResponse).json() as RuntimeEnvelope;
  expect(resumeConfiguration.restore?.url).toMatch(/\/state$/);
  const savedStateResponse = await page.request.get(resumeConfiguration.restore!.url);
  expect(savedStateResponse.ok()).toBe(true);
  const savedState = new Uint8Array(await savedStateResponse.body());
  expect(savedState.byteLength).toBe(resumeConfiguration.restore!.sizeBytes);
  expect(savedState.byteLength).toBeLessThanOrEqual(resumeConfiguration.runtime.checkpoint!.maxBytes);
  const savedDigest = createHash("sha256").update(savedState).digest("hex");
  expect(savedDigest).toBe(resumeConfiguration.restore!.sha256);
  expect(resumeConfiguration.runtime.checkpoint!.readFormats).toContain(resumeConfiguration.restore!.format);
  await waitForRuntime(page);
  expect((await runtimeCheckpoint(page)).format).toBe(resumeConfiguration.runtime.checkpoint!.writeFormat);

  const repeatedLaunch = await createLaunch(page, csrfToken, result.gameId, expectation.coreId, saveStateId);
  const repeatedConfigResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await exitRuntimePlayer(page);
  await page.goto(repeatedLaunch.playUrl);
  const repeatedConfiguration = await (await repeatedConfigResponse).json() as RuntimeEnvelope;
  expect(repeatedConfiguration.restore).toMatchObject({
    format: resumeConfiguration.restore!.format,
    sha256: resumeConfiguration.restore!.sha256,
    sizeBytes: resumeConfiguration.restore!.sizeBytes,
  });
  await waitForRuntime(page);
  expect(runtimeRequests.some((url) => url.endsWith(gameURLs[0]!))).toBe(true);
  expect(runtimeRequests.some((url) => url.endsWith(initialConfiguration.runtime.moduleUrl))).toBe(true);
  expect(errors).toEqual([]);
  await page.screenshot({ path: evidencePath(testInfo, `${expectation.fixtureId}-core-expansion.png`), fullPage: true });
}

export function registerCoreExpansionAcceptanceTests() {
  for (const expectation of cases) {
    test(`${expectation.caseId} ${expectation.coreId} public fixture executes, reacts, and restores deterministic core state`, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== "chrome-1280", "The core runtime case consumes one shared fixture.");
      await verifyCore(page, testInfo, expectation);
    });
  }
}
