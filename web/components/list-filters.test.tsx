import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { ListFilters } from "./list-filters";

afterEach(cleanup);

describe("ListFilters", () => {
  it("shows only directories belonging to the selected platform and clears a stale directory", async () => {
    const user = userEvent.setup();
    render(<ListFilters action="/library" placeholder="search" values={{}} filters={[
      { name: "platformId", label: "基础平台", options: [{ value: "", label: "所有平台" }, { value: "gba", label: "GBA" }, { value: "arcade", label: "Arcade" }] },
      { name: "platformInstanceId", label: "平台目录", dependsOn: "platformId", options: [{ value: "", label: "所有目录" }, { value: "gba-games", label: "GBA 游戏", parentValue: "gba" }, { value: "arcade-games", label: "街机游戏", parentValue: "arcade" }] },
    ]} />);

    const platform = screen.getByRole("combobox", { name: "基础平台" });
    const directory = screen.getByRole("combobox", { name: "平台目录" });
    await user.selectOptions(platform, "gba");
    expect(screen.getByRole("option", { name: "GBA 游戏" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "街机游戏" })).not.toBeInTheDocument();
    await user.selectOptions(directory, "gba-games");
    await user.selectOptions(platform, "arcade");
    expect(directory).toHaveValue("");
    expect(screen.getByRole("option", { name: "街机游戏" })).toBeInTheDocument();
  });
});
