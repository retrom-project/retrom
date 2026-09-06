import {describe, expect, it, vi} from "vitest";
import type {RuntimeCheckpointAvailabilityV1, RuntimeEventV1} from "./runtime/contract";
import type {RuntimeSavePayload} from "./runtime/runtime-actions";
import {GameSaveConflict} from "./game-save-upload-error";
import {GameSaveSync} from "./game-save-sync";

vi.mock("./manual-save-screenshot", () => ({
  prepareManualSaveScreenshot: async (capture: {screenshot: Blob}) => ({screenshot: new Blob([capture.screenshot], {type: "image/jpeg"}), format: "jpg"}),
}));

function fixture() {
  let availability: RuntimeCheckpointAvailabilityV1 = {available: false, reason: "NO_SAVE"};
  const listeners = new Set<(event: RuntimeEventV1) => void>();
  const runtime = {
    getCheckpointAvailability: () => availability,
    subscribe: (fn: (event: RuntimeEventV1) => void) => {listeners.add(fn); return () => {listeners.delete(fn);};},
    checkpoint: vi.fn(async () => ({bytes: Uint8Array.of(Number(availability.revision)), format: "native-v1", metadata: null})),
    screenshot: vi.fn(async () => new Blob(["image"], {type: "image/png"})),
    acknowledgeCheckpoint: vi.fn(async () => {availability = {available: false, reason: "UNCHANGED"};}),
  };
  const upload = vi.fn<(payload: RuntimeSavePayload) => Promise<boolean>>(async () => true), present = vi.fn();
  const store = {put: vi.fn<(payload: RuntimeSavePayload) => Promise<void>>(async () => undefined), remove: vi.fn(async () => undefined)};
  const sync = new GameSaveSync(runtime, upload, present, store);
  const emit = (next: RuntimeCheckpointAvailabilityV1) => {
    availability = next;
    for (const fn of listeners) {fn({type: "CHECKPOINT_AVAILABILITY_CHANGED", availability});}
  };
  sync.start();
  return {runtime, upload, present, sync, emit, store};
}
const changed = (revision: string): RuntimeCheckpointAvailabilityV1 => ({available: true, reason: null, revision});

describe("local native game save drafts", () => {
  it("captures changed data locally without uploading or changing the launch baseline", async () => {
    const f = fixture();
    f.emit({available: false, reason: "UNCHANGED"});
    expect(f.runtime.checkpoint).not.toHaveBeenCalled();
    f.emit(changed("1")); await f.sync.flush();
    expect(f.store.put).toHaveBeenCalledOnce();
    expect(f.store.put.mock.calls[0][0].screenshot.type).toBe("image/jpeg");
    expect(f.upload).not.toHaveBeenCalled();
    expect(f.runtime.acknowledgeCheckpoint).not.toHaveBeenCalled();
    expect(f.sync.hasChanges()).toBe(true);
    f.emit(changed("1")); await f.sync.flush();
    expect(f.store.put).toHaveBeenCalledOnce();
    await f.sync.stop();
  });

  it("uploads only on explicit save, then acknowledges and deletes the local copy", async () => {
    const f = fixture(); f.emit(changed("1")); await f.sync.flush();
    expect(await f.sync.save()).toBe(true);
    expect(f.upload).toHaveBeenCalledWith(f.store.put.mock.calls[0][0]);
    expect(f.runtime.acknowledgeCheckpoint).toHaveBeenCalledWith(f.store.put.mock.calls[0][0].checkpoint);
    expect(f.store.remove).toHaveBeenCalledOnce(); expect(f.sync.hasChanges()).toBe(false);
    await f.sync.stop();
  });

  it.each(["UNCHANGED", "NO_SAVE"] as const)("drops the draft when final data returns to the launch baseline (%s)", async (reason) => {
    const f = fixture(); f.emit(changed("1")); await f.sync.flush();
    f.emit({available: false, reason}); await f.sync.flush();
    expect(f.sync.hasChanges()).toBe(false); expect(f.store.remove).toHaveBeenCalledOnce();
    expect(await f.sync.save()).toBe(true); expect(f.upload).not.toHaveBeenCalled();
    await f.sync.stop();
  });

  it("keeps the exact request after upload failure and retries only on user action", async () => {
    const f = fixture(); f.upload.mockResolvedValueOnce(false);
    f.emit(changed("1")); await f.sync.flush();
    expect(await f.sync.save()).toBe(false);
    expect(f.store.remove).not.toHaveBeenCalled(); expect(f.runtime.acknowledgeCheckpoint).not.toHaveBeenCalled();
    f.emit(changed("1")); expect(f.upload).toHaveBeenCalledOnce();
    expect(await f.sync.save()).toBe(true);
    expect(f.upload.mock.calls[1][0]).toBe(f.upload.mock.calls[0][0]);
    await f.sync.stop();
  });

  it("captures the same changed content again after it first reverted to the launch baseline", async () => {
    const f = fixture(); f.emit(changed("1")); await f.sync.flush();
    f.emit({available: false, reason: "UNCHANGED"}); await f.sync.flush();
    f.emit(changed("1")); await f.sync.flush();
    expect(f.store.put).toHaveBeenCalledTimes(2);
    expect(await f.sync.save()).toBe(true);
    expect(f.upload).toHaveBeenCalledWith(f.store.put.mock.calls[1][0]);
    await f.sync.stop();
  });

  it("retries local write failures without uploading and retains the latest changed revision", async () => {
    const f = fixture(); f.store.put.mockRejectedValueOnce(Error("quota"));
    f.emit(changed("1")); await expect(f.sync.flush()).rejects.toThrow("LOCAL_DRAFT_STORAGE_FAILED");
    f.emit(changed("2")); expect(await f.sync.retry()).toBe(true);
    expect([...f.store.put.mock.calls.at(-1)![0].checkpoint.bytes]).toEqual([2]);
    expect(f.upload).not.toHaveBeenCalled(); await f.sync.stop();
  });

  it("serializes local captures and preserves a later revision while an earlier write is pending", async () => {
    const f = fixture(); let finish!: () => void;
    f.store.put.mockImplementationOnce(() => new Promise<void>((resolve) => {finish = resolve;}));
    f.emit(changed("1")); await vi.waitFor(() => expect(f.store.put).toHaveBeenCalledOnce());
    f.emit(changed("2")); finish(); await f.sync.flush();
    expect(f.store.put.mock.calls.map(([payload]) => [...payload.checkpoint.bytes])).toEqual([[1], [2]]);
    expect(f.upload).not.toHaveBeenCalled(); await f.sync.stop();
  });

  it("does not submit a stale captured revision when data reverts during its local write", async () => {
    const f = fixture(); let finish!: () => void;
    f.store.put.mockImplementationOnce(() => new Promise<void>((resolve) => {finish = resolve;}));
    f.emit(changed("1")); await vi.waitFor(() => expect(f.store.put).toHaveBeenCalledOnce());
    const saving = f.sync.save(); f.emit({available: false, reason: "UNCHANGED"}); finish();
    expect(await saving).toBe(true); expect(f.upload).not.toHaveBeenCalled();
    expect(f.sync.hasChanges()).toBe(false); await f.sync.stop();
  });

  it("discard waits for pending local persistence, deletes the draft, and never uploads", async () => {
    const f = fixture(); let finish!: () => void;
    f.store.put.mockImplementationOnce(() => new Promise<void>((resolve) => {finish = resolve;}));
    f.emit(changed("1")); await vi.waitFor(() => expect(f.store.put).toHaveBeenCalledOnce());
    const discarded = f.sync.discard(); expect(f.store.remove).not.toHaveBeenCalled(); finish(); await discarded;
    expect(f.store.remove).toHaveBeenCalledOnce(); expect(f.upload).not.toHaveBeenCalled();
    f.emit(changed("2")); expect(f.store.put).toHaveBeenCalledOnce();
  });

  it("abnormal teardown keeps the draft and a fresh runtime never reads it", async () => {
    const old = fixture(); old.emit(changed("1")); await old.sync.flush(); await old.sync.stop();
    expect(old.store.remove).not.toHaveBeenCalled(); expect(old.upload).not.toHaveBeenCalled();
    const fresh = fixture(); await fresh.sync.flush();
    expect(fresh.sync.hasChanges()).toBe(false); expect(fresh.runtime.checkpoint).not.toHaveBeenCalled();
    expect(fresh.upload).not.toHaveBeenCalled(); await fresh.sync.stop();
  });

  it("keeps capturing if local deletion fails and the user continues playing", async () => {
    const f = fixture(); f.emit(changed("1")); await f.sync.flush();
    f.store.remove.mockRejectedValueOnce(Error("storage unavailable"));
    await expect(f.sync.discard()).rejects.toThrow("storage unavailable");
    f.emit(changed("2")); await f.sync.flush();
    expect(f.store.put).toHaveBeenCalledTimes(2);
    expect(await f.sync.save()).toBe(true);
    expect(f.upload).toHaveBeenCalledWith(f.store.put.mock.calls[1][0]);
    await f.sync.stop();
  });

  it("keeps conflicting drafts without acknowledging or silently creating another save", async () => {
    const f = fixture(); f.upload.mockRejectedValue(new GameSaveConflict());
    f.emit(changed("1")); await f.sync.flush(); expect(await f.sync.save()).toBe(false);
    expect(await f.sync.save()).toBe(false); expect(f.upload).toHaveBeenCalledOnce();
    expect(f.runtime.acknowledgeCheckpoint).not.toHaveBeenCalled(); expect(f.store.remove).not.toHaveBeenCalled();
    await f.sync.stop();
  });

  it("retries a failed acknowledgment without uploading an already committed payload again", async () => {
    const f = fixture(); f.runtime.acknowledgeCheckpoint.mockRejectedValueOnce(Error("busy"));
    f.emit(changed("1")); await f.sync.flush(); expect(await f.sync.save()).toBe(false);
    expect(await f.sync.save()).toBe(true); expect(f.upload).toHaveBeenCalledOnce();
    expect(f.runtime.acknowledgeCheckpoint).toHaveBeenCalledTimes(2); await f.sync.stop();
  });
});
