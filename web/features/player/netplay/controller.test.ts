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
  closes: Array<[number | undefined, string | undefined]> = [];
  constructor(public readonly url: URL, public readonly protocol: string) {
    super(); FakeSocket.instances.push(this);
    queueMicrotask(() => this.dispatchEvent(new Event("open")));
  }
  send(value: unknown) { this.sent.push(value); }
  close(code?: number, reason?: string) {
    this.closes.push([code, reason]); this.readyState = 3;
    this.dispatchEvent(new CloseEvent("close", { code, reason }));
  }
  remoteClose() { this.close(); }
  message(value: unknown) { this.dispatchEvent(new MessageEvent("message", { data: value })); }
}

const launch: NetplayLaunchConfig = {
  roomId: "01980000-0000-7000-8000-000000000001",
  sessionId: "01980000-0000-7000-8000-000000000002",
  playerNo: 2,
  runtimeSocketUrl: "/runtime/netplay/rooms/01980000-0000-7000-8000-000000000001/socket",
  netplayProfile: {
    schemaVersion: 1, protocolVersion: "retrom-netplay-v1", profileId: "fceumm-423-v1", emulatorjsVersion: "4.2.3",
    playerAdapterId: "ejs-4.2.3-v2", netplayAdapterId: "ejs-netplay-4.2.3-v1",
    coreArtifactId: "01980000-0000-7000-8000-000000000003", gameVariantRevisionId: "01980000-0000-7000-8000-000000000004",
    coreArtifactSha256: "1".repeat(64), sourceManifestDigest: "2".repeat(64), dependencySnapshotDigest: "3".repeat(64), defaultCoreOptions: {},
    controlCount: 24, maxPlayers: 2, maxPredictionFrames: 8, maxRollbackFrames: 120,
    checkpointEveryFrames: 120, canonicalHistoryFrames: 600, maxStateBytes: 1_048_576,
  },
};

const fbneoLaunch: NetplayLaunchConfig = {
  ...launch,
  netplayProfile: {
    ...launch.netplayProfile,
    profileId: "fbneo-423-v1",
    maxPredictionFrames: 0,
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

afterEach(() => {
  vi.useRealTimers(); vi.unstubAllGlobals(); FakeSocket.instances = [];
});

describe("NetplayController reconnect lease", () => {
  it("normalizes an authority state to a verified native-load fixed point before transfer", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    const captured = raState([1]);
    const normalized = raState([2]);
    let current = captured;
    const loadStateForTransfer = vi.fn(async (state: Uint8Array) => {
      const coreExact = [...coreStateBytes(state)].every((byte, index) => byte === coreStateBytes(normalized)[index]);
      current = normalized;
      return {
        recaptured: normalized, byteExact: coreExact, coreExact,
        expectedCoreBytes: 1, recapturedCoreBytes: 1, firstCoreMismatch: coreExact ? -1 : 0,
      };
    });
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => current), loadStateForTransfer, loadStateAndWait: vi.fn(),
      runNetplayFrame: vi.fn(), sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController({ ...launch, playerNo: 1 }, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    });

    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message(JSON.stringify({
      v: 1, type: "REQUEST_STATE", sessionId: launch.sessionId, epoch: 0, seq: 1,
      transferId: "01980000-0000-7000-8000-000000000005", nextFrame: 0,
      targetPlayerNos: [2], reason: "INITIAL",
    }));

    await vi.waitFor(() => expect(socket.sent.some((value) => value instanceof Uint8Array)).toBe(true));
    expect(loadStateForTransfer).toHaveBeenCalledTimes(2);
    const frame = socket.sent.find((value): value is Uint8Array => value instanceof Uint8Array)!;
    expect([...coreStateBytes(decodeStateFrame(frame).state)]).toEqual([2]);
    const ready = socket.sent.filter((value): value is string => typeof value === "string")
      .map((value) => JSON.parse(value) as { type: string; recaptureMatched?: boolean })
      .find((message) => message.type === "STATE_READY");
    expect(ready?.recaptureMatched).toBe(true);
    controller.end();
  });

  it("reconnects a running player with HELLO seq zero and retained epoch history", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", FakeSocket);
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => new Uint8Array([1])), loadStateAndWait: vi.fn(), runNetplayFrame: vi.fn(), sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const onRunning = vi.fn();
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning, onPaused: vi.fn(), onEnded: vi.fn(),
    });
    await controller.start();
    expect(FakeSocket.instances).toHaveLength(1);
    const first = FakeSocket.instances[0]!;
    expect(JSON.parse(first.sent[0] as string)).toMatchObject({ type: "HELLO", seq: 0, lastCanonicalFrame: -1, lastServerSeq: 0 });
    expect(JSON.parse(first.sent[1] as string)).toMatchObject({ type: "RUNTIME_READY", seq: 1 });
    first.message(JSON.stringify({ v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 0, seq: 1, nextFrame: 0, occupiedSeatMask: 3 }));
    await Promise.resolve(); await Promise.resolve();
    expect(onRunning).toHaveBeenCalledOnce();
    first.remoteClose();
    await vi.advanceTimersByTimeAsync(500);
    expect(FakeSocket.instances).toHaveLength(2);
    const second = FakeSocket.instances[1]!;
    expect(JSON.parse(second.sent[0] as string)).toMatchObject({ type: "HELLO", seq: 0, epoch: 0, lastCanonicalFrame: -1, lastServerSeq: 1 });
    expect(second.sent).toHaveLength(1);
    controller.end();
  });

  it("reconnects during the initial synchronization without ending its launch", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", FakeSocket);
    const onEnded = vi.fn();
    const onConnect = vi.fn();
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame: vi.fn(),
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded,
    }, { onConnect });

    await controller.start();
    FakeSocket.instances[0]!.remoteClose();
    expect(onEnded).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(500);

    expect(FakeSocket.instances).toHaveLength(2);
    const replacement = FakeSocket.instances[1]!;
    const messages = replacement.sent.map((value) => JSON.parse(value as string) as { type: string; seq: number });
    expect(messages).toMatchObject([
      { type: "HELLO", seq: 0 },
      { type: "RUNTIME_READY", seq: 1 },
    ]);
    expect(onConnect.mock.calls.map(([reconnect]) => reconnect)).toEqual([false, true]);
    expect(onEnded).not.toHaveBeenCalled();
    controller.dispose();
  });

  it("disposes a superseded controller without ending the shared session", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", FakeSocket);
    const closeBridge = vi.fn();
    const onEnded = vi.fn();
    const onStatus = vi.fn();
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: closeBridge,
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame: vi.fn(),
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus, onRunning: vi.fn(), onPaused: vi.fn(), onEnded,
    });

    await controller.start();
    const socket = FakeSocket.instances[0]!;
    controller.dispose();
    const types = socket.sent.map((value) => (JSON.parse(value as string) as { type: string }).type);
    expect(types).not.toContain("END_REQUEST");
    expect(socket.closes).toContainEqual([1000, "CLIENT_DISPOSED"]);
    expect(closeBridge).toHaveBeenCalledOnce();
    expect(onEnded).not.toHaveBeenCalled();

    const replacementController = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus, onRunning: vi.fn(), onPaused: vi.fn(), onEnded,
    });
    await replacementController.start();
    const replacement = FakeSocket.instances[1]!;
    replacement.close(1008, "connection replaced");
    await vi.advanceTimersByTimeAsync(500);
    expect(FakeSocket.instances).toHaveLength(2);
    expect(onEnded).not.toHaveBeenCalled();
    expect(onStatus).toHaveBeenCalledWith("联机已由同一账户的另一页面接管", "warning");
  });

  it("advances an FBNeo frame only after its canonical lockstep input arrives", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    const runNetplayFrame = vi.fn().mockResolvedValue(undefined);
    const onRollback = vi.fn();
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame,
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(fbneoLaunch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    }, { onRollback });

    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message(JSON.stringify({
      v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 0, seq: 1,
      nextFrame: 0, occupiedSeatMask: 3,
    }));
    await vi.waitFor(() => expect(socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "INPUT")).toHaveLength(1));
    expect(runNetplayFrame).not.toHaveBeenCalled();

    const players = Array.from({ length: 4 }, () => Array(24).fill(0));
    socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: 2,
      frame: 0, occupiedSeatMask: 3, players,
    }));
    await vi.waitFor(() => expect(runNetplayFrame).toHaveBeenCalledOnce());
    expect(bridge.captureState).not.toHaveBeenCalled();

    for (let frame = 1; frame < 120; frame += 1) socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: frame + 2,
      frame, occupiedSeatMask: 3, players,
    }));
    await vi.waitFor(() => expect(runNetplayFrame).toHaveBeenCalledTimes(120));
    expect(bridge.captureState).toHaveBeenCalledOnce();
    expect(socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "HASH")
      .map((value) => JSON.parse(value as string))).toMatchObject([{ frame: 119 }]);

    const inputs = socket.sent.filter((value): value is string => typeof value === "string")
      .map((value) => JSON.parse(value) as { type: string; frame?: number })
      .filter((message) => message.type === "INPUT");
    expect(inputs).toHaveLength(121);
    expect(inputs.at(0)).toMatchObject({ frame: 0, playerNo: 2 });
    expect(inputs.at(-1)).toMatchObject({ frame: 120, playerNo: 2 });
    expect(onRollback).not.toHaveBeenCalled();
    controller.end();
  });

  it("expands the strict input buffer only when measured round-trip latency needs it", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    let currentTimeMS = 1_000;
    vi.spyOn(Date, "now").mockImplementation(() => currentTimeMS);
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(),
      runNetplayFrame: vi.fn().mockResolvedValue(undefined), sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(fbneoLaunch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    });

    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message(JSON.stringify({
      v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 0, seq: 1,
      nextFrame: 0, occupiedSeatMask: 3,
    }));
    await vi.waitFor(() => expect(socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "INPUT")).toHaveLength(1));

    currentTimeMS += 100;
    socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: 2,
      frame: 0, occupiedSeatMask: 3, players: Array.from({ length: 4 }, () => Array(24).fill(0)),
    }));
    await vi.waitFor(() => expect(socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "INPUT")).toHaveLength(8));
    const frames = socket.sent.filter((value): value is string => typeof value === "string")
      .map((value) => JSON.parse(value) as { type: string; frame?: number })
      .filter((message) => message.type === "INPUT")
      .map((message) => message.frame);
    expect(frames).toEqual([0, 1, 2, 3, 4, 5, 6, 7]);
    controller.end();
  });

  it("sends a deferred checkpoint after its post-frame state enters the rollback ring", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    let stateByte = 0;
    let releaseFirstFrame!: () => void;
    let calls = 0;
    const runNetplayFrame = vi.fn(() => {
      calls += 1;
      if (calls === 1) return new Promise<void>((resolve) => {
        releaseFirstFrame = () => { stateByte += 1; resolve(); };
      });
      stateByte += 1;
      return Promise.resolve();
    });
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([stateByte])), loadStateAndWait: vi.fn(), runNetplayFrame,
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const onCanonical = vi.fn();
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    }, { onCanonical });
    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message(JSON.stringify({
      v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 0, seq: 1,
      nextFrame: 0, occupiedSeatMask: 3,
    }));
    await vi.waitFor(() => expect(runNetplayFrame).toHaveBeenCalledOnce());
    const players = Array.from({ length: 4 }, () => Array(24).fill(0));
    for (let frame = 0; frame < 120; frame += 1) socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: frame + 2,
      frame, occupiedSeatMask: 3, players,
    }));
    await vi.waitFor(() => expect(onCanonical).toHaveBeenCalledWith({ frame: 119, predictionFrames: 0 }));
    expect(socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "HASH")).toHaveLength(0);
    releaseFirstFrame();
    await vi.waitFor(() => {
      const hashes = socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "HASH");
      expect(hashes).toHaveLength(1);
      expect(JSON.parse(hashes[0] as string)).toMatchObject({ type: "HASH", frame: 119 });
    });
    controller.end();
  });

  it("uses an application close code when the browser rejects a server message", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame: vi.fn(),
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const onEnded = vi.fn();
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded,
    });
    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message("{}");
    await vi.waitFor(() => expect(onEnded).toHaveBeenCalledWith("PROTOCOL_VIOLATION"));
    expect(socket.closes).toContainEqual([4008, "PROTOCOL_VIOLATION"]);
  });

  it("releases local controls on focus loss without suspending the network session", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", FakeSocket);
    const resetLocalControls = vi.fn();
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls, close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(),
      runNetplayFrame: vi.fn().mockResolvedValue(undefined), sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    }, { delayForMessage: (type) => type === "INPUT" ? 100 : 0 });
    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message(JSON.stringify({
      v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 0, seq: 1,
      nextFrame: 0, occupiedSeatMask: 3,
    }));
    await vi.waitFor(() => expect(bridge.runNetplayFrame).toHaveBeenCalled());
    const resetsBeforeBlur = resetLocalControls.mock.calls.length;
    controller.handleFocusLoss();
    await vi.advanceTimersByTimeAsync(101);
    const types = socket.sent.filter((value): value is string => typeof value === "string")
      .map((value) => (JSON.parse(value) as { type: string }).type);
    expect(types).not.toContain("SUSPEND_REQUEST");
    expect(resetLocalControls).toHaveBeenCalledTimes(resetsBeforeBlur + 1);
    controller.end();
  });

  it("does not suspend a player that loses focus before the initial sync epoch starts", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    const resetLocalControls = vi.fn();
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls, close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame: vi.fn(),
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const onEnded = vi.fn();
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded,
    });

    await controller.start();
    const socket = FakeSocket.instances[0]!;
    controller.handleFocusLoss();

    const types = socket.sent
      .filter((value): value is string => typeof value === "string")
      .map((value) => (JSON.parse(value) as { type: string }).type);
    expect(types).toEqual(["HELLO", "RUNTIME_READY"]);
    expect(socket.readyState).toBe(FakeSocket.OPEN);
    expect(resetLocalControls).toHaveBeenCalledOnce();
    expect(onEnded).not.toHaveBeenCalled();
    controller.end();
  });

  it("waits for the active native frame boundary before loading rollback state", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    let frameRunning = false;
    let releaseFrame!: () => void;
    let frameCalls = 0;
    const runNetplayFrame = vi.fn(() => {
      frameCalls += 1;
      if (frameCalls !== 1) return Promise.resolve();
      frameRunning = true;
      return new Promise<void>((resolve) => {
        releaseFrame = () => { frameRunning = false; resolve(); };
      });
    });
    const loadStateAndWait = vi.fn(async () => {
      expect(frameRunning).toBe(false);
    });
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait, runNetplayFrame,
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    });
    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message(JSON.stringify({
      v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 0, seq: 1,
      nextFrame: 0, occupiedSeatMask: 3,
    }));
    await vi.waitFor(() => expect(runNetplayFrame).toHaveBeenCalledOnce());
    const players = Array.from({ length: 4 }, () => Array(24).fill(0));
    players[0]![0] = 1;
    socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: 2,
      frame: 0, occupiedSeatMask: 3, players,
    }));
    await Promise.resolve();
    expect(loadStateAndWait).not.toHaveBeenCalled();
    releaseFrame();
    await vi.waitFor(() => expect(loadStateAndWait).toHaveBeenCalledOnce());
    controller.end();
  });
});
