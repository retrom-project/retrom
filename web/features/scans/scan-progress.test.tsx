import { act, cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { ScanProgressList } from "./scan-progress";

const calls = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("@/lib/api/client", async (original) => ({ ...await original<typeof ApiClient>(), api: { GET: calls.get, POST: calls.post } }));
const running: Schema<"ScanProgress"> = {
  id: "scan", scanType: "bios", status: "running", totalKnown: false,
  totalCount: 0, processedCount: 0, importedCount: 0, skippedCount: 0,
  failedCount: 0, error: null, createdAtMs: 1, updatedAtMs: 1,
};
const response = (items: Schema<"ScanProgress">[]) => ({ data: { items }, response: new Response() });
beforeEach(() => { vi.resetAllMocks(); vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); });
async function show() { await act(async () => { render(<ScanProgressList />); }); }
async function advance(ms = 3000) { await act(() => vi.advanceTimersByTimeAsync(ms)); }

it.each(([[], [{ ...running, status: "completed" }], [{ ...running, status: "failed" }], [{ ...running, status: "cancelled" }]] as Schema<"ScanProgress">[][]).map((items) => ({ items })))(
  "does not continuously poll an empty or terminal list (%j)", async ({ items }) => {
    calls.get.mockResolvedValue(response(items));
    await show();
    await advance(12000);
    expect(calls.get).toHaveBeenCalledTimes(1);
  },
);

it("waits for each response before polling again and stops when the task finishes", async () => {
  let finish!: (value: ReturnType<typeof response>) => void;
  calls.get.mockResolvedValueOnce(response([running])).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  await show();
  await advance();
  await advance(12000);
  expect(calls.get).toHaveBeenCalledTimes(2);
  await act(async () => finish(response([{ ...running, status: "completed" }])));
  await advance(12000);
  expect(calls.get).toHaveBeenCalledTimes(2);
  expect(screen.getByText("已完成")).toBeVisible();
});

it("pauses polling while hidden and refreshes once on returning to the page", async () => {
  calls.get.mockResolvedValue(response([running]));
  await show();
  const visibility = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
  fireEvent(document, new Event("visibilitychange"));
  await advance(9000);
  expect(calls.get).toHaveBeenCalledTimes(1);
  visibility.mockReturnValue("visible");
  await act(async () => fireEvent(document, new Event("visibilitychange")));
  expect(calls.get).toHaveBeenCalledTimes(2);
});

it("stops automatic requests after failure and exposes an explicit retry", async () => {
  calls.get.mockRejectedValueOnce(new Error("网络暂不可用")).mockResolvedValue(response([running]));
  await show();
  await advance(12000);
  expect(calls.get).toHaveBeenCalledTimes(1);
  await act(async () => fireEvent.click(screen.getByRole("button", { name: "重试" })));
  expect(calls.get).toHaveBeenCalledTimes(2);
});

it("refreshes a newly created scan after an idle list and resumes polling", async () => {
  calls.get.mockResolvedValueOnce(response([])).mockResolvedValue(response([running]));
  const view = render(<ScanProgressList revision={0} />);
  await act(async () => {});
  await advance(6000);
  expect(calls.get).toHaveBeenCalledTimes(1);
  await act(async () => view.rerender(<ScanProgressList revision={1} />));
  expect(calls.get).toHaveBeenCalledTimes(2);
  await advance();
  expect(calls.get).toHaveBeenCalledTimes(3);
  view.unmount();
  await advance(6000);
  expect(calls.get).toHaveBeenCalledTimes(3);
});
