import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { HorizontalRail, PlatformRail, type HomePlatform } from "./home-rails";

const platforms: HomePlatform[] = [
  { id: "arcade", name: "街机", gameCount: 3, playCount: 4 },
  { id: "gba", name: "Game Boy Advance", gameCount: 2, playCount: 1 },
];

describe("home rails", () => {
  beforeEach(() => localStorage.clear());

  it("moves a horizontally overflowing rail with the vertical mouse wheel", () => {
    const { container } = render(<HorizontalRail label="recent"><span>item</span></HorizontalRail>);
    const rail = container.querySelector(".home-horizontal-rail") as HTMLDivElement;
    Object.defineProperties(rail, {
      clientWidth: { configurable: true, value: 300 },
      scrollWidth: { configurable: true, value: 900 },
      scrollLeft: { configurable: true, writable: true, value: 0 },
    });
    const event = new WheelEvent("wheel", { bubbles: true, cancelable: true, deltaY: 120 });
    rail.dispatchEvent(event);
    expect(rail.scrollLeft).toBe(120);
    expect(event.defaultPrevented).toBe(true);
  });

  it("moves a pinned platform to the front and persists the choice", async () => {
    const user = userEvent.setup();
    render(<PlatformRail platforms={platforms} />);
    expect(screen.getAllByRole("article").map((item) => item.textContent)).toEqual([
      expect.stringContaining("街机"),
      expect.stringContaining("Game Boy Advance"),
    ]);
    await user.click(screen.getByRole("button", { name: "置顶“Game Boy Advance”" }));
    expect(screen.getAllByRole("article")[0]).toHaveTextContent("Game Boy Advance");
    expect(localStorage.getItem("retrom:pinned-home-platforms")).toBe('["gba"]');
    fireEvent.click(screen.getByRole("button", { name: "取消置顶“Game Boy Advance”" }));
    await waitFor(() => expect(localStorage.getItem("retrom:pinned-home-platforms")).toBe("[]"));
  });
});
