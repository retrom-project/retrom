import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useRuntimeShortcuts } from "./use-runtime-shortcuts";
import { runtimeFixture } from "./player-test-fixture";
import type { RuntimeEventListenerV1 } from "./runtime/contract";
afterEach(cleanup);
function fixture() {
  const instance = runtimeFixture();
  const listeners = new Set<RuntimeEventListenerV1>();
  vi.spyOn(instance, "subscribe").mockImplementation((listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; });
  vi.spyOn(instance, "getInputCapabilities").mockReturnValue({ hostShortcuts: ["MENU", "PAUSE"] });
  const policy = vi.spyOn(instance, "setHostShortcutPolicy");
  const options = { runtime: { current: instance }, state: "RUNNING" as const, immersive: false, suppressed: false, onMenu: vi.fn(), onPause: vi.fn(), onError: vi.fn() };
  const emit = (shortcut: "MENU" | "PAUSE") => act(() => { for (const listener of listeners) { listener({ type: "HOST_SHORTCUT", shortcut }); } });
  return { instance, policy, options, emit, listeners };
}
it("configures standard shortcuts, dispatches public events and clears policy for overlays", () => {
  const f = fixture();
  const { rerender, unmount } = renderHook((props) => useRuntimeShortcuts(props), { initialProps: f.options });
  expect(f.policy).toHaveBeenLastCalledWith({ menu: "Escape", pause: true });
  f.emit("MENU"); f.emit("PAUSE");
  expect(f.options.onMenu).toHaveBeenCalledOnce();
  expect(f.options.onPause).toHaveBeenCalledOnce();
  rerender({ ...f.options, suppressed: true });
  expect(f.policy).toHaveBeenLastCalledWith(null);
  f.emit("MENU"); f.emit("PAUSE");
  expect(f.options.onMenu).toHaveBeenCalledOnce();
  expect(f.options.onPause).toHaveBeenCalledOnce();
  unmount();
  expect(f.listeners.size).toBe(0);
});
it("uses the immersive menu key and does not accept unsupported pause or menu claims", () => {
  const f = fixture();
  const { rerender } = renderHook((props) => useRuntimeShortcuts(props), { initialProps: { ...f.options, immersive: true } });
  expect(f.policy).toHaveBeenLastCalledWith({ menu: "KeyM", pause: false });
  f.emit("MENU"); f.emit("PAUSE");
  expect(f.options.onMenu).toHaveBeenCalledOnce();
  expect(f.options.onPause).not.toHaveBeenCalled();
  vi.spyOn(f.instance, "getInputCapabilities").mockReturnValue({ hostShortcuts: [] });
  rerender({ ...f.options, immersive: false });
  expect(f.policy).toHaveBeenLastCalledWith({ menu: null, pause: false });
  f.emit("MENU"); f.emit("PAUSE");
  expect(f.options.onMenu).toHaveBeenCalledOnce();
  expect(f.options.onPause).not.toHaveBeenCalled();
});
