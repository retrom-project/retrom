import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AutoScrollingDescription } from "./auto-scrolling-description";

function installAnimationFrames(reducedMotion: boolean) {
  let nextFrame: FrameRequestCallback | null = null;
  vi.stubGlobal("matchMedia", vi.fn(() => ({
    matches: reducedMotion,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })));
  vi.stubGlobal("requestAnimationFrame", vi.fn((callback: FrameRequestCallback) => {
    nextFrame = callback;
    return 1;
  }));
  vi.stubGlobal("cancelAnimationFrame", vi.fn());
  return (timestamp: number) => {
    const frame = nextFrame;
    nextFrame = null;
    if (!frame) {throw new Error("AUTO_SCROLL_FRAME_MISSING");}
    act(() => frame(timestamp));
  };
}

function renderOverflowingDescription() {
  render(<AutoScrollingDescription className="description" text="长简介" />);
  const description = screen.getByLabelText("长简介");
  let browserScrollTop = 0;
  Object.defineProperties(description, {
    clientHeight: { configurable: true, value: 100 },
    scrollHeight: { configurable: true, value: 300 },
    scrollTop: {
      configurable: true,
      get: () => browserScrollTop,
      set: (value: number) => {browserScrollTop = Math.floor(value);},
    },
  });
  return description;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("automatically advances an overflowing description after the reading pause", () => {
  const runFrame = installAnimationFrames(false);
  const description = renderOverflowingDescription();
  runFrame(1);
  runFrame(1_190);
  runFrame(1_205);
  runFrame(1_221);
  runFrame(1_237);
  expect(description.scrollTop).toBeGreaterThan(0);
});

it("keeps a long description at the top when reduced motion is requested", () => {
  const runFrame = installAnimationFrames(true);
  const description = renderOverflowingDescription();
  runFrame(1);
  runFrame(2_000);
  expect(description.scrollTop).toBe(0);
});
