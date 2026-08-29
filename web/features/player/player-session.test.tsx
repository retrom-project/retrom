import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { initialPlayerOrientationState } from "./orientation";
import { usePlayerSession, type PlayerSessionParams } from "./player-session";

afterEach(() => vi.restoreAllMocks());

describe("Player page exit protection", () => {
  it("blocks accidental unload only while a started session remains active", () => {
    const params = sessionParams();
    const { unmount } = renderHook(() => usePlayerSession(params));

    expect(dispatchBeforeUnload()).toBe(true);
    params.started.current = true;
    expect(dispatchBeforeUnload()).toBe(false);
    params.finishing.current = true;
    expect(dispatchBeforeUnload()).toBe(true);

    params.finishing.current = false;
    unmount();
    expect(dispatchBeforeUnload()).toBe(true);
  });
});

function dispatchBeforeUnload() {
  return window.dispatchEvent(new Event("beforeunload", { cancelable: true }));
}

function sessionParams(): PlayerSessionParams {
  return {
    launchId: "launch-1",
    emulator: { current: undefined },
    playerMode: { current: "single" },
    sequence: { current: 0 },
    started: { current: false },
    finishing: { current: false },
    saveUploadQueue: { current: Promise.resolve() },
    discSetRef: { current: null },
    orientationStateRef: { current: initialPlayerOrientationState },
    returnTo: { current: "/library" },
    netplayController: { current: null },
    replaceImmersiveRoute: vi.fn(),
    setOrientationState: vi.fn(),
    setSaveUploadProgress: vi.fn(),
    setSyncText: vi.fn(),
    setSyncTone: vi.fn(),
    showToast: vi.fn(),
  };
}
