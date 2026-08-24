import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ImmersiveGame } from "./api";
import { MediaStage } from "./media-stage";

function game(videoUrl: string | null): ImmersiveGame {
  return {
    gameId: "00000000-0000-7000-8000-000000000001",
    title: "测试游戏",
    description: "简介",
    releaseYear: 2001,
    developer: "Retrom",
    genre: "测试",
    platformInstance: { id: "gba", name: "GBA" },
    defaultCore: { id: "mgba", name: "mGBA" },
    coverUrl: "/content/assets/cover",
    videoUrl,
    lastPlayedAtMs: null,
  };
}

function motion(reduced: boolean) {
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

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("immersive media stage", () => {
  it("mounts only the selected video after 700 ms", async () => {
    vi.useFakeTimers();
    motion(false);
    render(<MediaStage game={game("/content/assets/video")} />);
    expect(screen.queryByLabelText("测试游戏 游戏视频")).not.toBeInTheDocument();
    await act(async () => vi.advanceTimersByTime(699));
    expect(screen.queryByLabelText("测试游戏 游戏视频")).not.toBeInTheDocument();
    await act(async () => vi.advanceTimersByTime(1));
    expect(screen.getByLabelText("测试游戏 游戏视频")).toHaveAttribute("src", "/content/assets/video");
  });

  it("never starts video automatically with reduced motion", async () => {
    vi.useFakeTimers();
    motion(true);
    render(<MediaStage game={game("/content/assets/video")} />);
    await act(async () => vi.advanceTimersByTime(2_000));
    expect(screen.queryByLabelText("测试游戏 游戏视频")).not.toBeInTheDocument();
    expect(screen.getByText("已关闭自动播放")).toBeInTheDocument();
  });

  it("rejects non-content video URLs and keeps the cover", () => {
    motion(false);
    render(<MediaStage game={game("https://example.test/video.webm")} />);
    expect(screen.getByAltText("测试游戏 封面")).toBeInTheDocument();
    expect(screen.queryByLabelText("测试游戏 视频预览")).not.toBeInTheDocument();
  });
});
