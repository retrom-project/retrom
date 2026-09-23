import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { categoryForPlatform, matchesDirectory, type DirectoryChoice } from "./directory-categories";
import { DirectorySelector } from "./directory-selector";

const directories: DirectoryChoice[] = [
  { id: "fbneo", name: "FBNeo 游戏", platformId: "arcade", platformName: "Arcade", coreName: "FinalBurn Neo" },
  { id: "mame", name: "MAME 游戏", platformId: "arcade", platformName: "Arcade", coreName: "MAME 2003 Plus" },
  { id: "gba", name: "GBA 游戏", platformId: "gba", platformName: "Game Boy Advance", coreName: "mGBA" },
  { id: "custom", name: "我的项目", platformId: "future-platform", platformName: "未来平台", coreName: "Custom Core" },
];

beforeEach(() => window.localStorage.clear());
afterEach(() => cleanup());

describe("directory categories", () => {
  it("classifies every declared platform and gives future platforms a fallback", () => {
    const source = readFileSync(resolve(process.cwd(), "../data/runtime-target-bindings/v1/catalog.json"), "utf8");
    const catalog = JSON.parse(source) as { definitions: { platforms: Array<{ id: string }> } };
    expect(catalog.definitions.platforms.map((platform) => platform.id).filter((id) => categoryForPlatform(id) === "other")).toEqual([]);
    expect(categoryForPlatform("future-platform")).toBe("other");
  });

  it("matches directory, platform and core names without changing order", () => {
    expect(directories.filter((directory) => matchesDirectory(directory, "arcade neo")).map((directory) => directory.id)).toEqual(["fbneo"]);
    expect(directories.filter((directory) => matchesDirectory(directory, "mgba")).map((directory) => directory.id)).toEqual(["gba"]);
    expect(directories.filter((directory) => matchesDirectory(directory, "游戏")).map((directory) => directory.id)).toEqual(["fbneo", "mame", "gba"]);
  });
});

describe("DirectorySelector", () => {
  it("browses by platform type and selects a directory without a default", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<DirectorySelector directories={directories} selectedId="" onSelect={onSelect} reconfiguring={false} />);
    const trigger = screen.getByRole("button", { name: "目标游戏目录 请选择目标游戏目录" });
    expect(trigger).toHaveAttribute("aria-expanded", "false");

    await user.click(trigger);
    const panel = screen.getByRole("region", { name: "可选游戏目录" });
    expect(screen.getByRole("searchbox", { name: "搜索目录、平台或核心" })).toHaveFocus();
    expect(within(panel).queryByRole("button", { name: /FBNeo 游戏/ })).not.toBeInTheDocument();
    await user.click(within(panel).getByRole("button", { name: /街机/ }));
    expect(within(panel).getByRole("button", { name: /FBNeo 游戏/ })).toHaveTextContent("Arcade · FinalBurn Neo");
    await user.click(within(panel).getByRole("button", { name: /FBNeo 游戏/ }));
    expect(onSelect).toHaveBeenCalledWith("fbneo");
    expect(trigger).toHaveAttribute("aria-expanded", "false");
  });

  it("searches across groups and shows a useful empty state", async () => {
    const user = userEvent.setup();
    render(<DirectorySelector directories={directories} selectedId="" onSelect={vi.fn()} reconfiguring={false} />);
    await user.click(screen.getByRole("button", { name: /目标游戏目录/ }));
    const search = screen.getByRole("searchbox", { name: "搜索目录、平台或核心" });
    await user.type(search, "mGBA");
    expect(screen.getByRole("button", { name: /GBA 游戏/ })).toBeVisible();
    expect(screen.queryByRole("button", { name: /街机/ })).not.toBeInTheDocument();
    await user.clear(search);
    await user.type(search, "不存在");
    expect(screen.getByText("没有匹配的游戏目录。可尝试平台或核心名称。")).toBeVisible();
  });

  it("keeps recent choices per user and restores focus on Escape", async () => {
    const user = userEvent.setup();
    const key = "retrom:v2:user:admin:imports:recent-directories";
    render(<DirectorySelector directories={directories} selectedId="" onSelect={vi.fn()} reconfiguring={false} userId="admin" />);
    const trigger = screen.getByRole("button", { name: /目标游戏目录/ });
    await user.click(trigger);
    await user.type(screen.getByRole("searchbox", { name: "搜索目录、平台或核心" }), "GBA");
    await user.click(screen.getByRole("button", { name: /GBA 游戏/ }));
    expect(window.localStorage.getItem(key)).toBe('["gba"]');
    await user.click(trigger);
    expect(within(screen.getByRole("region", { name: "可选游戏目录" })).getByText("最近使用")).toBeVisible();
    await user.keyboard("{Escape}");
    expect(trigger).toHaveFocus();
    expect(screen.queryByRole("region", { name: "可选游戏目录" })).not.toBeInTheDocument();
  });

  it("offers grouped collection mapping, skip, and clearing without treating skip as recent", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    const view = render(<DirectorySelector collectionName="NES" directories={directories} selectedId="" onSelect={onSelect} />);
    const trigger = screen.getByRole("button", { name: "NES 处理方式" });
    expect(trigger).toHaveTextContent("请选择，不会自动映射");
    await user.click(trigger);
    const panel = screen.getByRole("region", { name: "可选游戏目录" });
    await user.click(within(panel).getByRole("button", { name: /掌机/ }));
    expect(within(panel).getByRole("button", { name: /GBA 游戏/ })).toBeVisible();
    await user.click(within(panel).getByRole("button", { name: "跳过此集合" }));
    expect(onSelect).toHaveBeenLastCalledWith("SKIP");

    view.rerender(<DirectorySelector collectionName="NES" directories={directories} selectedId="SKIP" onSelect={onSelect} />);
    expect(trigger).toHaveTextContent("跳过此集合");
    await user.click(trigger);
    expect(within(screen.getByRole("region", { name: "可选游戏目录" })).queryByText("最近使用")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "清除处理方式" }));
    expect(onSelect).toHaveBeenLastCalledWith("");
  });
});
