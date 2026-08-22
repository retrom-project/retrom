import { afterEach, describe, expect, it, vi } from "vitest";
import {
  initialPlayerOrientationState,
  observeStableOrientation,
  reducePlayerOrientation,
  requestFullscreenAndLandscape,
} from "./orientation";

describe("player orientation state", () => {
  it("blocks a portrait mobile launch before preflight", () => {
    const transition = reducePlayerOrientation(initialPlayerOrientationState, {
      type: "config-ready", mobile: true, portrait: true, runtimeKind: "single",
    });
    expect(transition.state.phase).toBe("orientation-blocked");
    expect(transition.effects).toEqual([]);
    expect(reducePlayerOrientation(transition.state, { type: "orientation-stable", portrait: false, paused: false }).state.phase).toBe("preflight");
  });

  it("restores only a single-player game that was running before rotation", () => {
    let state = reducePlayerOrientation(initialPlayerOrientationState, {
      type: "config-ready", mobile: true, portrait: false, runtimeKind: "single",
    }).state;
    state = reducePlayerOrientation(state, { type: "runtime-started", paused: false }).state;
    const blocked = reducePlayerOrientation(state, { type: "orientation-stable", portrait: true, paused: false });
    expect(blocked.effects).toEqual(["release-input", "pause-single"]);
    expect(reducePlayerOrientation(blocked.state, { type: "orientation-stable", portrait: false, paused: true }).effects).toEqual(["resume-single"]);

    state = reducePlayerOrientation(state, { type: "orientation-stable", portrait: true, paused: true }).state;
    expect(reducePlayerOrientation(state, { type: "orientation-stable", portrait: false, paused: true }).effects).toEqual([]);
  });

  it("keeps hidden state stronger and distinguishes netplay P1 from P2", () => {
    let p1 = reducePlayerOrientation(initialPlayerOrientationState, {
      type: "config-ready", mobile: true, portrait: false, runtimeKind: "netplay-p1",
    }).state;
    p1 = reducePlayerOrientation(p1, { type: "runtime-started", paused: false }).state;
    const p1Blocked = reducePlayerOrientation(p1, { type: "orientation-stable", portrait: true, paused: false });
    expect(p1Blocked.effects).toEqual(["release-input", "pause-netplay"]);
    p1 = reducePlayerOrientation(p1Blocked.state, { type: "netplay-pause-owned" }).state;
    p1 = reducePlayerOrientation(p1, { type: "visibility", hidden: true }).state;
    const landscapeHidden = reducePlayerOrientation(p1, { type: "orientation-stable", portrait: false, paused: true });
    expect(landscapeHidden.effects).toEqual([]);
    expect(landscapeHidden.state.phase).toBe("orientation-blocked");
    expect(reducePlayerOrientation(landscapeHidden.state, { type: "visibility", hidden: false }).effects).toEqual(["resume-netplay"]);

    let p2 = reducePlayerOrientation(initialPlayerOrientationState, {
      type: "config-ready", mobile: true, portrait: false, runtimeKind: "netplay-p2",
    }).state;
    p2 = reducePlayerOrientation(p2, { type: "runtime-started", paused: false }).state;
    expect(reducePlayerOrientation(p2, { type: "orientation-stable", portrait: true, paused: false }).effects)
      .toEqual(["release-input", "warn-netplay-p2"]);
  });
});

describe("stable orientation observation", () => {
  afterEach(() => vi.useRealTimers());

  it("keeps only the final orientation after 250ms", () => {
    vi.useFakeTimers();
    const target = new EventTarget();
    let portrait = true;
    Object.defineProperty(target, "matches", { get: () => portrait });
    const query = target as MediaQueryList;
    const callback = vi.fn();
    const stop = observeStableOrientation(query, callback);
    target.dispatchEvent(new Event("change"));
    vi.advanceTimersByTime(200);
    portrait = false;
    target.dispatchEvent(new Event("change"));
    vi.advanceTimersByTime(249);
    expect(callback).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(callback).toHaveBeenCalledWith(false);
    stop();
  });
});

describe("fullscreen and orientation request", () => {
  it("regression: preserves the trusted-click fullscreen request even when a test browser reports fullscreen", async () => {
    const fullscreenDescriptor = Object.getOwnPropertyDescriptor(document, "fullscreenElement");
    const requestDescriptor = Object.getOwnPropertyDescriptor(document.documentElement, "requestFullscreen");
    const request = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => document.documentElement });
    Object.defineProperty(document.documentElement, "requestFullscreen", { configurable: true, value: request });
    try {
      await requestFullscreenAndLandscape();
      expect(request).toHaveBeenCalledWith({ navigationUI: "hide" });
    } finally {
      if (fullscreenDescriptor) {Object.defineProperty(document, "fullscreenElement", fullscreenDescriptor);}
      else {Reflect.deleteProperty(document, "fullscreenElement");}
      if (requestDescriptor) {Object.defineProperty(document.documentElement, "requestFullscreen", requestDescriptor);}
      else {Reflect.deleteProperty(document.documentElement, "requestFullscreen");}
    }
  });
});
