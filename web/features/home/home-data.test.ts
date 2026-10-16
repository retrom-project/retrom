import { afterEach, expect, it, vi } from "vitest";
import { loadHome } from "./home-data";

afterEach(() => vi.unstubAllGlobals());

function responses(...payloads: unknown[]) {
  const fetch = vi.fn<(request: Request) => Promise<Response>>().mockImplementation(async () => Response.json(payloads.shift()));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
const latest = { game: { id: "latest" }, lastPlayedAtMs: 3000 };
const home = { recent: [latest], saves: [{ id: "unrelated", restorable: true, game: { id: "older" } }] };

it("queries the latest save for the last played game, even when it predates the latest play", async () => {
  const save = { id: "older-progress", restorable: true, updatedAtMs: 1000, game: latest.game };
  const fetch = responses(home, { items: [save], total: 120 });
  const data = await loadHome();
  expect(data.featured).toEqual({ recent: latest, save });
  const url = new URL(fetch.mock.calls[1][0].url);
  expect(url.pathname).toBe("/api/v1/saves");
  expect(Object.fromEntries(url.searchParams)).toEqual({ gameId: "latest", sort: "recent", limit: "1" });
  expect(fetch).toHaveBeenCalledTimes(2);
});

it("keeps the newest unavailable save visible rather than showing an older save", async () => {
  const save = { id: "newest", restorable: false, screenshotUrl: "/latest.png" };
  const fetch = responses(home, { items: [save], total: 2 });
  expect((await loadHome()).featured).toEqual({ recent: latest, save });
  expect(fetch).toHaveBeenCalledTimes(2);
});

it("uses the no-save state only when this game has no saves", async () => {
  responses(home, { items: [], total: 0 });
  expect((await loadHome()).featured).toEqual({ recent: latest, save: undefined });
});

it("never substitutes a saved game when there is no recent play", async () => {
  const fetch = responses({ ...home, recent: [] });
  expect((await loadHome()).featured).toBeNull();
  expect(fetch).toHaveBeenCalledOnce();
});

it("propagates a save lookup failure instead of misreporting no saves", async () => {
  const fetch = vi.fn().mockResolvedValueOnce(Response.json(home)).mockResolvedValueOnce(Response.json({ code: "FAILED", message: "读取失败" }, { status: 500 }));
  vi.stubGlobal("fetch", fetch);
  await expect(loadHome()).rejects.toThrow("读取失败");
});

it("cancels the dependent save request when navigation aborts the home load", async () => {
  const controller = new AbortController();
  const fetch = vi.fn(async (request: Request) => {
    if (new URL(request.url).pathname.endsWith("/home")) { return Response.json(home); }
    controller.abort();
    request.signal.throwIfAborted();
    return Response.json({ items: [], total: 0 });
  });
  vi.stubGlobal("fetch", fetch);
  await expect(loadHome(controller.signal)).rejects.toMatchObject({ name: "AbortError" });
});
