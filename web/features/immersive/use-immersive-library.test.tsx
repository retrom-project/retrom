import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useImmersiveLibrary } from "./use-immersive-library";
import { saveFixture } from "./test-fixtures";
import type { ImmersiveEntry, View } from "./immersive-data";

const mocks = vi.hoisted(() => ({ search: new URLSearchParams(), loadEntries: vi.fn(), loadDestinations: vi.fn(), loadFolders: vi.fn(), GET: vi.fn() }));
vi.mock("next/navigation", () => ({ useSearchParams: () => mocks.search }));
vi.mock("./immersive-data", async (original) => ({ ...await original<object>(), loadEntries: mocks.loadEntries, loadDestinations: mocks.loadDestinations, loadFolders: mocks.loadFolders }));
vi.mock("@/lib/api/client", () => ({ api: { GET: mocks.GET }, result: (response: { data: unknown }) => response.data }));
const entries: ImmersiveEntry[] = Array.from({ length: 55 }, (_, index) => { const save = saveFixture(`save-${index}`, `game-${index}`); return { game: save.game, save }; });
beforeEach(() => {
  mocks.search = new URLSearchParams("view=games&destination=all");
  mocks.loadDestinations.mockResolvedValue({ items: [{ id: "all", name: "全部游戏", view: "games" }] });
  mocks.loadFolders.mockResolvedValue({ items: [] });
  mocks.loadEntries.mockImplementation(async (_view, _directory, _folder, offset, limit) => ({ items: entries.slice(offset, offset + limit), offset, limit, total: entries.length }));
  mocks.GET.mockResolvedValue({ data: { items: [], total: 0, offset: 0 } });
});
afterEach(() => { cleanup(); vi.resetAllMocks(); });

it.each<View>(["games", "favorites", "recent", "saves"])("appends bounded %s pages without resetting the selected game", async (view) => {
  mocks.search = new URLSearchParams({ view, destination: view === "games" ? "all" : view });
  const hook = renderHook(useImmersiveLibrary);
  await waitFor(() => expect(hook.result.current.entries.data?.items).toHaveLength(24));
  act(() => hook.result.current.setSelected(23));
  await act(async () => hook.result.current.entries.loadMore());
  expect(hook.result.current.entries.data?.items).toHaveLength(48);
  expect(hook.result.current.selected).toBe(23);
  act(() => hook.result.current.setSelected(47));
  await act(async () => hook.result.current.entries.loadMore());
  expect(hook.result.current.entries.data?.items).toHaveLength(55);
  expect(hook.result.current.selected).toBe(47);
  expect(hook.result.current.entries.hasMore).toBe(false);
  expect(mocks.loadEntries.mock.calls.map((args) => args.slice(3, 5))).toEqual([[0, 24], [24, 24], [48, 24]]);
});

it("reopens the list at the selected game's page after returning from play with one read", async () => {
  mocks.search = new URLSearchParams("view=games&destination=all&offset=48&entry=game-50");
  const hook = renderHook(useImmersiveLibrary);
  await waitFor(() => expect(hook.result.current.entries.data?.items[hook.result.current.selected]?.game.id).toBe("game-50"));
  expect(hook.result.current.entries.base).toBe(48);
  expect(hook.result.current.returnTo).toContain("offset=48");
  expect(mocks.loadEntries.mock.calls.map((args) => args.slice(3, 5))).toEqual([[48, 24]]);
});

it("keeps the selected game while earlier pages load before the window", async () => {
  mocks.search = new URLSearchParams("view=games&destination=all&offset=48&entry=game-50");
  const hook = renderHook(useImmersiveLibrary);
  await waitFor(() => expect(hook.result.current.entries.base).toBe(48));
  act(() => hook.result.current.setListPosition(49));
  await act(async () => { await hook.result.current.entries.loadBefore(); });
  expect(hook.result.current.entries.base).toBe(24);
  expect(hook.result.current.entries.data?.items[hook.result.current.selected]?.game.id).toBe("game-49");
  expect(mocks.loadEntries.mock.calls.map((args) => args.slice(3, 5))).toEqual([[48, 24], [24, 24]]);
});

it("opens a far letter with one read instead of every page before it", async () => {
  const hook = renderHook(useImmersiveLibrary);
  await waitFor(() => expect(hook.result.current.entries.data?.items).toHaveLength(24));
  let index: number | null = null;
  await act(async () => { index = await hook.result.current.entries.moveTo(40); });
  expect(index).toBe(0);
  expect(hook.result.current.entries.base).toBe(40);
  expect(mocks.loadEntries.mock.calls.map((args) => args.slice(3, 5))).toEqual([[0, 24], [40, 24]]);
});

it("resets the loaded range when the favorite folder changes", async () => {
  mocks.search = new URLSearchParams("view=favorites&destination=favorites");
  const hook = renderHook(useImmersiveLibrary);
  await waitFor(() => expect(hook.result.current.entries.data?.items).toHaveLength(24));
  await act(async () => hook.result.current.entries.loadMore());
  act(() => { hook.result.current.setFolder("new-folder"); hook.result.current.setSelected(0); });
  await waitFor(() => expect(hook.result.current.entries.data?.items).toHaveLength(24));
  expect(mocks.loadEntries.mock.lastCall?.slice(2, 5)).toEqual(["new-folder", 0, 24]);
});
