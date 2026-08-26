import { afterEach, describe, expect, it, vi } from "vitest";
import { RetromRpgRuntimeController } from "./controller";
import type { CheckpointAvailability, CheckpointPayload, RuntimeEvent } from "./contract";
import type { RpgRuntimeAdapter } from "./internal-adapter";

afterEach(() => vi.useRealTimers());

function adapterFixture(overrides: Partial<RpgRuntimeAdapter> = {}) {
  const availability: CheckpointAvailability = { available: true, reason: null };
  return {
    checkpoint: vi.fn(async (): Promise<CheckpointPayload> => ({
      bytes: Uint8Array.of(1, 2, 3), payloadKind: "RUNTIME_STATE",
    })),
    exit: vi.fn(async () => undefined),
    getCanvas: () => document.createElement("canvas"),
    getCheckpointAvailability: () => availability,
    getFrameCount: () => 300,
    getPayloadKind: () => "RUNTIME_STATE" as const,
    getPosition: () => ({ mapId: 1, playerX: 2, playerY: 3, fixtureState: 4 }),
    pause: vi.fn(async () => undefined),
    resume: vi.fn(async () => undefined),
    screenshot: vi.fn(async () => new Blob([Uint8Array.of(1)], { type: "image/png" })),
    setVolume: vi.fn(),
    ...overrides,
  } satisfies RpgRuntimeAdapter;
}

describe("RetromRpgRuntimeController", () => {
  it("enforces the lifecycle and normalizes checkpoint/screenshot access", async () => {
    const adapter = adapterFixture();
    const events: RuntimeEvent[] = [];
    const runtime = new RetromRpgRuntimeController(async () => adapter);
    runtime.subscribe((event) => events.push(event));

    await runtime.mount(document.createElement("div"));
    expect(runtime.getState()).toBe("RUNNING");
    expect(events.some((event) => event.type === "READY")).toBe(true);
    await expect(runtime.mount(document.createElement("div"))).rejects.toThrow("RPG_RUNTIME_INVALID_STATE");

    await runtime.pause();
    expect(runtime.getState()).toBe("PAUSED");
    await runtime.resume();
    expect(runtime.getState()).toBe("RUNNING");
    const checkpoint = await runtime.checkpoint();
    expect(checkpoint).toEqual({ bytes: Uint8Array.of(1, 2, 3), payloadKind: "RUNTIME_STATE" });
    expect(await runtime.screenshot()).toMatchObject({ size: 1, type: "image/png" });

    await Promise.all([runtime.exit(), runtime.exit()]);
    expect(runtime.getState()).toBe("EXITED");
    expect(adapter.exit).toHaveBeenCalledOnce();
  });

  it("rejects unavailable checkpoints without calling the adapter or failing the runtime", async () => {
    const checkpoint = vi.fn(async (): Promise<CheckpointPayload> => ({
      bytes: Uint8Array.of(1), payloadKind: "RUNTIME_STATE",
    }));
    const adapter = adapterFixture({
      checkpoint,
      getCheckpointAvailability: () => ({ available: false, reason: "EVENT_ACTIVE" }),
    });
    const runtime = new RetromRpgRuntimeController(async () => adapter);
    await runtime.mount(document.createElement("div"));

    await expect(runtime.checkpoint()).rejects.toThrow("RPG_CHECKPOINT_UNAVAILABLE");
    expect(checkpoint).not.toHaveBeenCalled();
    expect(runtime.getState()).toBe("RUNNING");
    await runtime.exit();
  });

  it("returns to the prior state after a checkpoint error", async () => {
    const adapter = adapterFixture({checkpoint: vi.fn(async () => {throw new Error("RPG_CHECKPOINT_CREATE_FAILED");})});
    const runtime = new RetromRpgRuntimeController(async () => adapter);
    await runtime.mount(document.createElement("div"));
    await runtime.pause();

    await expect(runtime.checkpoint()).rejects.toThrow("RPG_CHECKPOINT_CREATE_FAILED");
    expect(runtime.getState()).toBe("PAUSED");
    await runtime.exit();
  });

  it("waits for an asynchronous toolbar pause before creating a checkpoint", async () => {
    let finishPause: (() => void) | undefined;
    const pause = vi.fn(() => new Promise<void>((resolve) => {finishPause = resolve;}));
    const checkpoint = vi.fn(async (): Promise<CheckpointPayload> => ({
      bytes: Uint8Array.of(4, 5, 6), payloadKind: "RUNTIME_STATE",
    }));
    const adapter = adapterFixture({checkpoint, pause});
    const runtime = new RetromRpgRuntimeController(async () => adapter);
    await runtime.mount(document.createElement("div"));
    const manager = runtime.getPlayerInstance().gameManager!;

    void manager.toggleMainLoop!(false);
    const state = manager.getStateAsync!();
    await Promise.resolve();
    expect(checkpoint).not.toHaveBeenCalled();
    finishPause?.();

    await expect(state).resolves.toEqual(Uint8Array.of(4, 5, 6));
    expect(runtime.getState()).toBe("PAUSED");
    await runtime.exit();
  });

  it("fails closed and cleans the adapter when an active lifecycle operation fails", async () => {
    const adapter = adapterFixture({pause: vi.fn(async () => {throw new Error("third party failure");})});
    const events: RuntimeEvent[] = [];
    const runtime = new RetromRpgRuntimeController(async () => adapter);
    runtime.subscribe((event) => events.push(event));
    await runtime.mount(document.createElement("div"));

    await expect(runtime.pause()).rejects.toThrow("RPG_RUNTIME_FAILED");
    expect(runtime.getState()).toBe("FAILED");
    expect(adapter.exit).toHaveBeenCalledOnce();
    expect(events).toContainEqual({ type: "FATAL_ERROR", code: "RPG_RUNTIME_FAILED" });
    await runtime.exit();
    expect(adapter.exit).toHaveBeenCalledOnce();
  });

  it("cleans an adapter that completes mounting after cancellation", async () => {
    const controller = new AbortController();
    const adapter = adapterFixture();
    let finishMount: ((adapter: RpgRuntimeAdapter) => void) | undefined;
    const runtime = new RetromRpgRuntimeController(() => new Promise((resolve) => {finishMount = resolve;}), controller.signal);
    const mounting = runtime.mount(document.createElement("div"));
    controller.abort();
    finishMount?.(adapter);

    await expect(mounting).rejects.toMatchObject({ name: "AbortError" });
    await runtime.exit();
    expect(adapter.exit).toHaveBeenCalledOnce();
    expect(runtime.getState()).toBe("EXITED");
  });

  it("lets exit win a pending checkpoint without an illegal follow-up transition", async () => {
    let finishCheckpoint: ((payload: CheckpointPayload) => void) | undefined;
    const adapter = adapterFixture({
      checkpoint: () => new Promise((resolve) => {finishCheckpoint = resolve;}),
    });
    const runtime = new RetromRpgRuntimeController(async () => adapter);
    await runtime.mount(document.createElement("div"));
    const checkpoint = runtime.checkpoint();
    const exiting = runtime.exit();
    finishCheckpoint?.({bytes: Uint8Array.of(1), payloadKind: "RUNTIME_STATE"});

    await expect(checkpoint).rejects.toMatchObject({name: "AbortError"});
    await exiting;
    expect(runtime.getState()).toBe("EXITED");
    expect(adapter.exit).toHaveBeenCalledOnce();
  });

  it("publishes live availability changes and stops polling on exit", async () => {
    vi.useFakeTimers();
    let availability: CheckpointAvailability = {available: false, reason: "NOT_ON_MAP"};
    const getAvailability = vi.fn(() => availability);
    const adapter = adapterFixture({getCheckpointAvailability: getAvailability});
    const events: RuntimeEvent[] = [];
    const runtime = new RetromRpgRuntimeController(async () => adapter);
    runtime.subscribe((event) => events.push(event));
    await runtime.mount(document.createElement("div"));
    availability = {available: true, reason: null};

    await vi.advanceTimersByTimeAsync(250);
    expect(events).toContainEqual({
      type: "CHECKPOINT_AVAILABILITY_CHANGED", availability: {available: true, reason: null},
    });
    await runtime.exit();
    const callsAfterExit = getAvailability.mock.calls.length;
    await vi.advanceTimersByTimeAsync(1_000);
    expect(getAvailability).toHaveBeenCalledTimes(callsAfterExit);
  });
});
