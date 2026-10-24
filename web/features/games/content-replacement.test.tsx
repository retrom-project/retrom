import { act, cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { GameContentReplacement } from "./content-replacement";

const calls = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("@/lib/api/client", async (original) => ({
  ...await original<typeof ApiClient>(), api: { GET: calls.get, POST: calls.post },
}));
beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); vi.resetAllMocks(); });

const detail = { game: { id: "game", version: 4 } } as Schema<"GameDetail">;
const progress: Schema<"ContentReplacement"> = {
  id: "replacement", gameId: "game", status: "running", phase: "normalizing", canCancel: true,
  result: null, error: null, createdAtMs: 1, updatedAtMs: 1, expiresAtMs: 100000,
};
const response = (data: Schema<"ContentReplacement">) => ({ response: new Response(null, { status: 200 }), data });
const missing = () => ({ response: new Response(null, { status: 404 }), error: { code: "NOT_FOUND", message: "Unavailable" } });
async function show(onChange = vi.fn()) {
  const view = render(<GameContentReplacement detail={detail} onChange={onChange} />);
  await act(async () => {});
  return view;
}
async function tick() { await act(async () => { await vi.advanceTimersByTimeAsync(2000); }); }

it("restores a queued replacement after refresh and cancels it without announcing success", async () => {
  const onChange = vi.fn();
  calls.get.mockResolvedValue(response({ ...progress, status: "queued", phase: "queued" }));
  calls.post.mockResolvedValue(response({ ...progress, status: "cancelled", phase: "queued", canCancel: false }));
  await show(onChange);
  expect(screen.getByText("内容替换正在等待")).toBeVisible();
  expect(screen.getByRole("button", { name: "替换游戏内容" })).toBeDisabled();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "取消内容替换" })); });
  expect(calls.post).toHaveBeenCalledWith("/api/v1/admin/games/{gameId}/content-replacement/{operationId}/cancel", {
    params: { path: { gameId: "game", operationId: "replacement" } },
  });
  expect(screen.getByText("内容替换已取消")).toBeVisible();
  expect(screen.getByRole("button", { name: "替换游戏内容" })).toBeEnabled();
  expect(onChange).not.toHaveBeenCalled();
  const reads = calls.get.mock.calls.length;
  await tick();
  expect(calls.get).toHaveBeenCalledTimes(reads);
});

it("waits for completed progress before refreshing game data and stops polling", async () => {
  const onChange = vi.fn();
  calls.get.mockResolvedValueOnce(response(progress)).mockResolvedValue(response({
    ...progress, status: "completed", phase: "completed", canCancel: false,
    result: { version: 5, contentHash: "new-content" },
  }));
  await show(onChange);
  expect(screen.getByText("正在转换游戏内容")).toBeVisible();
  expect(onChange).not.toHaveBeenCalled();
  await tick();
  expect(screen.getAllByText("游戏内容已替换")).toHaveLength(2);
  expect(onChange).toHaveBeenCalledTimes(1);
  await tick();
  expect(calls.get).toHaveBeenCalledTimes(2);
  expect(onChange).toHaveBeenCalledTimes(1);
});

it("does not reannounce an old completed replacement on page load", async () => {
  const onChange = vi.fn();
  calls.get.mockResolvedValue(response({ ...progress, status: "completed", phase: "completed", canCancel: false,
    result: { version: 4, contentHash: "current-content" } }));
  await show(onChange);
  expect(screen.getByText("游戏内容已替换")).toBeVisible();
  expect(onChange).not.toHaveBeenCalled();
  await tick();
  expect(calls.get).toHaveBeenCalledTimes(1);
});

it("refreshes stale game detail once when the first progress read already completed", async () => {
  const onChange = vi.fn();
  calls.get.mockResolvedValue(response({ ...progress, status: "completed", phase: "completed", canCancel: false,
    result: { version: 5, contentHash: "new-content" } }));
  const view = await show(onChange);
  expect(onChange).toHaveBeenCalledTimes(1);
  expect(screen.getAllByText("游戏内容已替换")).toHaveLength(1);
  await act(async () => { view.rerender(<GameContentReplacement detail={detail} onChange={onChange} />); });
  await tick();
  expect(onChange).toHaveBeenCalledTimes(1);
  expect(calls.get).toHaveBeenCalledTimes(1);
});

it("does not offer cancellation once publication begins", async () => {
  calls.get.mockResolvedValue(response({ ...progress, phase: "publishing", canCancel: false }));
  await show();
  expect(screen.getByText("正在发布替换结果")).toBeVisible();
  expect(screen.getByText("结果确认期间无法取消。")).toBeVisible();
  expect(screen.queryByRole("button", { name: "取消内容替换" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "替换游戏内容" })).toBeDisabled();
});

it("keeps polling cancellation until cleanup is confirmed", async () => {
  calls.get.mockResolvedValueOnce(response(progress)).mockResolvedValue(response({
    ...progress, status: "cancelled", canCancel: false,
  }));
  calls.post.mockResolvedValue(response({ ...progress, status: "canceling", canCancel: false }));
  await show();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "取消内容替换" })); });
  expect(screen.getByText("正在取消内容替换")).toBeVisible();
  expect(screen.getByRole("button", { name: "替换游戏内容" })).toBeDisabled();
  await tick();
  expect(screen.getByText("内容替换已取消")).toBeVisible();
  expect(screen.getByRole("button", { name: "替换游戏内容" })).toBeEnabled();
});

it("shows safe failure details and lets administrators refresh the game before retrying", async () => {
  const onChange = vi.fn();
  calls.get.mockResolvedValue(response({ ...progress, status: "failed", canCancel: false,
    error: { code: "RUNTIME_BUSY", message: "内容转换正在繁忙，请稍后重试。原活动内容保留。" } }));
  await show(onChange);
  expect(screen.getByText("内容替换失败")).toBeVisible();
  expect(screen.getByText("内容转换正在繁忙，请稍后重试。原活动内容保留。")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "刷新游戏资料" }));
  expect(onChange).toHaveBeenCalledTimes(1);
});

it("reports expired progress and stops instead of remaining pending forever", async () => {
  calls.get.mockResolvedValueOnce(response(progress)).mockResolvedValue(missing());
  await show();
  await tick();
  expect(screen.getByRole("alert")).toHaveTextContent("替换进度已过期或被后续操作替代");
  expect(screen.getByRole("button", { name: "刷新游戏资料" })).toBeVisible();
  expect(screen.getByRole("button", { name: "替换游戏内容" })).toBeEnabled();
  await tick();
  expect(calls.get).toHaveBeenCalledTimes(2);
});

it("offers explicit retry after a read failure and aborts reads when unmounted", async () => {
  calls.get.mockRejectedValueOnce(new Error("网络暂时不可用")).mockResolvedValue(missing());
  const view = await show();
  expect(screen.getByRole("alert")).toHaveTextContent("网络暂时不可用");
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "重试读取进度" })); });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "替换游戏内容" })).toBeEnabled();
  const options = calls.get.mock.calls.at(-1)![1] as { signal: AbortSignal };
  view.unmount();
  expect(options.signal.aborted).toBe(true);
  await tick();
  expect(calls.get).toHaveBeenCalledTimes(2);
});
