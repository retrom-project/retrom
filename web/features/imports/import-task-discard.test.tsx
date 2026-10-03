import { StrictMode } from "react";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImportTaskBoard } from "./import-task-board";
import type { ImportListItem } from "./import-workflow";

const id = "01980000-0000-7000-8000-000000000101";
function item(state: ImportListItem["discard"]["state"]): ImportListItem {
  return {
    id, state: "COMPLETED", platformInstanceName: "GBA 游戏", metadataProvider: "NONE",
    totalItemCount: 1, reviewPendingItemCount: 0, failedItemCount: 0, rejectedFileCount: 0,
    version: 1, createdAtMs: 1, updatedAtMs: 2,
    discard: { kind: "IMPORT", importId: id, state, errorCode: null },
  };
}

describe("task list discard status", () => {
  afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });

  it("does not fetch per-row status when a settled list mounts or filters remount rows", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const user = userEvent.setup();
    render(<StrictMode><ImportTaskBoard initial={{ items: [item("AVAILABLE"), { ...item("COMPLETED"), id: "other" }], nextCursor: null }} /></StrictMode>);
    await user.selectOptions(screen.getByRole("combobox", { name: "任务状态" }), "ATTENTION");
    await user.selectOptions(screen.getByRole("combobox", { name: "任务状态" }), "");
    expect(screen.getAllByRole("button", { name: "丢弃" })).toHaveLength(2);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("does not let an older running-task response undo an accepted discard command", async () => {
    let release!: (response: Response) => void;
    let reads = 0;
    const detail = (state: ImportListItem["discard"]["state"]) => Response.json({
      importJobId: id, state: "RUNNING", targetPlatformInstance: { name: "GBA 游戏" }, metadataProvider: "NONE",
      counts: { total: 1, reviewPending: 0, failed: 0, rejectedFiles: 0, unresolvedRejectedFiles: 0, alreadyImportedItems: 0, alreadyImportedFiles: 0 },
      version: 1, createdAtMs: 1, updatedAtMs: 2, discard: item(state).discard,
    });
    vi.stubGlobal("fetch", vi.fn((request: Request | string) => {
      if (typeof request !== "string") {return Promise.resolve(Response.json(item("REQUESTED").discard));}
      reads++;
      return reads === 1 ? new Promise<Response>((resolve) => { release = resolve; }) : Promise.resolve(detail("REQUESTED"));
    }));
    const user = userEvent.setup();
    render(<ImportTaskBoard initial={{ items: [{ ...item("AVAILABLE"), state: "RUNNING" }], nextCursor: null }} />);
    await user.click(screen.getByRole("button", { name: "丢弃" }));
    await user.click(screen.getByRole("button", { name: "确认丢弃" }));
    expect(await screen.findByRole("button", { name: "正在丢弃…" })).toBeDisabled();
    await act(async () => { release(detail("AVAILABLE")); });
    expect(screen.getByRole("button", { name: "正在丢弃…" })).toBeDisabled();
  });

  it.each(["COMPLETED", "FAILED"] as const)("restores pending discard, recovers a failed read and stops polling at %s", async (terminal) => {
    vi.useFakeTimers();
    let settled = false;
    const fetch = vi.fn(async (url: string) => {
      expect(url).toBe(`/api/v1/admin/imports/${id}`);
      return Response.json({
        importJobId: id, state: "COMPLETED", targetPlatformInstance: { name: "GBA 游戏" }, metadataProvider: "NONE",
        counts: { total: 1, reviewPending: 0, failed: 0, rejectedFiles: 0, unresolvedRejectedFiles: 0, alreadyImportedItems: 0, alreadyImportedFiles: 0 },
        version: 1, createdAtMs: 1, updatedAtMs: 2,
        discard: { ...item(settled ? terminal : "REQUESTED").discard, errorCode: settled && terminal === "FAILED" ? "IMPORT_BATCH_DISCARD_RELEASE_FAILED" : null },
      });
    });
    fetch.mockResolvedValueOnce(new Response(null, { status: 503 }));
    vi.stubGlobal("fetch", fetch);
    render(<ImportTaskBoard initial={{ items: [item("REQUESTED")], nextCursor: null }} />);
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(screen.getByRole("button", { name: "正在丢弃…" })).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent("任务进度暂时无法读取，正在重试");
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000); });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    settled = true;
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000); });
    if (terminal === "FAILED") {
      expect(screen.getByRole("button", { name: "重试丢弃" })).toBeEnabled();
      expect(screen.getByRole("alert")).toHaveTextContent("源文件清理任务失败");
    } else {
      expect(screen.getByRole("button", { name: "丢弃" })).toBeDisabled();
      expect(screen.getByRole("status")).toHaveTextContent("未发布内容已丢弃");
    }
    expect(fetch).toHaveBeenCalledTimes(3);
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    expect(fetch).toHaveBeenCalledTimes(3);
  });
});
