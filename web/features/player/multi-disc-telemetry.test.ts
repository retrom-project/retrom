import { afterEach, describe, expect, it, vi } from "vitest";
import { multiDiscPlayerResultCode, reportMultiDiscPlayerEvent } from "./multi-disc-telemetry";

describe("multi-disc Player telemetry", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("sends only the closed event fields to the Launch-scoped endpoint", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      void input;
      void init;
      return new Response(null, { status: 204 });
    });
    vi.stubGlobal("fetch", fetchMock);
    await reportMultiDiscPlayerEvent("launch/id", {
      eventType: "SWITCH_SUCCESS",
      resultCode: "OK",
      discCount: 3,
      observedDiscCount: 3,
    });
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/runtime/launches/launch%2Fid/player-events");
    expect(init).toMatchObject({ method: "POST", credentials: "same-origin", cache: "no-store" });
    expect(JSON.parse(String(init?.body))).toEqual({
      eventType: "SWITCH_SUCCESS",
      resultCode: "OK",
      discCount: 3,
      observedDiscCount: 3,
    });
  });

  it("normalizes arbitrary runtime failures to a stable result code", () => {
    expect(multiDiscPlayerResultCode(new Error("PLAYER_DISC_SET_INVALID"), "PLAYER_DISC_SWITCH_FAILED"))
      .toBe("PLAYER_DISC_SET_INVALID");
    expect(multiDiscPlayerResultCode(new Error("secret filename"), "PLAYER_DISC_SWITCH_FAILED"))
      .toBe("PLAYER_DISC_SWITCH_FAILED");
  });
});
