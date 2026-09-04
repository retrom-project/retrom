import {afterEach, describe, expect, it, vi} from "vitest";
import {NetplayController, type NetplayLaunchConfig} from "./controller";
import {testNetplayPort} from "./netplay-port.test-helper";
import {decodeStateFrame} from "./protocol";

class FakeSocket extends EventTarget {
  static readonly OPEN = 1;
  static instances: FakeSocket[] = [];
  readyState = FakeSocket.OPEN;
  binaryType = "";
  sent: unknown[] = [];
  constructor(public readonly url: URL, public readonly protocol: string) {
    super(); FakeSocket.instances.push(this); queueMicrotask(() => this.dispatchEvent(new Event("open")));
  }
  send(value: unknown) {this.sent.push(value);}
  close() {this.readyState = 3;}
  message(value: unknown) {this.dispatchEvent(new MessageEvent("message", {data: value}));}
}

const launch: NetplayLaunchConfig = {
  roomId: "01980000-0000-7000-8000-000000000001",
  sessionId: "01980000-0000-7000-8000-000000000002", playerNo: 1,
  runtimeSocketUrl: "/runtime/netplay/rooms/01980000-0000-7000-8000-000000000001/socket",
  netplayProfile: {
    schemaVersion: 2, protocolVersion: "retrom-netplay-v2", profileId: "fceumm-v1",
    providerId: "emulatorjs", targetId: "fceumm", bundleSha256: "1".repeat(64),
 coreId: "fceumm", platformIds: ["nes"],
    sourceManifestDigest: "2".repeat(64), dependencySnapshotDigest: "3".repeat(64),
    controlCount: 24, maxPlayers: 2, maxPredictionFrames: 8, maxRollbackFrames: 120,
    checkpointEveryFrames: 120, canonicalHistoryFrames: 600, maxStateBytes: 1_048_576,
  },
};

afterEach(() => {vi.restoreAllMocks(); vi.unstubAllGlobals(); FakeSocket.instances = [];});

describe("NetplayController authority state", () => {
  it("normalizes opaque Provider state to a fixed point before transfer", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    let current = Uint8Array.of(1);
    const load = vi.fn(async (state: Uint8Array) => {current = state[0] === 1 ? Uint8Array.of(2) : new Uint8Array(state);});
    const bridge = testNetplayPort({captureState: vi.fn(() => current), loadStateAndWait: load});
    const controller = new NetplayController(launch, "0".repeat(64), bridge, {
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
    expect(load).toHaveBeenCalledTimes(2);
    const frame = socket.sent.find((value): value is Uint8Array => value instanceof Uint8Array)!;
    expect([...decodeStateFrame(frame).state]).toEqual([2]);
    const ready = socket.sent.filter((value): value is string => typeof value === "string")
      .map((value) => JSON.parse(value) as {type: string; recaptureMatched?: boolean})
      .find((message) => message.type === "STATE_READY");
    expect(ready?.recaptureMatched).toBe(true);
    controller.end();
  });
});
