import { afterEach, describe, expect, it, vi } from "vitest";
import { NetplayController, type NetplayLaunchConfig } from "./controller";
import { coreStateBytes, type EJSNetplayFrameBridge } from "./ejs-netplay-4.2.3-v1";
import { decodeStateFrame } from "./protocol";

class FakeSocket extends EventTarget {
  static readonly OPEN = 1;
  static instances: FakeSocket[] = [];
  readyState = FakeSocket.OPEN;
  binaryType = "";
  sent: unknown[] = [];
  constructor(public readonly url: URL, public readonly protocol: string) {
    super(); FakeSocket.instances.push(this);
    queueMicrotask(() => this.dispatchEvent(new Event("open")));
  }
  send(value: unknown) {this.sent.push(value);}
  close() {this.readyState = 3;}
  message(value: unknown) {this.dispatchEvent(new MessageEvent("message", { data: value }));}
}

const launch: NetplayLaunchConfig = {
  roomId: "01980000-0000-7000-8000-000000000001",
  sessionId: "01980000-0000-7000-8000-000000000002",
  playerNo: 1,
  runtimeSocketUrl: "/runtime/netplay/rooms/01980000-0000-7000-8000-000000000001/socket",
  netplayProfile: {
    schemaVersion: 1, protocolVersion: "retrom-netplay-v2", profileId: "fceumm-423-v1", platformIds: ["nes"],
    emulatorjsVersion: "4.2.3",
    playerAdapterId: "ejs-4.2.3-v2", netplayAdapterId: "ejs-netplay-4.2.3-v1",
    coreArtifactId: "01980000-0000-7000-8000-000000000003", gameVariantRevisionId: "01980000-0000-7000-8000-000000000004",
    coreArtifactSha256: "1".repeat(64), sourceManifestDigest: "2".repeat(64), dependencySnapshotDigest: "3".repeat(64), defaultCoreOptions: {},
    controlCount: 24, maxPlayers: 2, maxPredictionFrames: 8, maxRollbackFrames: 120,
    checkpointEveryFrames: 120, canonicalHistoryFrames: 600, maxStateBytes: 1_048_576,
  },
};

function raState(core: number[]) {
  const padded = (core.length + 7) & ~7;
  const state = new Uint8Array(8 + 8 + padded + 8);
  state.set(new TextEncoder().encode("RASTATE")); state[7] = 1;
  state.set(new TextEncoder().encode("MEM "), 8);
  new DataView(state.buffer).setUint32(12, core.length, true);
  state.set(core, 16); state.set(new TextEncoder().encode("END "), 16 + padded);
  return state;
}

function nestopiaState(rootPayload: number[], trackedInput: number[], padding: number[] = []) {
  const root = [0x4e, 0x46, 0x4f, 0, 8, 0, 0, 0, ...rootPayload];
  const core = new Uint8Array(8 + root.length + trackedInput.length + padding.length);
  core.set([0x4e, 0x53, 0x54, 0x1a]);
  new DataView(core.buffer).setUint32(4, root.length, true);
  core.set(root, 8);
  core.set(trackedInput, 8 + root.length);
  core.set(padding, 8 + root.length + trackedInput.length);
  return raState([...core]);
}

function controllerWithStates(captured: Uint8Array, normalized: Uint8Array, coreExact: (state: Uint8Array) => boolean) {
  let current = captured;
  const loadStateForTransfer = vi.fn(async (state: Uint8Array) => {
    const exact = coreExact(state);
    current = normalized;
    return {
      recaptured: normalized, byteExact: exact, coreExact: exact,
      expectedCoreBytes: coreStateBytes(state).byteLength,
      recapturedCoreBytes: coreStateBytes(normalized).byteLength,
      firstCoreMismatch: exact ? -1 : 0,
      lastCoreMismatch: exact ? -1 : 0,
      coreMismatchCount: exact ? 0 : 1,
      coreMismatchRanges: exact ? [] : [{ start: 0, end: 1 }],
    };
  });
  const bridge = {
    pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
    captureState: vi.fn(() => current), loadStateForTransfer, loadStateAndWait: vi.fn(),
    runNetplayFrame: vi.fn(), sampleLocalControls: vi.fn(() => Array(24).fill(0)),
  } as unknown as EJSNetplayFrameBridge;
  return { bridge, loadStateForTransfer };
}

async function requestAuthorityState(controller: NetplayController, reason: "INITIAL" | "PEER_RECONNECTED") {
  await controller.start();
  const socket = FakeSocket.instances[0]!;
  socket.message(JSON.stringify({
    v: 1, type: "REQUEST_STATE", sessionId: launch.sessionId, epoch: 0, seq: 1,
    transferId: "01980000-0000-7000-8000-000000000005", nextFrame: 0,
    targetPlayerNos: [2], reason,
  }));
  await vi.waitFor(() => expect(socket.sent.some((value) => value instanceof Uint8Array)).toBe(true));
  return socket;
}

afterEach(() => {
  vi.restoreAllMocks(); vi.unstubAllGlobals(); FakeSocket.instances = [];
});

describe("NetplayController authority state", () => {
  it("normalizes an authority state to a verified native-load fixed point before transfer", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    const captured = raState([1]);
    const normalized = raState([2]);
    const setup = controllerWithStates(captured, normalized, (state) => coreStateBytes(state)[0] === 2);
    const controller = new NetplayController(launch, "0".repeat(64), setup.bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    });
    const socket = await requestAuthorityState(controller, "INITIAL");
    expect(setup.loadStateForTransfer).toHaveBeenCalledTimes(2);
    const frame = socket.sent.find((value): value is Uint8Array => value instanceof Uint8Array)!;
    expect([...coreStateBytes(decodeStateFrame(frame).state)]).toEqual([2]);
    const ready = socket.sent.filter((value): value is string => typeof value === "string")
      .map((value) => JSON.parse(value) as { type: string; recaptureMatched?: boolean })
      .find((message) => message.type === "STATE_READY");
    expect(ready?.recaptureMatched).toBe(true);
    controller.end();
  });

  it("transfers Nestopia's complete state when only its unrestorable input trailer changes", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    const deterministic = [1, 2, 3, 4, 5, 6, 7, 8];
    const captured = nestopiaState(deterministic, [2, 2, 2, 2, 0, 0, 0, 0], [7, 7]);
    const normalized = nestopiaState(deterministic, [1, 1, 1, 1, 0, 0, 0, 0], [7, 7]);
    const setup = controllerWithStates(captured, normalized, () => false);
    setup.loadStateForTransfer.mockImplementationOnce(async () => ({
      recaptured: normalized, byteExact: false, coreExact: false,
      expectedCoreBytes: 34, recapturedCoreBytes: 34, firstCoreMismatch: 24,
      lastCoreMismatch: 27, coreMismatchCount: 4, coreMismatchRanges: [{ start: 24, end: 28 }],
    }));
    const nestopiaLaunch = {
      ...launch, netplayProfile: { ...launch.netplayProfile, profileId: "nestopia-423-v1", maxPredictionFrames: 0 },
    };
    const controller = new NetplayController(nestopiaLaunch, "0".repeat(64), setup.bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    });
    const socket = await requestAuthorityState(controller, "PEER_RECONNECTED");
    expect(setup.loadStateForTransfer).toHaveBeenCalledOnce();
    const frame = socket.sent.find((value): value is Uint8Array => value instanceof Uint8Array)!;
    const transferredCore = coreStateBytes(decodeStateFrame(frame).state);
    expect([...transferredCore.subarray(0, 24)]).toEqual([...coreStateBytes(normalized).subarray(0, 24)]);
    expect([...transferredCore.subarray(24, 32)]).toEqual(Array(8).fill(0));
    expect([...transferredCore.subarray(32)]).toEqual([7, 7]);
    controller.end();
  });
});
