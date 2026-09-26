import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { initialPlayerOrientationState } from "./orientation";
import {PlayProgressClock} from "./play-progress-clock";
import type {LaunchEnvelopeV1} from "./runtime/contract";
import {
  createSaveForm,
  usePlayerSession,
  waitForSaveUploadPresentationTurn,
  type PlayerSessionParams,
} from "./player-session";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("Player page exit protection", () => {
  it("finishes a review preview through ordinary events after queued saves, then closes its popup", async () => {
    const order: string[] = [];
    let saved!: () => void;
    vi.stubGlobal("opener", {});
    vi.spyOn(window, "close").mockImplementation(() => {order.push("close"); vi.stubGlobal("closed", true);});
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => {order.push("finish"); return new Response("{}");});
    const params = sessionParams();
    params.envelope.current = {session: {purpose: "REVIEW_PREVIEW"}} as LaunchEnvelopeV1;
    params.started.current = true;
    params.saveUploadQueue.current = new Promise<void>((resolve) => {saved = resolve;});
    const {result} = renderHook(() => usePlayerSession(params));
    const exiting = result.current.exit();
    await Promise.resolve();
    expect(order).toEqual([]);
    saved();
    await act(() => exiting);
    expect(order).toEqual(["finish", "close"]);
    expect(params.finishing.current).toBe(true);
  });
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

  it("does not report progress before the game starts", async () => {
    const fetchEvent = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", {status: 200}));
    const params = sessionParams();
    const { result } = renderHook(() => usePlayerSession(params));

    await act(() => result.current.reportProgress());

    expect(fetchEvent).not.toHaveBeenCalled();
  });

  it("sends cumulative progress independently of a failed prior request", async () => {
    const response = new Response("{}", {status: 200});
    const fetchEvent = vi.spyOn(globalThis, "fetch").mockRejectedValueOnce(new Error("service restart"))
      .mockResolvedValueOnce(response);
    const params = sessionParams();
    params.started.current = true;
    params.envelope.current = {session: {purpose: "PRODUCT"}} as LaunchEnvelopeV1;
    params.progressClock.current.start(0, true);
    const { result } = renderHook(() => usePlayerSession(params));

    await act(() => result.current.reportProgress());
    await act(() => result.current.reportProgress());
    expect(fetchEvent).toHaveBeenCalledTimes(2);
    expect(fetchEvent.mock.calls[0]?.[0]).toBe("/runtime/launches/launch-1/progress");
    expect(JSON.parse(String(fetchEvent.mock.calls[1]?.[1]?.body))).toHaveProperty("activeDurationMs");
    expect(response.bodyUsed).toBe(true);
  });

  it("clears the progress timer when exit begins", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", {status: 200}));
    const clearHeartbeat = vi.spyOn(window, "clearInterval");
    const params = sessionParams();
    params.started.current = true;
    params.heartbeat.current = 42;
    const { result } = renderHook(() => usePlayerSession(params));

    await act(() => result.current.exitStrict());

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

  it("navigates on manual immersive exit when progress reporting fails", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", {status: 409}));
    const params = sessionParams();
    params.started.current = true;
    const { result } = renderHook(() => usePlayerSession(params));

    await act(() => result.current.exitStrict());

    expect(params.replaceImmersiveRoute).toHaveBeenCalledWith("/library");
  });
});

describe("manual save multipart", () => {
  it("does not wait for a throttled animation frame before starting an upload", async () => {
    vi.useFakeTimers();
    const animationFrame = vi.spyOn(window, "requestAnimationFrame").mockImplementation(() => 0);
    const waiting = waitForSaveUploadPresentationTurn();

    await vi.runAllTimersAsync();

    await expect(waiting).resolves.toBeUndefined();
    expect(animationFrame).not.toHaveBeenCalled();
  });

  it("keeps a valid checkpoint when its best-effort screenshot exceeds the server limit", () => {
    const form = createSaveForm({
      screenshot: new Blob([new Uint8Array(10 * 1024 * 1024 + 1)], { type: "image/png" }),
      checkpoint: {format: "fixture-v1", bytes: Uint8Array.of(1, 2, 3), metadata: null},
    }, {screenshot: new Blob([new Uint8Array(10 * 1024 * 1024 + 1)], {type: "image/png"}), format: "png"}, undefined);

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
    runtime: {current: null},
    envelope: {current: null},
    progressClock: {current: new PlayProgressClock()},
    started: { current: false },
    finishing: { current: false },
    heartbeat: { current: null },
    saveUploadQueue: { current: Promise.resolve() },
    orientationStateRef: { current: initialPlayerOrientationState },
    returnTo: { current: "/library" },
    replaceImmersiveRoute: vi.fn(),
    setOrientationState: vi.fn(),
    setSaveUploadProgress: vi.fn(),
    setSyncText: vi.fn(),
    setSyncTone: vi.fn(),
    showToast: vi.fn(),
  };
}
