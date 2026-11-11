import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { usePlayerMenu } from "./use-player-menu";

function setup(options: { immersive?: boolean; desktopPointer?: boolean; running?: boolean } = {}) {
  const onPause = vi.fn(), onImmersive = vi.fn();
  const hook = renderHook(() => usePlayerMenu({ immersive: options.immersive ?? false, desktopPointer: options.desktopPointer ?? false,
    running: () => options.running ?? true, onImmersive, onPause }));
  return { hook, onPause, onImmersive };
}

it("pauses a running game when a phone opens the menu, not when it closes", () => {
  const { hook, onPause } = setup();
  act(() => hook.result.current.toggle());
  expect(hook.result.current.menu).toBe(true);
  expect(onPause).toHaveBeenCalledOnce();
  act(() => hook.result.current.toggle());
  expect(hook.result.current.menu).toBe(false);
  expect(onPause).toHaveBeenCalledOnce();
});

it("pauses a running game when a phone reveals the toolbar", () => {
  const { hook, onPause } = setup();
  act(() => hook.result.current.pauseForTouch());
  expect(onPause).toHaveBeenCalledOnce();
  expect(hook.result.current.menu).toBe(false);
  for (const options of [{ desktopPointer: true }, { running: false }]) {
    const other = setup(options);
    act(() => other.hook.result.current.pauseForTouch());
    expect(other.onPause).not.toHaveBeenCalled();
  }
});

it("leaves desktop and already paused games to their own pause handling", () => {
  for (const options of [{ desktopPointer: true }, { running: false }]) {
    const { hook, onPause } = setup(options);
    act(() => hook.result.current.toggle());
    expect(hook.result.current.menu).toBe(true);
    expect(onPause).not.toHaveBeenCalled();
  }
});

it("opens the immersive game menu instead of the toolbar menu", () => {
  const { hook, onPause, onImmersive } = setup({ immersive: true });
  act(() => hook.result.current.toggle());
  expect(onImmersive).toHaveBeenCalledOnce();
  expect(hook.result.current.menu).toBe(false);
  expect(onPause).not.toHaveBeenCalled();
});
