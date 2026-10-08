import { cleanup, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { render } from "@/components/toast-test-utils";
import type { Game } from "@/lib/api/types";
import { GameCard } from "./game-card";

afterEach(cleanup);
const game: Game = {
  id: "game", version: 1, title: "Test game", platformId: "nes", platformInstanceId: "directory", directoryName: "NES",
  description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null, status: "published",
  source: "server_import", contentHash: "hash", favorite: false, tags: [], media: [], createdAtMs: 1, updatedAtMs: 2,
  lastPlayedAtMs: null,
};

it("shows only the current user's played time and updates without inventing a time from game metadata", () => {
  const view = render(<GameCard game={game} />);
  const time = view.container.querySelector(".library-game-played time")!;
  expect(time).toHaveTextContent("—");
  expect(time).not.toHaveAttribute("datetime");
  const value = Date.parse("2026-10-07T12:20:00Z");
  view.rerender(<GameCard game={{ ...game, lastPlayedAtMs: value }} />);
  expect(time).toHaveAttribute("datetime", new Date(value).toISOString());
  expect(time).not.toHaveTextContent("—");
  expect(time.getAttribute("title")).toMatch(/2026年10月7日/u);
  expect(time.textContent).toMatch(/^10\/07 \d{2}:20$/u);
  expect(screen.getByText("最近游玩")).toBeVisible();
  view.rerender(<GameCard game={game} />);
  expect(time).toHaveTextContent("—");
  expect(time).not.toHaveAttribute("title");
});
