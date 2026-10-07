import { act } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { renderHook } from "@/components/toast-test-utils";
import { api } from "@/lib/api/client";
import type * as ApiClient from "@/lib/api/client";
import type { Game } from "@/lib/api/types";
import { useReviewApproval } from "./use-review-approval";

vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { GET: vi.fn(), POST: vi.fn() } };
});

it("captures the current filters and prevents a second approval while the snapshot is loading", async () => {
  const query = { q: "本次范围", platformInstanceId: "directory", tagId: "tag", sort: "title" as const, offset: 24, limit: 24 };
  const game: Game = { id: "pending", title: "匹配的游戏", version: 4, platformInstanceId: "directory", platformId: "nes",
    directoryName: "目录", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null,
    status: "pending_review", source: "server_import", contentHash: "hash", tags: [], media: [], favorite: false, lastPlayedAtMs: null, createdAtMs: 1, updatedAtMs: 1 };
  const getReviews = api.GET<"/api/v1/admin/reviews", { params: { query: typeof query }; signal: AbortSignal }>;
  let release!: (value: Awaited<ReturnType<typeof getReviews>>) => void;
  vi.mocked(getReviews).mockImplementation(() => new Promise((resolve) => { release = resolve; }));
  vi.mocked(api.POST).mockImplementation(async (path) => ({
    data: path === "/api/v1/admin/reviews/readiness"
      ? { items: [{ id: game.id, version: game.version, biosSatisfied: true, error: null, missingBios: [] }] }
      : game,
    response: new Response(),
  }) as Awaited<ReturnType<typeof api.POST>>);
  const complete = vi.fn();
  const hook = renderHook(() => useReviewApproval(query, complete));
  let pending!: Promise<void>;
  act(() => { pending = hook.result.current.start(); void hook.result.current.start(); });
  expect(hook.result.current.busy).toBe(true);
  expect(api.GET).toHaveBeenCalledTimes(1);
  expect(api.GET).toHaveBeenCalledWith("/api/v1/admin/reviews", {
    params: { query: { ...query, offset: 0, limit: 100 } }, signal: expect.any(AbortSignal),
  });
  await act(async () => {
    release({ data: { items: [game], total: 1, offset: 0, limit: 100 }, response: new Response() });
    await pending;
  });
  expect(api.POST).toHaveBeenCalledTimes(2);
  expect(api.POST).toHaveBeenLastCalledWith("/api/v1/admin/reviews/{gameId}/approve", { params: { path: { gameId: game.id } }, body: { version: 4 } });
  expect(hook.result.current.busy).toBe(false);
  expect(complete).toHaveBeenCalledTimes(1);
});
