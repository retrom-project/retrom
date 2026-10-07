import { act, cleanup, fireEvent, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { usePlayerMenuDismiss } from "./use-player-menu-dismiss";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers(); document.body.replaceChildren(); });

function surface() {
  const mount = document.createElement("div");
  const frame = document.createElement("iframe");
  const canvas = document.createElement("canvas");
  mount.append(frame, canvas);
  document.body.append(mount);
  return { mount: { current: mount }, frame, canvas };
}

it("dismisses from the owned canvas without treating other Host controls as game input", () => {
  const { mount, canvas } = surface();
  const dismiss = vi.fn();
  renderHook(() => usePlayerMenuDismiss(mount, true, dismiss));
  fireEvent.pointerDown(document.body);
  expect(dismiss).not.toHaveBeenCalled();
  fireEvent.pointerDown(canvas);
  expect(dismiss).toHaveBeenCalledOnce();
});

it("recognizes an owned iframe focus without reading its document, and ignores leaving the browser", async () => {
  vi.useFakeTimers();
  const { mount, frame } = surface();
  const dismiss = vi.fn();
  const focused = vi.spyOn(document, "hasFocus").mockReturnValue(false);
  renderHook(() => usePlayerMenuDismiss(mount, true, dismiss));
  frame.focus();
  fireEvent.blur(window);
  await act(() => vi.advanceTimersByTime(20));
  expect(dismiss).not.toHaveBeenCalled();
  focused.mockReturnValue(true);
  fireEvent.blur(window);
  await act(() => vi.advanceTimersByTime(20));
  expect(dismiss).toHaveBeenCalledOnce();
});

it("does not dismiss for an unrelated frame and cancels pending work when the menu closes", async () => {
  vi.useFakeTimers();
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  const { mount, frame, canvas } = surface();
  const other = document.createElement("iframe");
  document.body.append(other);
  const dismiss = vi.fn();
  const view = renderHook(({ open }) => usePlayerMenuDismiss(mount, open, dismiss), { initialProps: { open: true } });
  other.focus(); fireEvent.blur(window);
  await act(() => vi.advanceTimersByTime(20));
  expect(dismiss).not.toHaveBeenCalled();
  frame.focus(); fireEvent.blur(window);
  view.rerender({ open: false });
  fireEvent.pointerDown(canvas);
  await act(() => vi.advanceTimersByTime(20));
  expect(dismiss).not.toHaveBeenCalled();
});
