import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ListFilters } from "./list-filters";

const router = vi.hoisted(() => ({ replace: vi.fn() }));

vi.mock("next/navigation", () => ({ useRouter: () => router }));

afterEach(() => {
  cleanup();
  router.replace.mockReset();
});

describe("ListFilters", () => {
  it("only offers clear when a filter is active", () => {
    const { rerender } = render(<ListFilters action="/library" placeholder="search" values={{ q: "" }} />);
    expect(screen.queryByRole("link", { name: "清除全部" })).not.toBeInTheDocument();

    rerender(<ListFilters action="/library" placeholder="search" values={{ q: "metroid" }} resultCount={3} />);
    expect(screen.getByRole("link", { name: "清除全部" })).toHaveAttribute("href", "/library");
    expect(screen.getByRole("link", { name: "移除关键词：metroid" })).toHaveAttribute("href", "/library");
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("shows only directories belonging to the selected platform and clears a stale directory", async () => {
    const user = userEvent.setup();
    render(<ListFilters action="/library" placeholder="search" values={{}} filters={[
      { name: "platformId", label: "游戏平台", options: [{ value: "", label: "所有平台" }, { value: "gba", label: "GBA" }, { value: "arcade", label: "Arcade" }] },
      { name: "platformInstanceId", label: "游戏目录", dependsOn: "platformId", options: [{ value: "", label: "所有目录" }, { value: "gba-games", label: "GBA 游戏", parentValue: "gba" }, { value: "arcade-games", label: "街机游戏", parentValue: "arcade" }] },
    ]} />);

    const platform = screen.getByRole("combobox", { name: "游戏平台" });
    const directory = screen.getByRole("combobox", { name: "游戏目录" });
    await user.selectOptions(platform, "gba");
    expect(screen.getByRole("option", { name: "GBA 游戏" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "街机游戏" })).not.toBeInTheDocument();
    await user.selectOptions(directory, "gba-games");
    await user.selectOptions(platform, "arcade");
    expect(directory).toHaveValue("");
    expect(screen.getByRole("option", { name: "街机游戏" })).toBeInTheDocument();
  });

  it("preserves an exact game filter while allowing the user to remove it", () => {
    render(<ListFilters action="/saves" placeholder="search" values={{ gameId: "game-1", availability: "AVAILABLE" }} fixedFilters={[{ name: "gameId", value: "game-1", label: "游戏：Metal Slug" }]} filters={[{ name: "availability", label: "存档状态", options: [{ value: "AVAILABLE", label: "可以继续" }] }]} />);
    expect(screen.getByDisplayValue("game-1")).toHaveAttribute("type", "hidden");
    expect(screen.getByRole("link", { name: "移除游戏：Metal Slug" })).toHaveAttribute("href", "/saves?availability=AVAILABLE");
  });

  it("updates the routed results without a native document submission", async () => {
    const user = userEvent.setup();
    render(<ListFilters action="/admin/games" placeholder="search" values={{}} filters={[
      { name: "status", label: "游戏状态", options: [{ value: "", label: "全部" }, { value: "PUBLISHED", label: "用户可见" }] },
    ]} />);

    await user.type(screen.getByRole("textbox", { name: "搜索" }), " 1942 ");
    await user.selectOptions(screen.getByRole("combobox", { name: "游戏状态" }), "PUBLISHED");
    await user.click(screen.getByRole("button", { name: "搜索" }));

    expect(router.replace).toHaveBeenCalledWith("/admin/games?q=1942&status=PUBLISHED", { scroll: false });
  });
});
