import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { ReviewScanProgress } from "./review-scan-progress";

const calls = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("@/lib/api/client", async (original) => ({ ...await original<typeof ApiClient>(), api: { GET: calls.get } }));
const pending: Schema<"ScanProgress"> = {
  id: "wanted", scanType: "game", status: "pending", totalKnown: false,
  totalCount: 0, processedCount: 0, importedCount: 0, skippedCount: 0,
  failedCount: 0, error: null, createdAtMs: 1, updatedAtMs: 1,
};
const response = (items: Schema<"ScanProgress">[]) => ({ data: { items }, response: new Response() });
beforeEach(() => { vi.resetAllMocks(); vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); });
async function show(onChange = vi.fn()) {
  await act(async () => { render(<ReviewScanProgress scanId="wanted" onChange={onChange} />); });
  return onChange;
}
async function poll() { await act(() => vi.advanceTimersByTimeAsync(3000)); }

it("finds the exact game task after the first page and renders unknown totals as indeterminate", async () => {
  calls.get.mockResolvedValueOnce(response(Array.from({ length: 100 }, (_, id) => ({ ...pending, id: `other-${id}` }))))
    .mockResolvedValueOnce(response([pending]));
  await show();
  expect(calls.get.mock.calls.map((call) => call[1].params.query)).toEqual([{ limit: 100, offset: 0 }, { limit: 100, offset: 100 }]);
  expect(screen.getByRole("progressbar")).not.toHaveAttribute("value");
  expect(screen.getByRole("status")).toHaveTextContent("等待扫描 · 已扫描 0 / 总数发现中");
});

it("refreshes the queue only when progress changes, and stops polling at completion", async () => {
  const running = { ...pending, status: "running" as const, totalKnown: true, totalCount: 3, processedCount: 1, importedCount: 1 };
  calls.get.mockResolvedValueOnce(response([running])).mockResolvedValueOnce(response([{ ...running }]))
    .mockResolvedValueOnce(response([{ ...running, processedCount: 2, importedCount: 2 }]))
    .mockResolvedValueOnce(response([{ ...running, status: "completed", processedCount: 3, importedCount: 3 }]));
  const changed = await show();
  expect(changed).toHaveBeenCalledTimes(1);
  await poll();
  expect(changed).toHaveBeenCalledTimes(1);
  await poll();
  expect(changed).toHaveBeenCalledTimes(2);
  expect(screen.getByRole("progressbar")).toHaveAttribute("value", "2");
  await poll();
  expect(changed).toHaveBeenCalledTimes(3);
  expect(screen.getByRole("status")).toHaveTextContent("扫描已完成");
  await poll();
  expect(calls.get).toHaveBeenCalledTimes(4);
});

it.each(["completed", "failed", "cancelled"] as const)("keeps %s outcomes and errors visible without further polling", async (status) => {
  calls.get.mockResolvedValue(response([{ ...pending, status, totalKnown: true, totalCount: 1, processedCount: 1, failedCount: 1, error: "包内容无法读取" }]));
  await show();
  expect(screen.getByRole("alert")).toHaveTextContent("包内容无法读取");
  if (status === "completed") { expect(screen.getByRole("status")).toHaveTextContent("扫描完成，部分游戏失败"); }
  await poll();
  expect(calls.get).toHaveBeenCalledTimes(1);
});

it("stops after a read error and allows retry without selecting another task", async () => {
  calls.get.mockRejectedValueOnce(new Error("暂时不可用")).mockResolvedValueOnce(response([{ ...pending, status: "completed" }]));
  await show();
  expect(screen.getByRole("alert")).toHaveTextContent("无法读取扫描进度：暂时不可用");
  await poll();
  expect(calls.get).toHaveBeenCalledTimes(1);
  await act(async () => fireEvent.click(screen.getByRole("button", { name: "重新读取进度" })));
  expect(screen.getByRole("status")).toHaveTextContent("扫描已完成");
});

it.each([{ items: [] }, { items: [{ ...pending, scanType: "bios" as const }] }])("does not display a missing or BIOS task as game progress", async ({ items }) => {
  calls.get.mockResolvedValue(response(items));
  await show();
  expect(screen.getByRole("status")).toHaveTextContent("未找到这项扫描");
  expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: "查看来源扫描" })).toHaveAttribute("href", "/admin/imports/server");
  await poll();
  expect(calls.get).toHaveBeenCalledTimes(1);
});

it("bounds historical lookup and stops rather than repeatedly querying a missing task", async () => {
  calls.get.mockResolvedValue(response(Array.from({ length: 100 }, (_, id) => ({ ...pending, id: `other-${id}` }))));
  await show();
  expect(calls.get).toHaveBeenCalledTimes(10);
  await poll();
  expect(calls.get).toHaveBeenCalledTimes(10);
  expect(screen.getByRole("button", { name: "重新读取进度" })).toBeEnabled();
});

it("does not paint fake completion or NaN for an empty completed scan", async () => {
  calls.get.mockResolvedValue(response([{ ...pending, status: "completed", totalKnown: true }]));
  await show();
  expect(screen.getByRole("status")).toHaveTextContent("扫描已完成 · 已扫描 0 / 0");
  expect(screen.getByRole("progressbar")).toHaveAttribute("value", "0");
  expect(screen.getByRole("progressbar")).toHaveAttribute("max", "1");
});
