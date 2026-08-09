import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImportTaskBoard } from "./import-task-board";

describe("ImportTaskBoard", () => {
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

  it("shows rejected files as actionable exceptions", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ fileOutcomes: [{ uploadFileId: "file-1", name: "fc/8只眼.zip", sizeBytes: 42, disposition: "REJECTED", reasonCode: "ARCHIVE_UNSAFE", resolution: null }] }), { status: 200, headers: { "Content-Type": "application/json" } })));
    render(<ImportTaskBoard items={[{
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
    }]} />);

    const card = screen.getByRole("heading", { name: /NES 游戏/ }).closest("article");
    expect(card).not.toBeNull();
    expect(within(card!).getByText("7 异常")).toBeVisible();
    expect(within(card!).getByText("7 个文件未被接受")).toBeVisible();

    await user.click(within(card!).getByRole("button", { name: "处理问题" }));
    const detail = screen.getByRole("region", { name: "NES 游戏 阶段详情" });
    expect(within(detail).getByText(/7 个文件未被接受/)).toBeVisible();
    expect(within(detail).getByRole("link", { name: "重新配置并导入" })).toHaveAttribute("href", "/admin/imports/new?fromImportJobId=import-1");
    expect(await within(detail).findByText("fc/8只眼.zip")).toBeVisible();
    expect(within(detail).getByText("归档内容或文件名未通过安全检查")).toBeVisible();
    expect(within(detail).getByText("ARCHIVE_UNSAFE")).toBeVisible();
  });

  it("shows files skipped because an active game already uses the same content", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({
      fileOutcomes: [{ uploadFileId: "file-1", name: "renamed-copy.gba", sizeBytes: 42, disposition: "ALREADY_IMPORTED", reasonCode: "ALREADY_IMPORTED", resolution: null }],
      alreadyImportedMatches: [{ importItemId: "item-1", contentIdentityDigest: "a".repeat(64), existingGame: { id: "game-1", title: "Already there", platformInstanceId: "platform-1", platformInstanceName: "GBA 游戏" } }],
    }), { status: 200, headers: { "Content-Type": "application/json" } })));
    render(<ImportTaskBoard items={[{
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
    }]} />);

    const card = screen.getByRole("heading", { name: /GBA 游戏/ }).closest("article");
    expect(within(card!).getByText(/已跳过 1 个已导入条目/)).toBeVisible();
    await user.click(within(card!).getByRole("button", { name: "查看已跳过" }));
    const detail = screen.getByRole("region", { name: "GBA 游戏 阶段详情" });
    expect(await within(detail).findByText("renamed-copy.gba")).toBeVisible();
    expect(within(detail).getByText("游戏文件已经导入")).toBeVisible();
    expect(within(detail).getByRole("link", { name: "查看已有游戏" })).toHaveAttribute("href", "/games/game-1");
  });
});
