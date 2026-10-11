import { cleanup, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { PlatformCoverStack } from "./platform-cover-stack";
import { saveFixture } from "./test-fixtures";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

it("shows distinct games before taking three covers when multiple saves belong to one game", () => {
  vi.stubGlobal("matchMedia", () => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
  const error = vi.spyOn(console, "error").mockImplementation(() => undefined);
  const games = ["a", "a", "b", "b", "c", "d"].map((id) => saveFixture("save", id).game);
  const { container, rerender } = render(<PlatformCoverStack games={games} platformName="我的存档" />);
  expect(Array.from(container.querySelectorAll("figure"), (element) => element.getAttribute("data-game-id"))).toEqual(["a", "b", "c"]);
  rerender(<PlatformCoverStack games={games.slice(0, 2)} platformName="我的存档" />);
  expect(container.querySelectorAll("figure")).toHaveLength(1);
  expect(error).not.toHaveBeenCalled();
});
