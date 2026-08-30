import { act, renderHook } from "@testing-library/react";
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

  it("does not send heartbeats after session finishing has begun", async () => {
    const fetchEvent = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", {status: 200}));
    const params = sessionParams();
    params.started.current = true;
    params.finishing.current = true;
    const { result } = renderHook(() => usePlayerSession(params));

    await act(() => result.current.sendEvent("heartbeat"));

    expect(fetchEvent).not.toHaveBeenCalled();
  });

  it("serializes an in-flight heartbeat before the terminal finish event", async () => {
    let resolveHeartbeat: ((response: Response) => void) | undefined;
    const fetchEvent = vi.spyOn(globalThis, "fetch")
      .mockImplementationOnce(() => new Promise<Response>((resolve) => {resolveHeartbeat = resolve;}))
      .mockResolvedValueOnce(new Response("{}", {status: 200}));
    const params = sessionParams();
    params.started.current = true;
    const { result } = renderHook(() => usePlayerSession(params));

    const heartbeat = result.current.sendEvent("heartbeat");
    await Promise.resolve();
    const finish = result.current.sendEvent("finish");

    expect(fetchEvent).toHaveBeenCalledTimes(1);
    resolveHeartbeat?.(new Response("{}", {status: 200}));
    await act(async () => {await Promise.all([heartbeat, finish]);});
    expect(fetchEvent).toHaveBeenCalledTimes(2);
    expect(JSON.parse(String(fetchEvent.mock.calls[0]?.[1]?.body))).toMatchObject({clientSequence: 1});
    expect(JSON.parse(String(fetchEvent.mock.calls[1]?.[1]?.body))).toMatchObject({clientSequence: 2});
  });

  it("clears the heartbeat timer when finish begins", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", {status: 200}));
    const clearHeartbeat = vi.spyOn(window, "clearInterval");
    const params = sessionParams();
    params.started.current = true;
    params.heartbeat.current = 42;
    const { result } = renderHook(() => usePlayerSession(params));

    await act(() => result.current.sendEvent("finish"));

    expect(clearHeartbeat).toHaveBeenCalledWith(42);
    expect(params.heartbeat.current).toBeNull();
  });

  it("returns through the immersive route after a core exit even when finish reporting fails", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", {status: 409}));
    const params = sessionParams();
    params.started.current = true;
    const { result } = renderHook(() => usePlayerSession(params));

    await act(() => result.current.exitImmersiveAfterRuntimeExit());

    expect(params.replaceImmersiveRoute).toHaveBeenCalledWith("/library");
  });

  it("keeps a manual immersive exit strict when finish reporting fails", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", {status: 409}));
    const params = sessionParams();
    params.started.current = true;
    const { result } = renderHook(() => usePlayerSession(params));

    await expect(act(() => result.current.exitStrict())).rejects.toThrow("PLAY_SESSION_EVENT_FAILED");

    expect(params.replaceImmersiveRoute).not.toHaveBeenCalled();
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
    heartbeat: { current: null },
    playEventQueue: { current: Promise.resolve() },
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
