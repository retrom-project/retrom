import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { Save } from "@/lib/api/types";
import { useSaveSelection } from "./use-save-selection";
import { saveFixture } from "./test-fixtures";

const requests = vi.hoisted(() => ({ GET: vi.fn() }));
vi.mock("@/lib/api/client", () => ({ api: requests, result: (response: { data: unknown }) => response.data }));
const restored = { entryId: "", saveId: "" };
function page(items: Save[], offset = 0, total = items.length) { return { data: { items, offset, total } }; }
afterEach(() => { cleanup(); vi.resetAllMocks(); });

it("keeps the new game's known screenshot while its saves load without making the preview launchable", async () => {
  const first = { ...saveFixture("first", "first-game"), screenshotUrl: "/first.png" };
  const next = { ...saveFixture("next", "next-game"), screenshotUrl: "/next.png" };
  requests.GET.mockResolvedValueOnce(page([first])).mockImplementationOnce(() => new Promise(() => {}));
  const hook = renderHook(({ save }) => useSaveSelection({ game: save.game, save }, true, restored), { initialProps: { save: first } });
  await waitFor(() => expect(hook.result.current.save?.id).toBe("first"));
  hook.rerender({ save: next });
  expect(hook.result.current.resource.loading).toBe(true);
  expect(hook.result.current.previewSave).toBe(next);
  expect(hook.result.current.save).toBeUndefined();
  expect(hook.result.current.hasNext).toBe(false);
});

it("does not substitute a representative preview for an explicitly restored older save", () => {
  const latest = saveFixture("latest");
  requests.GET.mockImplementation(() => new Promise(() => {}));
  const hook = renderHook(() => useSaveSelection({ game: latest.game, save: latest }, true, { entryId: "game", saveId: "older" }));
  expect(hook.result.current.previewSave).toBeUndefined();
  expect(hook.result.current.save).toBeUndefined();
});

it("removes a stale representative when the server confirms that the game has no saves", async () => {
  const latest = saveFixture("removed");
  requests.GET.mockResolvedValue(page([]));
  const hook = renderHook(() => useSaveSelection({ game: latest.game, save: latest }, true, restored));
  await waitFor(() => expect(hook.result.current.resource.loading).toBe(false));
  expect(hook.result.current.save).toBeUndefined();
  expect(hook.result.current.previewSave).toBeUndefined();
});

it("selects older and blocked saves within one game and resets when another game is chosen", async () => {
  const latest = saveFixture("latest");
  const older = { ...saveFixture("older"), restorable: false, restoreReason: "core_changed" as const };
  requests.GET.mockResolvedValue(page([latest, older]));
  const hook = renderHook(({ save }) => useSaveSelection({ game: save.game, save }, true, restored), { initialProps: { save: latest } });
  await waitFor(() => expect(hook.result.current.save?.id).toBe("latest"));
  act(() => hook.result.current.move("right"));
  await waitFor(() => expect(hook.result.current.save?.id).toBe("older"));
  expect(hook.result.current.save?.restorable).toBe(false);
  expect(requests.GET).toHaveBeenCalledTimes(1);
  act(() => hook.result.current.move("right"));
  expect(hook.result.current.save?.id).toBe("older");
  act(() => hook.result.current.move("left"));
  expect(hook.result.current.save?.id).toBe("latest");
  const another = saveFixture("another", "another-game");
  requests.GET.mockResolvedValue(page([another]));
  hook.rerender({ save: another });
  await waitFor(() => expect(hook.result.current.save?.id).toBe("another"));
});

it("keeps the chosen save when the same game's representative save changes", async () => {
  const latest = saveFixture("latest"), older = saveFixture("older"), newer = saveFixture("newer");
  requests.GET.mockResolvedValue(page([latest, older]));
  const hook = renderHook(({ save }) => useSaveSelection({ game: save.game, save }, true, restored), { initialProps: { save: latest } });
  await waitFor(() => expect(hook.result.current.save?.id).toBe("latest"));
  act(() => hook.result.current.select("older"));
  requests.GET.mockResolvedValue(page([newer, latest, older]));
  hook.rerender({ save: newer });
  await waitFor(() => expect(hook.result.current.save?.id).toBe("older"));
});

it("navigates beyond the first hundred saves and returns to the previous page's last save", async () => {
  const first = Array.from({ length: 100 }, (_, i) => saveFixture(String(i)));
  const second = [saveFixture("100"), saveFixture("101")];
  requests.GET.mockImplementation(async (_path, options) => page(options?.params?.query?.offset === 100 ? second : first, options?.params?.query?.offset ?? 0, 102));
  const hook = renderHook(() => useSaveSelection({ game: first[0].game, save: first[0] }, true, restored));
  await waitFor(() => expect(hook.result.current.save?.id).toBe("0"));
  act(() => hook.result.current.select("99"));
  await waitFor(() => expect(hook.result.current.save?.id).toBe("99"));
  act(() => hook.result.current.move("right"));
  expect(hook.result.current.save).toBeUndefined();
  await waitFor(() => expect(hook.result.current.save?.id).toBe("100"));
  act(() => hook.result.current.move("left"));
  await waitFor(() => expect(hook.result.current.save?.id).toBe("99"));
});

it("locates an explicitly selected older save and restores its saved page", async () => {
  const first = Array.from({ length: 100 }, (_, i) => saveFixture(String(i)));
  const second = [saveFixture("100"), saveFixture("101")];
  requests.GET.mockImplementation(async (_path, options) => page(options?.params?.query?.offset === 100 ? second : first, options?.params?.query?.offset ?? 0, 102));
  const hook = renderHook(() => useSaveSelection({ game: first[0].game, save: first[0] }, true, { entryId: "game", saveId: "100" }));
  await waitFor(() => expect(hook.result.current.save?.id).toBe("100"));
  expect(hook.result.current.resource.data?.offset).toBe(100);
  cleanup();
  const returned = renderHook(() => useSaveSelection({ game: first[0].game, save: first[0] }, true, { entryId: "game", saveId: "101", offset: 100 }));
  await waitFor(() => expect(returned.result.current.save?.id).toBe("101"));
  expect(returned.result.current.index).toBe(1);
});

it("initially selects the latest save for the chosen game", async () => {
  const latest = saveFixture("latest");
  const older = saveFixture("older");
  requests.GET.mockResolvedValue(page([latest, older]));
  const hook = renderHook(() => useSaveSelection({ game: latest.game, save: latest }, true, { entryId: "game", saveId: "" }));
  await waitFor(() => expect(hook.result.current.save?.id).toBe("latest"));
});

it("ignores a late response for another game and preserves failure/retry state without launching a fallback", async () => {
  const old = saveFixture("old", "old-game");
  const next = saveFixture("next", "next-game");
  let finishOld!: (value: ReturnType<typeof page>) => void;
  requests.GET.mockImplementationOnce(() => new Promise((resolve) => { finishOld = resolve; })).mockRejectedValueOnce(new Error("读取失败")).mockResolvedValue(page([next]));
  const hook = renderHook(({ save }) => useSaveSelection({ game: save.game, save }, true, restored), { initialProps: { save: old } });
  hook.rerender({ save: next });
  await waitFor(() => expect(hook.result.current.resource.error).toBe("读取失败"));
  await act(async () => finishOld(page([old])));
  expect(hook.result.current.save).toBeUndefined();
  act(() => hook.result.current.resource.reload());
  await waitFor(() => expect(hook.result.current.save?.id).toBe("next"));
});
