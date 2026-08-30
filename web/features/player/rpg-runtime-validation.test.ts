import { afterEach, describe, expect, it, vi } from "vitest";
import type { GameRuntime, RuntimeCapabilities } from "@xxxsen/retrom-runtime";
import type { EmulatorInstance, ManualStatePayload } from "./adapters/ejs-4.2.3-v2";
import type { RpgPosition, RpgRuntimeConfig as RpgMakerConfig } from "./rpg-runtime";
import type { RpgGateEvidence } from "./rpg-validation-protocol";

const captures = vi.hoisted(() => ({
  screenshot: vi.fn(async () => ({ screenshot: new Blob([Uint8Array.of(137, 80, 78, 71)], { type: "image/png" }), format: "png" })),
  state: vi.fn(async () => ({
    screenshot: new Blob([Uint8Array.of(137, 80, 78, 71)], { type: "image/png" }),
    format: "png",
    state: Uint8Array.of(1, 2, 3),
    payloadKind: "NATIVE_SAVE_BUNDLE_V1" as const,
    validationPurpose: true,
  })),
}));

vi.mock("./adapters/ejs-4.2.3-v2", () => ({
  captureManualScreenshot: captures.screenshot,
  captureManualState: captures.state,
}));

import { RpgRuntimeValidationDriver, waitForContinuousFrames, waitForRpgPosition } from "./rpg-runtime-validation";

type GateRequest = {
  sequence: number;
  eventId: string;
  gate: string;
  phase: string;
  evidence: Record<string, unknown>;
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe("RpgRuntimeValidationDriver", () => {
  it("drives A to B checkpoint to divergent C and ends the original Launch", async () => {
    const events: GateRequest[] = [];
    installValidationFetch(events);
    let position: RpgPosition = { mapId: 1, playerX: 1, playerY: 1, fixtureState: 0 };
    const mounted = validationRuntime(() => position);
    const uploaded: ManualStatePayload[] = [];
    const finish = vi.fn(async () => undefined);
    const driver = new RpgRuntimeValidationDriver({
      config: validationConfig(null),
      signal: new AbortController().signal,
      uploadCheckpoint: async (payload) => {
        uploaded.push(payload);
        return {
          payloadKind: "NATIVE_SAVE_BUNDLE_V1",
          sizeBytes: 3,
          sha256: "039058c6f2c0cb492c533b0a4d14ef77cc0f78abccced5287d84a1a2011cfb81",
        };
      },
      finishOriginalLaunch: finish,
    });

    await driver.prepare();
    await driver.attachRuntime(mounted.instance, mounted.runtime);
    expect(driver.getSnapshot().phase).toBe("input");

    position = { mapId: 1, playerX: 2, playerY: 1, fixtureState: 0 };
    await driver.runAction();
    expect(driver.getSnapshot().phase).toBe("audio");
    await driver.runAction();
    expect(driver.getSnapshot().phase).toBe("save");

    position = { mapId: 1, playerX: 3, playerY: 1, fixtureState: 1 };
    await driver.runAction();
    expect(driver.getSnapshot().phase).toBe("diverge");
    expect(uploaded).toHaveLength(1);

    await driver.runAction();
    expect(driver.getSnapshot().phase).toBe("diverge");
    expect(driver.getSnapshot().error).toContain("C 必须与 B");

    position = { mapId: 2, playerX: 4, playerY: 5, fixtureState: 2 };
    await driver.runAction();

    expect(driver.getSnapshot().phase).toBe("original-complete");
    expect(driver.getSnapshot()).toMatchObject({
      launchRole: "original",
      originalLaunchId: driver.getSnapshot().originalLaunchId,
      restoreLaunchId: null,
      validationState: "CHECKPOINTED",
      lastGateSequence: 20,
      initialPosition: { mapId: 1, playerX: 2, playerY: 1, fixtureState: 0 },
      savedPosition: { mapId: 1, playerX: 3, playerY: 1, fixtureState: 1 },
      divergedPosition: { mapId: 2, playerX: 4, playerY: 5, fixtureState: 2 },
      restoredPosition: null,
      restoreInputPosition: null,
    });
    expect(driver.getSnapshot().machineGates.find((gate) => gate.gate === "CHECKPOINT_CREATED")?.evidence)
      .toMatchObject({ payloadKind: "NATIVE_SAVE_BUNDLE_V1", sizeBytes: 3 });
    expect(finish).toHaveBeenCalledOnce();
    expect(events).toHaveLength(20);
    expect(events.map((event) => `${event.gate}:${event.phase}`)).toEqual(originalGateOrder());
    expect(events.find((event) => event.gate === "SAVE_POINT_RECORDED" && event.phase === "PASS")?.evidence)
      .toEqual({ mapId: 1, playerX: 3, playerY: 1, fixtureState: 1 });
    expect(events.find((event) => event.gate === "INITIAL_POSITION_RECORDED" && event.phase === "PASS")?.evidence)
      .toEqual({ mapId: 1, playerX: 2, playerY: 1, fixtureState: 0 });
    expect(events.find((event) => event.gate === "POST_SAVE_STATE_DIVERGED" && event.phase === "PASS")?.evidence)
      .toEqual({ mapId: 2, playerX: 4, playerY: 5, fixtureState: 2 });
    expect(events.find((event) => event.gate === "CHECKPOINT_CREATED" && event.phase === "PASS")?.evidence)
      .toMatchObject({ payloadKind: "NATIVE_SAVE_BUNDLE_V1", sizeBytes: 3, sha256: expect.stringMatching(/^[0-9a-f]{64}$/) });
  });

  it("automatically proves restore position and uploads its PNG before the screenshot gate passes", async () => {
    const events: GateRequest[] = [];
    const fetchMock = installValidationFetch(events);
    const restored = { mapId: 7, playerX: 8, playerY: 9, fixtureState: 10 };
    const driver = new RpgRuntimeValidationDriver({
      config: validationConfig({ payloadKind: "NATIVE_SAVE_BUNDLE_V1", payloadUrl: "/runtime/launches/restore/state" }),
      signal: new AbortController().signal,
      uploadCheckpoint: async () => ({
        payloadKind: "NATIVE_SAVE_BUNDLE_V1",
        sizeBytes: 3,
        sha256: "039058c6f2c0cb492c533b0a4d14ef77cc0f78abccced5287d84a1a2011cfb81",
      }),
      finishOriginalLaunch: async () => undefined,
    });

    await driver.prepare();
    const mounted = validationRuntime(() => restored);
    await driver.attachRuntime(mounted.instance, mounted.runtime);

    expect(driver.getSnapshot().phase).toBe("restore-input");
    expect(driver.getSnapshot().observedPosition).toEqual(restored);
    expect(events.map((event) => event.sequence)).toEqual([21, 22, 23, 24, 25, 26]);
    expect(events.map((event) => `${event.gate}:${event.phase}`)).toEqual([
      "RESTORE_STARTED:BEGIN", "RESTORE_STARTED:PASS",
      "RESTORE_POSITION_VERIFIED:BEGIN", "RESTORE_POSITION_VERIFIED:PASS",
      "RESTORE_SCREENSHOT:BEGIN", "RESTORE_SCREENSHOT:PASS",
    ]);
    const calls = fetchMock.mock.calls.map(([input]) => String(input));
    const screenshotIndex = calls.findIndex((url) => url.endsWith("/review-screenshot"));
    expect(screenshotIndex).toBeGreaterThan(-1);
    expect(screenshotIndex).toBeLessThan(calls.length - 1);

    await driver.runAction();
    expect(driver.getSnapshot().phase).toBe("restore-input");
    expect(driver.getSnapshot().error).toContain("尚未检测到恢复后");

    restored.playerX += 1;
    await driver.runAction();
    expect(driver.getSnapshot().phase).toBe("restore-complete");
    expect(driver.getSnapshot()).toMatchObject({
      launchRole: "restore",
      validationState: "AWAITING_DECISION",
      lastGateSequence: 28,
      restoredPosition: { mapId: 7, playerX: 8, playerY: 9, fixtureState: 10 },
      restoreInputPosition: { mapId: 7, playerX: 9, playerY: 9, fixtureState: 10 },
    });
    expect(driver.getSnapshot().restoreLaunchId).not.toBe(driver.getSnapshot().originalLaunchId);
    expect(driver.getSnapshot().machineGates.find((gate) => gate.gate === "RESTORE_INPUT")?.evidence)
      .toEqual({ mapId: 7, playerX: 9, playerY: 9, fixtureState: 10 });
    expect(events.slice(-2).map((event) => `${event.gate}:${event.phase}`)).toEqual([
      "RESTORE_INPUT:BEGIN", "RESTORE_INPUT:PASS",
    ]);
  });

  it("fails the checkpoint gate when the server receipt does not match the uploaded bytes", async () => {
    const events: GateRequest[] = [];
    installValidationFetch(events);
    let position: RpgPosition = { mapId: 1, playerX: 1, playerY: 1, fixtureState: 0 };
    const driver = new RpgRuntimeValidationDriver({
      config: validationConfig(null),
      signal: new AbortController().signal,
      uploadCheckpoint: async () => ({
        payloadKind: "NATIVE_SAVE_BUNDLE_V1",
        sizeBytes: 4,
        sha256: "039058c6f2c0cb492c533b0a4d14ef77cc0f78abccced5287d84a1a2011cfb81",
      }),
      finishOriginalLaunch: async () => undefined,
    });
    await driver.prepare();
    const mounted = validationRuntime(() => position);
    await driver.attachRuntime(mounted.instance, mounted.runtime);
    position = { mapId: 1, playerX: 2, playerY: 1, fixtureState: 1 };
    await driver.runAction();
    await driver.runAction();
    position = { mapId: 1, playerX: 3, playerY: 1, fixtureState: 1 };

    await driver.runAction();

    expect(driver.getSnapshot().phase).toBe("error");
    expect(driver.getSnapshot().error).toBe("RPG_CHECKPOINT_RESPONSE_MISMATCH");
    expect(events.at(-1)).toMatchObject({ gate: "CHECKPOINT_CREATED", phase: "FAIL", evidence: {} });
  });

  it("continues a RUNTIME_READY BEGIN reload without submitting a second BEGIN", async () => {
    const events: GateRequest[] = [];
    installValidationFetch(events);
    const config = validationConfig(null);
    const resume = config.runtimeValidation!;
    resume.machineGates[0] = {
      ...resume.machineGates[0], status: "IN_PROGRESS", begunAtMs: 1,
    };
    resume.lastGateSequence = 1;
    const driver = validationDriver(config);

    expect(driver.getSnapshot()).toMatchObject({
      launchRole: "original",
      lastGateSequence: 1,
    });
    expect(driver.getSnapshot().machineGates[0])
      .toMatchObject({ gate: "RUNTIME_READY", status: "IN_PROGRESS", begunAtMs: 1 });

    await driver.prepare();
    expect(events).toHaveLength(0);
    const mounted = validationRuntime(() => ({ mapId: 1, playerX: 1, playerY: 1, fixtureState: 0 }));
    await driver.attachRuntime(mounted.instance, mounted.runtime);

    expect(events[0]).toMatchObject({ sequence: 2, gate: "RUNTIME_READY", phase: "PASS" });
    expect(events.filter((event) => event.gate === "RUNTIME_READY" && event.phase === "BEGIN")).toHaveLength(0);
  });

  it("hydrates several completed original gates and resumes at the first unfinished action", async () => {
    const events: GateRequest[] = [];
    installValidationFetch(events);
    const config = validationConfig(null);
    const resume = config.runtimeValidation!;
    for (const gate of resume.machineGates.slice(0, 4)) {
      gate.status = "PASSED";
      gate.begunAtMs = 1;
      gate.completedAtMs = 2;
      gate.evidence = {};
    }
    resume.lastGateSequence = 8;
    const driver = validationDriver(config);

    await driver.prepare();
    const mounted = validationRuntime(() => ({ mapId: 1, playerX: 2, playerY: 1, fixtureState: 0 }));
    await driver.attachRuntime(mounted.instance, mounted.runtime);

    expect(events).toHaveLength(0);
    expect(driver.getSnapshot().phase).toBe("audio");
    expect(driver.getSnapshot().lastGateSequence).toBe(8);
    expect(driver.getSnapshot().machineGates.slice(0, 4).every((gate) => gate.status === "PASSED")).toBe(true);
  });

  it("hydrates a restore reload after screenshot upload and only completes its unfinished gate", async () => {
    const events: GateRequest[] = [];
    const fetchMock = installValidationFetch(events);
    const config = validationConfig({ payloadKind: "NATIVE_SAVE_BUNDLE_V1", payloadUrl: "/runtime/launches/restore/state" });
    const resume = config.runtimeValidation!;
    for (const index of [10, 11]) {
      resume.machineGates[index] = {
        ...resume.machineGates[index], status: "PASSED", begunAtMs: 21 + index, completedAtMs: 22 + index,
        evidence: index === 11 ? { mapId: 7, playerX: 8, playerY: 9, fixtureState: 10 } : {},
      };
    }
    resume.machineGates[12] = {
      ...resume.machineGates[12], status: "IN_PROGRESS", begunAtMs: 25,
    };
    resume.lastGateSequence = 25;
    resume.restoreScreenshotUploaded = true;
    const driver = validationDriver(config);

    await driver.prepare();
    const mounted = validationRuntime(() => ({ mapId: 7, playerX: 8, playerY: 9, fixtureState: 10 }));
    await driver.attachRuntime(mounted.instance, mounted.runtime);

    expect(events).toEqual([expect.objectContaining({ sequence: 26, gate: "RESTORE_SCREENSHOT", phase: "PASS" })]);
    expect(driver.getSnapshot().phase).toBe("restore-input");
    expect(driver.getSnapshot()).toMatchObject({
      launchRole: "restore",
      lastGateSequence: 26,
      restoredPosition: { mapId: 7, playerX: 8, playerY: 9, fixtureState: 10 },
    });
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith("/review-screenshot"))).toBe(false);
  });
});

describe("waitForContinuousFrames", () => {
  it("waits for the RPG probe before sampling the runtime frame counter", async () => {
    let positionAttempts = 0;
    const runtime = validationRuntime(() => {
      positionAttempts += 1;
      if (positionAttempts < 3) {throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");}
      return { mapId: 1, playerX: 1, playerY: 1, fixtureState: 0 };
    }, (() => {
      const frames = [10, 160, 310];
      return () => frames.shift() ?? 310;
    })());

    await expect(waitForContinuousFrames(runtime.runtime, new AbortController().signal, async () => undefined))
      .resolves.toBe(300);
    expect(positionAttempts).toBe(3);
  });

  it("rejects a counter reset instead of treating it as continuous progress", async () => {
    const frames = [50, 100, 20];
    const runtime = validationRuntime(() => ({ mapId: 1, playerX: 1, playerY: 1, fixtureState: 0 }), () => frames.shift() ?? 20);
    await expect(waitForContinuousFrames(runtime.runtime, new AbortController().signal, async () => undefined))
      .rejects.toThrow("RPG_RUNTIME_FRAME_DISCONTINUITY");
  });

  it("skips a transient unreadable runtime sample while preserving continuous frame evidence", async () => {
    const samples: Array<number | Error> = [10, new Error("RPG_RUNTIME_POSITION_UNAVAILABLE"), 160, 310];
    const runtime = validationRuntime(
      () => ({ mapId: 1, playerX: 1, playerY: 1, fixtureState: 0 }),
      () => {
        const sample = samples.shift() ?? 310;
        if (sample instanceof Error) {throw sample;}
        return sample;
      },
    );

    await expect(waitForContinuousFrames(runtime.runtime, new AbortController().signal, async () => undefined))
      .resolves.toBe(300);
  });

  it("still times out when every runtime frame sample remains unavailable", async () => {
    let now = 0;
    const clock = vi.spyOn(performance, "now").mockImplementation(() => {
      now += 10_000;
      return now;
    });
    const runtime = validationRuntime(
      () => ({ mapId: 1, playerX: 1, playerY: 1, fixtureState: 0 }),
      () => {throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");},
    );

    try {
      await expect(waitForContinuousFrames(runtime.runtime, new AbortController().signal, async () => undefined))
        .rejects.toThrow("RPG_RUNTIME_TIMEOUT");
    } finally {clock.mockRestore();}
  });

  it("waits for the administrator to enter a valid map before recording A", async () => {
    const positions = [
      { mapId: 0, playerX: 0, playerY: 0, fixtureState: 0 },
      { mapId: 3, playerX: 4, playerY: 5, fixtureState: 6 },
    ];
    const runtime = validationRuntime(() => positions.shift() ?? { mapId: 3, playerX: 4, playerY: 5, fixtureState: 6 });
    await expect(waitForRpgPosition(runtime.runtime, new AbortController().signal, async () => undefined))
      .resolves.toEqual({ mapId: 3, playerX: 4, playerY: 5, fixtureState: 6 });
  });
});

function installValidationFetch(events: GateRequest[]) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith("/review-screenshot")) {return new Response(null, { status: 201 });}
    const request = JSON.parse(String(init?.body)) as GateRequest;
    events.push(request);
    return Response.json({
      sequence: request.sequence,
      eventId: request.eventId,
      validationState: "RUNNING",
      idempotentReplay: false,
    });
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function validationRuntime(position: () => RpgPosition, frames?: () => number) {
  let frame = 0;
  const instance: EmulatorInstance = {
    paused: false,
    on: () => undefined,
    gameManager: {
      validationPurpose: true,
      savePayloadKind: "NATIVE_SAVE_BUNDLE_V1",
      getCheckpointAvailability: () => ({ available: true, reason: null }),
    },
  };
  const runtime: GameRuntime = {
    mount: vi.fn(async () => undefined),
    pause: vi.fn(async () => undefined),
    resume: vi.fn(async () => undefined),
    checkpoint: vi.fn(async () => ({ bytes: Uint8Array.of(1), format: "native-save-bundle-v1" })),
    screenshot: vi.fn(async () => new Blob()),
    exit: vi.fn(async () => undefined),
    getState: () => "RUNNING",
    getCapabilities: () => validationCapabilities,
    getCheckpointAvailability: () => ({ available: true, blocker: null }),
    getCanvas: () => null,
    getFrameCount: frames ?? (() => {frame += 100; return frame;}),
    getValidationProbe: (kind) => ({ kind, schemaVersion: 1, value: position() }),
    setVolume: vi.fn(),
    subscribe: vi.fn(() => vi.fn()),
  };
  return { instance, runtime };
}

const validationCapabilities: RuntimeCapabilities = {
  checkpoint: true,
  contentSources: ["FILE_TREE_V1"],
  frameCounter: true,
  pause: true,
  screenshot: true,
  standardGamepad: true,
  validationProbes: ["rpgmaker.position.v1"],
  volume: true,
};

function validationDriver(config: RpgMakerConfig) {
  return new RpgRuntimeValidationDriver({
    config,
    signal: new AbortController().signal,
    uploadCheckpoint: async () => ({
      payloadKind: "NATIVE_SAVE_BUNDLE_V1", sizeBytes: 3,
      sha256: "039058c6f2c0cb492c533b0a4d14ef77cc0f78abccced5287d84a1a2011cfb81",
    }),
    finishOriginalLaunch: async () => undefined,
  });
}

function validationConfig(checkpoint: RpgMakerConfig["checkpoint"]): RpgMakerConfig {
  const launchId = crypto.randomUUID();
  const originalLaunchId = checkpoint ? crypto.randomUUID() : launchId;
  const originalGates = new Set([
    "RUNTIME_READY", "ENGINE_PROFILE", "FRAMES_300", "INPUT", "AUDIO", "INITIAL_POSITION_RECORDED",
    "SAVE_POINT_RECORDED", "CHECKPOINT_CREATED", "POST_SAVE_STATE_DIVERGED", "ORIGINAL_LAUNCH_ENDED",
  ]);
  const machineGates = validationGateOrder().map((gate) => ({
    gate,
    status: checkpoint && originalGates.has(gate) ? "PASSED" as const : "NOT_STARTED" as const,
    begunAtMs: checkpoint && originalGates.has(gate) ? 1 : null,
    completedAtMs: checkpoint && originalGates.has(gate) ? 2 : null,
    evidence: checkpoint ? gateEvidence(gate) : null,
    failureCode: null,
  }));
  return {
    runtimeFamily: "RPGMAKER",
    protocolVersion: 1,
    mode: "single",
    purpose: "RPG_RUNTIME_VALIDATION",
    launchId,
    coreId: "rpgmaker_2000",
    coreName: "RPG Maker 2000",
    gameTitle: "Validation fixture",
    platformName: "RPG Maker",
    returnTo: "/admin/reviews/item",
    warnings: [],
    generation: "RPG2000",
    routeKey: "RPG2000_EASYRPG",
    artifactId: crypto.randomUUID(),
    checkpoint,
    checkpointAvailability: { available: true, reason: null },
    runtimeValidation: {
      validationId: crypto.randomUUID(),
      state: checkpoint ? "CHECKPOINTED" : "STARTING",
      originalLaunchId,
      restoreLaunchId: checkpoint ? launchId : null,
      lastGateSequence: checkpoint ? 20 : 0,
      machineGates,
      checkpointEvidence: checkpoint ? {
        payloadKind: "NATIVE_SAVE_BUNDLE_V1", sizeBytes: 3,
        sha256: "039058c6f2c0cb492c533b0a4d14ef77cc0f78abccced5287d84a1a2011cfb81",
      } : null,
      restoreScreenshotUploaded: false,
    },
    adapter: {
      adapterKind: "EASYRPG_WEB",
      adapterId: "easyrpg-web",
      engineMode: "rpg2k",
      runtimeBaseUrl: "/runtime/retrom-runtime/v0.2.0/",
      projectRootUrl: `/runtime/content/project/${"a".repeat(64)}/`,
      projectIndexUrl: `/runtime/content/project/${"a".repeat(64)}/index.json`,
      rtpSource: null,
      checkpointSlot: 100,
    },
  };
}

function originalGateOrder() {
  return [
    "RUNTIME_READY", "ENGINE_PROFILE", "FRAMES_300", "INPUT", "AUDIO", "INITIAL_POSITION_RECORDED", "SAVE_POINT_RECORDED",
    "CHECKPOINT_CREATED", "POST_SAVE_STATE_DIVERGED", "ORIGINAL_LAUNCH_ENDED",
  ].flatMap((gate) => [`${gate}:BEGIN`, `${gate}:PASS`]);
}

function validationGateOrder() {
  return [
    "RUNTIME_READY", "ENGINE_PROFILE", "FRAMES_300", "INPUT", "AUDIO", "INITIAL_POSITION_RECORDED",
    "SAVE_POINT_RECORDED", "CHECKPOINT_CREATED", "POST_SAVE_STATE_DIVERGED", "ORIGINAL_LAUNCH_ENDED",
    "RESTORE_STARTED", "RESTORE_POSITION_VERIFIED", "RESTORE_SCREENSHOT", "RESTORE_INPUT",
  ] as const;
}

function gateEvidence(gate: string): RpgGateEvidence {
  if (gate === "INITIAL_POSITION_RECORDED") {return { mapId: 7, playerX: 7, playerY: 9, fixtureState: 9 };}
  if (gate === "SAVE_POINT_RECORDED") {return { mapId: 7, playerX: 8, playerY: 9, fixtureState: 10 };}
  if (gate === "POST_SAVE_STATE_DIVERGED") {return { mapId: 7, playerX: 9, playerY: 9, fixtureState: 11 };}
  return {};
}
