import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { GameDescription, summarizeGameDescription } from "./game-description";

afterEach(cleanup);

it("keeps a short description and normalizes whitespace that could stretch a card", () => {
  expect(summarizeGameDescription("  游戏简介\n\n  操作说明  ")).toBe("游戏简介 操作说明");
  expect(summarizeGameDescription("文".repeat(160))).toHaveLength(160);
});

it("caps long descriptions at 160 Unicode characters including the three dots", () => {
  const description = summarizeGameDescription("🎮".repeat(161));
  expect(Array.from(description)).toHaveLength(160);
  expect(description).toBe("🎮".repeat(157) + "...");
  render(<GameDescription description={"长".repeat(10000)} className="description" />);
  expect(screen.getByText("长".repeat(157) + "...")).toBeInTheDocument();
});

it("omits empty home descriptions", () => {
  const { container } = render(<GameDescription description={" \n "} className="description" />);
  expect(container).toBeEmptyDOMElement();
});
