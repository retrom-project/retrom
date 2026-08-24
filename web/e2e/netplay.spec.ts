import { createHash } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { expect, test, type APIRequestContext, type Browser, type Page, type TestInfo } from "@playwright/test";
import {
  checkpointMismatches, checkpointPair, diagnosticEvents, matchingCheckpoint, verifySNESNoOpHashRecovery,
  type DiagnosticEvent,
} from "./netplay-checkpoints";

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
const alicePassword = "A1!retrom-netplay-acceptance";

type Auth = { csrfToken: string };
type Room = { roomId: string; version: number; currentSession?: { sessionId: string } | null };
type NetplayProfileId =
  | "fceumm-423-v1"
  | "fbneo-423-v1"
  | "snes9x-423-v1"
  | "nestopia-423-v1"
  | "mame2003-423-override-v1"
  | "mame2003-plus-423-v1"
  | "fbalpha2012-cps1-423-v1"
  | "fbalpha2012-cps2-423-v1";
type ExpansionResult = {
  caseId: string;
  coreId: string;
  expectsBios?: boolean;
  expectsParent?: boolean;
  fixtureSha256: string;
  gameId: string;
  profileId: NetplayProfileId;
};
type CatalogGame = {
  availability: "SUPPORTED" | "UNSUPPORTED";
  blockerCode: string | null;
  gameId: string;
  title: string;
  platformId: string;
  platformName: string;
  platformInstanceName: string;
  netplayProfiles: Array<{ id: string; coreName: string }>;
};

async function recordProfileEligibility(
  request: APIRequestContext,
  testInfo: TestInfo,
  gameId: string,
  profileId: NetplayProfileId,
) {
  const detailResponse = await request.get(`/api/v1/games/${gameId}`);
  const detail = detailResponse.ok() ? await detailResponse.json() as { coreOptions?: unknown } : {};
  let catalogStatus = 200;
  let cursor: string | null = null;
  let game: CatalogGame | undefined;
  do {
    const suffix = cursor ? `&cursor=${encodeURIComponent(cursor)}` : "";
    const catalogResponse = await request.get(`/api/v1/netplay/games?availability=ALL&limit=100${suffix}`);
    catalogStatus = catalogResponse.status();
    if (!catalogResponse.ok()) {break;}
    const catalog = await catalogResponse.json() as { items: CatalogGame[]; nextCursor: string | null };
    game = catalog.items.find((item) => item.gameId === gameId);
    cursor = game ? null : catalog.nextCursor;
  } while (cursor);
  writeFileSync(evidencePath(testInfo, `${profileId}-eligibility.json`), `${JSON.stringify({
    catalogStatus, detailStatus: detailResponse.status(), game,
    coreOptions: detail.coreOptions ?? null,
  }, null, 2)}\n`);
  expect(catalogStatus, "netplay eligibility catalog response").toBe(200);
  expect(detailResponse.ok(), "netplay game detail response").toBe(true);
  expect(game, `netplay catalog game ${gameId}`).toBeTruthy();
  expect(game).toMatchObject({ availability: "SUPPORTED", blockerCode: null });
  expect(game!.netplayProfiles.some((profile) => profile.id === profileId),
    `netplay profile ${profileId} eligibility`).toBe(true);
}

function evidencePath(testInfo: TestInfo, name: string) {
  const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!caseDirectory) {return testInfo.outputPath(name);}
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
      const record = (kind: string, value: Record<string, unknown>) => {
        state.events.push({ eventSeq: state.events.length + 1, kind, ...value });
      };
      return {
        delayForMessage: (type: string) => type === "INPUT" ? state.delayMS : 0,
        onConnect: (reconnect: boolean) => record("connect", { reconnect }),
        onStateCapture: (value: Record<string, unknown>) => record("state-capture", value),
        onAuthorityNormalization: (value: Record<string, unknown>) => record("authority-normalization", value),
        onStateLoad: (value: Record<string, unknown>) => record("state-load", value),
        onPause: (value: Record<string, unknown>) => record("pause", value),
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

async function waitForFrame(page: Page, frame: number) {
  await expect.poll(async () => Math.max(-1, ...(await diagnosticEvents(page))
    .filter((event) => event.kind === "canonical").map((event) => event.frame ?? -1)), {
    timeout: 60_000, intervals: [100, 250, 500],
  }).toBeGreaterThanOrEqual(frame);
}

async function canvasDigest(page: Page, testInfo: TestInfo, name: string) {
  const canvas = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas.ejs_canvas");
  await expect(canvas).toBeVisible({ timeout: 30_000 });
  const chrome = page.locator(".player-toolbar,.player-pause-overlay,.player-debug-panel,.player-toast,.player-controls-hint,.player-emulator-toolbar,nextjs-portal");
  const visibility = await chrome.evaluateAll((elements) => elements.map((element) => (element as HTMLElement).style.visibility));
  try {
    await chrome.evaluateAll((elements) => {for (const element of elements) {(element as HTMLElement).style.visibility = "hidden";}});
    await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    // A WebGL canvas without preserveDrawingBuffer may return an already
    // cleared backing store from toDataURL even while Chrome still displays
    // the correct compositor frame. Capture the visible canvas with Retrom
    // chrome hidden so the digest compares the frame users actually see.
    const png = await canvas.screenshot();
    writeFileSync(evidencePath(testInfo, name), png);
    const brightPixels = await page.evaluate(async (encoded) => {
      const bytes = Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0));
      const bitmap = await createImageBitmap(new Blob([bytes], { type: "image/png" }));
      const decoded = document.createElement("canvas");
      decoded.width = bitmap.width; decoded.height = bitmap.height;
      const context = decoded.getContext("2d", { willReadFrequently: true });
      if (!context) {throw new Error("canvas evidence decoder unavailable");}
      context.drawImage(bitmap, 0, 0); bitmap.close();
      const pixels = context.getImageData(0, 0, decoded.width, decoded.height).data;
      let count = 0;
      for (let offset = 0; offset < pixels.length; offset += 4) {
        if (pixels[offset]! > 0 || pixels[offset + 1]! > 0 || pixels[offset + 2]! > 0) {count += 1;}
      }
      return count;
    }, png.toString("base64"));
    return { digest: createHash("sha256").update(png).digest("hex"), brightPixels };
  } finally {
    await chrome.evaluateAll((elements, values) => {
      elements.forEach((element, index) => {(element as HTMLElement).style.visibility = values[index] ?? "";});
    }, visibility);
  }
}

async function waitForCPSVisualStability(
  session: Awaited<ReturnType<typeof createSession>>,
  coreId: string,
) {
  if (!coreId.startsWith("fbalpha2012_cps")) {return;}
  const epochs = (await diagnosticEvents(session.guestPage)).filter((event) => event.kind === "epoch");
  const nextFrame = epochs.at(-1)?.nextFrame;
  expect(nextFrame).toBeGreaterThanOrEqual(0);
  await Promise.all([
    waitForFrame(session.hostPage, nextFrame! + 180),
    waitForFrame(session.guestPage, nextFrame! + 180),
  ]);
}

async function setDirectionalInput(page: Page, pressed: boolean) {
  if (pressed) {
    const canvas = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas.ejs_canvas");
    await canvas.click({ position: { x: 64, y: 64 } });
    // Each netplay browser contributes its local P1 controls; the bridge maps
    // that input to the participant seat before sending it to the server.
    await page.keyboard.down("a");
  } else {
    await page.keyboard.up("a");
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
  profileId: NetplayProfileId,
  fixtureSha256: string,
) {
  const contexts = await prepareContexts(browser);
  const { host, guest, hostAuth, guestAuth } = contexts;
  await recordProfileEligibility(host.request, testInfo, gameId, profileId);
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
    page.on("console", (message) => { if (message.type() === "error") {consoleErrors.push(`${label}:${message.text()}`);} });
    page.on("pageerror", (error) => consoleErrors.push(`${label}:${error.message}`));
  }
  const configResponses: Array<Record<string, unknown>> = [];
  for (const page of [hostPage, guestPage]) {page.on("response", async (response) => {
    if (/\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.ok()) {configResponses.push(await response.json() as Record<string, unknown>);}
  });}
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
      fixtureSha256,
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

async function waitForTransportRecovery(
  session: Awaited<ReturnType<typeof createSession>>,
  sourceEpoch: number,
  hostEventSeq: number,
  guestEventSeq: number,
) {
  let recovery: {
    hostPause?: DiagnosticEvent; guestPause?: DiagnosticEvent; connect?: DiagnosticEvent;
    capture?: DiagnosticEvent; load?: DiagnosticEvent; hostEpoch?: DiagnosticEvent; guestEpoch?: DiagnosticEvent;
  } = {};
  await expect.poll(async () => {
    const [hostEvents, guestEvents] = await Promise.all([
      diagnosticEvents(session.hostPage), diagnosticEvents(session.guestPage),
    ]);
    const hostPause = hostEvents.find((event) => (event.eventSeq ?? 0) > hostEventSeq &&
      event.kind === "pause" && event.epoch === sourceEpoch && event.reason === "PEER_DISCONNECTED");
    const nextFrame = hostPause === undefined ? undefined : (hostPause.atFrame ?? -1) + 1;
    recovery = {
      hostPause,
      guestPause: guestEvents.find((event) => (event.eventSeq ?? 0) > guestEventSeq &&
        event.kind === "pause" && event.epoch === sourceEpoch && event.reason === "PEER_DISCONNECTED" &&
        event.atFrame === hostPause?.atFrame),
      connect: guestEvents.find((event) => (event.eventSeq ?? 0) > guestEventSeq && event.kind === "connect" && event.reconnect),
      capture: hostEvents.find((event) => (event.eventSeq ?? 0) > hostEventSeq &&
        event.kind === "state-capture" && event.epoch === sourceEpoch && event.nextFrame === nextFrame),
      load: guestEvents.find((event) => (event.eventSeq ?? 0) > guestEventSeq &&
        event.kind === "state-load" && event.epoch === sourceEpoch && event.nextFrame === nextFrame),
      hostEpoch: hostEvents.find((event) => (event.eventSeq ?? 0) > hostEventSeq &&
        event.kind === "epoch" && event.resync && event.epoch === sourceEpoch + 1 && event.nextFrame === nextFrame),
      guestEpoch: guestEvents.find((event) => (event.eventSeq ?? 0) > guestEventSeq &&
        event.kind === "epoch" && event.resync && event.epoch === sourceEpoch + 1 && event.nextFrame === nextFrame),
    };
    return Object.values(recovery).every(Boolean);
  }, { timeout: 30_000, intervals: [100, 250, 500] }).toBe(true);
  const result = recovery as Required<typeof recovery>;
  expect(result.guestPause.atFrame).toBe(result.hostPause.atFrame);
  expect(result.capture.stateDigest).toMatch(/^[0-9a-f]{64}$/);
  expect(result.capture.coreDigest).toMatch(/^[0-9a-f]{64}$/);
  expect(result.capture.stateDigest).toBe(result.load.stateDigest);
  expect(result.capture.coreDigest).toBe(result.load.coreDigest);
  expect(result.load).toMatchObject({ nativeCompletion: true, coreExact: true });
  return result.guestEpoch;
}

async function verifyStrictCheckpointPhase(
  session: Awaited<ReturnType<typeof createSession>>,
  profileId: NetplayProfileId,
) {
  const checkpoints = await Promise.all([119, 239, 719]
    .map((frame) => checkpointPair(session.hostPage, session.guestPage, frame)));
  for (const pair of checkpoints) {
    expect(pair.host.coreDigest).toMatch(/^[0-9a-f]{64}$/);
    expect(pair.guest.coreDigest).toMatch(/^[0-9a-f]{64}$/);
  }
  const checkpointEvents = await Promise.all([
    diagnosticEvents(session.hostPage), diagnosticEvents(session.guestPage),
  ]);
  const mismatches = checkpointMismatches(...checkpointEvents);
  const recoveredHashMismatch = mismatches.length > 0;
  if (recoveredHashMismatch) {
    expect(profileId, "only the documented SNES boundary no-op recovery is accepted").toBe("snes9x-423-v1");
    expect(mismatches).toHaveLength(1);
    await verifySNESNoOpHashRecovery(session, mismatches[0]!);
  } else {
    for (const pair of checkpoints) {expect(pair.host.coreDigest).toBe(pair.guest.coreDigest);}
  }
  const [hostEvents, guestEvents] = await Promise.all([
    diagnosticEvents(session.hostPage), diagnosticEvents(session.guestPage),
  ]);
  expect([...hostEvents, ...guestEvents].filter((event) => event.kind === "rollback")).toHaveLength(0);
  expect([...hostEvents, ...guestEvents].filter((event) => event.kind === "canonical")
    .every((event) => event.predictionFrames === 0)).toBe(true);
  return { recoveredHashMismatch, guestEvents };
}

async function verifyLockstepBufferAdaptation(session: Awaited<ReturnType<typeof createSession>>) {
  await tapDirectionalInput(session.hostPage);
  const beforeDelay = (await diagnosticEvents(session.guestPage)).filter((event) => event.kind === "lockstep");
  const delayStartFrame = beforeDelay.at(-1)?.frame ?? -1;
  const baselineBuffer = beforeDelay.at(-1)?.inputBufferFrames ?? 1;
  await session.guestPage.evaluate(() => {
    const state = (window as typeof window & { __RETROM_NETPLAY_ACCEPTANCE__?: { delayMS: number } })
      .__RETROM_NETPLAY_ACCEPTANCE__!;
    state.delayMS = 100;
  });
  await setDirectionalInput(session.guestPage, true);
  let elevatedBuffer = baselineBuffer;
  await expect.poll(async () => {
    const samples = (await diagnosticEvents(session.guestPage)).filter((event) =>
      event.kind === "lockstep" && (event.frame ?? -1) > delayStartFrame);
    elevatedBuffer = Math.max(baselineBuffer, ...samples.map((event) => event.inputBufferFrames ?? 1));
    return elevatedBuffer;
  }, { timeout: 30_000 }).toBeGreaterThan(baselineBuffer);
  const recoveryStartFrame = Math.max(...(await diagnosticEvents(session.guestPage))
    .filter((event) => event.kind === "lockstep").map((event) => event.frame ?? -1));
  await session.guestPage.evaluate(() => {
    const state = (window as typeof window & { __RETROM_NETPLAY_ACCEPTANCE__?: { delayMS: number } })
      .__RETROM_NETPLAY_ACCEPTANCE__!;
    state.delayMS = 0;
  });
  await setDirectionalInput(session.guestPage, false);
  let recoverySamples = 0;
  await expect.poll(async () => {
    const samples = (await diagnosticEvents(session.guestPage)).filter((event) =>
      event.kind === "lockstep" && (event.frame ?? -1) > recoveryStartFrame);
    const recoveredIndex = samples.findIndex((event) => (event.inputBufferFrames ?? 1) < elevatedBuffer);
    recoverySamples = recoveredIndex + 1;
    return recoveredIndex >= 0;
  }, { timeout: 30_000 }).toBe(true);
  expect(recoverySamples).toBeGreaterThanOrEqual(120);
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

function expansionResult(caseId: string) {
  const encoded = process.env.RETROM_NETPLAY_EXPANSION_RESULTS;
  expect(encoded, "netplay expansion product-flow results").toBeTruthy();
  const result = (JSON.parse(encoded!) as ExpansionResult[]).find((item) => item.caseId === caseId);
  expect(result, `${caseId} product-flow result`).toBeTruthy();
  expect(result?.fixtureSha256).toMatch(/^[0-9a-f]{64}$/);
  return result!;
}

async function verifyRoomPickerGame(browser: Browser, result: ExpansionResult) {
  const host = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const loginResult = await login(host.request, "test", "test");
  expect(loginResult.response.ok()).toBe(true);
  const catalogResponse = await host.request.get("/api/v1/netplay/games?availability=SUPPORTED&limit=100");
  expect(catalogResponse.ok()).toBe(true);
  const catalog = await catalogResponse.json() as { items: CatalogGame[] };
  const game = catalog.items.find((item) => item.gameId === result.gameId);
  expect(game, `${result.profileId} game missing from room picker catalog`).toBeTruthy();
  const profile = game!.netplayProfiles.find((candidate) => candidate.id === result.profileId);
  expect(profile).toBeTruthy();
  const expectedPlatform = result.coreId === "snes9x" ? "snes"
    : result.coreId === "nestopia" ? "nes" : "arcade";
  expect(game!.platformId).toBe(expectedPlatform);

  const created = await host.request.post("/api/v1/netplay/rooms", {
    data: {}, headers: writeHeaders(loginResult.auth!.csrfToken),
  });
  expect(created.status()).toBe(201);
  const room = await created.json() as Room;
  const page = await host.newPage();
  try {
    await page.goto(`/netplay/rooms/${room.roomId}`);
    const platformButton = page.getByRole("button", { name: game!.platformName, exact: true });
    for (let pageIndex = 0; pageIndex < 5 && await platformButton.count() === 0; pageIndex += 1) {
      const loadMore = page.getByRole("button", { name: /加载更多|重试加载/ });
      const loadedSummary = page.locator(".netplay-picker-title strong");
      const previousSummary = await loadedSummary.textContent();
      await expect(loadMore).toBeVisible();
      await loadMore.click();
      await expect.poll(async () =>
        await platformButton.count() > 0 || await loadedSummary.textContent() !== previousSummary,
      ).toBe(true);
    }
    await expect(platformButton).toBeVisible();
    const card = page.locator(".netplay-game-card")
      .filter({ has: page.getByRole("heading", { name: game!.title, exact: true }) })
      .filter({ has: page.getByText(game!.platformInstanceName, { exact: true }) });
    await expect(card).toBeVisible();
    if (game!.netplayProfiles.length > 1) {
      await card.getByRole("button", { name: "选择", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: `选择 ${game!.title} 的联机配置` });
      await expect(dialog.getByText(profile!.coreName, { exact: true })).toBeVisible();
    } else {
      await expect(card.getByText(profile!.coreName, { exact: true })).toBeVisible();
    }
  } finally {
    const current = await host.request.get(`/api/v1/netplay/rooms/${room.roomId}`);
    if (current.ok()) {
      await host.request.delete(`/api/v1/netplay/rooms/${room.roomId}`, {
        headers: writeHeaders(loginResult.auth!.csrfToken, current.headers().etag),
      });
    }
    await host.close();
  }
}

async function verifyStrictExpansionProfile(
  browser: Browser,
  testInfo: TestInfo,
  result: ExpansionResult,
) {
  await verifyRoomPickerGame(browser, result);
  const session = await createSession(
    browser, testInfo, result.gameId, result.profileId, result.fixtureSha256,
  );
  try {
    expect(session.configResponses).toHaveLength(2);
    for (const configuration of session.configResponses) {
      expect(configuration).toMatchObject({ core: result.coreId, runtimeCore: result.coreId, mode: "netplay" });
      const netplay = configuration.netplay as { netplayProfile?: { profileId?: string } } | undefined;
      expect(netplay?.netplayProfile?.profileId).toBe(result.profileId);
      const expectsParent = result.expectsParent ?? [
        "mame2003", "mame2003_plus", "fbalpha2012_cps2",
      ].includes(result.coreId);
      const expectsBios = result.expectsBios ?? ["mame2003", "mame2003_plus"].includes(result.coreId);
      expect(Boolean(configuration.parentUrl)).toBe(expectsParent);
      expect(Boolean(configuration.biosUrl)).toBe(expectsBios);
    }
    const initial = await Promise.all([
      diagnosticEvents(session.hostPage), diagnosticEvents(session.guestPage),
    ]);
    const captures = initial[0].filter((event) => event.kind === "state-capture");
    expect(captures.length).toBeGreaterThan(0);
    expect(captures.every((event) => (event.byteLength ?? 0) > 0 && (event.byteLength ?? 0) <= 1024 * 1024)).toBe(true);
    expect(initial[1].some((event) => event.kind === "state-load" && event.nativeCompletion && event.coreExact)).toBe(true);
    await Promise.all([waitForFrame(session.hostPage, 119), waitForFrame(session.guestPage, 119)]);
    await verifyLockstepBufferAdaptation(session);
    const checkpointPhase = await verifyStrictCheckpointPhase(
      session, result.profileId,
    );
    const { guestEvents } = checkpointPhase;
    let recoveredHashMismatch = checkpointPhase.recoveredHashMismatch;

    const beforeFreeze = Math.max(...guestEvents
      .filter((event) => event.kind === "canonical").map((event) => event.frame ?? -1));
    const cdp = await session.guest.newCDPSession(session.guestPage);
    await cdp.send("Page.setWebLifecycleState", { state: "frozen" });
    await new Promise((resolve) => setTimeout(resolve, 3_000));
    await cdp.send("Page.setWebLifecycleState", { state: "active" });
    await waitForFrame(session.guestPage, beforeFreeze + 60);
    const [beforeDropHostEvents, beforeDropGuestEvents] = await Promise.all([
      diagnosticEvents(session.hostPage), diagnosticEvents(session.guestPage),
    ]);
    const epochBefore = Math.max(...beforeDropGuestEvents
      .filter((event) => event.kind === "epoch").map((event) => event.epoch ?? 0));
    const hostEventSeq = beforeDropHostEvents.at(-1)?.eventSeq ?? 0;
    const guestEventSeq = beforeDropGuestEvents.at(-1)?.eventSeq ?? 0;
    await session.guestPage.evaluate(() => {
      (window as typeof window & { __RETROM_NETPLAY_ACCEPTANCE__?: { controls?: { dropConnection: (durationMS: number) => void } } })
        .__RETROM_NETPLAY_ACCEPTANCE__!.controls!.dropConnection(3_000);
    });
    const reconnectEpoch = await waitForTransportRecovery(
      session, epochBefore, hostEventSeq, guestEventSeq,
    );
    const reconnectCheckpoint = Math.ceil((reconnectEpoch.nextFrame! + 1) / 120) * 120 - 1;
    const reconnectPair = await checkpointPair(
      session.hostPage, session.guestPage, reconnectCheckpoint, reconnectEpoch.epoch,
    );
    expect(reconnectPair.host.coreDigest).toMatch(/^[0-9a-f]{64}$/);
    if (reconnectPair.host.coreDigest !== reconnectPair.guest.coreDigest) {
      expect(result.profileId).toBe("snes9x-423-v1");
      expect(recoveredHashMismatch, "SNES may consume only one no-op hash recovery per session").toBe(false);
      await verifySNESNoOpHashRecovery(session, reconnectPair);
      recoveredHashMismatch = true;
    }
    const room = await session.host.request.get(`/api/v1/netplay/rooms/${session.roomId}`);
    expect(room.ok()).toBe(true);
    expect((await room.json() as Room).currentSession?.sessionId).toBe(session.sessionId);
    const postReconnectEvents = await diagnosticEvents(session.guestPage);
    expect(postReconnectEvents.filter((event) => event.kind === "ended")).toHaveLength(0);
    const stateLoads = postReconnectEvents.filter((event) => event.kind === "state-load");
    expect(stateLoads).toHaveLength(recoveredHashMismatch ? 3 : 2);
    expect(stateLoads.every((event) => event.nativeCompletion && event.coreExact)).toBe(true);
    await waitForCPSVisualStability(session, result.coreId);
    await pauseSession(session);
    await session.hostPage.waitForTimeout(300);
    const hostCanvas = await canvasDigest(session.hostPage, testInfo, `${result.coreId}-p1-canvas.png`);
    const guestCanvas = await canvasDigest(session.guestPage, testInfo, `${result.coreId}-p2-canvas.png`);
    expect(hostCanvas.digest).toBe(guestCanvas.digest);
    expect(hostCanvas.brightPixels).toBeGreaterThan(16);
    expect(guestCanvas.brightPixels).toBeGreaterThan(16);
    const finalEvents = await Promise.all([
      diagnosticEvents(session.hostPage), diagnosticEvents(session.guestPage),
    ]);
    for (const events of finalEvents) {
      expect(events.filter((event) => event.kind === "pause" && event.reason === "STATE_MISMATCH"))
        .toHaveLength(recoveredHashMismatch ? 1 : 0);
      expect(events.filter((event) => event.kind === "ended")).toHaveLength(0);
    }
    expect(session.consoleErrors).toEqual([]);
  } finally {
    await session.cleanup();
  }
}

test.use({ trace: "off" });

test.describe.serial("real dual-browser netplay", () => {
  test("ACC-NP-014 FCEUmm dual-browser rollback product chain", async ({ browser }, testInfo) => {
    test.setTimeout(240_000);
    test.skip(testInfo.project.name !== "chrome-1280", "The stateful dual-browser core case runs once.");
    const gameId = process.env.RETROM_NETPLAY_NES_GAME_ID;
    expect(gameId).toBeTruthy();
    const fixtureSha256 = process.env.RETROM_NETPLAY_NES_FIXTURE_SHA256;
    expect(fixtureSha256).toMatch(/^[0-9a-f]{64}$/);
    const session = await createSession(browser, testInfo, gameId!, "fceumm-423-v1", fixtureSha256!);
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
      const hostCanvas = await canvasDigest(session.hostPage, testInfo, "fceumm-p1-canvas.png");
      const guestCanvas = await canvasDigest(session.guestPage, testInfo, "fceumm-p2-canvas.png");
      expect(hostCanvas.digest).toBe(guestCanvas.digest);
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
    const fixtureSha256 = process.env.RETROM_NETPLAY_FBNEO_FIXTURE_SHA256;
    expect(fixtureSha256).toMatch(/^[0-9a-f]{64}$/);
    const session = await createSession(browser, testInfo, gameId!, "fbneo-423-v1", fixtureSha256!);
    try {
      await waitForFrame(session.hostPage, 239);
      await tapDirectionalInput(session.hostPage);
      await session.guestPage.evaluate(() => {
        const state = (window as typeof window & { __RETROM_NETPLAY_ACCEPTANCE__?: { delayMS: number } }).__RETROM_NETPLAY_ACCEPTANCE__!;
        state.delayMS = 100;
      });
      await setDirectionalInput(session.guestPage, true);
      await expect.poll(async () => Math.max(1, ...(await diagnosticEvents(session.guestPage))
        .filter((event) => event.kind === "lockstep").map((event) => event.inputBufferFrames ?? 1)), { timeout: 30_000 }).toBeGreaterThan(1);
      await session.guestPage.evaluate(() => {
        const state = (window as typeof window & { __RETROM_NETPLAY_ACCEPTANCE__?: { delayMS: number } }).__RETROM_NETPLAY_ACCEPTANCE__!;
        state.delayMS = 0;
      });
      await setDirectionalInput(session.guestPage, false);
      await matchingCheckpoint(session.hostPage, session.guestPage, 719);
      const events = await diagnosticEvents(session.guestPage);
      expect(events.filter((event) => event.kind === "canonical").every((event) => (event.predictionFrames ?? 0) === 0)).toBe(true);
      expect(events.filter((event) => event.kind === "rollback")).toHaveLength(0);
      const buffers = events.filter((event) => event.kind === "lockstep").map((event) => event.inputBufferFrames ?? 1);
      expect(buffers.at(-1)!).toBeLessThan(Math.max(...buffers));
      await pauseSession(session);
      const hostCanvas = await canvasDigest(session.hostPage, testInfo, "fbneo-p1-canvas.png");
      const guestCanvas = await canvasDigest(session.guestPage, testInfo, "fbneo-p2-canvas.png");
      expect(hostCanvas.digest).toBe(guestCanvas.digest);
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
      const fixtureSha256 = profileId === "fceumm-423-v1"
        ? process.env.RETROM_NETPLAY_NES_FIXTURE_SHA256
        : process.env.RETROM_NETPLAY_FBNEO_FIXTURE_SHA256;
      expect(fixtureSha256).toMatch(/^[0-9a-f]{64}$/);
      const session = await createSession(browser, testInfo, gameId!, profileId, fixtureSha256!);
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

  for (const [caseId, profileId] of [
    ["ACC-NP-017", "snes9x-423-v1"],
    ["ACC-NP-018", "nestopia-423-v1"],
    ["ACC-NP-019", "mame2003-423-override-v1"],
    ["ACC-NP-020", "mame2003-plus-423-v1"],
    ["ACC-NP-021", "fbalpha2012-cps1-423-v1"],
    ["ACC-NP-022", "fbalpha2012-cps2-423-v1"],
  ] as const) {
    test(`${caseId} ${profileId} strict dual-browser product matrix`, async ({ browser }, testInfo) => {
      test.setTimeout(300_000);
      test.skip(testInfo.project.name !== "chrome-1280", "The strict dual-browser core case runs once.");
      const result = expansionResult(caseId);
      expect(result.profileId).toBe(profileId);
      await verifyStrictExpansionProfile(browser, testInfo, result);
    });
  }
});
