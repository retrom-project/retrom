import {describe, expect, it, vi} from "vitest";
import type {RuntimeCheckpointAvailabilityV1, RuntimeEventV1} from "./runtime/contract";
import type {RuntimeSavePayload} from "./runtime/runtime-actions";
import {GameSaveConflict} from "./game-save-upload-error";
import {GameSaveSync} from "./game-save-sync";

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
  const sync = new GameSaveSync(runtime, upload, present);
  const emit = (next: RuntimeCheckpointAvailabilityV1) => {
    availability = next;
    for (const fn of listeners) {fn({type: "CHECKPOINT_AVAILABILITY_CHANGED", availability});}
  };
  sync.start();
  return {runtime, upload, present, sync, emit};
}
const changed = (revision: string): RuntimeCheckpointAvailabilityV1 => ({available: true, reason: null, revision});

describe("native game save synchronization", () => {
  it("disables empty/restored content and automatically uploads each changed revision once", async () => {
    const f = fixture();
    expect(f.present).toHaveBeenLastCalledWith(expect.objectContaining({available: false, text: "尚无游戏数据变化"}));
    f.emit({available: false, reason: "UNCHANGED"});
    expect(f.runtime.checkpoint).not.toHaveBeenCalled();
    f.emit(changed("1"));
    await vi.waitFor(() => expect(f.runtime.acknowledgeCheckpoint).toHaveBeenCalledOnce());
    expect(f.upload).toHaveBeenCalledWith(expect.objectContaining({source: "GAME_SAVE"}));
    expect(f.runtime.acknowledgeCheckpoint).toHaveBeenCalledWith(f.upload.mock.calls[0][0].checkpoint);
    expect(f.present).toHaveBeenLastCalledWith(expect.objectContaining({available: false, text: "游戏数据已同步"}));
    f.emit({available: false, reason: "UNCHANGED"});
    expect(f.upload).toHaveBeenCalledOnce();
    await f.sync.stop();
  });

  it("does not acknowledge failed uploads or loop on failure, and permits manual retry", async () => {
    const f = fixture(); f.upload.mockResolvedValueOnce(false);
    f.emit(changed("1"));
    await vi.waitFor(() => expect(f.present).toHaveBeenLastCalledWith(expect.objectContaining({available: false, retryAvailable: true, text: "游戏数据同步失败，请重试同步"})));
    f.emit(changed("1")); f.emit(changed("1"));
    expect(f.upload).toHaveBeenCalledOnce();
    expect(f.runtime.acknowledgeCheckpoint).not.toHaveBeenCalled();
    expect(await f.sync.save()).toBe(true);
    expect(f.upload).toHaveBeenCalledTimes(2);
    expect(f.runtime.acknowledgeCheckpoint).toHaveBeenCalledOnce();
    await f.sync.stop();
  });

  it("serializes captures, keeps changes during upload, and drains before exit", async () => {
    const f = fixture(); let finish!: (value: boolean) => void;
    f.upload.mockImplementationOnce(() => new Promise<boolean>((resolve) => {finish = resolve;}));
    f.runtime.acknowledgeCheckpoint.mockImplementation(async () => undefined);
    f.emit(changed("1"));
    await vi.waitFor(() => expect(f.upload).toHaveBeenCalledOnce());
    expect(await f.sync.save()).toBe(false);
    f.emit(changed("2"));
    finish(true);
    await vi.waitFor(() => expect(f.upload).toHaveBeenCalledTimes(2));
    expect(f.upload.mock.calls.map(([payload]) => [...payload.checkpoint.bytes])).toEqual([[1], [2]]);
    await f.sync.stop();
    f.emit(changed("3"));
    expect(f.upload).toHaveBeenCalledTimes(2);
  });

  it("waits for an in-flight upload on stop, without attempting a newer revision after teardown", async () => {
    const f = fixture(); let finish!: (value: boolean) => void;
    f.upload.mockImplementationOnce(() => new Promise<boolean>((resolve) => {finish = resolve;}));
    f.emit(changed("1"));
    await vi.waitFor(() => expect(f.upload).toHaveBeenCalledOnce());
    let stopped = false;
    const stopping = f.sync.stop().then(() => {stopped = true;});
    await Promise.resolve(); expect(stopped).toBe(false);
    f.emit(changed("2")); finish(true);
    await stopping;
    expect(stopped).toBe(true); expect(f.upload).toHaveBeenCalledOnce();
  });

  it("retries a failed acknowledgment without uploading an already persisted payload twice", async () => {
    const f = fixture();
    f.runtime.acknowledgeCheckpoint.mockRejectedValueOnce(new Error("temporarily busy"));
    f.emit(changed("1"));
    await vi.waitFor(() => expect(f.present).toHaveBeenLastCalledWith(expect.objectContaining({available: false, retryAvailable: true})));
    expect(await f.sync.save()).toBe(true);
    expect(f.upload).toHaveBeenCalledOnce();
    expect(f.runtime.acknowledgeCheckpoint).toHaveBeenCalledTimes(2);
    await f.sync.stop();
  });
});

it("keeps manual creation disabled after failure and retries the exact captured request", async () => {
  const f = fixture(); f.upload.mockResolvedValueOnce(false);
  f.emit(changed("1"));
  await vi.waitFor(() => expect(f.upload).toHaveBeenCalledOnce());
  await vi.waitFor(() => expect(f.present).toHaveBeenLastCalledWith(expect.objectContaining({available: false, retryAvailable: true})));
  f.emit(changed("2"));
  expect(f.upload).toHaveBeenCalledOnce();
  expect(await f.sync.save()).toBe(true);
  expect(f.upload.mock.calls[1][0]).toBe(f.upload.mock.calls[0][0]);
  await f.sync.stop();
});

it("flush waits for a stable native write before allowing exit", async () => {
  const f = fixture();
  f.emit({available: false, reason: "BUSY"});
  let done = false;
  const flushed = f.sync.flush().then(() => {done = true;});
  await Promise.resolve(); expect(done).toBe(false);
  f.emit(changed("1"));
  await flushed;
  expect(f.upload).toHaveBeenCalledOnce();
  await f.sync.stop();
});

it("flush rejects an unsynchronized failure and keeps the retry request", async () => {
  const f = fixture(); f.upload.mockResolvedValue(false);
  f.emit(changed("1"));
  await vi.waitFor(() => expect(f.present).toHaveBeenLastCalledWith(expect.objectContaining({retryAvailable: true})));
  await expect(f.sync.flush()).rejects.toThrow("GAME_DATA_SYNC_FAILED");
  f.upload.mockResolvedValue(true);
  expect(await f.sync.save()).toBe(true);
  await expect(f.sync.flush()).resolves.toBeUndefined();
  await f.sync.stop();
});

it("stops a stale writer and keeps manual creation and retry disabled", async () => {
  const f = fixture(); f.upload.mockRejectedValue(new GameSaveConflict());
  f.emit(changed("1"));
  await vi.waitFor(() => expect(f.present).toHaveBeenLastCalledWith(expect.objectContaining({available: false, retryAvailable: false, tone: "warning", text: expect.stringContaining("其他会话")})));
  f.emit(changed("2"));
  expect(await f.sync.save()).toBe(false);
  await expect(f.sync.flush()).resolves.toBeUndefined();
  expect(f.upload).toHaveBeenCalledOnce();
  expect(f.runtime.acknowledgeCheckpoint).not.toHaveBeenCalled();
  await f.sync.stop();
});
