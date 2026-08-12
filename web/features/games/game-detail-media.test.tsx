import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GameDetailMedia } from "./game-detail-media";

let intersectionCallback: IntersectionObserverCallback;
let visibility: DocumentVisibilityState;
let reducedMotion = false;

beforeEach(() => {
  vi.useFakeTimers();
  visibility = "visible";
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });
  vi.stubGlobal("IntersectionObserver", class {
    constructor(callback: IntersectionObserverCallback) { intersectionCallback = callback; }
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords() { return []; }
    root = null;
    rootMargin = "0px";
    thresholds = [0, 0.01];
  });
  vi.stubGlobal("matchMedia", vi.fn(() => ({
    matches: reducedMotion,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })));
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
});

afterEach(() => {
  cleanup();
  reducedMotion = false;
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function enterViewport() {
  intersectionCallback([{ isIntersecting: true, intersectionRatio: 1 } as IntersectionObserverEntry], {} as IntersectionObserver);
}

describe("GameDetailMedia", () => {
  it("keeps the cover until two cumulative foreground-visible seconds and the playing event", async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    render(<GameDetailMedia title="重装机兵" coverUrl="/cover.png" videoUrl="/video.mp4" />);
    act(enterViewport);
    act(() => { vi.advanceTimersByTime(1_000); });
    visibility = "hidden";
    act(() => { document.dispatchEvent(new Event("visibilitychange")); });
    act(() => { vi.advanceTimersByTime(4_000); });
    expect(play).not.toHaveBeenCalled();
    visibility = "visible";
    act(() => { document.dispatchEvent(new Event("visibilitychange")); });
    act(() => { vi.advanceTimersByTime(999); });
    expect(play).not.toHaveBeenCalled();
    await act(async () => { vi.advanceTimersByTime(1); });
    expect(play).toHaveBeenCalledOnce();
    expect(screen.getByText("正在载入视频预览")).toBeVisible();
    const video = screen.getByLabelText("重装机兵 视频预览");
    fireEvent.playing(video);
    expect(video).toHaveClass("is-playing");
    expect(screen.getByText("正在循环播放视频预览")).toBeVisible();
  });

  it("does not autoplay with reduced motion and exposes manual play and pause controls", async () => {
    reducedMotion = true;
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    render(<GameDetailMedia title="重装机兵" coverUrl="/cover.png" videoUrl="/video.mp4" />);
    act(enterViewport);
    act(() => { vi.advanceTimersByTime(10_000); });
    expect(play).not.toHaveBeenCalled();
    expect(screen.getByText("已减少动态效果，可手动播放视频预览")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "播放视频预览" }));
    expect(play).toHaveBeenCalledOnce();
    fireEvent.playing(screen.getByLabelText("重装机兵 视频预览"));
    fireEvent.click(screen.getByRole("button", { name: "暂停预览" }));
    expect(screen.getByText("视频预览已暂停")).toBeVisible();
    act(() => { vi.advanceTimersByTime(10_000); });
    expect(play).toHaveBeenCalledOnce();
  });

  it("falls back to the cover when play is rejected", async () => {
    vi.spyOn(HTMLMediaElement.prototype, "play").mockRejectedValue(new Error("blocked"));
    render(<GameDetailMedia title="重装机兵" coverUrl="/cover.png" videoUrl="/video.mp4" />);
    act(enterViewport);
    await act(async () => { vi.advanceTimersByTime(2_000); });
    expect(screen.getByText("此视频无法在当前浏览器播放，已恢复封面")).toBeVisible();
    expect(screen.getByAltText("重装机兵 封面")).toBeVisible();
  });
});
