import {afterEach, describe, expect, it, vi} from "vitest";
import type {LaunchEnvelopeV1, PlayerRuntimeV1, RuntimeCapabilitiesV1} from "./runtime/contract";
import type {RpgGateEvidence, RpgPosition} from "./rpg-validation-protocol";
import {RpgRuntimeValidationDriver, waitForContinuousFrames, waitForRpgPosition} from "./rpg-runtime-validation";

type GateRequest = {
  sequence: number; eventId: string; gate: string; phase: string; evidence: Record<string, unknown>;
};

afterEach(() => {vi.unstubAllGlobals(); vi.restoreAllMocks();});

describe("RpgRuntimeValidationDriver", () => {
  it("drives A to B checkpoint to divergent C through PlayerRuntimeV1", async () => {
    const events: GateRequest[] = [];
    installValidationFetch(events);
    let position: RpgPosition = {mapId: 1, playerX: 1, playerY: 1, fixtureState: 0};
    const runtime = validationRuntime(() => position);
    const uploaded: unknown[] = [];
    const finish = vi.fn(async () => undefined);
    const driver = new RpgRuntimeValidationDriver({
      envelope: validationEnvelope(false), signal: new AbortController().signal,
      uploadCheckpoint: async (payload) => {
        uploaded.push(payload);
        return {checkpointFormat: "fixture-v1", sizeBytes: 3, sha256: fixtureDigest};
      },
      finishOriginalLaunch: finish,
    });

    await driver.prepare();
    await driver.attachRuntime(runtime);
    expect(driver.getSnapshot().phase).toBe("input");
    position = {mapId: 1, playerX: 2, playerY: 1, fixtureState: 0};
    await driver.runAction();
    await driver.runAction();
    expect(driver.getSnapshot().phase).toBe("save");
    position = {mapId: 1, playerX: 3, playerY: 1, fixtureState: 1};
    await driver.runAction();
    expect(driver.getSnapshot().phase).toBe("diverge");
    expect(uploaded).toHaveLength(1);
    position = {mapId: 2, playerX: 4, playerY: 5, fixtureState: 2};
    await driver.runAction();

    expect(driver.getSnapshot()).toMatchObject({
      phase: "original-complete", launchRole: "original", validationState: "CHECKPOINTED",
      lastGateSequence: 20,
      initialPosition: {mapId: 1, playerX: 2, playerY: 1, fixtureState: 0},
      savedPosition: {mapId: 1, playerX: 3, playerY: 1, fixtureState: 1},
      divergedPosition: {mapId: 2, playerX: 4, playerY: 5, fixtureState: 2},
    });
    expect(events.map((event) => `${event.gate}:${event.phase}`)).toEqual(originalGateOrder());
    expect(events.find((event) => event.gate === "CHECKPOINT_CREATED" && event.phase === "PASS")?.evidence)
      .toEqual({checkpointFormat: "fixture-v1", sizeBytes: 3, sha256: fixtureDigest});
    expect(finish).toHaveBeenCalledOnce();
  });

  it("restores, captures a PNG, and proves post-restore input", async () => {
    const events: GateRequest[] = [];
    const fetchMock = installValidationFetch(events);
    const position = {mapId: 7, playerX: 8, playerY: 9, fixtureState: 10};
    const driver = new RpgRuntimeValidationDriver({
      envelope: validationEnvelope(true), signal: new AbortController().signal,
      uploadCheckpoint: async () => ({checkpointFormat: "fixture-v1", sizeBytes: 3, sha256: fixtureDigest}),
      finishOriginalLaunch: async () => undefined,
    });
    await driver.prepare();
    await driver.attachRuntime(validationRuntime(() => position));
    expect(driver.getSnapshot().phase).toBe("restore-input");
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith("/review-screenshot"))).toBe(true);
    position.playerX += 1;
    await driver.runAction();
    expect(driver.getSnapshot()).toMatchObject({
      phase: "restore-complete", validationState: "AWAITING_DECISION", lastGateSequence: 28,
      restoredPosition: {mapId: 7, playerX: 8, playerY: 9, fixtureState: 10},
      restoreInputPosition: {mapId: 7, playerX: 9, playerY: 9, fixtureState: 10},
    });
  });

  it("fails the checkpoint gate on an inexact server receipt", async () => {
    const events: GateRequest[] = [];
    installValidationFetch(events);
    let position: RpgPosition = {mapId: 1, playerX: 1, playerY: 1, fixtureState: 0};
    const driver = new RpgRuntimeValidationDriver({
      envelope: validationEnvelope(false), signal: new AbortController().signal,
      uploadCheckpoint: async () => ({checkpointFormat: "fixture-v1", sizeBytes: 4, sha256: fixtureDigest}),
      finishOriginalLaunch: async () => undefined,
    });
    await driver.prepare();
    await driver.attachRuntime(validationRuntime(() => position));
    position = {mapId: 1, playerX: 2, playerY: 1, fixtureState: 0};
    await driver.runAction();
    await driver.runAction();
    position = {mapId: 1, playerX: 3, playerY: 1, fixtureState: 1};
    await driver.runAction();
    expect(driver.getSnapshot()).toMatchObject({phase: "error", error: "RPG_CHECKPOINT_RESPONSE_MISMATCH"});
    expect(events.at(-1)).toMatchObject({gate: "CHECKPOINT_CREATED", phase: "FAIL", evidence: {}});
  });

  it("rejects malformed generic validation input before any runtime call", () => {
    const envelope = validationEnvelope(false);
    envelope.validation = {probeId: "rpgmaker.position.v1", input: {generation: "RPG2000", resume: {}}};
    expect(() => new RpgRuntimeValidationDriver({
      envelope, signal: new AbortController().signal,
      uploadCheckpoint: async () => ({checkpointFormat: "fixture-v1", sizeBytes: 3, sha256: fixtureDigest}),
      finishOriginalLaunch: async () => undefined,
    })).toThrow("RPG_RUNTIME_PROTOCOL_VIOLATION");
  });
});

describe("RPG Provider probes", () => {
  it("waits for a valid position and 300 continuous frames", async () => {
    let attempts = 0;
    const frames = [10, 160, 310];
    const runtime = validationRuntime(() => {
      attempts += 1;
      if (attempts < 3) {throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");}
      return {mapId: 3, playerX: 4, playerY: 5, fixtureState: 6};
    }, () => frames.shift() ?? 310);
    await expect(waitForRpgPosition(runtime, new AbortController().signal, async () => undefined))
      .resolves.toEqual({mapId: 3, playerX: 4, playerY: 5, fixtureState: 6});
    await expect(waitForContinuousFrames(runtime, new AbortController().signal, async () => undefined))
      .resolves.toBe(300);
  });

  it("rejects a Provider frame counter reset", async () => {
    const frames = [50, 100, 20];
    const runtime = validationRuntime(
      () => ({mapId: 1, playerX: 1, playerY: 1, fixtureState: 0}),
      () => frames.shift() ?? 20,
    );
    await expect(waitForContinuousFrames(runtime, new AbortController().signal, async () => undefined))
      .rejects.toThrow("RPG_RUNTIME_FRAME_DISCONTINUITY");
  });
});

const fixtureDigest = "039058c6f2c0cb492c533b0a4d14ef77cc0f78abccced5287d84a1a2011cfb81";

function validationRuntime(position: () => RpgPosition, frames?: () => number): PlayerRuntimeV1 {
  let frame = 0;
  return {
    mount: vi.fn(async () => undefined), pause: vi.fn(async () => undefined), resume: vi.fn(async () => undefined),
    checkpoint: vi.fn(async () => ({bytes: Uint8Array.of(1, 2, 3), format: "fixture-v1", metadata: null})),
    screenshot: vi.fn(async () => new Blob([Uint8Array.of(137, 80, 78, 71)], {type: "image/png"})),
    setVolume: vi.fn(async () => undefined), setVideoMode: vi.fn(async () => undefined),
    openNativeSettings: vi.fn(async () => undefined), closeNativeSettings: vi.fn(async () => undefined),
    getDiscState: vi.fn(async () => ({count: 1, currentIndex: 0, labels: ["Game"]})),
    switchDisc: vi.fn(async () => ({count: 1, currentIndex: 0, labels: ["Game"]})),
    setInputFilter: vi.fn(async () => undefined),
    getNetplayPort: vi.fn(async () => {throw new Error("PLAYER_RUNTIME_CAPABILITY_UNSUPPORTED");}),
    runValidationProbe: vi.fn(async (id) => ({probeId: id, passed: true, evidence: position()})),
    getState: () => "RUNNING", getCapabilities: () => validationCapabilities,
    getCheckpointAvailability: () => ({available: true, reason: null}), getCanvas: () => null,
    getFrameCount: frames ?? (() => {frame += 100; return frame;}), subscribe: () => () => undefined,
    exit: vi.fn(async () => undefined),
  };
}

const validationCapabilities: RuntimeCapabilitiesV1 = {
  checkpoint: true, frameCounter: true, frameMode: "NONE", pause: true, screenshot: true,
  standardGamepad: true, volume: true, discSwitch: false, nativeSettings: false,
  inputFilter: false, netplayPort: false, videoModes: [], requiresThreads: false,
  validationProbes: ["rpgmaker.position.v1"],
};

function validationEnvelope(restoring: boolean): LaunchEnvelopeV1 {
  const launchId = crypto.randomUUID();
  const originalLaunchId = restoring ? crypto.randomUUID() : launchId;
  const originalGates = new Set(validationGateOrder().slice(0, 10));
  const machineGates = validationGateOrder().map((gate) => ({
    gate,
    status: restoring && originalGates.has(gate) ? "PASSED" as const : "NOT_STARTED" as const,
    begunAtMs: restoring && originalGates.has(gate) ? 1 : null,
    completedAtMs: restoring && originalGates.has(gate) ? 2 : null,
    evidence: restoring ? gateEvidence(gate) : null,
    failureCode: null,
  }));
  const resume = {
    validationId: crypto.randomUUID(), state: restoring ? "CHECKPOINTED" : "STARTING",
    originalLaunchId, restoreLaunchId: restoring ? launchId : null,
    lastGateSequence: restoring ? 20 : 0, machineGates,
    checkpointEvidence: restoring ? {checkpointFormat: "fixture-v1", sizeBytes: 3, sha256: fixtureDigest} : null,
    restoreScreenshotUploaded: false,
  };
  return {
    schemaVersion: 1,
    session: {coreName: "Fixture Core", id: launchId, purpose: "RUNTIME_VALIDATION", mode: "SINGLE", title: "Fixture",
      platformName: "RPG Maker", returnTo: "/admin/reviews/item", warnings: []},
    runtime: {providerId: "retrom-runtime", providerVersion: "0.12.0", providerApiVersion: 1,
      bundleSha256: "a".repeat(64), targetId: "rpgmaker-2000", gameCompatibilityLine: "rpgmaker-2000-v1",
      targetContractSha256: "b".repeat(64), capabilities: validationCapabilities,
      checkpoint: {writeFormat: "fixture-v1", readFormats: ["fixture-v1"], maxBytes: 1024},
      moduleUrl: "/runtime/providers/retrom-runtime/a/client.mjs", moduleSha256: "c".repeat(64),
      runtimeBaseUrl: "/runtime/providers/retrom-runtime/a/"},
    resources: [], targetOptions: {expectedRestorePosition: null},
    restore: restoring ? {url: `/runtime/launches/${launchId}/state`, format: "fixture-v1", sha256: fixtureDigest, sizeBytes: 3} : null,
    validation: {probeId: "rpgmaker.position.v1", input: {generation: "RPG2000", resume}}, netplay: null,
  };
}

function installValidationFetch(events: GateRequest[]) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith("/review-screenshot")) {return Response.json({screenshotId: "shot"}, {status: 201});}
    const request = JSON.parse(String(init?.body)) as GateRequest;
    events.push(request);
    return Response.json({sequence: request.sequence, eventId: request.eventId,
      validationState: "RUNNING", idempotentReplay: false});
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function originalGateOrder() {
  return validationGateOrder().slice(0, 10).flatMap((gate) => [`${gate}:BEGIN`, `${gate}:PASS`]);
}

function validationGateOrder() {
  return ["RUNTIME_READY", "ENGINE_PROFILE", "FRAMES_300", "INPUT", "AUDIO", "INITIAL_POSITION_RECORDED",
    "SAVE_POINT_RECORDED", "CHECKPOINT_CREATED", "POST_SAVE_STATE_DIVERGED", "ORIGINAL_LAUNCH_ENDED",
    "RESTORE_STARTED", "RESTORE_POSITION_VERIFIED", "RESTORE_SCREENSHOT", "RESTORE_INPUT"] as const;
}

function gateEvidence(gate: string): RpgGateEvidence {
  if (gate === "ENGINE_PROFILE") {return {generation: "RPG2000", engineProfile: "rpg2k"};}
  if (gate === "FRAMES_300") {return {continuousFrames: 300};}
  if (gate === "INPUT" || gate === "AUDIO") {return {observed: true};}
  if (gate === "INITIAL_POSITION_RECORDED") {return {mapId: 7, playerX: 7, playerY: 9, fixtureState: 9};}
  if (gate === "SAVE_POINT_RECORDED") {return {mapId: 7, playerX: 8, playerY: 9, fixtureState: 10};}
  if (gate === "CHECKPOINT_CREATED") {return {checkpointFormat: "fixture-v1", sizeBytes: 3, sha256: fixtureDigest};}
  if (gate === "POST_SAVE_STATE_DIVERGED") {return {mapId: 7, playerX: 9, playerY: 9, fixtureState: 11};}
  return {};
}
