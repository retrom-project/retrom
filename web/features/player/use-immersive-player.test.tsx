import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { setActiveImmersiveGamepadIndex } from "@/features/immersive/active-gamepad";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import { useImmersivePlayer } from "./use-immersive-player";

function emulator(): EmulatorInstance {
  return {
    on: () => undefined,
    gameManager: { toggleMainLoop: vi.fn() },
  } satisfies EmulatorInstance;
}

function renderImmersivePlayer(saveGame: () => Promise<boolean>, saveAvailable = true) {
  const current = emulator();
  const params = {
    enabled: true,
    emulator: { current },
    pausedRef: { current: false },
    running: true,
    setPaused: vi.fn(),
    exitStrict: vi.fn(async () => undefined),
    saveAvailable,
    saveGame,
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
    expect(current.gameManager?.toggleMainLoop).toHaveBeenCalledWith(false);
    expect(params.setPaused).toHaveBeenCalledWith(true);

    await act(async () => {resolveSave(true); await pendingSave;});
    expect(result.current.overlay).toMatchObject({ kind: "menu", pending: false, notice: "存档已创建。" });
    expect(current.paused).toBe(true);
    expect(current.gameManager?.toggleMainLoop).toHaveBeenCalledTimes(1);
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
