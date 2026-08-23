import { afterEach, describe, expect, it, vi } from "vitest";
import { NetplayController, type NetplayLaunchConfig } from "./controller";
import { coreStateBytes, type EJSNetplayFrameBridge } from "./ejs-netplay-4.2.3-v1";

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

class HangingSocket extends EventTarget {
  static readonly OPEN = 1;
  static instances: HangingSocket[] = [];
  readyState = 0;
  binaryType = "";
  closes: Array<[number | undefined, string | undefined]> = [];
  constructor(public readonly url: URL, public readonly protocol: string) { super(); HangingSocket.instances.push(this); }
  send() { throw new Error("SOCKET_NOT_OPEN"); }
  close(code?: number, reason?: string) {
    if (this.readyState === 3) {return;}
    this.closes.push([code, reason]); this.readyState = 3;
    this.dispatchEvent(new CloseEvent("close", { code, reason }));
  }
}

const launch: NetplayLaunchConfig = {
  roomId: "01980000-0000-7000-8000-000000000001",
  sessionId: "01980000-0000-7000-8000-000000000002",
  playerNo: 2,
  runtimeSocketUrl: "/runtime/netplay/rooms/01980000-0000-7000-8000-000000000001/socket",
  netplayProfile: {
    schemaVersion: 1, protocolVersion: "retrom-netplay-v2", profileId: "fceumm-423-v1", platformIds: ["nes"],
    emulatorjsVersion: "4.2.3",
    playerAdapterId: "ejs-4.2.3-v3", netplayAdapterId: "ejs-netplay-4.2.3-v2",
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
  vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); FakeSocket.instances = []; HangingSocket.instances = [];
});

describe("NetplayController reconnect lease", () => {
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
    await vi.advanceTimersByTimeAsync(0);
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
    await vi.advanceTimersByTimeAsync(0);

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

  it("uses WELCOME leaseMs without letting a replacement WELCOME or stale socket extend it", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", FakeSocket);
    const onEnded = vi.fn();
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame: vi.fn(),
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded,
    });
    await controller.start();
    const first = FakeSocket.instances[0]!;
    first.message(JSON.stringify({
      v: 1, type: "WELCOME", sessionId: launch.sessionId, epoch: 0, seq: 1,
      roomVersion: 1, sessionVersion: 1, leaseMs: 1_000, historyStartFrame: -1,
      historyEndFrame: -1, occupiedSeatMask: 3, playerNo: 2,
    }));
    await Promise.resolve(); await Promise.resolve();
    first.remoteClose();
    await vi.advanceTimersByTimeAsync(0);
    const second = FakeSocket.instances[1]!;
    first.message(JSON.stringify({
      v: 1, type: "SESSION_ENDED", sessionId: launch.sessionId, epoch: 0, seq: 99,
      reason: "PROTOCOL_VIOLATION", roomDisposition: "WAITING",
    }));
    second.message(JSON.stringify({
      v: 1, type: "WELCOME", sessionId: launch.sessionId, epoch: 0, seq: 2,
      roomVersion: 1, sessionVersion: 1, leaseMs: 60_000, historyStartFrame: -1,
      historyEndFrame: -1, occupiedSeatMask: 3, playerNo: 2,
    }));
    await vi.advanceTimersByTimeAsync(999);
    expect(onEnded).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(onEnded).toHaveBeenCalledOnce();
    expect(onEnded).toHaveBeenCalledWith("PEER_TIMEOUT");
  });

  it.each([999, 60_001, 1_000.5])("rejects unsafe WELCOME leaseMs %s", async (leaseMs) => {
    vi.stubGlobal("WebSocket", FakeSocket);
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame: vi.fn(),
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    });
    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message(JSON.stringify({
      v: 1, type: "WELCOME", sessionId: launch.sessionId, epoch: 0, seq: 1,
      roomVersion: 1, sessionVersion: 1, leaseMs, historyStartFrame: -1,
      historyEndFrame: -1, occupiedSeatMask: 3, playerNo: 2,
    }));
    await vi.waitFor(() => {
      const endRequests = socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "END_REQUEST");
      expect(endRequests).toHaveLength(1);
      expect(JSON.parse(endRequests[0] as string)).toMatchObject({ reason: "PROTOCOL_VIOLATION" });
    });
    controller.dispose();
  });

  it("applies the 5s initial and 2s reconnect open limits with bounded retry backoff", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", HangingSocket);
    const onEnded = vi.fn();
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame: vi.fn(),
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const initialController = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded,
    });
    const started = initialController.start();
    await vi.advanceTimersByTimeAsync(4_999);
    expect(HangingSocket.instances).toHaveLength(1);
    expect(HangingSocket.instances[0]!.closes).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    await started;
    expect(HangingSocket.instances[0]!.closes).toContainEqual([4000, "open timeout"]);
    initialController.dispose();

    HangingSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeSocket);
    const reconnectController = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded,
    });
    await reconnectController.start();
    const initialSocket = FakeSocket.instances[0]!;
    vi.stubGlobal("WebSocket", HangingSocket);
    initialSocket.remoteClose();
    await vi.advanceTimersByTimeAsync(0);
    expect(HangingSocket.instances).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1_999);
    expect(HangingSocket.instances[0]!.closes).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect(HangingSocket.instances[0]!.closes).toContainEqual([4000, "open timeout"]);
    await vi.advanceTimersByTimeAsync(249);
    expect(HangingSocket.instances).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(HangingSocket.instances).toHaveLength(2);
    expect(onEnded).not.toHaveBeenCalled();
    reconnectController.dispose();
  });

  it("clears the reconnect deadline only when the replacement START_EPOCH arrives", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", FakeSocket);
    const onEnded = vi.fn();
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame: vi.fn().mockResolvedValue(undefined),
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded,
    });
    await controller.start();
    const first = FakeSocket.instances[0]!;
    first.message(JSON.stringify({
      v: 1, type: "WELCOME", sessionId: launch.sessionId, epoch: 0, seq: 1,
      roomVersion: 1, sessionVersion: 1, leaseMs: 1_000, historyStartFrame: -1,
      historyEndFrame: -1, occupiedSeatMask: 3, playerNo: 2,
    }));
    await Promise.resolve(); await Promise.resolve();
    first.remoteClose();
    await vi.advanceTimersByTimeAsync(0);
    const second = FakeSocket.instances[1]!;
    second.message(JSON.stringify({
      v: 1, type: "WELCOME", sessionId: launch.sessionId, epoch: 0, seq: 2,
      roomVersion: 1, sessionVersion: 1, leaseMs: 1_000, historyStartFrame: -1,
      historyEndFrame: -1, occupiedSeatMask: 3, playerNo: 2,
    }));
    await vi.advanceTimersByTimeAsync(999);
    second.message(JSON.stringify({
      v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 1, seq: 3,
      nextFrame: 0, occupiedSeatMask: 3,
    }));
    await vi.advanceTimersByTimeAsync(2_000);
    expect(onEnded).not.toHaveBeenCalled();
    controller.dispose();
  });
});

describe("NetplayController lockstep", () => {
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

    for (let frame = 1; frame < 120; frame += 1) {socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: frame + 2,
      frame, occupiedSeatMask: 3, players,
    }));}
    await vi.waitFor(() => expect(runNetplayFrame).toHaveBeenCalledTimes(120));
    expect(bridge.captureState).toHaveBeenCalledOnce();
    expect(socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "HASH")
      .map((value) => JSON.parse(value as string))).toMatchObject([{ frame: 119 }]);

    const inputs = socket.sent.filter((value): value is string => typeof value === "string")
      .map((value) => JSON.parse(value) as { type: string; frame?: number })
      .filter((message) => message.type === "INPUT");
    expect(inputs.length).toBeGreaterThanOrEqual(121);
    expect(inputs.length).toBeLessThanOrEqual(128);
    expect(inputs.at(0)).toMatchObject({ frame: 0, playerNo: 2 });
    expect(inputs.at(-1)?.frame).toBeLessThanOrEqual(127);
    expect(onRollback).not.toHaveBeenCalled();
    controller.end();
  });

  it("expands the strict input buffer only when measured round-trip latency needs it", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    let currentTimeMS = 1_000;
    vi.spyOn(performance, "now").mockImplementation(() => currentTimeMS);
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

  it("shrinks an expanded lockstep buffer only after 120 consecutive lower RTT samples", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    let currentTimeMS = 1_000;
    vi.spyOn(performance, "now").mockImplementation(() => currentTimeMS);
    const buffers: number[] = [];
    const runNetplayFrame = vi.fn().mockResolvedValue(undefined);
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame,
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(fbneoLaunch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    }, { onLockstep: ({ inputBufferFrames }) => buffers.push(inputBufferFrames) });
    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message(JSON.stringify({
      v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 0, seq: 1,
      nextFrame: 0, occupiedSeatMask: 3,
    }));
    await vi.waitFor(() => expect(socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "INPUT")).toHaveLength(1));
    const players = Array.from({ length: 4 }, () => Array(24).fill(0));
    currentTimeMS += 100;
    socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: 2,
      frame: 0, occupiedSeatMask: 3, players,
    }));
    await vi.waitFor(() => expect(buffers.at(-1)).toBe(7));

    for (let frame = 1; frame <= 130; frame += 1) {
      currentTimeMS += 1;
      socket.message(JSON.stringify({
        v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: frame + 2,
        frame, occupiedSeatMask: 3, players,
      }));
    }
    await vi.waitFor(() => expect(runNetplayFrame).toHaveBeenCalledTimes(131));
    expect(buffers.slice(0, -1)).toContain(7);
    expect(buffers.at(-1)).toBe(6);
    controller.end();
  });

  it("timestamps canonical arrival before queued core work and pre-submits the next lockstep input", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    let currentTimeMS = 1_000;
    vi.spyOn(performance, "now").mockImplementation(() => currentTimeMS);
    let releaseFirstFrame!: () => void;
    const runNetplayFrame = vi.fn()
      .mockImplementationOnce(() => new Promise<void>((resolve) => { releaseFirstFrame = resolve; }))
      .mockResolvedValue(undefined);
    const lockstep: Array<{ frame: number; inputBufferFrames: number; roundTripMS: number | null }> = [];
    const onRollback = vi.fn();
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame,
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(fbneoLaunch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    }, { onLockstep: (sample) => lockstep.push(sample), onRollback });
    await controller.start();
    const socket = FakeSocket.instances[0]!;
    const players = Array.from({ length: 4 }, () => Array(24).fill(0));
    socket.message(JSON.stringify({
      v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 0, seq: 1,
      nextFrame: 0, occupiedSeatMask: 3,
    }));
    await vi.waitFor(() => expect(socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "INPUT")).toHaveLength(1));
    currentTimeMS = 1_100;
    socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: 2,
      frame: 0, occupiedSeatMask: 3, players,
    }));
    await vi.waitFor(() => expect(runNetplayFrame).toHaveBeenCalledOnce());
    expect(socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "INPUT")
      .map((value) => JSON.parse(value as string).frame)).toContain(1);

    currentTimeMS = 1_110;
    socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: 3,
      frame: 1, occupiedSeatMask: 3, players,
    }));
    currentTimeMS = 1_310;
    releaseFirstFrame();
    await vi.waitFor(() => expect(runNetplayFrame).toHaveBeenCalledTimes(2));
    expect(lockstep.at(-1)).toMatchObject({ frame: 1, roundTripMS: 77.5 });
    expect(onRollback).not.toHaveBeenCalled();
    controller.end();
  });

  it("deduplicates stable status and debounces peer-input waiting for exactly 100ms", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", FakeSocket);
    const onStatus = vi.fn();
    const runNetplayFrame = vi.fn().mockResolvedValue(undefined);
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([0])), loadStateAndWait: vi.fn(), runNetplayFrame,
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus, onRunning: vi.fn(), onPaused: vi.fn(), onEnded: vi.fn(),
    });
    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message(JSON.stringify({
      v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 0, seq: 1,
      nextFrame: 0, occupiedSeatMask: 3,
    }));
    for (let index = 0; index < 20; index += 1) {await Promise.resolve();}
    expect(runNetplayFrame).toHaveBeenCalledTimes(8);
    await vi.advanceTimersByTimeAsync(99);
    expect(onStatus.mock.calls.filter(([text]) => text === "等待其他玩家输入…")).toHaveLength(0);
    const players = Array.from({ length: 4 }, () => Array(24).fill(0));
    socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: 2,
      frame: 0, occupiedSeatMask: 3, players,
    }));
    for (let index = 0; index < 10; index += 1) {await Promise.resolve();}
    expect(runNetplayFrame).toHaveBeenCalledTimes(9);
    expect(onStatus.mock.calls.filter(([text]) => text === "网络稳定")).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(99);
    expect(onStatus.mock.calls.filter(([text]) => text === "等待其他玩家输入…")).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect(onStatus.mock.calls.filter(([text]) => text === "等待其他玩家输入…")).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(500);
    expect(onStatus.mock.calls.filter(([text]) => text === "等待其他玩家输入…")).toHaveLength(1);
    controller.end();
  });
});

describe("NetplayController rollback", () => {
  it("sends a deferred checkpoint after its post-frame state enters the rollback ring", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    let stateByte = 0;
    let releaseFirstFrame!: () => void;
    let calls = 0;
    const runNetplayFrame = vi.fn(() => {
      calls += 1;
      if (calls === 1) {return new Promise<void>((resolve) => {
        releaseFirstFrame = () => { stateByte += 1; resolve(); };
      });}
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
    for (let frame = 0; frame < 120; frame += 1) {socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: frame + 2,
      frame, occupiedSeatMask: 3, players,
    }));}
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

  it("reports a normalized protocol failure and finalizes only after the server terminal message", async () => {
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
    await vi.waitFor(() => {
      const endRequests = socket.sent.filter((value) => typeof value === "string" && JSON.parse(value).type === "END_REQUEST");
      expect(endRequests).toHaveLength(1);
      expect(JSON.parse(endRequests[0] as string)).toMatchObject({ reason: "PROTOCOL_VIOLATION" });
    });
    expect(onEnded).not.toHaveBeenCalled();
    socket.message(JSON.stringify({
      v: 1, type: "SESSION_ENDED", sessionId: launch.sessionId, epoch: 0, seq: 1,
      reason: "PROTOCOL_VIOLATION", roomDisposition: "WAITING",
    }));
    await vi.waitFor(() => expect(onEnded).toHaveBeenCalledOnce());
    expect(onEnded).toHaveBeenCalledWith("PROTOCOL_VIOLATION");
    socket.message(JSON.stringify({
      v: 1, type: "SESSION_ENDED", sessionId: launch.sessionId, epoch: 0, seq: 2,
      reason: "PROTOCOL_VIOLATION", roomDisposition: "WAITING",
    }));
    expect(onEnded).toHaveBeenCalledOnce();
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
      if (frameCalls !== 1) {return Promise.resolve();}
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

  it("replays the canonical pause boundary so speculative canvases visibly converge", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    let currentState = 0;
    const loadStateAndWait = vi.fn(async (state: Uint8Array) => { currentState = coreStateBytes(state)[0]!; });
    const runNetplayFrame = vi.fn(async () => { currentState += 1; });
    const onPaused = vi.fn();
    const bridge = {
      pauseAtBoundary: vi.fn().mockResolvedValue(undefined), resetLocalControls: vi.fn(), close: vi.fn(),
      captureState: vi.fn(() => raState([currentState])), loadStateAndWait, runNetplayFrame,
      sampleLocalControls: vi.fn(() => Array(24).fill(0)),
    } as unknown as EJSNetplayFrameBridge;
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
      onStatus: vi.fn(), onRunning: vi.fn(), onPaused, onEnded: vi.fn(),
    });

    await controller.start();
    const socket = FakeSocket.instances[0]!;
    socket.message(JSON.stringify({
      v: 1, type: "START_EPOCH", sessionId: launch.sessionId, epoch: 0, seq: 1,
      nextFrame: 0, occupiedSeatMask: 3,
    }));
    await vi.waitFor(() => expect(runNetplayFrame).toHaveBeenCalledTimes(8));
    const players = Array.from({ length: 4 }, () => Array(24).fill(0));
    players[0]![0] = 7;
    socket.message(JSON.stringify({
      v: 1, type: "CANONICAL", sessionId: launch.sessionId, epoch: 0, seq: 2,
      frame: 0, occupiedSeatMask: 3, players,
    }));
    socket.message(JSON.stringify({
      v: 1, type: "PAUSE", sessionId: launch.sessionId, epoch: 0, seq: 3,
      reason: "HOST_PAUSE", atFrame: 0,
    }));

    await vi.waitFor(() => expect(onPaused).toHaveBeenCalledOnce());
    expect(loadStateAndWait).toHaveBeenLastCalledWith(raState([0]));
    expect(runNetplayFrame).toHaveBeenLastCalledWith(players);
    expect(socket.sent.filter((value): value is string => typeof value === "string")
      .map((value) => (JSON.parse(value) as { type: string }).type)).toContain("PAUSED");
    controller.end();
  });
});
