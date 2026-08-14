import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { randomUUID } from "node:crypto";
import {
  chromium, expect, request as playwrightRequest, test,
  type APIRequestContext, type Browser, type BrowserContext, type BrowserServer, type Page, type TestInfo,
} from "@playwright/test";

type DiagnosticSnapshot = {
  connections: number;
  reconnects: number;
  canonicalFrame: number;
  maxPredictionFrames: number;
  rollbackCount: number;
  rollbackFrames: number;
  maxRollbackFrames: number;
  resyncs: number;
  stateCaptures: Array<{ byteLength: number; stateDigest: string; coreDigest: string }>;
  stateLoads: Array<{
    byteLength: number; stateDigest: string; coreDigest: string;
    changed: boolean; nativeCompletion: boolean; byteExact: boolean; coreExact: boolean;
    expectedCoreBytes: number; recapturedCoreBytes: number; firstCoreMismatch: number;
  }>;
  checkpoints: Array<{ frame: number; coreDigest: string }>;
  endedReason: string | null;
};

type DiagnosticControl = {
  snapshot: () => DiagnosticSnapshot;
  press: (control: number, value: number) => void;
  dropConnection: (durationMs: number) => void;
  injectDesync: () => Promise<void>;
};

declare global {
  interface Window {
    __RETROM_NETPLAY_ACCEPTANCE__?: DiagnosticControl;
  }
}

type PlayerProcess = {
  server: BrowserServer;
  browser: Browser;
  context: BrowserContext;
  csrf: string;
  username: string;
  pid: number;
};

type RoomSnapshot = {
  roomId: string;
  version: number;
  state: string;
  currentSession: null | { sessionId: string; state: string };
  members: Array<{ memberId: string; playerNo: number; ready: boolean; role: string }>;
};

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
const databasePath = process.env.RETROM_E2E_DATABASE ?? "";
const dedicatedNetplayRunner = process.env.RETROM_NETPLAY_ACCEPTANCE === "1";
const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR ?? "";
const chromeArguments = [
  "--autoplay-policy=no-user-gesture-required",
  "--enable-unsafe-swiftshader",
  "--use-angle=swiftshader",
];

test.beforeEach(() => {
  test.skip(!dedicatedNetplayRunner, "ACC-NP cases require their isolated seeded database runner");
});

function evidencePath(testInfo: TestInfo, name: string) {
  if (!caseDirectory) return testInfo.outputPath(name);
  const directory = path.join(caseDirectory, "screenshots");
  mkdirSync(directory, { recursive: true });
  return path.join(directory, name);
}

function writeEvidence(testInfo: TestInfo, value: unknown) {
  const target = caseDirectory ? path.join(caseDirectory, "runtime-result.json") : testInfo.outputPath("runtime-result.json");
  writeFileSync(target, `${JSON.stringify(value, null, 2)}\n`);
}

async function launchPlayer(username: string, latency: boolean, delayInitialApplied = false): Promise<PlayerProcess> {
  const server = await chromium.launchServer({ headless: true, args: chromeArguments });
  const browser = await chromium.connect(server.wsEndpoint());
  const context = await browser.newContext({ baseURL: origin, viewport: { width: 1280, height: 800 } });
  await context.grantPermissions(["clipboard-read", "clipboard-write"], { origin });
  await context.addInitScript(({ injectLatency, player, delayApplied }) => {
    type Controls = Pick<DiagnosticControl, "press" | "dropConnection" | "injectDesync">;
    type DiagnosticFactoryWindow = Window & {
      __RETROM_NETPLAY_DIAGNOSTICS_FACTORY__?: (controls: Controls) => Record<string, unknown>;
    };
    const scope = window as DiagnosticFactoryWindow;
    scope.__RETROM_NETPLAY_DIAGNOSTICS_FACTORY__ = (controls) => {
      const state: DiagnosticSnapshot = {
        connections: 0, reconnects: 0, canonicalFrame: -1, maxPredictionFrames: 0,
        rollbackCount: 0, rollbackFrames: 0, maxRollbackFrames: 0, resyncs: 0,
        stateCaptures: [], stateLoads: [], checkpoints: [], endedReason: null,
      };
      window.__RETROM_NETPLAY_ACCEPTANCE__ = {
        ...controls,
        snapshot: () => JSON.parse(JSON.stringify(state)) as DiagnosticSnapshot,
      };
      return {
        perturbInitialState: player === 2,
        delayForMessage: (type: string, fields: Record<string, unknown>) => {
          if (delayApplied && type === "STATE_APPLIED") return 2_000;
          if (!injectLatency || type !== "INPUT") return 0;
          const frame = typeof fields.frame === "number" ? fields.frame : 0;
          const jitter = ((frame * 17 + player * 13) % 41) - 20;
          return 100 + jitter + (frame > 0 && frame % 300 === 0 ? 100 : 0);
        },
        onConnect: (reconnect: boolean) => {
          state.connections += 1;
          if (reconnect) state.reconnects += 1;
        },
        onStateCapture: (item: DiagnosticSnapshot["stateCaptures"][number]) => state.stateCaptures.push(item),
        onStateLoad: (item: DiagnosticSnapshot["stateLoads"][number]) => state.stateLoads.push(item),
        onEpoch: (item: { resync: boolean }) => { if (item.resync) state.resyncs += 1; },
        onCanonical: (item: { frame: number; predictionFrames: number }) => {
          state.canonicalFrame = Math.max(state.canonicalFrame, item.frame);
          state.maxPredictionFrames = Math.max(state.maxPredictionFrames, item.predictionFrames);
        },
        onRollback: (item: { depth: number }) => {
          state.rollbackCount += 1;
          state.rollbackFrames += item.depth;
          state.maxRollbackFrames = Math.max(state.maxRollbackFrames, item.depth);
        },
        onCheckpoint: (item: { frame: number; coreDigest: string }) => state.checkpoints.push(item),
        onEnded: (reason: string) => {
          state.endedReason = reason;
          console.info("RETROM_NETPLAY_DIAGNOSTIC_END", reason);
        },
      };
    };
  }, { injectLatency: latency, player: username === "test" ? 1 : 2, delayApplied: delayInitialApplied });
  const response = await context.request.post("/api/v1/auth/login", {
    data: { username, password: "test" }, headers: { Origin: origin },
  });
  expect(response.ok()).toBe(true);
  const login = await response.json() as { csrfToken: string };
  const pid = server.process().pid;
  if (pid === undefined) throw new Error("NETPLAY_BROWSER_PID_UNAVAILABLE");
  return { server, browser, context, csrf: login.csrfToken, username, pid };
}

async function closePlayer(player: PlayerProcess) {
  await player.context.close().catch(() => undefined);
  await player.browser.close().catch(() => undefined);
  await player.server.close().catch(() => undefined);
}

async function mutation<T>(
  client: Pick<PlayerProcess, "context" | "csrf">,
  method: "POST" | "PUT" | "DELETE",
  target: string,
  body?: unknown,
  version?: number,
): Promise<{ body: T; status: number }> {
  const headers: Record<string, string> = {
    Origin: origin, "X-Retrom-Csrf": client.csrf, "Idempotency-Key": randomUUID(),
  };
  if (version !== undefined) headers["If-Match"] = `"v${version}"`;
  const response = await client.context.request.fetch(
    target,
    body === undefined ? { method, headers } : { method, data: body, headers },
  );
  const responseText = response.status() === 204 ? "" : await response.text();
  let responseBody: unknown = null;
  if (responseText) {
    try { responseBody = JSON.parse(responseText) as unknown; } catch { responseBody = responseText; }
  }
  if (!response.ok()) throw new Error(`mutation ${method} ${target} = ${response.status()} ${JSON.stringify(responseBody)}`);
  return { body: responseBody as T, status: response.status() };
}

async function setupRoom(host: PlayerProcess, guest: PlayerProcess, game: "fceumm" | "fbneo") {
  const gameID = game === "fceumm"
    ? "01980000-0000-7000-8000-00000000c101"
    : "01980000-0000-7000-8000-00000000c201";
  const profileID = game === "fceumm" ? "fceumm-423-v1" : "fbneo-423-v1";
  let room = (await mutation<RoomSnapshot>(host, "POST", "/api/v1/netplay/rooms", {})).body;
  room = (await mutation<RoomSnapshot>(
    host, "PUT", `/api/v1/netplay/rooms/${room.roomId}/game`,
    { gameId: gameID, netplayProfileId: profileID }, room.version,
  )).body;
  room = (await mutation<RoomSnapshot>(
    guest, "PUT", `/api/v1/netplay/rooms/${room.roomId}/members/me/seat`, { playerNo: 2 }, room.version,
  )).body;
  room = (await mutation<RoomSnapshot>(
    host, "PUT", `/api/v1/netplay/rooms/${room.roomId}/members/me/ready`, { ready: true }, room.version,
  )).body;
  room = (await mutation<RoomSnapshot>(
    guest, "PUT", `/api/v1/netplay/rooms/${room.roomId}/members/me/ready`, { ready: true }, room.version,
  )).body;
  room = (await mutation<RoomSnapshot>(
    host, "POST", `/api/v1/netplay/rooms/${room.roomId}/start`, {}, room.version,
  )).body;
  expect(room.state).toBe("STARTING");
  expect(room.currentSession).not.toBeNull();
  const sessionID = room.currentSession!.sessionId;
  const launchBody = {
    clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true },
  };
  const hostLaunch = (await mutation<{ playUrl: string; launchId: string }>(
    host, "POST", `/api/v1/netplay/rooms/${room.roomId}/sessions/${sessionID}/launch`, launchBody,
  )).body;
  const guestLaunch = (await mutation<{ playUrl: string; launchId: string }>(
    guest, "POST", `/api/v1/netplay/rooms/${room.roomId}/sessions/${sessionID}/launch`, launchBody,
  )).body;
  return { room, sessionID, hostLaunch, guestLaunch, profileID, gameID };
}

async function openRuntime(player: PlayerProcess, playURL: string) {
  const page = await player.context.newPage();
  await page.goto(`${playURL}?netplayAcceptance=1`, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => Boolean(window.__RETROM_NETPLAY_ACCEPTANCE__), null, { timeout: 120_000 });
  return page;
}

async function snapshot(page: Page) {
  return page.evaluate(() => window.__RETROM_NETPLAY_ACCEPTANCE__!.snapshot());
}

async function waitForFrame(page: Page, frame: number, timeout = 180_000) {
  const effectiveTimeout = process.env.RETROM_ACCEPTANCE_DEBUG_TIMEOUT_MS
    ? Number(process.env.RETROM_ACCEPTANCE_DEBUG_TIMEOUT_MS)
    : timeout;
  try {
    await page.waitForFunction(
      (target) => {
        const current = window.__RETROM_NETPLAY_ACCEPTANCE__?.snapshot();
        return (current?.canonicalFrame ?? -1) >= target || current?.endedReason !== null;
      },
      frame,
      { timeout: effectiveTimeout },
    );
  } catch (error) {
    const current = await snapshot(page).catch(() => null);
    const status = await page.locator("[role=status]").allTextContents().catch(() => []);
    throw new Error(`NETPLAY_FRAME_TIMEOUT:${frame}:${JSON.stringify({ current, status })}`, { cause: error });
  }
  const current = await snapshot(page);
  if (current.endedReason !== null) {
    throw new Error(`NETPLAY_ENDED_BEFORE_FRAME_${frame}: ${current.endedReason} ${JSON.stringify(current)}`);
  }
}

function matchingCheckpoints(left: DiagnosticSnapshot, right: DiagnosticSnapshot) {
  const rightByFrame = new Map(right.checkpoints.map((item) => [item.frame, item.coreDigest]));
  return left.checkpoints.filter((item) => rightByFrame.get(item.frame) === item.coreDigest);
}

async function closeAcceptanceRoom(host: PlayerProcess, roomID: string, sessionID: string) {
  await mutation(host, "POST", `/api/v1/netplay/rooms/${roomID}/sessions/${sessionID}/end`, {});
  const current = await host.context.request.get(`/api/v1/netplay/rooms/${roomID}`);
  expect(current.ok()).toBe(true);
  const room = await current.json() as RoomSnapshot;
  await mutation(host, "DELETE", `/api/v1/netplay/rooms/${roomID}`, undefined, room.version);
}

async function exerciseInputs(hostPage: Page, guestPage: Page) {
  const [hostBefore, guestBefore] = await Promise.all([snapshot(hostPage), snapshot(guestPage)]);
  const releaseAtFrame = Math.max(hostBefore.canonicalFrame, guestBefore.canonicalFrame) + 12;
  await hostPage.evaluate(() => window.__RETROM_NETPLAY_ACCEPTANCE__!.press(3, 1));
  await guestPage.evaluate(() => window.__RETROM_NETPLAY_ACCEPTANCE__!.press(8, 1));
  await Promise.all([waitForFrame(hostPage, releaseAtFrame), waitForFrame(guestPage, releaseAtFrame)]);
  await hostPage.evaluate(() => window.__RETROM_NETPLAY_ACCEPTANCE__!.press(3, 0));
  await guestPage.evaluate(() => window.__RETROM_NETPLAY_ACCEPTANCE__!.press(8, 0));
}

async function runBaseline(
  testInfo: TestInfo, game: "fceumm" | "fbneo", targetFrame: number, latency: boolean,
) {
  const host = await launchPlayer("test", latency);
  const guest = await launchPlayer("alice", latency);
  let roomID: string | null = null;
  let sessionID: string | null = null;
  try {
    expect(host.pid).not.toBe(guest.pid);
    const setup = await setupRoom(host, guest, game);
    roomID = setup.room.roomId;
    sessionID = setup.sessionID;
    const [hostPage, guestPage] = await Promise.all([
      openRuntime(host, setup.hostLaunch.playUrl), openRuntime(guest, setup.guestLaunch.playUrl),
    ]);
    await Promise.all([waitForFrame(hostPage, 60), waitForFrame(guestPage, 60)]);
    await guestPage.evaluate(() => window.dispatchEvent(new Event("blur")));
    await Promise.all([waitForFrame(hostPage, 90), waitForFrame(guestPage, 90)]);
    const afterBlur = await snapshot(guestPage);
    expect(afterBlur.connections).toBe(1);
    expect(afterBlur.reconnects).toBe(0);
    await exerciseInputs(hostPage, guestPage);
    await Promise.all([waitForFrame(hostPage, targetFrame), waitForFrame(guestPage, targetFrame)]);
    const [hostResult, guestResult] = await Promise.all([snapshot(hostPage), snapshot(guestPage)]);
    const checkpoints = matchingCheckpoints(hostResult, guestResult);
    expect(checkpoints.length).toBeGreaterThanOrEqual(3);
    expect(checkpoints.slice(-3).map((item) => item.frame)).toEqual(
      hostResult.checkpoints.slice(-3).map((item) => item.frame),
    );
    expect(guestResult.stateLoads.some((item) => item.changed && item.nativeCompletion && item.coreExact)).toBe(true);
    expect(hostResult.endedReason).toBeNull();
    expect(guestResult.endedReason).toBeNull();
    if (latency) {
      const expectedMaxPredictionFrames = game === "fbneo" ? 0 : 8;
      if (game === "fbneo") {
        expect(hostResult.rollbackCount).toBe(0);
        expect(guestResult.rollbackCount).toBe(0);
      } else {
        expect(hostResult.rollbackCount).toBeGreaterThan(0);
        expect(guestResult.rollbackCount).toBeGreaterThan(0);
      }
      expect(hostResult.maxPredictionFrames).toBeLessThanOrEqual(expectedMaxPredictionFrames);
      expect(guestResult.maxPredictionFrames).toBeLessThanOrEqual(expectedMaxPredictionFrames);
      expect(hostResult.maxRollbackFrames).toBeLessThanOrEqual(120);
      expect(guestResult.maxRollbackFrames).toBeLessThanOrEqual(120);
    } else {
      expect(hostResult.resyncs).toBe(0);
      expect(guestResult.resyncs).toBe(0);
    }
    await Promise.all([
      hostPage.screenshot({ path: evidencePath(testInfo, `${game}-host.png`) }),
      guestPage.screenshot({ path: evidencePath(testInfo, `${game}-guest.png`) }),
    ]);
    const evidence = {
      browser: await host.browser.version(), profileId: setup.profileID,
      core: game, targetFrame, latency: latency ? { rttMs: 100, jitterMs: 20, lateEveryFrames: 300 } : null,
      pids: [host.pid, guest.pid], launchIds: [setup.hostLaunch.launchId, setup.guestLaunch.launchId],
      roomId: setup.room.roomId, sessionId: setup.sessionID,
      host: hostResult, guest: guestResult, matchingCheckpoints: checkpoints.slice(-3),
      focusLossRetainedConnection: afterBlur.connections === 1 && afterBlur.reconnects === 0,
      logicalBasename: game === "fbneo" ? "ldrun.zip" : "f1-race.nes",
    };
    await closeAcceptanceRoom(host, roomID, sessionID);
    roomID = null;
    sessionID = null;
    return evidence;
  } finally {
    if (roomID !== null && sessionID !== null) {
      await closeAcceptanceRoom(host, roomID, sessionID).catch(() => undefined);
    }
    await Promise.all([closePlayer(host), closePlayer(guest)]);
  }
}

test("ACC-NP-001 navigation search share seat conflict and permissions", async ({}, testInfo) => {
  const host = await launchPlayer("test", false);
  const guest = await launchPlayer("alice", false, true);
  let third: APIRequestContext | null = null;
  try {
    let room = (await mutation<RoomSnapshot>(host, "POST", "/api/v1/netplay/rooms", {})).body;
    const hostPage = await host.context.newPage();
    await hostPage.goto(`/netplay/rooms/${room.roomId}`);
    const search = hostPage.getByRole("searchbox", { name: "搜索联机游戏" });
    await expect(search).toBeVisible();
    await expect(search).toHaveAttribute("data-shortcut-ready", "true");
    await hostPage.keyboard.press("/");
    await expect(search).toBeFocused();
    await search.fill("F-1");
    await expect(hostPage.getByRole("heading", { name: "F-1 Race" })).toBeVisible();
    await search.fill("FCEUmm");
    await expect(hostPage.getByRole("heading", { name: "F-1 Race" })).toBeVisible();
    await search.fill("");
    await hostPage.getByRole("button", { name: "Arcade" }).click();
    await expect(hostPage.getByRole("heading", { name: "Lode Runner" })).toBeVisible();
    await hostPage.getByRole("combobox", { name: "游戏集合" }).selectOption(
      "01980000-0000-7000-8000-000000000006",
    );
    await expect(hostPage.getByRole("heading", { name: "Lode Runner" })).toBeVisible();
    await hostPage.getByRole("combobox", { name: "联机支持" }).selectOption("ALL");
    await hostPage.getByRole("button", { name: "全部" }).click();
    await expect(hostPage.getByText("游戏当前不可用")).toBeVisible();
    await hostPage.reload();
    await expect(hostPage.getByRole("combobox", { name: "联机支持" })).toHaveValue("ALL");
    room = (await mutation<RoomSnapshot>(
      host, "PUT", `/api/v1/netplay/rooms/${room.roomId}/game`,
      { gameId: "01980000-0000-7000-8000-00000000c101", netplayProfileId: "fceumm-423-v1" },
      room.version,
    )).body;
    await hostPage.reload();
    await hostPage.getByRole("button", { name: "复制房间链接" }).click();
    await expect(hostPage.getByRole("button", { name: "已复制链接" })).toBeVisible();
    expect(hostPage.url()).not.toMatch(/token|capability|credential/i);
    const guestPage = await guest.context.newPage();
    await guestPage.goto(`/netplay/rooms/${room.roomId}`);
    await guestPage.getByRole("button", { name: "选择 P2" }).click();
    await expect(guestPage.getByRole("button", { name: "准备" })).toBeVisible();
    const current = await guest.context.request.get(`/api/v1/netplay/rooms/${room.roomId}`);
    room = await current.json() as RoomSnapshot;
    third = await playwrightRequest.newContext({ baseURL: origin });
    const thirdLogin = await third.post("/api/v1/auth/login", {
      data: { username: "charlie", password: "test" }, headers: { Origin: origin },
    });
    const thirdCSRF = (await thirdLogin.json() as { csrfToken: string }).csrfToken;
    const conflict = await third.put(`/api/v1/netplay/rooms/${room.roomId}/members/me/seat`, {
      data: { playerNo: 2 },
      headers: {
        Origin: origin, "X-Retrom-Csrf": thirdCSRF, "Idempotency-Key": randomUUID(),
        "If-Match": `"v${room.version}"`,
      },
    });
    expect(conflict.status()).toBe(409);
    expect((await conflict.json() as { error: { code: string } }).error.code).toBe("NETPLAY_SEAT_TAKEN");
    const adminBypass = await host.context.request.put(`/api/v1/netplay/rooms/${room.roomId}/members/me/seat`, {
      data: { playerNo: 2 },
      headers: {
        Origin: origin, "X-Retrom-Csrf": host.csrf, "Idempotency-Key": randomUUID(),
        "If-Match": `"v${room.version}"`,
      },
    });
    expect(adminBypass.status()).toBe(403);
    await hostPage.screenshot({ path: evidencePath(testInfo, "netplay-room-host.png"), fullPage: true });
    await guestPage.screenshot({ path: evidencePath(testInfo, "netplay-room-guest.png"), fullPage: true });
    await guestPage.getByRole("button", { name: "准备" }).click();
    await expect(guestPage.getByRole("button", { name: "取消准备" })).toBeVisible();
    await expect(hostPage.locator(".netplay-seat").filter({ hasText: "P2" }).getByText("已准备")).toBeVisible();
    await hostPage.getByRole("button", { name: "准备" }).click();
    await expect(hostPage.getByRole("button", { name: "取消准备" })).toBeVisible();
    const start = hostPage.getByRole("button", { name: "开始联机" });
    await expect(start).toBeEnabled();
    let releaseHostConfig: () => void = () => undefined;
    const hostConfigGate = new Promise<void>((resolve) => { releaseHostConfig = resolve; });
    await hostPage.route("**/runtime/launches/*/config", async (route) => {
      await hostConfigGate;
      await route.continue();
    });
    await start.click();
    await Promise.all([
      expect(hostPage).toHaveURL(/\/play\/[0-9a-f-]+$/),
      expect(guestPage).toHaveURL(/\/play\/[0-9a-f-]+$/),
    ]);
    expect(new URL(hostPage.url()).pathname).not.toBe(new URL(guestPage.url()).pathname);
    await expect(guestPage.locator(".player-loading strong")).toHaveText("正在建立联机同步屏障…", { timeout: 120_000 });
    await guestPage.evaluate(() => window.dispatchEvent(new Event("blur")));
    await guestPage.waitForTimeout(1_500);
    await expect(guestPage).toHaveURL(/\/play\/[0-9a-f-]+$/);
    await expect(guestPage.getByText("联机启动配置已失效")).toHaveCount(0);
    releaseHostConfig();
    await guestPage.waitForFunction(
      () => (window.__RETROM_NETPLAY_ACCEPTANCE__?.snapshot().stateLoads.length ?? 0) >= 1,
      null,
      { timeout: 120_000 },
    );
    await guestPage.evaluate(() => window.__RETROM_NETPLAY_ACCEPTANCE__!.dropConnection(500));
    await guestPage.waitForFunction(
      () => (window.__RETROM_NETPLAY_ACCEPTANCE__?.snapshot().reconnects ?? 0) >= 1,
      null,
      { timeout: 120_000 },
    );
    await expect.poll(async () => {
      const response = await host.context.request.get(`/api/v1/netplay/rooms/${room.roomId}`);
      return (await response.json() as RoomSnapshot).state;
    }, { timeout: 120_000 }).toBe("RUNNING");
    const initialSyncReconnect = await snapshot(guestPage);
    expect(initialSyncReconnect.connections).toBe(2);
    expect(initialSyncReconnect.reconnects).toBe(1);
    expect(initialSyncReconnect.stateLoads.length).toBeGreaterThanOrEqual(2);
    expect(initialSyncReconnect.endedReason).toBeNull();
    writeEvidence(testInfo, {
      browser: await host.browser.version(), pids: [host.pid, guest.pid], roomId: room.roomId,
      filters: ["F-1", "FCEUmm", "Arcade", "FBNeo 游戏", "ALL"],
      seatConflict: "NETPLAY_SEAT_TAKEN", adminBypass: "NETPLAY_FORBIDDEN",
      launchPaths: [new URL(hostPage.url()).pathname, new URL(guestPage.url()).pathname],
      preSyncBlurRetained: true, initialSyncReconnect,
    });
  } finally {
    await third?.dispose();
    await Promise.all([closePlayer(host), closePlayer(guest)]);
  }
});

test("ACC-NP-002 start barrier rolls back a partial launch then retries atomically", async ({}, testInfo) => {
  const host = await launchPlayer("test", false);
  const guest = await launchPlayer("alice", false);
  let coreMutated = false;
  try {
    const gameID = "01980000-0000-7000-8000-00000000c101";
    let room = (await mutation<RoomSnapshot>(host, "POST", "/api/v1/netplay/rooms", {})).body;
    room = (await mutation<RoomSnapshot>(host, "PUT", `/api/v1/netplay/rooms/${room.roomId}/game`, {
      gameId: gameID, netplayProfileId: "fceumm-423-v1",
    }, room.version)).body;
    room = (await mutation<RoomSnapshot>(guest, "PUT", `/api/v1/netplay/rooms/${room.roomId}/members/me/seat`, {
      playerNo: 2,
    }, room.version)).body;
    room = (await mutation<RoomSnapshot>(host, "PUT", `/api/v1/netplay/rooms/${room.roomId}/members/me/ready`, {
      ready: true,
    }, room.version)).body;
    room = (await mutation<RoomSnapshot>(guest, "PUT", `/api/v1/netplay/rooms/${room.roomId}/members/me/ready`, {
      ready: true,
    }, room.version)).body;
    room = (await mutation<RoomSnapshot>(host, "POST", `/api/v1/netplay/rooms/${room.roomId}/start`, {}, room.version)).body;
    const failedSessionID = room.currentSession!.sessionId;
    const launchBody = { clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true } };
    const firstLaunch = (await mutation<{ launchId: string }>(
      host, "POST", `/api/v1/netplay/rooms/${room.roomId}/sessions/${failedSessionID}/launch`, launchBody,
    )).body;
    execFileSync("sqlite3", [databasePath, "UPDATE cores SET requires_threads=1 WHERE id='fceumm';"]);
    coreMutated = true;
    const failedGuest = await guest.context.request.post(
      `/api/v1/netplay/rooms/${room.roomId}/sessions/${failedSessionID}/launch`,
      {
        data: { clientCapabilities: { secureContext: false, crossOriginIsolated: false, sharedArrayBuffer: false } },
        headers: { Origin: origin, "X-Retrom-Csrf": guest.csrf, "Idempotency-Key": randomUUID() },
      },
    );
    expect(failedGuest.status()).toBe(409);
    expect((await failedGuest.json() as { error: { code: string } }).error.code).toBe("NETPLAY_PROFILE_STALE");
    execFileSync("sqlite3", [databasePath, "UPDATE cores SET requires_threads=0 WHERE id='fceumm';"]);
    coreMutated = false;
    const failedRows = execFileSync("sqlite3", [databasePath, `
SELECT session.state,session.end_reason,launch.state
FROM netplay_sessions session JOIN launch_sessions launch ON launch.netplay_session_id=session.id
WHERE session.id='${failedSessionID}';
`], { encoding: "utf8" }).trim();
    expect(failedRows).toBe("FAILED|PREPARE_FAILED|REVOKED");
    const waitingResponse = await host.context.request.get(`/api/v1/netplay/rooms/${room.roomId}`);
    room = await waitingResponse.json() as RoomSnapshot;
    expect(room.state).toBe("WAITING");
    expect(room.currentSession).toBeNull();
    expect(room.members.every((member) => !member.ready)).toBe(true);

    room = (await mutation<RoomSnapshot>(host, "PUT", `/api/v1/netplay/rooms/${room.roomId}/members/me/ready`, { ready: true }, room.version)).body;
    room = (await mutation<RoomSnapshot>(guest, "PUT", `/api/v1/netplay/rooms/${room.roomId}/members/me/ready`, { ready: true }, room.version)).body;
    room = (await mutation<RoomSnapshot>(host, "POST", `/api/v1/netplay/rooms/${room.roomId}/start`, {}, room.version)).body;
    const retrySessionID = room.currentSession!.sessionId;
    const hostLaunch = (await mutation<{ launchId: string }>(host, "POST", `/api/v1/netplay/rooms/${room.roomId}/sessions/${retrySessionID}/launch`, launchBody)).body;
    const guestLaunch = (await mutation<{ launchId: string }>(guest, "POST", `/api/v1/netplay/rooms/${room.roomId}/sessions/${retrySessionID}/launch`, launchBody)).body;
    expect(hostLaunch.launchId).not.toBe(guestLaunch.launchId);
    const ownership = execFileSync("sqlite3", [databasePath, `
SELECT player_no,count(*),count(DISTINCT profile_id),count(DISTINCT launch_session_id)
FROM netplay_session_participants WHERE netplay_session_id='${retrySessionID}'
GROUP BY player_no ORDER BY player_no;
`], { encoding: "utf8" }).trim();
    expect(ownership).toBe("1|1|1|1\n2|1|1|1");
    writeEvidence(testInfo, {
      pids: [host.pid, guest.pid], roomId: room.roomId, failedSessionId: failedSessionID,
      failedLaunchId: firstLaunch.launchId, failedRows, retrySessionId: retrySessionID,
      retryLaunchIds: [hostLaunch.launchId, guestLaunch.launchId], ownership,
    });
    await closeAcceptanceRoom(host, room.roomId, retrySessionID);
  } finally {
    if (coreMutated) execFileSync("sqlite3", [databasePath, "UPDATE cores SET requires_threads=0 WHERE id='fceumm';"]);
    await Promise.all([closePlayer(host), closePlayer(guest)]);
  }
});

test("ACC-NP-003 FCEUmm independent process baseline", async ({}, testInfo) => {
  writeEvidence(testInfo, await runBaseline(testInfo, "fceumm", 3_000, false));
});

test("ACC-NP-004 FBNeo independent process baseline", async ({}, testInfo) => {
  writeEvidence(testInfo, await runBaseline(testInfo, "fbneo", 3_000, false));
});

test("ACC-NP-005 deterministic 100ms rollback convergence", async ({}, testInfo) => {
  const results = [
    await runBaseline(testInfo, "fceumm", 3_000, true),
    await runBaseline(testInfo, "fbneo", 3_000, true),
  ];
  writeEvidence(testInfo, { seed: "frame*17+player*13", results });
});

test("ACC-NP-006 disconnect retains participant and resumes the original seat", async ({}, testInfo) => {
  const host = await launchPlayer("test", false);
  const guest = await launchPlayer("alice", false);
  try {
    const setup = await setupRoom(host, guest, "fceumm");
    const [hostPage, guestPage] = await Promise.all([
      openRuntime(host, setup.hostLaunch.playUrl), openRuntime(guest, setup.guestLaunch.playUrl),
    ]);
    await Promise.all([waitForFrame(hostPage, 600), waitForFrame(guestPage, 600)]);
    const before = await snapshot(guestPage);
    await guestPage.evaluate(() => window.__RETROM_NETPLAY_ACCEPTANCE__!.dropConnection(5_000));
    await guestPage.waitForFunction(
      () => (window.__RETROM_NETPLAY_ACCEPTANCE__?.snapshot().reconnects ?? 0) >= 1,
      null,
      { timeout: 15_000 },
    );
    await Promise.all([waitForFrame(hostPage, 1_200), waitForFrame(guestPage, 1_200)]);
    const [hostResult, guestResult] = await Promise.all([snapshot(hostPage), snapshot(guestPage)]);
    expect(guestResult.reconnects).toBe(1);
    expect(guestResult.resyncs).toBeGreaterThanOrEqual(1);
    expect(matchingCheckpoints(hostResult, guestResult).length).toBeGreaterThanOrEqual(3);
    const counts = execFileSync("sqlite3", [databasePath, `
SELECT
 (SELECT count(*) FROM netplay_session_participants WHERE netplay_session_id='${setup.sessionID}'),
 (SELECT count(*) FROM launch_sessions WHERE netplay_session_id='${setup.sessionID}'),
 (SELECT count(*) FROM play_sessions WHERE launch_session_id IN
   (SELECT id FROM launch_sessions WHERE netplay_session_id='${setup.sessionID}'));
`], { encoding: "utf8" }).trim();
    expect(counts).toBe("2|2|2");
    writeEvidence(testInfo, {
      browser: await host.browser.version(), pids: [host.pid, guest.pid], roomId: setup.room.roomId,
      sessionId: setup.sessionID, disconnectAtFrame: before.canonicalFrame, reconnectDelayMs: 5_000,
      rowCounts: { participants: 2, launches: 2, playSessions: 2 }, host: hostResult, guest: guestResult,
    });
  } finally {
    await Promise.all([closePlayer(host), closePlayer(guest)]);
  }
});

test("ACC-NP-007 guest timeout releases P2 and host timeout ends the room", async ({}, testInfo) => {
  const host = await launchPlayer("test", false);
  const guest = await launchPlayer("alice", false);
  const evidence: Record<string, unknown> = { browser: await host.browser.version(), pids: [host.pid, guest.pid] };
  try {
    const guestTimeout = await setupRoom(host, guest, "fceumm");
    const [hostPage, guestPage] = await Promise.all([
      openRuntime(host, guestTimeout.hostLaunch.playUrl), openRuntime(guest, guestTimeout.guestLaunch.playUrl),
    ]);
    await Promise.all([waitForFrame(hostPage, 120), waitForFrame(guestPage, 120)]);
    await guestPage.evaluate(() => window.__RETROM_NETPLAY_ACCEPTANCE__!.dropConnection(11_000));
    await hostPage.waitForFunction(
      () => window.__RETROM_NETPLAY_ACCEPTANCE__?.snapshot().endedReason === "PEER_TIMEOUT",
      null,
      { timeout: 20_000 },
    );
    const waitingResponse = await host.context.request.get(`/api/v1/netplay/rooms/${guestTimeout.room.roomId}`);
    expect(waitingResponse.ok()).toBe(true);
    const waiting = await waitingResponse.json() as RoomSnapshot;
    expect(waiting.state).toBe("WAITING");
    expect(waiting.members.map((member) => member.playerNo)).toEqual([1]);
    const revokedGuestLaunch = await guest.context.request.get(`/runtime/launches/${guestTimeout.guestLaunch.launchId}/config`);
    expect(revokedGuestLaunch.status()).toBe(401);
    await mutation(host, "DELETE", `/api/v1/netplay/rooms/${waiting.roomId}`, undefined, waiting.version);
    await Promise.all([hostPage.close(), guestPage.close()]);

    const hostTimeout = await setupRoom(host, guest, "fceumm");
    const [secondHostPage, secondGuestPage] = await Promise.all([
      openRuntime(host, hostTimeout.hostLaunch.playUrl), openRuntime(guest, hostTimeout.guestLaunch.playUrl),
    ]);
    await Promise.all([waitForFrame(secondHostPage, 120), waitForFrame(secondGuestPage, 120)]);
    await secondHostPage.evaluate(() => window.__RETROM_NETPLAY_ACCEPTANCE__!.dropConnection(11_000));
    await secondGuestPage.waitForFunction(
      () => window.__RETROM_NETPLAY_ACCEPTANCE__?.snapshot().endedReason === "HOST_LOST",
      null,
      { timeout: 20_000 },
    );
    const endedResponse = await guest.context.request.get(`/api/v1/netplay/rooms/${hostTimeout.room.roomId}`);
    expect(endedResponse.ok()).toBe(true);
    const ended = await endedResponse.json() as RoomSnapshot;
    expect(ended.state).toBe("ENDED");
    const staleConfig = await host.context.request.get(`/runtime/launches/${hostTimeout.hostLaunch.launchId}/config`);
    expect(staleConfig.status()).toBe(401);
    evidence.guestTimeout = {
      roomId: waiting.roomId, reason: "PEER_TIMEOUT", state: waiting.state,
      members: waiting.members.map((member) => member.playerNo), oldLaunchStatus: revokedGuestLaunch.status(),
    };
    evidence.hostTimeout = {
      roomId: ended.roomId, reason: "HOST_LOST", state: ended.state, oldLaunchStatus: staleConfig.status(),
    };
    writeEvidence(testInfo, evidence);
  } finally {
    await Promise.all([closePlayer(host), closePlayer(guest)]);
  }
});

test("ACC-NP-008 fourth desync closes an unstable room", async ({}, testInfo) => {
  const host = await launchPlayer("test", false);
  const guest = await launchPlayer("alice", false);
  try {
    const setup = await setupRoom(host, guest, "fceumm");
    const [hostPage, guestPage] = await Promise.all([
      openRuntime(host, setup.hostLaunch.playUrl), openRuntime(guest, setup.guestLaunch.playUrl),
    ]);
    for (let index = 1; index <= 4; index += 1) {
      await waitForFrame(guestPage, index * 180, 120_000);
      await guestPage.evaluate(() => window.__RETROM_NETPLAY_ACCEPTANCE__!.injectDesync());
      if (index <= 3) {
        await guestPage.waitForFunction(
          (expected) => (window.__RETROM_NETPLAY_ACCEPTANCE__?.snapshot().resyncs ?? 0) >= expected,
          index,
          { timeout: 60_000 },
        );
      }
    }
    await Promise.all([hostPage, guestPage].map((page) => page.waitForFunction(
      () => window.__RETROM_NETPLAY_ACCEPTANCE__?.snapshot().endedReason === "NETPLAY_UNSTABLE",
      null,
      { timeout: 60_000 },
    )));
    const [hostResult, guestResult] = await Promise.all([snapshot(hostPage), snapshot(guestPage)]);
    expect(guestResult.resyncs).toBe(3);
    expect(guestResult.stateLoads.filter((item) => item.nativeCompletion && item.coreExact).length).toBeGreaterThanOrEqual(4);
    expect(hostResult.endedReason).toBe("NETPLAY_UNSTABLE");
    writeEvidence(testInfo, {
      browser: await host.browser.version(), pids: [host.pid, guest.pid], roomId: setup.room.roomId,
      sessionId: setup.sessionID, mismatches: 4, host: hostResult, guest: guestResult,
    });
  } finally {
    await Promise.all([closePlayer(host), closePlayer(guest)]);
  }
});

test("ACC-NP-009 netplay never reads or mutates personal saves and capabilities stay owner-bound", async ({}, testInfo) => {
  const host = await launchPlayer("test", false);
  const guest = await launchPlayer("alice", false);
  const before = execFileSync("sqlite3", [databasePath, `
SELECT save.profile_id,blob.sha256,save.version,
 (SELECT count(*) FROM save_states state WHERE state.profile_id=save.profile_id)
FROM persistent_saves save
JOIN persistent_save_revisions revision ON revision.id=save.current_revision_id
JOIN blobs blob ON blob.id=revision.blob_id
WHERE save.game_variant_revision_id='01980000-0000-7000-8000-00000000c105'
ORDER BY save.profile_id;
`], { encoding: "utf8" }).trim();
  try {
    const setup = await setupRoom(host, guest, "fceumm");
    for (const [player, launch] of [[host, setup.hostLaunch], [guest, setup.guestLaunch]] as const) {
      const configResponse = await player.context.request.get(`/runtime/launches/${launch.launchId}/config`);
      expect(configResponse.ok()).toBe(true);
      const config = await configResponse.json() as {
        mode: string; persistentSaveMode: string; persistentSaveUrl: unknown; stateUrl: unknown;
      };
      expect(config).toMatchObject({
        mode: "netplay", persistentSaveMode: "NONE", persistentSaveUrl: null, stateUrl: null,
      });
      const probes = [
        await player.context.request.get(`/runtime/launches/${launch.launchId}/persistent-save`),
        await player.context.request.put(`/runtime/launches/${launch.launchId}/persistent-save`, {
          data: Buffer.from("x"),
          headers: {
            Origin: origin,
            "Content-Type": "application/octet-stream",
            "Content-Digest": "sha-256=:LXEWQrcmsEQBYnyp+6wy9chTD7GQPMTbAiWHF5IaSIE=:",
            "Idempotency-Key": randomUUID(), "X-Retrom-Save-Sequence": "1", "X-Retrom-Save-Event": "EXIT",
          },
        }),
        await player.context.request.post(`/runtime/launches/${launch.launchId}/save-states`, {
          headers: { Origin: origin, "Idempotency-Key": randomUUID() },
          multipart: {
            metadata: JSON.stringify({ name: "forbidden", discIndex: null }),
            state: { name: "state.bin", mimeType: "application/octet-stream", buffer: Buffer.from("state") },
            screenshot: { name: "shot.png", mimeType: "image/png", buffer: Buffer.from("shot") },
          },
        }),
        await player.context.request.get(`/runtime/launches/${launch.launchId}/state`),
      ];
      for (const response of probes) {
        expect(response.status()).toBe(409);
        expect((await response.json() as { error: { code: string } }).error.code).toBe("NETPLAY_SAVE_UNSUPPORTED");
      }
    }
    const crossOwner = await guest.context.request.get(
      `/runtime/launches/${setup.hostLaunch.launchId}/game/f1-race.nes`,
    );
    expect(crossOwner.status()).toBe(401);
    await closeAcceptanceRoom(host, setup.room.roomId, setup.sessionID);
    const after = execFileSync("sqlite3", [databasePath, `
SELECT save.profile_id,blob.sha256,save.version,
 (SELECT count(*) FROM save_states state WHERE state.profile_id=save.profile_id)
FROM persistent_saves save
JOIN persistent_save_revisions revision ON revision.id=save.current_revision_id
JOIN blobs blob ON blob.id=revision.blob_id
WHERE save.game_variant_revision_id='01980000-0000-7000-8000-00000000c105'
ORDER BY save.profile_id;
`], { encoding: "utf8" }).trim();
    expect(after).toBe(before);
    writeEvidence(testInfo, {
      pids: [host.pid, guest.pid], roomId: setup.room.roomId, saveRowsBefore: before, saveRowsAfter: after,
      saveRoutes: 8, errorCode: "NETPLAY_SAVE_UNSUPPORTED", crossOwnerStatus: crossOwner.status(),
    });
  } finally {
    await Promise.all([closePlayer(host), closePlayer(guest)]);
  }
});
