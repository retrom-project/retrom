import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PlatformView } from "./platform-view";

const mocks = vi.hoisted(() => ({ fetchDestinations: vi.fn(), push: vi.fn(), replace: vi.fn() }));

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: mocks.push, replace: mocks.replace }) }));
vi.mock("./gamepad-source", () => ({ browserGamepadSource: { subscribe: () => () => undefined } }));
vi.mock("./api", () => ({
  ImmersiveAPIError: class extends Error {constructor(public status: number, message: string) {super(message);}},
  fetchImmersiveDestinations: mocks.fetchDestinations,
}));

beforeEach(() => {
  vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
  vi.stubGlobal("matchMedia", vi.fn(() => ({
    matches: true,
    media: "",
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })));
  mocks.fetchDestinations.mockResolvedValue({
    generatedAtMs: 1_000,
    items: [
      {
        destinationId: "gba",
        kind: "platform",
        name: "Game Boy Advance",
        gameCount: 2,
        lastPlayedAtMs: null,
        featuredGames: [
          { gameId: "gba-1", title: "Golden Sun", coverUrl: "/content/assets/gba-1", lastPlayedAtMs: null },
        ],
      },
      {
        destinationId: "nes",
        kind: "platform",
        name: "NES / Famicom",
        gameCount: 3,
        lastPlayedAtMs: 500,
        featuredGames: [
          { gameId: "nes-1", title: "Adventure", coverUrl: "/content/assets/nes-1", lastPlayedAtMs: 500 },
        ],
      },
    ],
  });
});

afterEach(() => {
  cleanup();
  mocks.fetchDestinations.mockReset();
  mocks.push.mockReset();
  mocks.replace.mockReset();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("immersive platform view", () => {
  it("marks the direction and remounts the carousel for each card transition", async () => {
    render(<PlatformView />);
    let carousel = await screen.findByRole("listbox", { name: "游戏平台" });
    const indicator = screen.getByTestId("platform-position-indicator");
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("Game Boy Advance");
    expect(screen.getByLabelText("Game Boy Advance 最近游戏封面")).toBeInTheDocument();
    expect(screen.queryByLabelText("NES / Famicom 最近游戏封面")).not.toBeInTheDocument();
    expect(carousel).toHaveAttribute("data-selected-index", "0");
    expect(indicator).toHaveStyle({ transform: "translateX(0%)" });
    fireEvent.keyDown(window, { key: "ArrowRight" });
    carousel = screen.getByRole("listbox", { name: "游戏平台" });
    expect(carousel).toHaveAttribute("data-direction", "right");
    expect(carousel).toHaveAttribute("data-selected-index", "1");
    expect(screen.getByTestId("platform-position-indicator")).toHaveStyle({ transform: "translateX(100%)" });
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("NES / Famicom");
    expect(screen.getByLabelText("NES / Famicom 最近游戏封面")).toBeInTheDocument();
    fireEvent.keyDown(window, { key: "ArrowLeft" });
    expect(screen.getByRole("listbox", { name: "游戏平台" })).toHaveAttribute("data-direction", "left");
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("Game Boy Advance");
  });
});
