import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { RuntimeStateV1 } from "./runtime/contract";
import { usePlayerHud } from "./use-player-hud";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

it("hides running controls after two seconds and reveals them on demand", async () => {
  vi.useFakeTimers();
  const view = renderHook(
    ({ state, pinned }) => usePlayerHud(state, pinned, true),
    { initialProps: { state: "MOUNTING" as RuntimeStateV1, pinned: false } },
  );
  await act(() => vi.advanceTimersByTime(3000));
  expect(view.result.current.visible).toBe(true);
  view.rerender({ state: "RUNNING", pinned: false });
  await act(() => vi.advanceTimersByTime(2000));
  expect(view.result.current.visible).toBe(false);
  await act(() => view.result.current.show());
  expect(view.result.current.visible).toBe(true);
  await act(() => vi.advanceTimersByTime(1500));
  await act(() => view.result.current.show());
  await act(() => vi.advanceTimersByTime(1500));
  expect(view.result.current.visible).toBe(true);
  await act(() => vi.advanceTimersByTime(500));
  expect(view.result.current.visible).toBe(false);
});

it("keeps touch controls available after opening until the user taps the game", async () => {
  vi.useFakeTimers();
  const view = renderHook(() => usePlayerHud("RUNNING", false, false));
  await act(() => vi.advanceTimersByTime(2000));
  expect(view.result.current.visible).toBe(false);
  await act(() => view.result.current.show());
  await act(() => vi.advanceTimersByTime(10000));
  expect(view.result.current.visible).toBe(true);
  await act(() => view.result.current.hide());
  expect(view.result.current.visible).toBe(false);
});

it("keeps hover, keyboard focus, paused and modal controls reachable", async () => {
  vi.useFakeTimers();
  const view = renderHook(
    ({ state, pinned }) => usePlayerHud(state, pinned),
    { initialProps: { state: "RUNNING" as RuntimeStateV1, pinned: false } },
  );
  await act(() => view.result.current.onHover(true));
  await act(() => view.result.current.onFocus(true));
  await act(() => vi.advanceTimersByTime(3000));
  await act(() => view.result.current.onHover(false));
  await act(() => vi.advanceTimersByTime(3000));
  expect(view.result.current.visible).toBe(true);
  await act(() => view.result.current.onFocus(false));
  await act(() => vi.advanceTimersByTime(2000));
  expect(view.result.current.visible).toBe(false);
  view.rerender({ state: "PAUSED", pinned: false });
  expect(view.result.current.visible).toBe(true);
  view.rerender({ state: "RUNNING", pinned: true });
  await act(() => vi.advanceTimersByTime(3000));
  expect(view.result.current.visible).toBe(true);
  view.rerender({ state: "RUNNING", pinned: false });
  await act(() => vi.advanceTimersByTime(2000));
  expect(view.result.current.visible).toBe(false);
});

it("cleans the reveal timer on unmount", async () => {
  vi.useFakeTimers();
  const view = renderHook(() => usePlayerHud("RUNNING", false));
  await act(() => view.result.current.show());
  expect(view.result.current.visible).toBe(true);
  view.unmount();
  expect(vi.getTimerCount()).toBe(0);
});

it.each([false, true])("preserves hidden, explicitly shown and paused controls across exports: desktop=%s", async desktop => {
  vi.useFakeTimers();
  const view = renderHook(({state}) => usePlayerHud(state, false, desktop),
    {initialProps: {state: "RUNNING" as RuntimeStateV1}});
  await act(() => view.result.current.hide());
  view.rerender({state: "CHECKPOINTING"});
  expect(view.result.current.visible).toBe(false);
  view.rerender({state: "RUNNING"});
  await act(() => view.result.current.show());
  view.rerender({state: "CHECKPOINTING"});
  expect(view.result.current.visible).toBe(true);
  view.rerender({state: "PAUSED"});
  await act(() => view.result.current.onFocus(true));
  view.rerender({state: "CHECKPOINTING"});
  expect(view.result.current.visible).toBe(true);
  view.rerender({state: "PAUSED"});
  expect(view.result.current.visible).toBe(true);
});

it("reveals desktop controls on hover and retracts immediately after leaving, including while paused", async () => {
  vi.useFakeTimers();
  const view = renderHook(({state}) => usePlayerHud(state, false, true),
    {initialProps: {state: "RUNNING" as RuntimeStateV1}});
  await act(() => vi.advanceTimersByTime(2000));
  expect(view.result.current.visible).toBe(false);
  await act(() => view.result.current.onHover(true));
  expect(view.result.current.visible).toBe(true);
  view.rerender({state: "PAUSED"});
  await act(() => view.result.current.onHover(false));
  expect(view.result.current.visible).toBe(false);
  await act(() => view.result.current.onFocus(true));
  expect(view.result.current.visible).toBe(true);
});

it.each([false, true])("dismisses sticky toolbar focus on a game click, including an in-flight resume: desktop=%s", async (desktop) => {
  const view = renderHook(({state, pinned}) => usePlayerHud(state, pinned, desktop),
    {initialProps: {state: "RUNNING" as RuntimeStateV1, pinned: false}});
  await act(() => {view.result.current.show(); view.result.current.onHover(true); view.result.current.onFocus(true);});
  expect(view.result.current.visible).toBe(true);
  await act(() => view.result.current.hide());
  expect(view.result.current.visible).toBe(false);
  view.rerender({state: "PAUSED", pinned: false});
  expect(view.result.current.visible).toBe(false);
  view.rerender({state: "PAUSED", pinned: true});
  expect(view.result.current.visible).toBe(true);
  view.rerender({state: "RUNNING", pinned: false});
  await act(() => view.result.current.show());
  expect(view.result.current.visible).toBe(true);
});
