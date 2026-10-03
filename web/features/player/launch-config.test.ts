import { afterEach, expect, it, vi } from "vitest";
import { readLaunchConfig } from "./launch-config";

afterEach(() => vi.unstubAllGlobals());

it.each([
  [401, "PLAYER_LAUNCH_SESSION_EXPIRED"], [403, "PLAYER_LAUNCH_SESSION_EXPIRED"],
  [404, "PLAYER_LAUNCH_UNAVAILABLE"], [410, "PLAYER_LAUNCH_UNAVAILABLE"],
  [429, "PLAYER_LAUNCH_SERVICE_UNAVAILABLE"], [503, "PLAYER_LAUNCH_SERVICE_UNAVAILABLE"],
  [400, "PLAYER_LAUNCH_CONFIG_INVALID"],
])("classifies config HTTP %i without treating it as account expiry", async (status, code) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status })));
  await expect(readLaunchConfig("launch", new AbortController().signal)).rejects.toThrow(code);
});

it("distinguishes network failure, invalid configuration and owner cancellation", async () => {
  const network = new TypeError("fetch failed");
  const fetchMock = vi.fn().mockRejectedValueOnce(network).mockResolvedValueOnce(new Response("invalid json")).mockRejectedValueOnce(network);
  vi.stubGlobal("fetch", fetchMock);
  await expect(readLaunchConfig("launch", new AbortController().signal)).rejects.toThrow("PLAYER_LAUNCH_NETWORK_FAILED");
  await expect(readLaunchConfig("launch", new AbortController().signal)).rejects.toThrow("PLAYER_LAUNCH_CONFIG_INVALID");
  const controller = new AbortController(); controller.abort();
  await expect(readLaunchConfig("launch", controller.signal)).rejects.toBe(network);
});
