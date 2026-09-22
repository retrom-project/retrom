import { describe, expect, it } from "vitest";
import { canResumeFromGameSurface, readBoundedResponse } from "./player-shell";

describe("readBoundedResponse", () => {
  it("assembles a bounded streamed state", async () => {
    const response = new Response(new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(Uint8Array.of(1, 2));
        controller.enqueue(Uint8Array.of(3));
        controller.close();
      },
    }));
    await expect(readBoundedResponse(response, 3)).resolves.toEqual(Uint8Array.of(1, 2, 3));
  });

  it("rejects both declared and streamed state overflow", async () => {
    await expect(readBoundedResponse(new Response(Uint8Array.of(1), { headers: { "Content-Length": "4" } }), 3))
      .rejects.toThrow("PLAYER_SAVE_STATE_TOO_LARGE");
    const streamed = new Response(new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(Uint8Array.of(1, 2));
        controller.enqueue(Uint8Array.of(3, 4));
        controller.close();
      },
    }));
    await expect(readBoundedResponse(streamed, 3)).rejects.toThrow("PLAYER_SAVE_STATE_TOO_LARGE");
  });
});

describe("canResumeFromGameSurface", () => {
  it("allows explicit pause-overlay activation while read-only chrome stays pinned", () => {
    expect(canResumeFromGameSurface({running: true, paused: true, chromePinned: true, source: "pause-overlay"}))
      .toBe(true);
  });

  it.each([
    {running: false, paused: true},
    {running: true, paused: false},
  ])("does not bypass runtime state for explicit resume: %j", (state) => {
    expect(canResumeFromGameSurface({...state, chromePinned: true, source: "pause-overlay"})).toBe(false);
  });

  it("does not resume while host chrome owns the interaction", () => {
    expect(canResumeFromGameSurface({running: true, paused: true, chromePinned: true}))
      .toBe(false);
    expect(canResumeFromGameSurface({running: true, paused: true, chromePinned: false}))
      .toBe(true);
  });
});
