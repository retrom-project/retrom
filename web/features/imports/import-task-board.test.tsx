import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImportTaskBoard } from "./import-task-board";

describe("ImportTaskBoard", () => {
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

  it("shows rejected files as actionable exceptions", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ fileOutcomes: [{ uploadFileId: "file-1", name: "fc/8只眼.zip", sizeBytes: 42, disposition: "REJECTED", reasonCode: "ARCHIVE_UNSAFE", resolution: null }] }), { status: 200, headers: { "Content-Type": "application/json" } })));
    render(<ImportTaskBoard initial={{ items: [{
      id: "import-1",
      state: "PARTIAL_FAILURE",
      platformInstanceName: "NES 游戏",
      metadataProvider: "HASHEOUS",
      totalItemCount: 2,
      reviewPendingItemCount: 0,
      failedItemCount: 0,
      rejectedFileCount: 7,
      version: 1,
      createdAtMs: 1,
      updatedAtMs: 2,
    }], nextCursor: null }} />);

    const card = screen.getByRole("heading", { name: /NES 游戏/ }).closest("article");
    expect(card).not.toBeNull();
    expect(within(card!).getByText("7 异常")).toBeVisible();
    expect(within(card!).getByText("7 个文件未被接受")).toBeVisible();

    expect(within(card!).queryByRole("button", { name: "查看 7 个异常" })).not.toBeInTheDocument();
    await user.click(within(card!).getByRole("button", { name: "7 异常" }));
    const detail = screen.getByRole("region", { name: "NES 游戏 阶段详情" });
    expect(within(detail).getByText(/7 个文件未被接受/)).toBeVisible();
    expect(within(detail).getByRole("link", { name: "重新配置并导入" })).toHaveAttribute("href", "/admin/imports/new?fromImportJobId=import-1");
    expect(await within(detail).findByText("fc/8只眼.zip")).toBeVisible();
    expect(within(detail).getByText("归档内容或文件名未通过安全检查")).toBeVisible();
    expect(within(detail).getByText("ARCHIVE_UNSAFE")).toHaveAttribute("title", expect.stringContaining("不安全路径"));
  });

  it("keeps rejected-file details available when the same batch also has reviews", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ fileOutcomes: [{ uploadFileId: "bad-1", name: "unexpected.txt", sizeBytes: 42, disposition: "REJECTED", reasonCode: "UNSUPPORTED_CONTENT_FORMAT", resolution: null }] }), { status: 200, headers: { "Content-Type": "application/json" } })));
    render(<ImportTaskBoard initial={{ items: [{
      id: "mixed-import", state: "PARTIAL_FAILURE", platformInstanceName: "MAME 2003 Plus 游戏", metadataProvider: "HASHEOUS",
      totalItemCount: 1, reviewPendingItemCount: 1, failedItemCount: 0, rejectedFileCount: 5, unresolvedRejectedFileCount: 5,
      version: 1, createdAtMs: 1, updatedAtMs: 2,
    }], nextCursor: null }} />);

    const card = screen.getByRole("heading", { name: /MAME 2003 Plus 游戏/ }).closest("article");
    expect(within(card!).getByRole("link", { name: "查看待审核" })).toHaveAttribute("href", "/admin/reviews?importJobId=mixed-import");
    await user.click(within(card!).getByRole("button", { name: "5 异常" }));
    expect(await screen.findByText("unexpected.txt")).toBeVisible();
    expect(screen.getByRole("link", { name: "重新配置并导入" })).toHaveAttribute("href", "/admin/imports/new?fromImportJobId=mixed-import");
  });

  it("shows files skipped because an active game already uses the same content", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({
      fileOutcomes: [{ uploadFileId: "file-1", name: "renamed-copy.gba", sizeBytes: 42, disposition: "ALREADY_IMPORTED", reasonCode: "ALREADY_IMPORTED", resolution: null }],
      alreadyImportedMatches: [{ importItemId: "item-1", contentIdentityDigest: "a".repeat(64), existingGame: { id: "game-1", title: "Already there", platformInstanceId: "platform-1", platformInstanceName: "GBA 游戏" } }],
    }), { status: 200, headers: { "Content-Type": "application/json" } })));
    render(<ImportTaskBoard initial={{ items: [{
      id: "import-duplicate",
      state: "COMPLETED",
      platformInstanceName: "GBA 游戏",
      metadataProvider: "NONE",
      totalItemCount: 1,
      reviewPendingItemCount: 0,
      failedItemCount: 0,
      rejectedFileCount: 0,
      alreadyImportedItemCount: 1,
      alreadyImportedFileCount: 1,
      version: 2,
      createdAtMs: 1,
      updatedAtMs: 2,
    }], nextCursor: null }} />);

    const card = screen.getByRole("heading", { name: /GBA 游戏/ }).closest("article");
    expect(within(card!).getByText(/已跳过 1 个已导入条目/)).toBeVisible();
    await user.click(within(card!).getByRole("button", { name: "查看已跳过" }));
    const detail = screen.getByRole("region", { name: "GBA 游戏 阶段详情" });
    expect(await within(detail).findByText("renamed-copy.gba")).toBeVisible();
    expect(within(detail).getByText("游戏文件已经导入")).toBeVisible();
    expect(within(detail).getByRole("link", { name: "查看已有游戏" })).toHaveAttribute("href", "/games/game-1");
  });

  it("polls every loaded non-terminal task and stops after their terminal updates", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const id = String(input).split("/").at(-1) ?? "";
      return new Response(JSON.stringify({
        importJobId: id, state: "COMPLETED", metadataProvider: "NONE", targetPlatformInstance: { id: `platform-${id}`, name: `${id} 游戏` },
        counts: { total: 1, reviewPending: 0, failed: 0, rejectedFiles: 0, unresolvedRejectedFiles: 0, alreadyImportedItems: 0, alreadyImportedFiles: 0 },
        fileOutcomes: [], version: 2, createdAtMs: 1, updatedAtMs: 3,
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    vi.stubGlobal("fetch", fetchMock);
    render(<ImportTaskBoard initial={{ items: ["running-1", "running-2"].map((id) => ({
      id, state: "RUNNING", platformInstanceName: `${id} 游戏`, metadataProvider: "NONE", totalItemCount: 1,
      reviewPendingItemCount: 0, failedItemCount: 0, rejectedFileCount: 0, version: 1, createdAtMs: 1, updatedAtMs: 2,
    })), nextCursor: null }} />);

    await waitFor(() => expect(screen.getAllByText("已完成", { selector: ".status" })).toHaveLength(2));
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/imports/running-1", expect.objectContaining({ cache: "no-store" }));
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/imports/running-2", expect.objectContaining({ cache: "no-store" }));
    const completedCalls = fetchMock.mock.calls.length;
    await new Promise((resolve) => window.setTimeout(resolve, 1_100));
    expect(fetchMock).toHaveBeenCalledTimes(completedCalls);
  });
});
