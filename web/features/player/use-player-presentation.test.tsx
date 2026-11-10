import {StrictMode} from "react";
import {act, cleanup, renderHook} from "@testing-library/react";
import {afterEach, beforeEach, expect, it, vi} from "vitest";
import {acquirePlayerPresentation, playerLandscapeLockSupported, requestPlayerFullscreen} from "./player-fullscreen";
import {usePlayerPresentation} from "./use-player-presentation";

vi.mock("./player-fullscreen", () => ({requestPlayerFullscreen: vi.fn(), acquirePlayerPresentation: vi.fn(), playerLandscapeLockSupported: vi.fn()}));
const scope = {stopped: false, declined: false, requestFullscreen: vi.mocked(requestPlayerFullscreen),
  stop: vi.fn(() => {scope.stopped = true;}), restore: vi.fn(async () => {scope.stopped = true;})};
beforeEach(() => {
  scope.stopped = false; scope.declined = false; vi.mocked(playerLandscapeLockSupported).mockReturnValue(true); scope.stop.mockClear(); scope.restore.mockClear();
  vi.mocked(acquirePlayerPresentation).mockReturnValue(scope as unknown as ReturnType<typeof acquirePlayerPresentation>);
});
const request = vi.mocked(requestPlayerFullscreen);
afterEach(() => {cleanup(); vi.restoreAllMocks(); request.mockReset(); Reflect.deleteProperty(document, "fullscreenElement");});

it("waits for the automatic attempt before showing the portrait fallback, then retries on rotation", async () => {
  let finish!: (value: Element | null) => void;
  request.mockImplementationOnce(() => new Promise(resolve => {finish = resolve;})).mockResolvedValue(null);
  const view = renderHook(usePlayerPresentation, {initialProps: {phone: true, known: true, blocked: true}});
  await act(async () => {});
  expect(request).toHaveBeenCalledOnce();
  expect(view.result.current.attempting).toBe(true);
  await act(async () => {finish(null);});
  expect(view.result.current.attempting).toBe(false);
  view.rerender({phone: true, known: true, blocked: false});
  await act(async () => {});
  expect(request).toHaveBeenCalledTimes(2);
});

it("retries after wake and from a trusted surface gesture, but avoids hidden and duplicate requests", async () => {
  request.mockResolvedValue(null);
  let visibility = "visible";
  vi.spyOn(document, "visibilityState", "get").mockImplementation(() => visibility as DocumentVisibilityState);
  const view = renderHook(() => usePlayerPresentation({phone: true, known: true, blocked: false}));
  await act(async () => {});
  await act(async () => {visibility = "hidden"; document.dispatchEvent(new Event("visibilitychange"));});
  expect(request).toHaveBeenCalledOnce();
  await act(async () => {visibility = "visible"; document.dispatchEvent(new Event("visibilitychange")); window.dispatchEvent(new Event("focus"));});
  expect(request).toHaveBeenCalledTimes(2);
  await act(async () => {view.result.current.retry();});
  expect(request).toHaveBeenCalledTimes(3);
  view.unmount();
  window.dispatchEvent(new Event("pageshow"));
  expect(request).toHaveBeenCalledTimes(3);
});

it("preserves desktop fullscreen behavior and waits until phone identity is known", async () => {
  request.mockResolvedValue(null);
  const view = renderHook(usePlayerPresentation, {initialProps: {phone: false, known: false, blocked: true}});
  await act(async () => {});
  view.rerender({phone: false, known: true, blocked: false});
  await act(async () => {view.result.current.retry();});
  expect(request).not.toHaveBeenCalled();
  view.rerender({phone: true, known: true, blocked: true});
  await act(async () => {});
  expect(request).toHaveBeenCalledOnce();
});


it("ignores a queued surface retry after the player has unmounted", async () => {
  request.mockResolvedValue(null);
  const view = renderHook(() => usePlayerPresentation({phone: true, known: true, blocked: false}));
  await act(async () => {});
  const queuedRetry = view.result.current.retry;
  view.unmount();
  await act(async () => {queuedRetry();});
  expect(request).toHaveBeenCalledOnce();
});

it("stops retries synchronously before exit-induced rotation, focus and visibility changes", async () => {
  request.mockResolvedValue(null);
  const view = renderHook(usePlayerPresentation, {initialProps: {phone: true, known: true, blocked: false}});
  await act(async () => {});
  act(() => view.result.current.stop());
  view.rerender({phone: true, known: true, blocked: true});
  await act(async () => {
    window.dispatchEvent(new Event("focus"));
    document.dispatchEvent(new Event("visibilitychange"));
    view.result.current.retry();
    await view.result.current.restore();
  });
  expect(request).toHaveBeenCalledOnce();
  expect(scope.stop).toHaveBeenCalledOnce();
  expect(scope.restore).toHaveBeenCalledOnce();
});

it("releases on navigation but keeps the scope alive during Strict Mode effect replay", async () => {
  request.mockResolvedValue(null);
  const view = renderHook(() => usePlayerPresentation({phone: true, known: true, blocked: false}),
    {wrapper: StrictMode});
  await act(async () => {});
  expect(scope.restore).not.toHaveBeenCalled();
  expect(request).toHaveBeenCalledOnce();
  view.unmount();
  await act(async () => {});
  expect(scope.restore).toHaveBeenCalledOnce();
});

function fullscreen(initial: Element | null) {
  let element = initial;
  Object.defineProperty(document, "fullscreenElement", {configurable: true, get: () => element});
  return (next: Element | null) => {element = next; document.dispatchEvent(new Event("fullscreenchange"));};
}

it("reports a phone fullscreen lost mid-play and restores it only from an explicit gesture", async () => {
  request.mockResolvedValue(null);
  const change = fullscreen(document.documentElement);
  const view = renderHook(() => usePlayerPresentation({phone: true, known: true, blocked: false}));
  await act(async () => {});
  expect(view.result.current.lost).toBe(false);
  expect(request).not.toHaveBeenCalled();
  await act(async () => {change(null);});
  expect(view.result.current.lost).toBe(true);
  await act(async () => {view.result.current.enter();});
  expect(request).toHaveBeenCalledOnce();
  await act(async () => {change(document.documentElement);});
  expect(view.result.current.lost).toBe(false);
});

it("keeps playing windowed after the user declines, without automatic re-entry", async () => {
  request.mockResolvedValue(null);
  const change = fullscreen(document.documentElement);
  const view = renderHook(() => usePlayerPresentation({phone: true, known: true, blocked: false}));
  await act(async () => {});
  await act(async () => {change(null);});
  await act(async () => {view.result.current.decline();});
  expect(view.result.current.lost).toBe(false);
  expect(scope.declined).toBe(true);
  await act(async () => {window.dispatchEvent(new Event("focus")); view.result.current.retry();});
  expect(request).not.toHaveBeenCalled();
  await act(async () => {view.result.current.enter();});
  expect(scope.declined).toBe(false);
  expect(request).toHaveBeenCalledOnce();
});

it("does not treat a windowed start, a deliberate exit, a desktop or a stopped player as a lost fullscreen", async () => {
  request.mockResolvedValue(null);
  const change = fullscreen(null);
  const view = renderHook(usePlayerPresentation, {initialProps: {phone: true, known: true, blocked: false}});
  await act(async () => {change(null);});
  expect(view.result.current.lost).toBe(false);
  await act(async () => {change(document.documentElement);});
  scope.declined = true;
  await act(async () => {change(null);});
  expect(view.result.current.lost).toBe(false);
  scope.declined = false;
  await act(async () => {change(document.documentElement); change(null);});
  expect(view.result.current.lost).toBe(true);
  view.rerender({phone: false, known: true, blocked: false});
  expect(view.result.current.lost).toBe(false);
  view.rerender({phone: true, known: true, blocked: false});
  await act(async () => {change(document.documentElement);});
  act(() => view.result.current.stop());
  await act(async () => {change(null);});
  expect(view.result.current.lost).toBe(false);
});
