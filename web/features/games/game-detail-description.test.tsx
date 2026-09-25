import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it } from "vitest";
import { GameDetailDescription } from "./game-detail-description";

afterEach(cleanup);

it("expands Unicode text without losing paragraphs or splitting a character", async () => {
  const user = userEvent.setup();
  const description = "🎮".repeat(321) + "\n\n最后一段";
  const { container } = render(<GameDetailDescription description={description} />);
  expect(container.querySelector("p")).toHaveTextContent("🎮".repeat(320) + "…");
  const toggle = screen.getByRole("button", { name: "展开完整简介" });
  expect(toggle).toHaveAttribute("aria-controls", container.querySelector("p")!.id);
  await user.click(toggle);
  expect(container.querySelector("p")!.textContent).toBe(description);
  expect(toggle).toHaveAttribute("aria-expanded", "true");
  await user.click(toggle);
  expect(toggle).toHaveAttribute("aria-expanded", "false");
  expect(container.querySelector("p")).not.toHaveTextContent("最后一段");
});

it("does not offer expansion for short or empty descriptions", () => {
  const { rerender } = render(<GameDetailDescription description="短简介" />);
  expect(screen.getByText("短简介")).toBeVisible();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
  rerender(<GameDetailDescription description="  " />);
  expect(screen.getByText("尚未填写游戏简介。")).toBeVisible();
});
