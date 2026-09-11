import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ScrollableGameDescription } from "./scrollable-game-description";

afterEach(() => {cleanup(); vi.useRealTimers();});

it("keeps the full description in a keyboard-accessible region", () => {
  const description = "完整简介\n".repeat(1000);
  render(<ScrollableGameDescription description={description} />);
  const region = screen.getByRole("region", { name: "游戏简介" });
  expect(region.textContent).toBe(description);
  expect(region).toHaveAttribute("tabindex", "0");
  expect(region).not.toHaveClass("is-scrolling");
});

it("reveals the thin scrollbar during scroll and hides it after scrolling stops", async () => {
  vi.useFakeTimers();
  const { unmount } = render(<ScrollableGameDescription description="游戏简介" />);
  const region = screen.getByRole("region", { name: "游戏简介" });
  fireEvent.scroll(region);
  expect(region).toHaveClass("is-scrolling");
  await act(() => vi.advanceTimersByTime(600));
  fireEvent.scroll(region);
  await act(() => vi.advanceTimersByTime(600));
  expect(region).toHaveClass("is-scrolling");
  await act(() => vi.advanceTimersByTime(100));
  expect(region).not.toHaveClass("is-scrolling");
  fireEvent.scroll(region);
  unmount();
  expect(vi.getTimerCount()).toBe(0);
});

it("shows the missing-description message for whitespace only", () => {
  render(<ScrollableGameDescription description={" \n "} />);
  expect(screen.getByText("尚未填写游戏简介。")).toBeVisible();
});
