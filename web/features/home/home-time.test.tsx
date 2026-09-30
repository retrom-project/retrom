import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { FeaturedGame } from "./home-data";
import { HomeFeatured } from "./home-featured";
import { HomeGameCard } from "./home-game-card";

vi.mock("@/lib/use-browser-time-zone", () => ({ useBrowserTimeZone: () => "Asia/Shanghai" }));
vi.mock("@/features/player/launch-button", () => ({ LaunchButton: () => <button>启动</button> }));

const originalTimeZone = process.env.TZ;
const playedAtMs = Date.parse("2026-09-02T12:43:00.000Z");

function game(): FeaturedGame {
  return {
    gameId: "game-1", title: "游戏", platform: { id: "gba", name: "GBA" },
    platformInstance: { id: "one", name: "目录" }, lastPlayedAtMs: playedAtMs,
    activeDurationMs: 1_000, sessionCount: 1, coverUrl: null, tags: [],
    description: "", hasSaveStates: false, defaultDosEntry: null, lastSessionSave: null,
  };
}

afterEach(() => {
  process.env.TZ = originalTimeZone;
  cleanup();
});

describe("home timestamps", () => {
  it("shows recent play in the browser timezone on the featured and rail cards", () => {
    process.env.TZ = "UTC";
    render(<><HomeFeatured game={game()} /><HomeGameCard game={game()} /></>);
    const times = screen.getAllByText("2026年9月2日 20:43");
    expect(times).toHaveLength(2);
    for (const time of times) {expect(time).toHaveAttribute("datetime", "2026-09-02T12:43:00.000Z");}
  });

  it("shows the last session save in the browser timezone", () => {
    process.env.TZ = "UTC";
    const lastSessionSave = {
      saveStateId: "save-1", createdAtMs: playedAtMs, activeDurationMs: 1_000,
      screenshotUrl: null, discIndex: null, discLabel: null,
    };
    render(<HomeFeatured game={{ ...game(), hasSaveStates: true, lastSessionSave }} />);
    expect(screen.getByText("2026年9月2日 20:43")).toHaveAttribute("datetime", "2026-09-02T12:43:00.000Z");
  });
});
