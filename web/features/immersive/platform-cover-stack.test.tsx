import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PlatformCoverStack } from "./platform-cover-stack";

const games = [
  { gameId: "a", title: "Alpha", coverUrl: "/content/assets/a", lastPlayedAtMs: 300 },
  { gameId: "b", title: "Beta", coverUrl: "/content/assets/b", lastPlayedAtMs: 200 },
  { gameId: "c", title: "Gamma", coverUrl: null, lastPlayedAtMs: null },
];

function stubMotionPreference(reduced: boolean) {
  vi.stubGlobal("matchMedia", vi.fn(() => ({
    matches: reduced,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })));
}

beforeEach(() => {
  vi.useFakeTimers();
  stubMotionPreference(false);
});

afterEach(() => {
  cleanup();
  Reflect.deleteProperty(document, "hidden");
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("immersive platform cover stack", () => {
  it("rotates three cover positions every three seconds without changing layout order", async () => {
    render(<PlatformCoverStack games={games} platformName="Arcade" />);
    const alpha = screen.getByAltText("Alpha 封面").closest("figure");
    const beta = screen.getByAltText("Beta 封面").closest("figure");
    const gamma = screen.getByRole("img", { name: "Gamma 暂无封面" }).closest("figure");
    expect(alpha).toHaveAttribute("data-cover-slot", "0");
    expect(beta).toHaveAttribute("data-cover-slot", "1");
    expect(gamma).toHaveAttribute("data-cover-slot", "2");

    await act(async () => vi.advanceTimersByTime(3_000));

    expect(alpha).toHaveAttribute("data-cover-slot", "2");
    expect(beta).toHaveAttribute("data-cover-slot", "0");
    expect(gamma).toHaveAttribute("data-cover-slot", "1");
  });

  it("keeps the first cover stable when reduced motion is requested", async () => {
    stubMotionPreference(true);
    render(<PlatformCoverStack games={games} platformName="Arcade" />);

    await act(async () => vi.advanceTimersByTime(9_000));

    expect(screen.getByAltText("Alpha 封面").closest("figure")).toHaveAttribute("data-cover-slot", "0");
  });

  it("pauses while the page is hidden and resumes from the same cover", async () => {
    Object.defineProperty(document, "hidden", { configurable: true, value: true });
    render(<PlatformCoverStack games={games} platformName="Arcade" />);

    await act(async () => vi.advanceTimersByTime(6_000));
    expect(screen.getByAltText("Alpha 封面").closest("figure")).toHaveAttribute("data-cover-slot", "0");

    Object.defineProperty(document, "hidden", { configurable: true, value: false });
    await act(async () => document.dispatchEvent(new Event("visibilitychange")));
    await act(async () => vi.advanceTimersByTime(3_000));
    expect(screen.getByAltText("Beta 封面").closest("figure")).toHaveAttribute("data-cover-slot", "0");
  });
});
