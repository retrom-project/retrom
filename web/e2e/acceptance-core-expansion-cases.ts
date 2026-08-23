import { createHash } from "node:crypto";
import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { currentEmulatorBrightRatio, evidencePath } from "./acceptance-support";

type ExpansionResult = {
  coreId: string;
  fixtureId: string;
  fixtureSha256: string;
  gameId: string;
  platformInstanceId: string;
};

type RuntimeConfiguration = {
  biosUrl: string | null;
  core: string;
  emulatorjsVersion: string;
  gameUrl: string;
  parentUrl: string | null;
  playerAdapterId: string;
  runtimeCore: string;
  runtimePathOverrides: Record<string, string>;
  stateUrl: string | null;
};

type ExpansionCase = {
  artifact: string;
  caseId: string;
  coreId: string;
  expectsBios: boolean;
  expectsParent: boolean;
  fixtureId: string;
};

const cases: ExpansionCase[] = [
  { caseId: "ACC-RUN-008", fixtureId: "snes9x", coreId: "snes9x", artifact: "snes9x-wasm.data", expectsBios: false, expectsParent: false },
  { caseId: "ACC-RUN-009", fixtureId: "nestopia", coreId: "nestopia", artifact: "nestopia-wasm.data", expectsBios: false, expectsParent: false },
  { caseId: "ACC-RUN-010", fixtureId: "mame2003_plus", coreId: "mame2003_plus", artifact: "mame2003_plus-wasm.data", expectsBios: true, expectsParent: true },
  { caseId: "ACC-RUN-011", fixtureId: "fbalpha2012_cps1", coreId: "fbalpha2012_cps1", artifact: "fbalpha2012_cps1-wasm.data", expectsBios: false, expectsParent: false },
  { caseId: "ACC-RUN-012", fixtureId: "fbalpha2012_cps2", coreId: "fbalpha2012_cps2", artifact: "fbalpha2012_cps2-wasm.data", expectsBios: false, expectsParent: true },
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

function coreState(bytes: Uint8Array) {
  expect(new TextDecoder().decode(bytes.subarray(0, 7))).toBe("RASTATE");
  expect(bytes[7]).toBe(1);
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  for (let offset = 8; offset + 8 <= bytes.byteLength;) {
    const marker = new TextDecoder().decode(bytes.subarray(offset, offset + 4));
    const size = view.getUint32(offset + 4, true);
    const start = offset + 8;
    const end = start + size;
    expect(end).toBeLessThanOrEqual(bytes.byteLength);
    if (marker === "MEM ") {return bytes.subarray(start, end);}
    if (marker === "END ") {break;}
    offset = start + ((size + 7) & ~7);
  }
  throw new Error("STATE_INVALID");
}

async function createLaunch(page: Page, csrfToken: string, gameId: string, coreId: string, saveStateId: string | null) {
  const response = await page.request.post("/api/v1/launches", {
    data: {
      gameId, coreId, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`,
      clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true },
    },
    headers: {
      Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000",
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
  const frame = page.frames().find((candidate) => candidate !== page.mainFrame());
  expect(frame, "EmulatorJS iframe").toBeTruthy();
  return frame!.evaluate(async () => {
    const manager = window.EJS_emulator?.gameManager;
    if (!manager?.getState || !manager.getFrameNum) {throw new Error("STATE_INVALID");}
    const bytes = new Uint8Array(manager.getState());
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    let core = new Uint8Array();
    for (let offset = 8; offset + 8 <= bytes.byteLength;) {
      const marker = new TextDecoder().decode(bytes.subarray(offset, offset + 4));
      const size = view.getUint32(offset + 4, true);
      const start = offset + 8;
      const end = start + size;
      if (end > bytes.byteLength) {throw new Error("STATE_INVALID");}
      if (marker === "MEM ") {core = bytes.subarray(start, end); break;}
      if (marker === "END ") {break;}
      offset = start + ((size + 7) & ~7);
    }
    if (!core.byteLength) {throw new Error("STATE_INVALID");}
    const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", core));
    const includes = (needle: number[]) => {
      outer: for (let offset = 0; offset <= core.byteLength - needle.length; offset += 1) {
        for (let index = 0; index < needle.length; index += 1) {
          if (core[offset + index] !== needle[index]) {continue outer;}
        }
        return true;
      }
      return false;
    };
    return {
      coreBytes: core.byteLength,
      coreDigest: Array.from(digest, (value) => value.toString(16).padStart(2, "0")).join(""),
      frame: manager.getFrameNum(),
      fixtureMarker: includes([0x52, 0x54, 0x52, 0x4d]) || includes([0x54, 0x52, 0x4d, 0x52]),
      fixturePalette: includes(Array<number>(64).fill(0xff)) || includes(Array<number>(64).fill(0xf0)),
      stateBytes: bytes.byteLength,
    };
  });
}

async function waitForRuntime(page: Page) {
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
  const canvas = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas.ejs_canvas");
  await expect(canvas).toBeVisible({ timeout: 15_000 });
  const frame = page.frames().find((candidate) => candidate !== page.mainFrame());
  expect(frame, "EmulatorJS iframe").toBeTruthy();
  await expect.poll(() => frame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0), {
    timeout: 30_000,
  }).toBeGreaterThan(30);
  return { canvas, frame: frame! };
}

async function installRestoreEvidence(page: Page) {
  await page.addInitScript(() => {
    const target = window as typeof window & {
      __RETROM_RESTORE_EVIDENCE__?: { coreBytes: number; coreDigest: string };
    };
    const timer = window.setInterval(() => {
      const manager = window.EJS_emulator?.gameManager as NonNullable<typeof window.EJS_emulator>["gameManager"] & {
        __retromRestoreWrapped?: boolean;
      } | undefined;
      const original = manager?.loadExplicitStateAndWait;
      if (!manager || manager.__retromRestoreWrapped || typeof original !== "function" || !manager.getState) {return;}
      manager.__retromRestoreWrapped = true;
      manager.loadExplicitStateAndWait = async (state, timeoutMs) => {
        await original.call(manager, state, timeoutMs);
        const bytes = new Uint8Array(manager.getState!());
        const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
        for (let offset = 8; offset + 8 <= bytes.byteLength;) {
          const marker = new TextDecoder().decode(bytes.subarray(offset, offset + 4));
          const size = view.getUint32(offset + 4, true);
          const start = offset + 8;
          const end = start + size;
          if (end > bytes.byteLength) {throw new Error("STATE_INVALID");}
          if (marker === "MEM ") {
            const core = bytes.subarray(start, end);
            const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", core));
            target.__RETROM_RESTORE_EVIDENCE__ = {
              coreBytes: core.byteLength,
              coreDigest: Array.from(digest, (value) => value.toString(16).padStart(2, "0")).join(""),
            };
            return;
          }
          if (marker === "END ") {break;}
          offset = start + ((size + 7) & ~7);
        }
        throw new Error("STATE_INVALID");
      };
      window.clearInterval(timer);
    }, 0);
  });
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
    headers: { Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000" },
  });
  expect(loginResponse.ok()).toBe(true);
  const csrfToken = (await loginResponse.json() as { csrfToken: string }).csrfToken;
  const initialLaunch = await createLaunch(page, csrfToken, result.gameId, expectation.coreId, null);
  const initialConfigResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await page.goto(initialLaunch.playUrl);
  const initialConfiguration = await (await initialConfigResponse).json() as RuntimeConfiguration;
  expect(initialConfiguration).toMatchObject({
    core: expectation.coreId,
    emulatorjsVersion: "4.2.3",
    playerAdapterId: "ejs-4.2.3-v2",
    runtimeCore: expectation.coreId,
    stateUrl: null,
  });
  expect(Boolean(initialConfiguration.parentUrl)).toBe(expectation.expectsParent);
  expect(Boolean(initialConfiguration.biosUrl)).toBe(expectation.expectsBios);
  expect(Object.keys(initialConfiguration.runtimePathOverrides)).toContain(expectation.artifact);
  expect(initialConfiguration.runtimePathOverrides[expectation.artifact]).toMatch(new RegExp(`/${expectation.artifact.replace(".", "\\.")}$`));
  const initialRuntime = await waitForRuntime(page);
  const initialState = await captureRuntimeState(page);
  expect(initialState.stateBytes).toBeLessThanOrEqual(1024 * 1024);
  expect(initialState.coreBytes).toBeGreaterThan(0);
  if (expectation.coreId.startsWith("fbalpha2012_cps")) {
    expect(initialState.fixtureMarker, "the project-owned 68000 program executed").toBe(true);
    expect(initialState.fixturePalette, "the 68000 program initialized CPS palette RAM").toBe(true);
  }
  const firstFrame = await initialRuntime.canvas.screenshot();
  await initialRuntime.canvas.click({ position: { x: 64, y: 64 } });
  await page.keyboard.down("a");
  await page.keyboard.down("ArrowLeft");
  try {
    await expect.poll(() => initialRuntime.frame.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0), {
      timeout: 15_000,
    }).toBeGreaterThan(initialState.frame + 30);
  } finally {
    await page.keyboard.up("ArrowLeft");
    await page.keyboard.up("a");
  }
  const afterInputState = await captureRuntimeState(page);
  expect(afterInputState.coreDigest).not.toBe(initialState.coreDigest);
  const secondFrame = await initialRuntime.canvas.screenshot();
  expect(firstFrame.equals(secondFrame)).toBe(false);
  // The NES fixture intentionally changes three 8x8 indicator tiles rather
  // than painting a full-screen scene. A 64x64 downsample still has to retain
  // multiple bright samples, while the independent frame comparison above
  // proves that the visible input indicators changed.
  await expect.poll(() => currentEmulatorBrightRatio(page), { timeout: 15_000, intervals: [500] }).toBeGreaterThan(0.001);

  await page.mouse.move(640, 1);
  const saveResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/save-states$/.test(response.url()) && response.request().method() === "POST");
  await page.locator(".player-save-button").click();
  const savedResponse = await saveResponse;
  expect(savedResponse.status()).toBe(201);
  const saveStateId = (await savedResponse.json() as { saveStateId: string }).saveStateId;
  const resumeLaunch = await createLaunch(page, csrfToken, result.gameId, expectation.coreId, saveStateId);
  const resumeConfigResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await installRestoreEvidence(page);
  await page.goto(resumeLaunch.playUrl);
  const resumeConfiguration = await (await resumeConfigResponse).json() as RuntimeConfiguration;
  expect(resumeConfiguration.stateUrl).toMatch(/\/state$/);
  const savedStateResponse = await page.request.get(resumeConfiguration.stateUrl!);
  expect(savedStateResponse.ok()).toBe(true);
  const savedState = new Uint8Array(await savedStateResponse.body());
  expect(savedState.byteLength).toBeLessThanOrEqual(1024 * 1024);
  const savedCore = coreState(savedState);
  const savedCoreDigest = createHash("sha256").update(savedCore).digest("hex");
  expect(savedCoreDigest).toMatch(/^[0-9a-f]{64}$/);
  await waitForRuntime(page);
  await expect.poll(() => page.frames().find((candidate) => candidate !== page.mainFrame())?.evaluate(() =>
    (window as typeof window & { __RETROM_RESTORE_EVIDENCE__?: { coreDigest: string } })
      .__RETROM_RESTORE_EVIDENCE__?.coreDigest), { timeout: 15_000 }).toMatch(/^[0-9a-f]{64}$/);
  const firstRestore = await page.frames().find((candidate) => candidate !== page.mainFrame())!.evaluate(() =>
    (window as typeof window & { __RETROM_RESTORE_EVIDENCE__?: { coreBytes: number; coreDigest: string } })
      .__RETROM_RESTORE_EVIDENCE__!);
  expect(firstRestore.coreBytes).toBeGreaterThan(0);
  expect(firstRestore.coreBytes).toBeLessThanOrEqual(1024 * 1024);
  expect(firstRestore.coreDigest).toMatch(/^[0-9a-f]{64}$/);

  const repeatedLaunch = await createLaunch(page, csrfToken, result.gameId, expectation.coreId, saveStateId);
  const repeatedConfigResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await installRestoreEvidence(page);
  await page.goto(repeatedLaunch.playUrl);
  expect(((await repeatedConfigResponse).status())).toBe(200);
  await waitForRuntime(page);
  await expect.poll(() => page.frames().find((candidate) => candidate !== page.mainFrame())?.evaluate(() =>
    (window as typeof window & { __RETROM_RESTORE_EVIDENCE__?: { coreDigest: string } })
      .__RETROM_RESTORE_EVIDENCE__?.coreDigest), { timeout: 15_000 }).toBe(firstRestore.coreDigest);
  const repeatedRestore = await page.frames().find((candidate) => candidate !== page.mainFrame())!.evaluate(() =>
    (window as typeof window & { __RETROM_RESTORE_EVIDENCE__?: { coreBytes: number; coreDigest: string } })
      .__RETROM_RESTORE_EVIDENCE__!);
  expect(repeatedRestore).toEqual(firstRestore);
  expect(runtimeRequests.some((url) => url.endsWith(initialConfiguration.gameUrl))).toBe(true);
  expect(runtimeRequests.some((url) => url.endsWith(initialConfiguration.runtimePathOverrides[expectation.artifact]!))).toBe(true);
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
