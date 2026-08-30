import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { SaveItem } from "@/features/saves/save-library";
import { GameDetailSaves } from "./game-detail-saves";

vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: vi.fn() }) }));

const nowMs = Date.parse("2026-08-08T12:00:00+08:00");

function makeSave(index: number): SaveItem {
  return {
    saveStateId: `save-${index}`,
    gameId: "game-1",
    gameTitle: "1943: The Battle of Midway",
    name: `手动存档 ${index}`,
    version: 1,
    createdAtMs: nowMs - index * 60_000,
    activeDurationMs: 60_000,
    sizeBytes: 1024,
    screenshotUrl: `/api/v1/saves/save-${index}/screenshot`,
    core: { id: index === 5 ? "mame2003_plus" : "fbneo", name: index === 5 ? "MAME 2003 Plus" : "FinalBurn Neo" },
    platform: { id: "arcade", name: "Arcade" },
    platformInstance: { id: "instance-1", name: "FBNeo 游戏" },
    availability: { status: "AVAILABLE", reasons: [] },
  };
}

describe("GameDetailSaves", () => {
  afterEach(cleanup);

  it("keeps three recent saves in the page and exposes every save in a drawer", async () => {
    const user = userEvent.setup();
    const saves = Array.from({ length: 6 }, (_, index) => makeSave(index));
    const { container } = render(<GameDetailSaves gameId="game-1" gameTitle="1943: The Battle of Midway" saves={saves} nowMs={nowMs} />);

    expect(container.querySelectorAll(".game-detail-save-card")).toHaveLength(3);
    expect(screen.getByText("共 6 份")).toBeInTheDocument();
    const recentCard = container.querySelector<HTMLElement>(".game-detail-save-card");
    expect(recentCard).not.toBeNull();
    expect(within(recentCard!).getByText("最近存档")).toBeInTheDocument();
    expect(within(recentCard!).getByText("保存位置")).toBeInTheDocument();
    expect(within(recentCard!).getByText("运行核心")).toBeInTheDocument();
    expect(within(recentCard!).getByText("当时已游玩")).toBeInTheDocument();
    expect(within(recentCard!).getByRole("button", { name: "▶ 从这里继续" })).toBeInTheDocument();
    const drawerTrigger = screen.getByRole("button", { name: "查看全部存档" });
    await user.click(drawerTrigger);

    const drawer = screen.getByRole("dialog", { name: "全部存档" });
    expect(within(drawer).getAllByRole("article")).toHaveLength(6);
    expect(within(drawer).getAllByRole("button", { name: "▶ 继续" })).toHaveLength(6);
    expect(within(drawer).getByText("1943: The Battle of Midway · 共 6 份")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "全部存档" })).not.toBeInTheDocument();
    expect(drawerTrigger).toHaveFocus();
  });

  it("opens a ratio-preserving screenshot preview and closes it with the explicit action", async () => {
    const user = userEvent.setup();
    render(<GameDetailSaves gameId="game-1" gameTitle="1943: The Battle of Midway" saves={[makeSave(0)]} nowMs={nowMs} />);

    const previewTrigger = screen.getByRole("button", { name: /预览.*的存档截图/ });
    await user.click(previewTrigger);
    const preview = screen.getByRole("dialog", { name: "存档截图预览" });
    expect(within(preview).getByAltText("1943: The Battle of Midway 存档截图完整预览")).toBeInTheDocument();
    await user.click(within(preview).getByRole("button", { name: "关闭" }));
    expect(screen.queryByRole("dialog", { name: "存档截图预览" })).not.toBeInTheDocument();
    expect(previewTrigger).toHaveFocus();
  });

  it("does not offer an image preview when the save has no screenshot", () => {
    render(<GameDetailSaves gameId="game-1" gameTitle="1943: The Battle of Midway" saves={[makeSave(0), { ...makeSave(1), screenshotUrl: null }]} nowMs={nowMs} />);

    expect(screen.getByRole("button", { name: /的存档没有截图/ })).toBeDisabled();
    expect(screen.getByRole("img", { name: "存档截图无预览图" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /从这里继续|恢复此存档/ })).toHaveLength(2);
  });
});
