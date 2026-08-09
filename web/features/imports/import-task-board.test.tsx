import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImportTaskBoard } from "./import-task-board";

describe("ImportTaskBoard", () => {
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

  it("shows rejected files as actionable exceptions", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ fileOutcomes: [{ name: "fc/8只眼.zip", disposition: "REJECTED", reasonCode: "ARCHIVE_UNSAFE" }] }), { status: 200, headers: { "Content-Type": "application/json" } })));
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
    expect(within(detail).getByRole("link", { name: "重新整理并导入" })).toHaveAttribute("href", "/admin/imports/new");
    expect(await within(detail).findByText("fc/8只眼.zip")).toBeVisible();
    expect(within(detail).getByText("归档内容或文件名未通过安全检查")).toBeVisible();
    expect(within(detail).getByText("ARCHIVE_UNSAFE")).toBeVisible();
  });
});
