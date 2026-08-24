import { act, cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getActiveImmersiveGamepadIndex, setActiveImmersiveGamepadIndex } from "./active-gamepad";
import type { GamepadFrame, GamepadFrameListener, GamepadFrameSource } from "./gamepad-source";
import { ImmersiveShell } from "./immersive-shell";
import type { GamepadSnapshot } from "./input-model";

vi.mock("next/link", () => ({ default: ({ children, href }: { children: ReactNode; href: string }) => <a href={href}>{children}</a> }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: vi.fn() }) }));

class ManualSource implements GamepadFrameSource {
  private listener: GamepadFrameListener | null = null;
  subscribe(listener: GamepadFrameListener) {this.listener = listener; return () => {this.listener = null;};}
  emit(gamepad: GamepadSnapshot, nowMs: number) {
    const frame: GamepadFrame = { gamepads: [gamepad], nowMs, suspended: false };
    act(() => this.listener?.(frame));
  }
}

function pad(connected: boolean): GamepadSnapshot {
  return {
    axes: [0, 0],
    buttons: Array.from({ length: 16 }, () => ({ pressed: false, value: 0 })),
    connected,
    index: 2,
    mapping: "standard",
  };
}

beforeEach(() => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({
    matches: true,
    media: "",
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })));
});

afterEach(() => {
  cleanup();
  setActiveImmersiveGamepadIndex(null);
  vi.unstubAllGlobals();
});

describe("ImmersiveShell controller ownership", () => {
  it("releases an active claim when the indexed pad is no longer a connected standard pad", () => {
    const source = new ManualSource();
    setActiveImmersiveGamepadIndex(2);
    render(<ImmersiveShell source={source} help={[]} onAction={() => undefined}><h1>平台视图</h1></ImmersiveShell>);
    source.emit(pad(true), 0);
    expect(screen.queryByText("等待手柄")).not.toBeInTheDocument();
    source.emit(pad(false), 1);
    expect(screen.getByText("等待手柄")).toBeInTheDocument();
    expect(getActiveImmersiveGamepadIndex()).toBeNull();
  });
});
