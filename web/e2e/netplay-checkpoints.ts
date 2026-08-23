import { expect, type Page } from "@playwright/test";

type CheckpointBlock = { tag: string; start: number; end: number; digest: string };

export type DiagnosticEvent = {
  eventSeq?: number; kind: string; frame?: number; epoch?: number; nextFrame?: number; atFrame?: number;
  depth?: number; predictionFrames?: number; coreDigest?: string; stateDigest?: string;
  inputBufferFrames?: number; phase?: string; reason?: string; reconnect?: boolean; resync?: boolean;
  states?: number; predicted?: number; canonical?: number; stateBytes?: number;
  nativeCompletion?: boolean; byteExact?: boolean; coreExact?: boolean; changed?: boolean;
  byteLength?: number; attempt?: number; expectedCoreBytes?: number; recapturedCoreBytes?: number;
  firstCoreMismatch?: number; coreMismatchCount?: number; stateBlocks?: CheckpointBlock[];
};

type NetplayPages = { hostPage: Page; guestPage: Page };

export async function diagnosticEvents(page: Page) {
  return page.evaluate(() => ((window as typeof window & {
    __RETROM_NETPLAY_ACCEPTANCE__?: { events: DiagnosticEvent[] };
  }).__RETROM_NETPLAY_ACCEPTANCE__?.events ?? []));
}

export async function checkpointPair(p1: Page, p2: Page, frame: number, epoch?: number) {
  let pair: { host?: DiagnosticEvent; guest?: DiagnosticEvent } = {};
  await expect.poll(async () => {
    const [hostEvents, guestEvents] = await Promise.all([diagnosticEvents(p1), diagnosticEvents(p2)]);
    const select = (events: DiagnosticEvent[]) => events.filter((event) =>
      event.kind === "checkpoint" && event.frame === frame && (epoch === undefined || event.epoch === epoch)).at(-1);
    pair = { host: select(hostEvents), guest: select(guestEvents) };
    return [Boolean(pair.host), Boolean(pair.guest)];
  }, { timeout: 60_000, intervals: [100, 250, 500] }).toEqual([true, true]);
  return pair as { host: DiagnosticEvent; guest: DiagnosticEvent };
}

export async function matchingCheckpoint(p1: Page, p2: Page, frame: number, epoch?: number) {
  const pair = await checkpointPair(p1, p2, frame, epoch);
  expect(pair.host.coreDigest, `host checkpoint ${frame}`).toMatch(/^[0-9a-f]{64}$/);
  expect(pair.host.coreDigest, `checkpoint ${frame} differs`).toBe(pair.guest.coreDigest);
  return pair.host;
}

export function checkpointMismatches(hostEvents: DiagnosticEvent[], guestEvents: DiagnosticEvent[]) {
  const guests = new Map(guestEvents.filter((event) => event.kind === "checkpoint")
    .map((event) => [`${event.epoch}:${event.frame}`, event]));
  return hostEvents.filter((event) => event.kind === "checkpoint").flatMap((host) => {
    const guest = guests.get(`${host.epoch}:${host.frame}`);
    return guest && host.coreDigest !== guest.coreDigest ? [{ host, guest }] : [];
  });
}

function expectSNESBoundarySignature(mismatch: { host: DiagnosticEvent; guest: DiagnosticEvent }) {
  const hostBlocks = new Map((mismatch.host.stateBlocks ?? []).map((block) => [block.tag, block.digest]));
  const guestBlocks = new Map((mismatch.guest.stateBlocks ?? []).map((block) => [block.tag, block.digest]));
  expect(hostBlocks.size).toBe(12);
  expect([...hostBlocks.keys()]).toEqual([...guestBlocks.keys()]);
  expect([...hostBlocks].filter(([tag, digest]) => guestBlocks.get(tag) !== digest)).not.toHaveLength(0);
}

export async function verifySNESNoOpHashRecovery(
  session: NetplayPages,
  mismatch: { host: DiagnosticEvent; guest: DiagnosticEvent },
) {
  expect(mismatch.host.coreDigest).toMatch(/^[0-9a-f]{64}$/);
  expect(mismatch.guest.coreDigest).toMatch(/^[0-9a-f]{64}$/);
  expect(mismatch.host.coreDigest).not.toBe(mismatch.guest.coreDigest);
  expect(mismatch.host.epoch).toBe(mismatch.guest.epoch);
  expect(mismatch.host.frame).toBe(mismatch.guest.frame);
  expectSNESBoundarySignature(mismatch);
  const sourceEpoch = mismatch.host.epoch!;
  const mismatchFrame = mismatch.host.frame!;
  expect(sourceEpoch).toBeGreaterThanOrEqual(0);
  expect(mismatchFrame).toBeGreaterThanOrEqual(0);
  let recovery: {
    pause?: DiagnosticEvent; guestPause?: DiagnosticEvent; hostCapture?: DiagnosticEvent; guestLoad?: DiagnosticEvent;
    normalization?: DiagnosticEvent; hostEpoch?: DiagnosticEvent; guestEpoch?: DiagnosticEvent;
  } = {};
  await expect.poll(async () => {
    const [hostEvents, guestEvents] = await Promise.all([
      diagnosticEvents(session.hostPage), diagnosticEvents(session.guestPage),
    ]);
    const pause = hostEvents.find((event) => event.kind === "pause" && event.epoch === sourceEpoch &&
      event.reason === "STATE_MISMATCH" && (event.atFrame ?? -1) >= mismatchFrame);
    const nextFrame = pause === undefined ? undefined : (pause.atFrame ?? -1) + 1;
    recovery = {
      pause,
      guestPause: guestEvents.find((event) => event.kind === "pause" && event.epoch === sourceEpoch &&
        event.reason === "STATE_MISMATCH" && event.atFrame === pause?.atFrame),
      hostCapture: hostEvents.find((event) => event.kind === "state-capture" &&
        event.epoch === sourceEpoch && event.nextFrame === nextFrame),
      guestLoad: guestEvents.find((event) => event.kind === "state-load" &&
        event.epoch === sourceEpoch && event.nextFrame === nextFrame),
      normalization: hostEvents.find((event) => event.kind === "authority-normalization" &&
        event.epoch === sourceEpoch && event.nextFrame === nextFrame),
      hostEpoch: hostEvents.find((event) => event.kind === "epoch" && event.resync &&
        event.epoch === sourceEpoch + 1 && event.nextFrame === nextFrame),
      guestEpoch: guestEvents.find((event) => event.kind === "epoch" && event.resync &&
        event.epoch === sourceEpoch + 1 && event.nextFrame === nextFrame),
    };
    return Object.values(recovery).every(Boolean);
  }, { timeout: 30_000, intervals: [100, 250, 500] }).toBe(true);

  const { pause, guestPause, hostCapture, guestLoad, normalization, hostEpoch, guestEpoch } = recovery as {
    pause: DiagnosticEvent; guestPause: DiagnosticEvent; hostCapture: DiagnosticEvent; guestLoad: DiagnosticEvent;
    normalization: DiagnosticEvent; hostEpoch: DiagnosticEvent; guestEpoch: DiagnosticEvent;
  };
  expect(pause.atFrame).toBeGreaterThanOrEqual(mismatchFrame);
  expect(guestPause.atFrame).toBe(pause.atFrame);
  expect(hostEpoch.nextFrame).toBe(pause.atFrame! + 1);
  expect(guestEpoch.nextFrame).toBe(hostEpoch.nextFrame);
  expect(hostCapture.stateDigest).toMatch(/^[0-9a-f]{64}$/);
  expect(hostCapture.coreDigest).toMatch(/^[0-9a-f]{64}$/);
  expect(hostCapture.byteLength).toBeGreaterThan(0);
  expect(hostCapture.stateDigest).toBe(guestLoad.stateDigest);
  expect(hostCapture.coreDigest).toBe(guestLoad.coreDigest);
  expect(normalization).toMatchObject({ attempt: 1, firstCoreMismatch: -1, coreMismatchCount: 0 });
  expect(normalization.expectedCoreBytes).toBeGreaterThan(0);
  expect(normalization.recapturedCoreBytes).toBe(normalization.expectedCoreBytes);
  expect(guestLoad).toMatchObject({
    changed: false, nativeCompletion: true, byteExact: true, coreExact: true, firstCoreMismatch: -1,
  });
  expect(pause.eventSeq).toBeLessThan(hostCapture.eventSeq!);
  expect(hostCapture.eventSeq).toBeLessThan(hostEpoch.eventSeq!);
  expect(guestPause.eventSeq).toBeLessThan(guestLoad.eventSeq!);
  expect(guestLoad.eventSeq).toBeLessThan(guestEpoch.eventSeq!);

  const firstCheckpoint = Math.ceil((hostEpoch.nextFrame! + 1) / 120) * 120 - 1;
  await matchingCheckpoint(session.hostPage, session.guestPage, firstCheckpoint, sourceEpoch + 1);
  await matchingCheckpoint(session.hostPage, session.guestPage, firstCheckpoint + 120, sourceEpoch + 1);
  const [hostEvents, guestEvents] = await Promise.all([
    diagnosticEvents(session.hostPage), diagnosticEvents(session.guestPage),
  ]);
  for (const events of [hostEvents, guestEvents]) {
    expect(events.filter((event) => event.kind === "pause" && event.reason === "STATE_MISMATCH")).toHaveLength(1);
    expect(events.filter((event) => event.kind === "epoch" && event.resync && event.epoch === sourceEpoch + 1))
      .toHaveLength(1);
    expect(events.filter((event) => event.kind === "ended")).toHaveLength(0);
  }
}
