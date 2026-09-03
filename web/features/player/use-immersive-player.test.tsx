import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { setActiveImmersiveGamepadIndex } from "@/features/immersive/active-gamepad";
import type {PlayerRuntimeV1} from "./runtime/contract";
import { useImmersivePlayer } from "./use-immersive-player";

function playerRuntime(): PlayerRuntimeV1 {
  return {
    pause: vi.fn(async () => undefined), resume: vi.fn(async () => undefined),
    getCapabilities: () => ({pause: true}),
  } as unknown as PlayerRuntimeV1;
}

function gamepad(select = false, start = false): Gamepad {
  const buttons = Array.from({ length: 16 }, () => ({ pressed: false, touched: false, value: 0 }));
  buttons[8] = { pressed: select, touched: select, value: select ? 1 : 0 };
  buttons[9] = { pressed: start, touched: start, value: start ? 1 : 0 };
  return {
    axes: [0, 0, 0, 0], buttons, connected: true, hapticActuators: [], id: "test-pad",
    index: 0, mapping: "standard", timestamp: 1, vibrationActuator: null,
  } as unknown as Gamepad;
}

function renderImmersivePlayer(saveGame: () => Promise<boolean>, saveAvailable = true) {
  const current = playerRuntime();
  const pausedRef = {current: false};
  const params = {
    enabled: true,
    runtime: {current},
    pausedRef,
    running: true,
    setPaused: vi.fn(),
    exitStrict: vi.fn(async () => undefined),
    saveAvailable,
    saveGame,
    beforeMenuPause: vi.fn(),
    onFatalError: vi.fn(),
  };
  const hook = renderHook(() => useImmersivePlayer(params));
  return { ...hook, current, params };
}

afterEach(() => {
  setActiveImmersiveGamepadIndex(null);
  vi.restoreAllMocks();
});

describe("useImmersivePlayer save menu", () => {
  it("samples the shell gamepad reader when the runtime never polls input", () => {
    let poll: FrameRequestCallback = () => undefined;
    let gamepads: Gamepad[] = [gamepad()];
    const previous = Object.getOwnPropertyDescriptor(window.navigator, "getGamepads");
    Object.defineProperty(window.navigator, "getGamepads", {
      configurable: true,
      value: () => gamepads,
    });
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      poll = callback;
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    setActiveImmersiveGamepadIndex(0);

    try {
      const { result, current } = renderImmersivePlayer(vi.fn(async () => true));
      const sample = (nowMs: number, select = false, start = false) => act(() => {
        gamepads = [gamepad(select, start)];
        poll(nowMs);
      });
      sample(0, true);
      sample(40, true, true);
      sample(50);
      sample(110, true);
      sample(150, true, true);

      expect(result.current.overlay).toMatchObject({ kind: "menu" });
      expect(current.pause).toHaveBeenCalledOnce();
    } finally {
      if (previous) {Object.defineProperty(window.navigator, "getGamepads", previous);}
      else {Reflect.deleteProperty(window.navigator, "getGamepads");}
    }
  });

  it("prevents duplicate saves and remains paused after a successful upload", async () => {
    vi.spyOn(window, "requestAnimationFrame").mockReturnValue(1);
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    let resolveSave: (saved: boolean) => void = () => undefined;
    const pendingSave = new Promise<boolean>((resolve) => {resolveSave = resolve;});
    const saveGame = vi.fn(() => pendingSave);
    const { result, current, params } = renderImmersivePlayer(saveGame);

    act(() => result.current.requestMenu());
    act(() => result.current.menuSelect(1));
    act(() => {
      result.current.runSelectedMenuAction();
      result.current.runSelectedMenuAction();
      result.current.menuCancel();
    });
    expect(saveGame).toHaveBeenCalledOnce();
    expect(result.current.overlay).toMatchObject({ kind: "menu", pending: true, notice: "正在创建存档…" });
    expect(current.pause).toHaveBeenCalledOnce();
    await vi.waitFor(() => expect(params.setPaused).toHaveBeenCalledWith(true));
    expect(params.beforeMenuPause).toHaveBeenCalledOnce();
    expect(params.beforeMenuPause.mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(current.pause).mock.invocationCallOrder[0]!,
    );

    await act(async () => {resolveSave(true); await pendingSave;});
    expect(result.current.overlay).toMatchObject({ kind: "menu", pending: false, notice: "存档已创建。" });
    expect(params.pausedRef.current).toBe(true);
    expect(current.pause).toHaveBeenCalledTimes(1);
  });

  it("shows a retryable error while keeping the menu open", async () => {
    vi.spyOn(window, "requestAnimationFrame").mockReturnValue(1);
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    const saveGame = vi.fn(async () => false);
    const { result } = renderImmersivePlayer(saveGame);
    act(() => result.current.requestMenu());
    act(() => result.current.menuSelect(1));
    await act(async () => result.current.runSelectedMenuAction());
    expect(result.current.overlay).toMatchObject({
      kind: "menu",
      pending: false,
      error: "创建存档失败，请重试。",
    });
    await act(async () => result.current.runSelectedMenuAction());
    expect(saveGame).toHaveBeenCalledTimes(2);
  });
});
