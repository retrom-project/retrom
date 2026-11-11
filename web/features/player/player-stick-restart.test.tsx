import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { LaunchEnvelopeV1, PlayerRuntimeV1 } from "./runtime/contract";
import { PlayerStickRestartDialog, usePlayerStickRestart } from "./player-stick-restart";

afterEach(() => { cleanup(); localStorage.clear(); vi.unstubAllGlobals(); });

function setup(semantics: "INSTANT" | "GAME_SAVE", available = true) {
  const checkpoint = { format: "state", bytes: Uint8Array.of(1), metadata: null };
  const runtime = { current: {
    subscribe: () => () => undefined,
    getTouchStick: () => ({ getState: () => ({ mode: "FLOATING" as const }) }),
    checkpoint: vi.fn(async () => checkpoint),
  } as unknown as PlayerRuntimeV1 };
  const envelope = { runtime: { coreId: "fceumm", checkpoint: { semantics } } } as unknown as LaunchEnvelopeV1;
  const callbacks = { onOpen: vi.fn(), onRestore: vi.fn(), onError: vi.fn() };
  const hook = renderHook(() => usePlayerStickRestart({ runtime, state: "RUNNING", envelope,
    availability: { available, reason: null }, ...callbacks }));
  return { hook, runtime, checkpoint, ...callbacks };
}

it("asks to restart only when the saved mode differs from the running game", () => {
  const { hook, onOpen } = setup("INSTANT");
  act(() => { hook.result.current.touchStick!.toggle(); });
  expect(onOpen).toHaveBeenCalledOnce();
  expect(hook.result.current.dialog).toMatchObject({ open: true, mode: "FIXED", keepsProgress: true });
  act(() => hook.result.current.dialog.onContinue());
  expect(hook.result.current.dialog.open).toBe(false);
  act(() => { hook.result.current.touchStick!.toggle(); });
  expect(hook.result.current.dialog.open).toBe(false);
  expect(onOpen).toHaveBeenCalledOnce();
});

it("relaunches from an instant checkpoint so the restart keeps the current progress", async () => {
  const { hook, runtime, checkpoint, onRestore } = setup("INSTANT");
  act(() => { hook.result.current.touchStick!.toggle(); });
  act(() => hook.result.current.dialog.onRestart());
  await waitFor(() => expect(onRestore).toHaveBeenCalledWith({ checkpoint, screenshot: null }));
  expect(runtime.current!.checkpoint).toHaveBeenCalledOnce();
  expect(hook.result.current.dialog.open).toBe(false);
});

it("reloads the game when no instant checkpoint can carry the progress", () => {
  const reload = vi.fn();
  vi.stubGlobal("location", { ...window.location, reload });
  for (const [semantics, available] of [["GAME_SAVE", true], ["INSTANT", false]] as const) {
    const { hook, runtime, onRestore } = setup(semantics, available);
    expect(hook.result.current.dialog.keepsProgress).toBe(false);
    act(() => hook.result.current.dialog.onRestart());
    expect(runtime.current!.checkpoint).not.toHaveBeenCalled();
    expect(onRestore).not.toHaveBeenCalled();
  }
  expect(reload).toHaveBeenCalledTimes(2);
});

it("offers continue and restart and says what a restart keeps", () => {
  const onContinue = vi.fn(), onRestart = vi.fn();
  const view = render(<PlayerStickRestartDialog open mode="FIXED" keepsProgress busy={false} onContinue={onContinue} onRestart={onRestart} />);
  expect(screen.getByText(/已切换为固定摇杆。立即重启会保留当前进度/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "继续游玩" }));
  fireEvent.click(screen.getByRole("button", { name: "立即重启" }));
  expect(onContinue).toHaveBeenCalledOnce();
  expect(onRestart).toHaveBeenCalledOnce();
  view.rerender(<PlayerStickRestartDialog open mode="FLOATING" keepsProgress={false} busy={false} onContinue={onContinue} onRestart={onRestart} />);
  expect(screen.getByText(/尚未保存的进度会丢失/)).toBeInTheDocument();
});
