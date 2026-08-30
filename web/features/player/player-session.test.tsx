import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { initialPlayerOrientationState } from "./orientation";
import { createSaveForm, usePlayerSession, type PlayerSessionParams } from "./player-session";

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

describe("manual save multipart", () => {
  it("keeps a valid checkpoint when its best-effort screenshot exceeds the server limit", () => {
    const form = createSaveForm({
      screenshot: new Blob([new Uint8Array(10 * 1024 * 1024 + 1)], { type: "image/png" }),
      format: "png",
      state: Uint8Array.of(1, 2, 3),
      payloadKind: "KIRIKIRI_SAVE_BUNDLE_V1",
    }, undefined);

    expect(form.get("payload")).toBeInstanceOf(Blob);
    expect(form.get("screenshot")).toBeNull();
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
