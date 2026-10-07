import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { loadDetail } from "@/features/library/api";
import { GameDetail } from "./game-detail";

const calls = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({
  push: (path: string) => window.history.pushState(null, "", path),
  replace: (path: string) => window.history.replaceState(null, "", path),
}) }));
vi.mock("@/features/library/api", () => ({ loadDetail: vi.fn(), toggleFavorite: vi.fn() }));
vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { GET: calls.get, POST: calls.post } };
});
vi.mock("./admin-game-detail", () => ({
  AdminGameDetail: ({ detail, onReview, busy }: {
    detail: Schema<"GameDetail">;
    onReview: (action: "approve" | "discard") => void;
    busy: boolean;
  }) => <>
    <h1>{detail.game.title}</h1>
    <button disabled={busy} onClick={() => onReview("approve")}>通过并发布</button>
    <button disabled={busy} onClick={() => onReview("discard")}>丢弃条目</button>
  </>,
}));

const detail: Schema<"GameDetail"> = {
  game: {
    id: "current", platformInstanceId: "directory", directoryName: "NES", platformId: "nes",
    title: "当前待审", description: "", developer: "", publisher: "", genre: "", players: null,
    releaseYear: null, status: "pending_review", source: "server_import", version: 1,
    contentHash: "hash", tags: [], media: [], favorite: false, lastPlayedAtMs: null,
    createdAtMs: 1, updatedAtMs: 1,
  },
  files: [], coreIds: ["fceumm"], defaultCoreId: "fceumm", saves: [], favoriteFolderIds: [],
  runtimeConfig: { content: { kind: "SINGLE_FILE", entryFile: "game.nes" } },
};
beforeEach(() => {
  vi.resetAllMocks();
  window.history.replaceState(null, "", "/admin/reviews/current");
  vi.mocked(loadDetail).mockResolvedValue(detail);
  calls.post.mockResolvedValue({ data: { ...detail.game, status: "published" }, response: new Response() });
  calls.get.mockResolvedValue({ data: { items: [], total: 0, offset: 0, limit: 1 }, response: new Response() });
});
afterEach(cleanup);

it("waits for publication, then replaces the reviewed entry with the first shared pending game", async () => {
  const published = Promise.withResolvers<{ data: Schema<"Game">; response: Response }>();
  calls.post.mockReturnValueOnce(published.promise);
  calls.get.mockResolvedValueOnce({ data: { items: [{ ...detail.game, id: "next" }], total: 1 }, response: new Response() });
  const view = render(<GameDetail gameId="current" mode="review" />);
  const approve = await screen.findByRole("button", { name: "通过并发布" });
  const historyLength = window.history.length;
  fireEvent.click(approve);
  expect(approve).toBeDisabled();
  expect(calls.get).not.toHaveBeenCalled();
  expect(window.location.pathname).toBe("/admin/reviews/current");
  await act(async () => published.resolve({ data: { ...detail.game, status: "published" }, response: new Response() }));
  await waitFor(() => expect(window.location.pathname).toBe("/admin/reviews/next"));
  expect(window.history.length).toBe(historyLength);
  expect(calls.post).toHaveBeenCalledExactlyOnceWith("/api/v1/admin/reviews/{gameId}/approve", {
    params: { path: { gameId: "current" } }, body: { version: 1 },
  });
  expect(calls.get).toHaveBeenCalledExactlyOnceWith("/api/v1/admin/reviews", { params: { query: { offset: 0, limit: 1 } } });
  expect(screen.getByRole("status")).toHaveTextContent("游戏已通过审核并发布");
  vi.mocked(loadDetail).mockResolvedValue({ ...detail, game: { ...detail.game, id: "next", title: "下一条同版本待审" } });
  view.rerender(<GameDetail gameId="next" mode="review" />);
  expect(await screen.findByRole("heading", { name: "下一条同版本待审" })).toBeVisible();
  expect(screen.queryByRole("heading", { name: "当前待审" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled();
});

it("returns to the shared review list when no pending game remains", async () => {
  render(<GameDetail gameId="current" mode="review" />);
  const historyLength = window.history.length;
  fireEvent.click(await screen.findByRole("button", { name: "通过并发布" }));
  await waitFor(() => expect(window.location.pathname + window.location.search).toBe("/admin/reviews"));
  expect(window.history.length).toBe(historyLength);
  expect(screen.getByRole("status")).toHaveTextContent("游戏已通过审核并发布");
});

it("stays on the current game if publication fails without reading the next item", async () => {
  calls.post.mockResolvedValueOnce({ error: { code: "VERSION_CONFLICT", message: "资料已经更新，请重试。" }, response: new Response(null, { status: 409 }) });
  render(<GameDetail gameId="current" mode="review" />);
  fireEvent.click(await screen.findByRole("button", { name: "通过并发布" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("资料已经更新，请重试。");
  expect(window.location.pathname).toBe("/admin/reviews/current");
  expect(calls.get).not.toHaveBeenCalled();
  await waitFor(() => expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled());
});

it("reports successful publication and returns to the list if reading the next item fails", async () => {
  calls.get.mockResolvedValueOnce({ error: { code: "INTERNAL_ERROR", message: "队列暂时不可用" }, response: new Response(null, { status: 500 }) });
  render(<GameDetail gameId="current" mode="review" />);
  fireEvent.click(await screen.findByRole("button", { name: "通过并发布" }));
  await waitFor(() => expect(window.location.pathname).toBe("/admin/reviews"));
  expect(screen.getByRole("status")).toHaveTextContent("游戏已发布，但无法读取下一条待审核游戏，已返回待审核列表。");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(loadDetail).toHaveBeenCalledOnce();
  expect(calls.post).toHaveBeenCalledOnce();
});

it("retains discard navigation and accepts its empty 204 response", async () => {
  calls.post.mockResolvedValueOnce({ response: new Response(null, { status: 204 }) });
  render(<GameDetail gameId="current" mode="review" />);
  const historyLength = window.history.length;
  fireEvent.click(await screen.findByRole("button", { name: "丢弃条目" }));
  await waitFor(() => expect(window.location.pathname).toBe("/admin/reviews"));
  expect(window.history.length).toBe(historyLength + 1);
  expect(calls.get).not.toHaveBeenCalled();
  expect(screen.getByRole("status")).toHaveTextContent("审核条目已丢弃");
});
