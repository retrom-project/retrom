import { describe, expect, it, vi } from "vitest";
import { NetplayCheckpointQueue } from "./checkpoint-queue";
import type { NetplayDiagnostics, NetplayProfile } from "./controller-model";
import type { EJSNetplayFrameBridge } from "./ejs-netplay-4.2.3-v1";
import type { RollbackTimeline } from "./rollback";

const profile: NetplayProfile = {
  schemaVersion: 1, protocolVersion: "retrom-netplay-v2", profileId: "snes9x-423-v1",
  platformIds: ["snes"], emulatorjsVersion: "4.2.3", playerAdapterId: "ejs-4.2.3-v3",
  netplayAdapterId: "ejs-netplay-4.2.3-v2", coreArtifactId: "core", coreArtifactSha256: "1".repeat(64),
  gameVariantRevisionId: "revision", sourceManifestDigest: "2".repeat(64), dependencySnapshotDigest: "3".repeat(64),
  defaultCoreOptions: {}, controlCount: 24, maxPlayers: 2, maxPredictionFrames: 0,
  maxRollbackFrames: 120, checkpointEveryFrames: 120, canonicalHistoryFrames: 600, maxStateBytes: 1_048_576,
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

function createQueue(send = vi.fn(), onCheckpoint = vi.fn(), digest?: (state: Uint8Array) => Promise<string>) {
  const bridge = { captureState: vi.fn(() => raState([1, 2, 3])) } as unknown as EJSNetplayFrameBridge;
  const timeline = { stateAt: vi.fn() } as unknown as RollbackTimeline;
  const diagnostics = { onCheckpoint } satisfies NetplayDiagnostics;
  const onFailure = vi.fn();
  return {
    queue: new NetplayCheckpointQueue(profile, bridge, timeline, diagnostics, send, onFailure, digest),
    send, onCheckpoint, onFailure,
  };
}

describe("NetplayCheckpointQueue", () => {
  it("binds a checkpoint digest and its diagnostics to the capture epoch", async () => {
    const { queue, send, onCheckpoint } = createQueue();
    queue.queueLockstep(119, 7);
    await vi.waitFor(() => expect(send).toHaveBeenCalledOnce());
    expect(onCheckpoint).toHaveBeenCalledWith(expect.objectContaining({ epoch: 7, frame: 119 }));
  });

  it("drops an asynchronous digest completed after an epoch reset", async () => {
    const { queue, send, onCheckpoint } = createQueue();
    queue.queueLockstep(119, 7);
    queue.reset();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(send).not.toHaveBeenCalled();
    expect(onCheckpoint).not.toHaveBeenCalled();
  });

  it("ignores a stale digest failure and flushes the checkpoint queued by the new epoch", async () => {
    let rejectStale!: (reason: unknown) => void;
    const digest = vi.fn()
      .mockImplementationOnce(() => new Promise<string>((_resolve, reject) => {rejectStale = reject;}))
      .mockResolvedValueOnce("a".repeat(64));
    const { queue, send, onCheckpoint, onFailure } = createQueue(vi.fn(), vi.fn(), digest);
    queue.queueLockstep(119, 7);
    await vi.waitFor(() => expect(digest).toHaveBeenCalledOnce());
    queue.reset();
    queue.queueLockstep(239, 8);
    rejectStale(new Error("stale digest failed"));
    await vi.waitFor(() => expect(send).toHaveBeenCalledWith(239, "a".repeat(64)));
    expect(onCheckpoint).toHaveBeenCalledWith(expect.objectContaining({ epoch: 8, frame: 239 }));
    expect(onFailure).not.toHaveBeenCalled();
  });
});
