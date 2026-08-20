import { describe, expect, it } from "vitest";
import { readBoundedResponse, reportsNativeExit } from "./player-shell";

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

describe("reportsNativeExit", () => {
  it("leaves netplay termination to its global session controller", () => {
    expect(reportsNativeExit("single")).toBe(true);
    expect(reportsNativeExit("netplay")).toBe(false);
    expect(reportsNativeExit("single", true)).toBe(false);
  });
});
