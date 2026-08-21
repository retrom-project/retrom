import { describe, expect, it, vi } from "vitest";
import { PersistentStateSync } from "./persistent-state-sync";

describe("PersistentStateSync", () => {
  it("pauses, copies, and uploads a changed automatic state", async () => {
    const loop = vi.fn();
    const state = Uint8Array.of(1, 2, 3);
    const upload = vi.fn<(bytes: Uint8Array, event: "AUTO_INTERVAL" | "EXIT") => Promise<boolean>>(async () => true);
    const sync = new PersistentStateSync({
      getState: () => state,
      toggleMainLoop: loop,
    }, null, upload, { isPaused: () => false });

    expect(await sync.poll()).toBe(true);
    state[0] = 9;
    expect(upload.mock.calls[0]?.[0]).toEqual(Uint8Array.of(1, 2, 3));
    expect(loop.mock.calls).toEqual([[false], [true]]);
  });

  it("does not upload an unchanged restored state", async () => {
    const state = Uint8Array.of(4, 5, 6);
    const upload = vi.fn<(bytes: Uint8Array, event: "AUTO_INTERVAL" | "EXIT") => Promise<boolean>>(async () => true);
    const sync = new PersistentStateSync({
      getState: () => state,
      toggleMainLoop: vi.fn(),
    }, state, upload);

    expect(await sync.poll()).toBe(false);
    expect(upload).not.toHaveBeenCalled();
  });

  it("keeps an already paused game stopped during exit capture", async () => {
    const loop = vi.fn();
    const upload = vi.fn<(bytes: Uint8Array, event: "AUTO_INTERVAL" | "EXIT") => Promise<boolean>>(async () => true);
    const sync = new PersistentStateSync({
      getState: () => Uint8Array.of(7),
      toggleMainLoop: loop,
    }, null, upload, { isPaused: () => true });

    expect(await sync.flush()).toBe(true);
    expect(loop.mock.calls).toEqual([[false], [false]]);
    expect(upload.mock.calls[0]?.[1]).toBe("EXIT");
  });
});
