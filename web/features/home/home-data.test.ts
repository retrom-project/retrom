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

it("queries saves for the latest played game, including progress absent from the global home slice", async () => {
  const save = { id: "older-progress", restorable: true, game: latest.game };
  const fetch = responses(home, { items: [save], total: 1 });
  const data = await loadHome();
  expect(data.featured).toEqual({ recent: latest, save, saveCount: 1 });
  const url = new URL(fetch.mock.calls[1][0].url);
  expect(url.pathname).toBe("/api/v1/saves");
  expect(Object.fromEntries(url.searchParams)).toEqual({ gameId: "latest", sort: "recent", offset: "0", limit: "100" });
});

it("finds the newest restorable save beyond a page of unavailable progress", async () => {
  const unavailable = Array.from({ length: 100 }, (_, id) => ({ id: `${id}`, restorable: false }));
  const save = { id: "valid", restorable: true };
  const fetch = responses(home, { items: unavailable, total: 102 }, { items: [save, { id: "older-valid", restorable: true }], total: 102 });
  expect((await loadHome()).featured).toEqual({ recent: latest, save, saveCount: 102 });
  expect(new URL(fetch.mock.calls[2][0].url).searchParams.get("offset")).toBe("100");
});

it.each([{ items: [] }, { items: [{ id: "unavailable", restorable: false }] }])("distinguishes no saves from unavailable saves", async ({ items }) => {
  responses(home, { items, total: items.length });
  expect((await loadHome()).featured).toEqual({ recent: latest, save: undefined, saveCount: items.length });
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
