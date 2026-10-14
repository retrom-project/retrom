import {act, cleanup, renderHook} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import {requestPlayerFullscreen} from "./player-fullscreen";
import {usePlayerPresentation} from "./use-player-presentation";

vi.mock("./player-fullscreen", () => ({requestPlayerFullscreen: vi.fn()}));
const request = vi.mocked(requestPlayerFullscreen);
afterEach(() => {cleanup(); vi.restoreAllMocks(); request.mockReset();});

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
