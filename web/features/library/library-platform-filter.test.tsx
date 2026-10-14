import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { Directory } from "@/lib/api/types";
import { LibraryPlatformFilter } from "./library-platform-filter";

afterEach(cleanup);
function directory(id: string, platformId: string, gameCount: number, enabled = true): Directory {
  return { id, platformId, gameCount, enabled, name: id, slug: id, description: "", defaultCoreId: "core", coreIds: ["core"], version: 1 };
}
const platforms = ["NES", "步步高 RPG", "Arcade", "SNES", "Atari"].map(name => ({ id: name, name }));
const directories = [directory("a", "Arcade", 800), directory("b", "Arcade", 63), directory("c", "NES", 1001), directory("d", "步步高 RPG", 2), directory("empty", "SNES", 0), directory("off", "Arcade", 100, false), directory("off-only", "Atari", 5, false)];

it("adds enabled directory counts, hides empty platforms, sorts by pinyin, and preserves selection IDs", () => {
  const select = vi.fn();
  render(<LibraryPlatformFilter platforms={platforms} directories={directories} selected="Arcade" onSelect={select} />);
  expect(screen.getAllByRole("button").map(button => button.textContent)).toEqual(["全部平台", "Arcade(863)", "步步高 RPG(2)", "NES(999+)"]);
  expect(screen.getByRole("button", { name: "Arcade(863)" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByRole("button", { name: "NES(999+)" })).toHaveAttribute("title", "NES：1001 款已发布游戏");
  fireEvent.click(screen.getByRole("button", { name: "步步高 RPG(2)" }));
  expect(select).toHaveBeenCalledWith("步步高 RPG");
});

it.each([999, 1000, 1001])("shows the requested count boundary for %s games", count => {
  render(<LibraryPlatformFilter platforms={platforms} directories={[directory("a", "NES", count)]} selected="" onSelect={vi.fn()} />);
  expect(screen.getByRole("button", { name: `NES(${count > 1000 ? "999+" : count})` })).toBeVisible();
});

it("does not mislabel global library totals as personal favorite counts", () => {
  render(<LibraryPlatformFilter platforms={platforms} directories={directories} selected="" onSelect={vi.fn()} showCounts={false} />);
  expect(screen.getAllByRole("button").map(button => button.textContent)).toEqual(["全部平台", "Arcade", "步步高 RPG", "NES"]);
});
