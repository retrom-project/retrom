import { describe, expect, it, vi } from "vitest";
import { NetplayCheckpointQueue } from "./checkpoint-queue";
import type { NetplayDiagnostics, NetplayProfile } from "./controller-model";
import type { RollbackTimeline } from "./rollback";
import {testNetplayPort} from "./netplay-port.test-helper";

const profile: NetplayProfile = {
  schemaVersion: 2, protocolVersion: "retrom-netplay-v2", profileId: "snes9x-v1",
  providerId: "emulatorjs", targetId: "snes9x", targetContractSha256: "1".repeat(64),
  netplayCompatibilityLine: "emulatorjs-netplay-v1", coreId: "snes9x", platformIds: ["snes"],
  gameVariantRevisionId: "revision", sourceManifestDigest: "2".repeat(64), dependencySnapshotDigest: "3".repeat(64),
  controlCount: 24, maxPlayers: 2, maxPredictionFrames: 0,
  maxRollbackFrames: 120, checkpointEveryFrames: 120, canonicalHistoryFrames: 600, maxStateBytes: 1_048_576,
};

function createQueue(send = vi.fn(), onCheckpoint = vi.fn(), digest?: (state: Uint8Array) => Promise<string>) {
  const bridge = testNetplayPort({captureState: vi.fn(() => Uint8Array.of(1, 2, 3))});
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
    await queue.queueLockstep(119, 7);
    await vi.waitFor(() => expect(send).toHaveBeenCalledOnce());
    expect(onCheckpoint).toHaveBeenCalledWith(expect.objectContaining({ epoch: 7, frame: 119 }));
  });

  it("drops an asynchronous digest completed after an epoch reset", async () => {
    const { queue, send, onCheckpoint } = createQueue();
    await queue.queueLockstep(119, 7);
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
    await queue.queueLockstep(119, 7);
    await vi.waitFor(() => expect(digest).toHaveBeenCalledOnce());
    queue.reset();
    await queue.queueLockstep(239, 8);
    rejectStale(new Error("stale digest failed"));
    await vi.waitFor(() => expect(send).toHaveBeenCalledWith(239, "a".repeat(64)));
    expect(onCheckpoint).toHaveBeenCalledWith(expect.objectContaining({ epoch: 8, frame: 239 }));
    expect(onFailure).not.toHaveBeenCalled();
  });
});
