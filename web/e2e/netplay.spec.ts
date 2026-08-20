import { createHash } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { expect, test, type APIRequestContext, type Browser, type Page, type TestInfo } from "@playwright/test";

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
const alicePassword = "A1!retrom-netplay-acceptance";

type Auth = { csrfToken: string };
type Room = { roomId: string; version: number; currentSession?: { sessionId: string } | null };
type DiagnosticEvent = {
  kind: string; frame?: number; epoch?: number; depth?: number; predictionFrames?: number;
  coreDigest?: string; inputBufferFrames?: number; phase?: string; reason?: string;
  states?: number; predicted?: number; canonical?: number; stateBytes?: number;
  nativeCompletion?: boolean; coreExact?: boolean;
};

function evidencePath(testInfo: TestInfo, name: string) {
  const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!caseDirectory) return testInfo.outputPath(name);
  const directory = path.join(caseDirectory, "screenshots");
  mkdirSync(directory, { recursive: true });
  return path.join(directory, `${testInfo.project.name}-${name}`);
}

async function login(request: APIRequestContext, username: string, password: string) {
  const response = await request.post("/api/v1/auth/login", { data: { username, password }, headers: { Origin: origin } });
  return { response, auth: response.ok() ? await response.json() as Auth : null };
}

async function prepareContexts(browser: Browser) {
  const host = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const guest = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const hostLogin = await login(host.request, "test", "test");
  expect(hostLogin.response.ok()).toBe(true);
  let guestLogin = await login(guest.request, "alice", alicePassword);
  if (!guestLogin.response.ok()) {
    const invitation = await host.request.post("/api/v1/admin/invitations", {
      data: { role: "USER", confirmAdminRole: false },
      headers: { Origin: origin, "X-Retrom-Csrf": hostLogin.auth!.csrfToken, "Idempotency-Key": crypto.randomUUID() },
    });
    expect(invitation.status()).toBe(201);
    const token = new URL((await invitation.json() as { url: string }).url).hash.replace("#invite=", "");
    const accepted = await guest.request.post("/api/v1/auth/invitations/accept", {
      data: { token, username: "alice", displayName: "Alice", password: alicePassword, passwordConfirmation: alicePassword },
      headers: { Origin: origin },
    });
    expect(accepted.status()).toBe(201);
    guestLogin = { response: accepted, auth: await accepted.json() as Auth };
  }
  return { host, guest, hostAuth: hostLogin.auth!, guestAuth: guestLogin.auth! };
}

function writeHeaders(csrfToken: string, etag?: string) {
  return {
    Origin: origin,
    "X-Retrom-Csrf": csrfToken,
    "Idempotency-Key": crypto.randomUUID(),
    ...(etag ? { "If-Match": etag } : {}),
  };
}

async function mutateRoom(request: APIRequestContext, csrfToken: string, roomId: string, method: "POST" | "PUT", suffix: string, data: unknown) {
  const current = await request.get(`/api/v1/netplay/rooms/${roomId}`);
  expect(current.ok()).toBe(true);
  const response = await request.fetch(`/api/v1/netplay/rooms/${roomId}${suffix}`, {
    method, data, headers: writeHeaders(csrfToken, current.headers().etag),
  });
  expect(response.ok(), `${method} ${suffix}: ${await response.text()}`).toBe(true);
  return response.json() as Promise<Room>;
}

async function installDiagnostics(page: Page) {
  await page.addInitScript(() => {
    const state = { events: [] as Array<Record<string, unknown>>, delayMS: 0 };
    const target = window as typeof window & {
      __RETROM_NETPLAY_DIAGNOSTICS_FACTORY__?: (controls: unknown) => unknown;
      __RETROM_NETPLAY_ACCEPTANCE__?: typeof state & { controls?: { dropConnection: (durationMS: number) => void } };
    };
    target.__RETROM_NETPLAY_ACCEPTANCE__ = state;
    target.__RETROM_NETPLAY_DIAGNOSTICS_FACTORY__ = (controls) => {
      target.__RETROM_NETPLAY_ACCEPTANCE__!.controls = controls as { dropConnection: (durationMS: number) => void };
      const record = (kind: string, value: Record<string, unknown>) => state.events.push({ kind, ...value });
      return {
        delayForMessage: (type: string) => type === "INPUT" ? state.delayMS : 0,
        onConnect: (reconnect: boolean) => record("connect", { reconnect }),
        onStateCapture: (value: Record<string, unknown>) => record("state-capture", value),
        onStateLoad: (value: Record<string, unknown>) => record("state-load", value),
        onEpoch: (value: Record<string, unknown>) => record("epoch", value),
        onCanonical: (value: Record<string, unknown>) => record("canonical", value),
        onRollback: (value: Record<string, unknown>) => record("rollback", value),
        onCheckpoint: (value: Record<string, unknown>) => record("checkpoint", value),
        onLockstep: (value: Record<string, unknown>) => record("lockstep", value),
        onFrameStep: (value: Record<string, unknown>) => record("frame-step", value),
        onRetained: (value: Record<string, unknown>) => record("retained", value),
        onEnded: (reason: string) => record("ended", { reason }),
      };
    };
  });
}

async function diagnosticEvents(page: Page) {
  return page.evaluate(() => ((window as typeof window & {
    __RETROM_NETPLAY_ACCEPTANCE__?: { events: DiagnosticEvent[] };
  }).__RETROM_NETPLAY_ACCEPTANCE__?.events ?? []));
}

async function waitForFrame(page: Page, frame: number) {
  await expect.poll(async () => Math.max(-1, ...(await diagnosticEvents(page))
    .filter((event) => event.kind === "canonical").map((event) => event.frame ?? -1)), {
    timeout: 60_000, intervals: [100, 250, 500],
  }).toBeGreaterThanOrEqual(frame);
}

async function canvasDigest(page: Page, testInfo: TestInfo, name: string) {
  const canvas = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas.ejs_canvas");
  await expect(canvas).toBeVisible({ timeout: 30_000 });
  const overlays = page.locator(".player-pause-overlay, .player-controls-hint");
  await overlays.evaluateAll((elements) => elements.forEach((element) => {
    (element as HTMLElement).style.visibility = "hidden";
  }));
  try {
    return createHash("sha256").update(await canvas.screenshot({ path: evidencePath(testInfo, name) })).digest("hex");
  } finally {
    await overlays.evaluateAll((elements) => elements.forEach((element) => {
      (element as HTMLElement).style.removeProperty("visibility");
    }));
  }
}

async function setDirectionalInput(page: Page, pressed: boolean) {
  if (pressed) {
    const canvas = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas.ejs_canvas");
    await canvas.click({ position: { x: 64, y: 64 } });
    await page.keyboard.down("ArrowLeft");
  } else {
    await page.keyboard.up("ArrowLeft");
  }
}

async function tapDirectionalInput(page: Page, frames = 12) {
  const before = Math.max(-1, ...(await diagnosticEvents(page))
    .filter((event) => event.kind === "canonical").map((event) => event.frame ?? -1));
  await setDirectionalInput(page, true);
  await waitForFrame(page, before + frames);
  await setDirectionalInput(page, false);
}

async function createSession(
  browser: Browser,
  testInfo: TestInfo,
  gameId: string,
  profileId: "fceumm-423-v1" | "fbneo-423-v1",
) {
  const contexts = await prepareContexts(browser);
  const { host, guest, hostAuth, guestAuth } = contexts;
  await host.tracing.start({ screenshots: true, snapshots: true });
  await guest.tracing.start({ screenshots: true, snapshots: true });
  const created = await host.request.post("/api/v1/netplay/rooms", { data: {}, headers: writeHeaders(hostAuth.csrfToken) });
  expect(created.status()).toBe(201);
  let room = await created.json() as Room;
  room = await mutateRoom(host.request, hostAuth.csrfToken, room.roomId, "PUT", "/game", { gameId, netplayProfileId: profileId });
  room = await mutateRoom(guest.request, guestAuth.csrfToken, room.roomId, "PUT", "/members/me/seat", { playerNo: 2 });
  room = await mutateRoom(host.request, hostAuth.csrfToken, room.roomId, "PUT", "/members/me/ready", { ready: true });
  room = await mutateRoom(guest.request, guestAuth.csrfToken, room.roomId, "PUT", "/members/me/ready", { ready: true });
  room = await mutateRoom(host.request, hostAuth.csrfToken, room.roomId, "POST", "/start", {});
  const sessionId = room.currentSession?.sessionId;
  expect(sessionId).toBeTruthy();
  const launchPath = `/api/v1/netplay/rooms/${room.roomId}/sessions/${sessionId}/launch`;
  const capabilities = { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true };
  const [hostLaunchResponse, guestLaunchResponse] = await Promise.all([
    host.request.post(launchPath, { data: { clientCapabilities: capabilities }, headers: writeHeaders(hostAuth.csrfToken) }),
    guest.request.post(launchPath, { data: { clientCapabilities: capabilities }, headers: writeHeaders(guestAuth.csrfToken) }),
  ]);
  expect(hostLaunchResponse.ok()).toBe(true);
  expect(guestLaunchResponse.ok()).toBe(true);
  const hostLaunch = await hostLaunchResponse.json() as { launchId: string; playUrl: string };
  const guestLaunch = await guestLaunchResponse.json() as { launchId: string; playUrl: string };
  const hostPage = await host.newPage();
  const guestPage = await guest.newPage();
  await Promise.all([installDiagnostics(hostPage), installDiagnostics(guestPage)]);
  const consoleErrors: string[] = [];
  for (const [label, page] of [["P1", hostPage], ["P2", guestPage]] as const) {
    page.on("console", (message) => { if (message.type() === "error") consoleErrors.push(`${label}:${message.text()}`); });
    page.on("pageerror", (error) => consoleErrors.push(`${label}:${error.message}`));
  }
  const configResponses: Array<Record<string, unknown>> = [];
  for (const page of [hostPage, guestPage]) page.on("response", async (response) => {
    if (/\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.ok()) configResponses.push(await response.json() as Record<string, unknown>);
  });
  await Promise.all([hostPage.goto(hostLaunch.playUrl), guestPage.goto(guestLaunch.playUrl)]);
  await Promise.all([
    expect(hostPage.locator(".player-loading")).toBeHidden({ timeout: 45_000 }),
    expect(guestPage.locator(".player-loading")).toBeHidden({ timeout: 45_000 }),
  ]);
  await expect.poll(async () => (await diagnosticEvents(hostPage)).filter((event) => event.kind === "epoch").length, { timeout: 45_000 }).toBeGreaterThan(0);
  await expect.poll(async () => (await diagnosticEvents(guestPage)).filter((event) => event.kind === "epoch").length, { timeout: 45_000 }).toBeGreaterThan(0);
  const cleanup = async () => {
    const [hostEvents, guestEvents] = await Promise.all([
      diagnosticEvents(hostPage).catch(() => []), diagnosticEvents(guestPage).catch(() => []),
    ]);
    writeFileSync(evidencePath(testInfo, `${profileId}-diagnostics.json`), `${JSON.stringify({
      roomId: room.roomId, sessionId, profileId,
      fixtureSha256: profileId === "fceumm-423-v1"
        ? process.env.RETROM_NETPLAY_NES_FIXTURE_SHA256
        : process.env.RETROM_NETPLAY_FBNEO_FIXTURE_SHA256,
      configResponses, hostEvents, guestEvents, consoleErrors,
    }, null, 2)}\n`);
    await Promise.all([
      hostPage.screenshot({ path: evidencePath(testInfo, `${profileId}-p1-final.png`), fullPage: true }).catch(() => undefined),
      guestPage.screenshot({ path: evidencePath(testInfo, `${profileId}-p2-final.png`), fullPage: true }).catch(() => undefined),
    ]);
    await Promise.all([
      host.tracing.stop({ path: evidencePath(testInfo, `${profileId}-p1-trace.zip`) }).catch(() => undefined),
      guest.tracing.stop({ path: evidencePath(testInfo, `${profileId}-p2-trace.zip`) }).catch(() => undefined),
    ]);
    const currentRoom = await host.request.get(`/api/v1/netplay/rooms/${room.roomId}`).catch(() => null);
    if (currentRoom?.ok()) {
      await host.request.delete(`/api/v1/netplay/rooms/${room.roomId}`, {
        headers: writeHeaders(hostAuth.csrfToken, currentRoom.headers().etag),
      }).catch(() => undefined);
    }
    await Promise.all([host.close(), guest.close()]);
  };
  return { ...contexts, hostPage, guestPage, roomId: room.roomId, sessionId: sessionId!, hostLaunch, guestLaunch, configResponses, consoleErrors, cleanup };
}

async function matchingCheckpoint(p1: Page, p2: Page, minimumFrame: number) {
  await Promise.all([waitForFrame(p1, minimumFrame), waitForFrame(p2, minimumFrame)]);
  const [left, right] = await Promise.all([diagnosticEvents(p1), diagnosticEvents(p2)]);
  const rightByFrame = new Map(right.filter((event) => event.kind === "checkpoint").map((event) => [event.frame, event.coreDigest]));
  const matched = left.filter((event) => event.kind === "checkpoint" && (event.frame ?? -1) >= minimumFrame)
    .find((event) => rightByFrame.get(event.frame) === event.coreDigest);
  expect(matched, `no matching checkpoint at or after ${minimumFrame}`).toBeTruthy();
  return matched!;
}

async function pauseSession(session: Awaited<ReturnType<typeof createSession>>) {
  const response = await session.host.request.post(
    `/api/v1/netplay/rooms/${session.roomId}/sessions/${session.sessionId}/pause`,
    { data: {}, headers: writeHeaders(session.hostAuth.csrfToken) },
  );
  expect(response.status()).toBe(202);
  await Promise.all([
    expect(session.hostPage.locator(".player-pause-pill")).toContainText("联机已暂停", { timeout: 30_000 }),
    expect(session.guestPage.locator(".player-pause-pill")).toContainText("联机已暂停", { timeout: 30_000 }),
  ]);
}

test.use({ trace: "off" });

test.describe.serial("real dual-browser netplay", () => {
  test("ACC-NP-014 FCEUmm dual-browser rollback product chain", async ({ browser }, testInfo) => {
    test.setTimeout(240_000);
    test.skip(testInfo.project.name !== "chrome-1280", "The stateful dual-browser core case runs once.");
    const gameId = process.env.RETROM_NETPLAY_NES_GAME_ID;
    expect(gameId).toBeTruthy();
    const session = await createSession(browser, testInfo, gameId!, "fceumm-423-v1");
    try {
      expect(session.configResponses).toHaveLength(2);
      expect(session.configResponses.every((config) => !config.biosUrl)).toBe(true);
      const initial = await Promise.all([diagnosticEvents(session.hostPage), diagnosticEvents(session.guestPage)]);
      expect(initial[0].some((event) => event.kind === "state-capture")).toBe(true);
      expect(initial[1].some((event) => event.kind === "state-load" && event.nativeCompletion && event.coreExact)).toBe(true);
      await tapDirectionalInput(session.hostPage);
      await session.guestPage.evaluate(() => {
        const state = (window as typeof window & { __RETROM_NETPLAY_ACCEPTANCE__?: { delayMS: number } }).__RETROM_NETPLAY_ACCEPTANCE__!;
        state.delayMS = 80;
      });
      await setDirectionalInput(session.guestPage, true);
      await expect.poll(async () => (await diagnosticEvents(session.hostPage))
        .filter((event) => event.kind === "rollback" && (event.depth ?? 0) >= 1 && (event.depth ?? 0) <= 8).length,
      { timeout: 30_000 }).toBeGreaterThan(0);
      await session.guestPage.evaluate(() => {
        const state = (window as typeof window & { __RETROM_NETPLAY_ACCEPTANCE__?: { delayMS: number } }).__RETROM_NETPLAY_ACCEPTANCE__!;
        state.delayMS = 0;
      });
      await setDirectionalInput(session.guestPage, false);
      const checkpoint = await matchingCheckpoint(session.hostPage, session.guestPage, 359);
      expect(checkpoint.coreDigest).toMatch(/^[0-9a-f]{64}$/);
      const [hostEvents, guestEvents] = await Promise.all([
        diagnosticEvents(session.hostPage), diagnosticEvents(session.guestPage),
      ]);
      const guestCheckpointByFrame = new Map(guestEvents
        .filter((event) => event.kind === "checkpoint").map((event) => [event.frame, event.coreDigest]));
      for (const frame of [119, 239, 359]) {
        expect(hostEvents.find((event) => event.kind === "checkpoint" && event.frame === frame)?.coreDigest)
          .toBe(guestCheckpointByFrame.get(frame));
      }
      for (const retained of [...hostEvents, ...guestEvents].filter((event) => event.kind === "retained")) {
        expect(retained.states ?? 0).toBeLessThanOrEqual(121);
        expect(retained.predicted ?? 0).toBeLessThanOrEqual(130);
        expect(retained.canonical ?? 0).toBeLessThanOrEqual(130);
        expect(retained.stateBytes ?? 0).toBeLessThanOrEqual(128 * 1024 * 1024);
      }
      await pauseSession(session);
      expect(await canvasDigest(session.hostPage, testInfo, "fceumm-p1-canvas.png"))
        .toBe(await canvasDigest(session.guestPage, testInfo, "fceumm-p2-canvas.png"));
      await Promise.all([
        session.hostPage.screenshot({ path: evidencePath(testInfo, "fceumm-p1-terminal.png"), fullPage: true }),
        session.guestPage.screenshot({ path: evidencePath(testInfo, "fceumm-p2-terminal.png"), fullPage: true }),
      ]);
      expect(session.consoleErrors).toEqual([]);
    } finally {
      await session.cleanup();
    }
  });

  test("ACC-NP-015 FBNeo strict lockstep and LAN baseline", async ({ browser }, testInfo) => {
    test.setTimeout(240_000);
    test.skip(testInfo.project.name !== "chrome-1280", "The stateful dual-browser core case runs once.");
    const gameId = process.env.RETROM_NETPLAY_FBNEO_GAME_ID;
    expect(gameId).toBeTruthy();
    const session = await createSession(browser, testInfo, gameId!, "fbneo-423-v1");
    try {
      await waitForFrame(session.hostPage, 239);
      await tapDirectionalInput(session.hostPage);
      await session.guestPage.evaluate(() => {
        const state = (window as typeof window & { __RETROM_NETPLAY_ACCEPTANCE__?: { delayMS: number } }).__RETROM_NETPLAY_ACCEPTANCE__!;
        state.delayMS = 100;
      });
      await setDirectionalInput(session.guestPage, true);
      await expect.poll(async () => Math.max(1, ...(await diagnosticEvents(session.hostPage))
        .filter((event) => event.kind === "lockstep").map((event) => event.inputBufferFrames ?? 1)), { timeout: 30_000 }).toBeGreaterThan(1);
      await session.guestPage.evaluate(() => {
        const state = (window as typeof window & { __RETROM_NETPLAY_ACCEPTANCE__?: { delayMS: number } }).__RETROM_NETPLAY_ACCEPTANCE__!;
        state.delayMS = 0;
      });
      await setDirectionalInput(session.guestPage, false);
      await matchingCheckpoint(session.hostPage, session.guestPage, 719);
      const events = await diagnosticEvents(session.hostPage);
      expect(events.filter((event) => event.kind === "canonical").every((event) => (event.predictionFrames ?? 0) === 0)).toBe(true);
      expect(events.filter((event) => event.kind === "rollback")).toHaveLength(0);
      const buffers = events.filter((event) => event.kind === "lockstep").map((event) => event.inputBufferFrames ?? 1);
      expect(buffers.at(-1)!).toBeLessThan(Math.max(...buffers));
      await pauseSession(session);
      expect(await canvasDigest(session.hostPage, testInfo, "fbneo-p1-canvas.png"))
        .toBe(await canvasDigest(session.guestPage, testInfo, "fbneo-p2-canvas.png"));
      expect(session.configResponses.every((config) => Boolean(config.parentUrl) && Boolean(config.biosUrl))).toBe(true);
      expect(session.consoleErrors).toEqual([]);
    } finally {
      await session.cleanup();
    }
  });

  for (const [profileId, gameEnvironment] of [
    ["fceumm-423-v1", "RETROM_NETPLAY_NES_GAME_ID"],
    ["fbneo-423-v1", "RETROM_NETPLAY_FBNEO_GAME_ID"],
  ] as const) {
    test(`ACC-NP-016 ${profileId} background scheduling and reconnect identity`, async ({ browser }, testInfo) => {
      test.setTimeout(240_000);
      test.skip(testInfo.project.name !== "chrome-1280", "The stateful dual-browser lifecycle case runs once.");
      const gameId = process.env[gameEnvironment];
      expect(gameId).toBeTruthy();
      const session = await createSession(browser, testInfo, gameId!, profileId);
      try {
        await waitForFrame(session.guestPage, 59);
        const cdp = await session.guest.newCDPSession(session.guestPage);
        await cdp.send("Page.setWebLifecycleState", { state: "frozen" });
        await new Promise((resolve) => setTimeout(resolve, 3_000));
        await cdp.send("Page.setWebLifecycleState", { state: "active" });
        await waitForFrame(session.guestPage, 119);
        expect((await diagnosticEvents(session.guestPage)).filter((event) => event.kind === "ended")).toHaveLength(0);
        const epochBefore = Math.max(...(await diagnosticEvents(session.guestPage)).filter((event) => event.kind === "epoch").map((event) => event.epoch ?? 0));
        await session.guestPage.evaluate(() => {
          (window as typeof window & { __RETROM_NETPLAY_ACCEPTANCE__?: { controls?: { dropConnection: (durationMS: number) => void } } })
            .__RETROM_NETPLAY_ACCEPTANCE__!.controls!.dropConnection(3_000);
        });
        await expect.poll(async () => Math.max(...(await diagnosticEvents(session.guestPage))
          .filter((event) => event.kind === "epoch").map((event) => event.epoch ?? 0)), { timeout: 30_000 }).toBeGreaterThan(epochBefore);
        await waitForFrame(session.guestPage, 239);
        const room = await session.host.request.get(`/api/v1/netplay/rooms/${session.roomId}`);
        expect(room.ok()).toBe(true);
        expect((await room.json() as Room).currentSession?.sessionId).toBe(session.sessionId);
        expect(session.configResponses).toHaveLength(2);
        expect((await diagnosticEvents(session.guestPage)).filter((event) => event.kind === "ended")).toHaveLength(0);
        expect(session.consoleErrors).toEqual([]);
      } finally {
        await session.cleanup();
      }
    });
  }
});
