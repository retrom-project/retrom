import {act, renderHook} from "@testing-library/react";
import {beforeEach, describe, expect, it, vi} from "vitest";
import {useGamepadCursor} from "./use-gamepad-cursor";
import {rememberPlayerGame, readGamepadCursorPreference, readPlayerGame} from "./gamepad-cursor-preference";
import {useImmersivePlayer} from "./use-immersive-player";
import type {PlayerRuntimeV1} from "./runtime/contract";
import {setActiveImmersiveGamepadIndex} from "@/features/immersive/active-gamepad";

function runtime(defaultEnabled: boolean) {
  let enabled = defaultEnabled;
  const cursor = {getState: () => ({enabled, defaultEnabled}), setEnabled: vi.fn((next: boolean) => {enabled = next;})};
  return {cursor, runtime: {getGamepadCursor: () => cursor}};
}
beforeEach(() => {localStorage.clear(); sessionStorage.clear();});
describe("player gamepad cursor", () => {
  it("preserves a pending immersive menu chord across unrelated player renders", () => {
    vi.useFakeTimers();
    const previous = Object.getOwnPropertyDescriptor(navigator, "getGamepads");
    const buttons = Array.from({length: 17}, () => ({pressed: false, touched: false, value: 0}));
    Object.defineProperty(navigator, "getGamepads", {configurable: true, value: () => [{
      index: 0, connected: true, mapping: "standard", axes: [0, 0], buttons,
    }]});
    const source = runtime(true);
    const current = {...source.runtime, pause: vi.fn(async () => undefined),
      resume: vi.fn(async () => undefined), getCapabilities: () => ({pause: true})} as unknown as PlayerRuntimeV1;
    const toast = vi.fn();
    const params = {enabled: true, runtime: {current}, pausedRef: {current: false}, running: true,
      setPaused: vi.fn(), exitStrict: vi.fn(async () => undefined), saveAvailable: true,
      saveGame: vi.fn(async () => true), beforeMenuPause: vi.fn(), onFatalError: vi.fn()};
    const hook = renderHook(() => {
      const cursor = useGamepadCursor("user-1", "launch-1", toast);
      const immersive = useImmersivePlayer({...params, gamepadCursor: cursor.control});
      return {cursor, immersive};
    });
    const sample = (ms: number, select = false, start = false) => act(() => {
      buttons[8].pressed = select; buttons[9].pressed = start;
      vi.advanceTimersByTime(ms);
    });
    try {
      act(() => hook.result.current.cursor.initialize(source.runtime));
      sample(20, true); sample(20, true, true); sample(80);
      hook.rerender();
      sample(20, true); sample(20, true, true);
      expect(hook.result.current.immersive.overlay.kind).toBe("menu");
    } finally {
      hook.unmount(); vi.useRealTimers(); setActiveImmersiveGamepadIndex(null);
      if (previous) {Object.defineProperty(navigator, "getGamepads", previous);}
      else {Reflect.deleteProperty(navigator, "getGamepads");}
    }
  });
  it("uses runtime defaults, persists explicit false, and restores it for the same game on another launch", () => {
    rememberPlayerGame("launch-1", "game-1");
    const first = runtime(true);
    const hook = renderHook(() => useGamepadCursor("user-1", "launch-1", vi.fn()));
    act(() => hook.result.current.initialize(first.runtime));
    expect(hook.result.current.control?.enabled).toBe(true);
    act(() => hook.result.current.control?.toggle());
    expect(readGamepadCursorPreference("user-1", "game-1")).toBe(false);
    hook.unmount();
    rememberPlayerGame("launch-2", "game-1");
    const second = runtime(true);
    const restored = renderHook(() => useGamepadCursor("user-1", "launch-2", vi.fn()));
    act(() => restored.result.current.initialize(second.runtime));
    expect(second.cursor.getState().enabled).toBe(false);
    expect(readGamepadCursorPreference("user-2", "game-1")).toBeNull();
    expect(readGamepadCursorPreference("user-1", "game-2")).toBeNull();
  });
  it("does not expose unsupported controls or reuse another launch's preference context", () => {
    rememberPlayerGame("product", "game-1");
    expect(readPlayerGame("preview")).toBeNull();
    const hook = renderHook(() => useGamepadCursor("user-1", "preview", vi.fn()));
    act(() => hook.result.current.initialize({}));
    expect(hook.result.current.control).toBeNull();
  });
});
