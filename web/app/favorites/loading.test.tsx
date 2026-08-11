import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import FavoritesLoading from "./loading";

afterEach(cleanup);

describe("FavoritesLoading", () => {
  it("preserves the page, rail, filters, and card dimensions without rendering account data", () => {
    const { container } = render(<FavoritesLoading />);
    expect(screen.getByRole("status")).toHaveAccessibleName("正在加载收藏");
    expect(screen.getByRole("heading", { name: "我的收藏" })).toBeInTheDocument();
    expect(container.querySelector(".favorite-rail")).toHaveTextContent("收藏导航");
    expect(container.querySelector(".favorite-toolbar")).toBeInTheDocument();
    expect(container.querySelectorAll(".favorite-loading-card")).toHaveLength(4);
    expect(container.textContent).not.toContain("Favorite Layout Game");
  });
});
