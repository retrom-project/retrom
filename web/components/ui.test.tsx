import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Kpi } from "./ui";

afterEach(cleanup);

describe("Kpi", () => {
  it("places the accent after the value in one aligned visual group", () => {
    const { container } = render(<Kpi label="游戏数" value={38} note="已经整理并可浏览的游戏" />);

    expect(screen.getByText("游戏数")).toHaveClass("kpi-label");
    expect(screen.getByText("38").closest(".kpi-value")).not.toBeNull();
    expect(container.querySelector(".kpi-value")?.lastElementChild).toHaveClass("kpi-accent");
  });
});
