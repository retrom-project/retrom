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
  expect(screen.getByText("将从这里继续")).toBeVisible();
  expect(container.querySelector("video")).toBeNull();
  await user.click(screen.getByRole("button", { name: "查看视频" }));
  expect(container.querySelector("video")).not.toBeNull();
  await user.click(screen.getByRole("button", { name: "查看最近存档" }));
  expect(container.querySelector("video")).toBeNull();
  expect(screen.getByAltText("Sudoku 最近存档")).toBeVisible();
});

it("keeps missing screenshot size visible without a dead preview action", () => {
  render(<GameDetailPreview title="Sudoku" coverUrl={null} videoUrl={null} save={{ ...save, screenshotUrl: null }} />);
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
  expect(screen.getByText("2KB")).toBeVisible();
  expect(screen.queryByText("查看大图")).not.toBeInTheDocument();
});
