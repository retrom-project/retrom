import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import type { SaveItem } from "@/features/saves/save-library";
import { GameDetailPreview } from "./game-detail-preview";

vi.mock("./game-detail-media", () => ({ GameDetailMedia: () => <video aria-label="视频预览" /> }));
afterEach(cleanup);
const save: SaveItem = {
  saveStateId: "save", gameId: "game", gameTitle: "Sudoku", name: "手动存档", version: 1,
  createdAtMs: 1000, lastSyncedAtMs: 2000, activeDurationMs: 60000, sizeBytes: 2048,
  screenshotUrl: "/save.png", core: { id: "mgba", name: "mGBA" }, platform: { id: "gba", name: "GBA" },
  platformInstance: { id: "directory", name: "GBA" }, availability: { status: "AVAILABLE", reasons: [] },
};

it("prioritizes the save, mounts video only when selected and restores save preview", async () => {
  const user = userEvent.setup();
  const { container } = render(<GameDetailPreview title="Sudoku" coverUrl={null} videoUrl="/video.mp4" save={save} />);
  expect(screen.getByRole("tab", { name: "最近存档" })).toHaveAttribute("aria-selected", "true");
  expect(screen.getByRole("tabpanel", { name: "最近存档" })).toBeVisible();
  for (const tab of screen.getAllByRole("tab")) {
    expect(document.getElementById(tab.getAttribute("aria-controls")!)).toBeInTheDocument();
  }
  expect(container.querySelector("video")).toBeNull();
  await user.click(screen.getByRole("tab", { name: "视频预览" }));
  expect(screen.getByRole("tabpanel", { name: "视频预览" })).toBeVisible();
  expect(container.querySelector("video")).not.toBeNull();
  await user.click(screen.getByRole("tab", { name: "最近存档" }));
  expect(container.querySelector("video")).toBeNull();
  expect(screen.getByAltText("Sudoku 最近存档")).toBeVisible();
});

it("supports keyboard preview switching with one tab in the focus order", async () => {
  const user = userEvent.setup();
  render(<GameDetailPreview title="Sudoku" coverUrl={null} videoUrl="/video.mp4" save={save} />);
  const saved = screen.getByRole("tab", { name: "最近存档" });
  const video = screen.getByRole("tab", { name: "视频预览" });
  expect(video).toHaveAttribute("tabindex", "-1");
  saved.focus();
  await user.keyboard("{ArrowRight}");
  expect(video).toHaveFocus();
  expect(video).toHaveAttribute("aria-selected", "true");
  await user.keyboard("{Home}");
  expect(saved).toHaveFocus();
  expect(screen.getByRole("tabpanel", { name: "最近存档" })).toBeVisible();
  await user.keyboard("{End}{ArrowLeft}");
  expect(saved).toHaveFocus();
});

it.each([true, false])("shows a single heading without tabs when only one preview exists (save: %s)", (hasSave) => {
  render(<GameDetailPreview title="Sudoku" coverUrl={null} videoUrl={hasSave ? null : "/video.mp4"} save={hasSave ? save : null} />);
  expect(screen.queryByRole("tablist")).not.toBeInTheDocument();
  expect(screen.getByText(hasSave ? "将从这里继续" : "视频预览")).toBeVisible();
  if (hasSave) {expect(screen.getByAltText("Sudoku 最近存档")).toBeVisible();}
  else {expect(screen.getByLabelText("视频预览")).toBeVisible();}
});

it("keeps missing screenshot size visible without a dead preview action", () => {
  render(<GameDetailPreview title="Sudoku" coverUrl={null} videoUrl={null} save={{ ...save, screenshotUrl: null }} />);
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
  expect(screen.getByText("2KB")).toBeVisible();
  expect(screen.queryByText("查看大图")).not.toBeInTheDocument();
});
