import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { setActiveImmersiveGamepadIndex } from "./active-gamepad";
import { ImmersiveEntryDialog } from "./entry-dialog";
import type { GamepadFrame, GamepadFrameListener, GamepadFrameSource } from "./gamepad-source";
import type { GamepadSnapshot } from "./input-model";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));

class ManualSource implements GamepadFrameSource {
  private listener: GamepadFrameListener | null = null;
  subscribe(listener: GamepadFrameListener) {this.listener = listener; return () => {this.listener = null;};}
  emit(gamepads: readonly GamepadSnapshot[], nowMs: number) {
    const frame: GamepadFrame = { gamepads, nowMs, suspended: false };
    act(() => this.listener?.(frame));
  }
}

function pad(buttons: number[] = []): GamepadSnapshot {
  const pressed = new Set(buttons);
  return {
    axes: [0, 0],
    buttons: Array.from({ length: 16 }, (_, index) => ({ pressed: pressed.has(index), value: pressed.has(index) ? 1 : 0 })),
    connected: true,
    index: 0,
    mapping: "standard",
  };
}

afterEach(() => {
  cleanup();
  push.mockReset();
  setActiveImmersiveGamepadIndex(null);
});

describe("immersive home entry dialog", () => {
  it("consumes the trigger, waits for neutral, defaults to cancel and re-arms after cooldown", () => {
    const source = new ManualSource();
    render(<ImmersiveEntryDialog source={source} />);
    source.emit([pad()], 0);
    source.emit([pad([7])], 1);
    expect(screen.getByRole("alertdialog", { name: "进入沉浸模式？" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "取消" })).toBeDisabled();
    source.emit([pad([7])], 200);
    expect(push).not.toHaveBeenCalled();
    source.emit([pad()], 201);
    source.emit([pad()], 321);
    expect(screen.getByRole("button", { name: "取消" })).toHaveFocus();
    source.emit([pad([0])], 322);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(push).not.toHaveBeenCalled();
    source.emit([pad()], 323);
    source.emit([pad()], 823);
    source.emit([pad([5])], 824);
    expect(screen.getByRole("alertdialog", { name: "进入沉浸模式？" })).toBeInTheDocument();
  });

  it("supports right then A to enter and B to cancel", () => {
    const source = new ManualSource();
    render(<ImmersiveEntryDialog source={source} />);
    source.emit([pad()], 0);
    source.emit([pad([3])], 1);
    source.emit([pad()], 2);
    source.emit([pad()], 122);
    source.emit([pad([15])], 123);
    expect(screen.getByRole("button", { name: "进入沉浸模式" })).toHaveFocus();
    source.emit([pad()], 124);
    source.emit([pad([0])], 125);
    expect(push).toHaveBeenCalledWith("/immersive");
  });

  it("keeps keyboard and mouse semantics aligned with the dialog", () => {
    const source = new ManualSource();
    render(<ImmersiveEntryDialog source={source} />);
    source.emit([pad()], 0);
    source.emit([pad([4])], 1);
    source.emit([pad()], 2);
    source.emit([pad()], 122);
    fireEvent.keyDown(window, { key: "ArrowRight" });
    fireEvent.keyDown(window, { key: "Enter" });
    expect(push).toHaveBeenCalledWith("/immersive");
  });
});
