import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { GameDetailDescription } from "./game-detail-description";

afterEach(cleanup);

it("shows the complete Unicode description and paragraphs immediately without an expansion button", () => {
  const description = "🎮".repeat(321) + "\n\n最后一段";
  const { container } = render(<GameDetailDescription description={description} />);
  expect(container.querySelector("p")!.textContent).toBe(description);
  expect(screen.getByRole("region", { name: "游戏简介" })).toHaveAttribute("tabindex", "0");
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});

it("renders both newline characters and escaped newline text as paragraph breaks", () => {
  const { container } = render(<GameDetailDescription description={"第一段\\n第二段\n第三段\\r\\n第四段"} />);
  expect(container.querySelector("p")!.textContent).toBe("第一段\n第二段\n第三段\n第四段");
});

it("shows short and empty descriptions without a toggle", () => {
  const { rerender } = render(<GameDetailDescription description="短简介" />);
  expect(screen.getByText("短简介")).toBeVisible();
  rerender(<GameDetailDescription description="  " />);
  expect(screen.getByText("尚未填写游戏简介。")).toBeVisible();
  expect(screen.getByRole("region", { name: "游戏简介" })).not.toHaveAttribute("tabindex");
});
