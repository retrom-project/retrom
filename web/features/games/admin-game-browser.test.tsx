import { act, cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { AdminGameBrowser } from "./admin-game-browser";

const calls = vi.hoisted(() => ({ get: vi.fn(), games: vi.fn() }));
vi.mock("next/navigation", () => ({ usePathname: () => "/admin/reviews", useRouter: () => ({ replace: vi.fn() }) }));
vi.mock("@/lib/api/client", async (original) => ({ ...await original<typeof ApiClient>(), api: { GET: calls.get } }));
vi.mock("@/features/library/api", () => ({ loadGames: calls.games, loadDirectories: async () => ({ items: [] }), loadTags: async () => ({ items: [] }) }));
const game: Schema<"Game"> = {
  id: "new", platformInstanceId: "directory", directoryName: "NES", platformId: "nes", title: "新扫描的游戏",
  description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null,
  status: "pending_review", source: "server_import", version: 1, contentHash: "hash",
  tags: [], media: [], favorite: false, lastPlayedAtMs: null, createdAtMs: 1, updatedAtMs: 1,
};
beforeEach(() => { vi.resetAllMocks(); vi.useFakeTimers(); calls.games.mockResolvedValue({ items: [], total: 0, offset: 0, limit: 24 }); });
afterEach(() => { cleanup(); vi.useRealTimers(); });
it("refreshes the initially empty review list when the current scan imports games", async () => {
  const task = { id: "current", scanType: "game", status: "running", totalKnown: true, totalCount: 1, processedCount: 0, importedCount: 0, skippedCount: 0, failedCount: 0, error: null };
  calls.get.mockResolvedValueOnce({ data: { items: [task] }, response: new Response() })
    .mockResolvedValueOnce({ data: { items: [{ ...task, status: "completed", processedCount: 1, importedCount: 1 }] }, response: new Response() });
  await act(async () => { render(<AdminGameBrowser kind="review" initial={{ scanId: "current" }} />); });
  expect(screen.getByRole("heading", { name: "没有待审核的游戏" })).toBeVisible();
  calls.games.mockResolvedValue({ items: [game], total: 1, offset: 0, limit: 24 });
  await act(() => vi.advanceTimersByTimeAsync(3000));
  expect(screen.getByRole("link", { name: "新扫描的游戏" })).toBeVisible();
  expect(screen.queryByRole("heading", { name: "没有待审核的游戏" })).not.toBeInTheDocument();
  expect(calls.games.mock.calls.every((call) => !("scanId" in call[1]))).toBe(true);
});
it("does not fetch or display scan progress on a normal review-list visit", async () => {
  await act(async () => { render(<AdminGameBrowser kind="review" initial={{}} />); });
  expect(screen.queryByRole("region", { name: "当前游戏扫描" })).not.toBeInTheDocument();
  expect(calls.get).not.toHaveBeenCalled();
});
