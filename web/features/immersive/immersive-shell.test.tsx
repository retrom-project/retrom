import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  consumeImmersivePlayerReturn,
  getActiveImmersiveGamepadIndex,
  markImmersivePlayerReturn,
  setActiveImmersiveGamepadIndex,
} from "./active-gamepad";
import type { GamepadFrame, GamepadFrameListener, GamepadFrameSource } from "./gamepad-source";
import { ImmersiveShell } from "./immersive-shell";
import type { GamepadSnapshot } from "./input-model";

vi.mock("next/link", () => ({ default: ({ children, href }: { children: ReactNode; href: string }) => <a href={href}>{children}</a> }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: vi.fn() }) }));

class ManualSource implements GamepadFrameSource {
  private listener: GamepadFrameListener | null = null;
  subscribe(listener: GamepadFrameListener) {this.listener = listener; return () => {this.listener = null;};}
  emit(gamepad: GamepadSnapshot, nowMs: number, suspended = false) {
    const frame: GamepadFrame = { gamepads: [gamepad], nowMs, suspended };
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
  consumeImmersivePlayerReturn();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("ImmersiveShell controller ownership", () => {
  it("accepts the first direction immediately after a Player return", () => {
    const source = new ManualSource();
    const onAction = vi.fn();
    setActiveImmersiveGamepadIndex(2);
    markImmersivePlayerReturn();
    render(<ImmersiveShell source={source} help={[]} onAction={onAction}><h1>平台视图</h1></ImmersiveShell>);
    const neutral = pad(true);
    const down = {
      ...neutral,
      buttons: neutral.buttons.map((button, index) => index === 13 ? { pressed: true, value: 1 } : button),
    };
    source.emit(down, 0);
    expect(onAction).toHaveBeenCalledWith("down");
  });

  it("renders centered vector direction icons instead of overflowing font glyphs", () => {
    const source = new ManualSource();
    render(<ImmersiveShell
      source={source}
      help={[{ button: "vertical", label: "选择游戏" }, { button: "horizontal", label: "快速翻页" }]}
      onAction={() => undefined}
    ><h1>平台视图</h1></ImmersiveShell>);
    expect(screen.getByLabelText("上下方向键").querySelector("svg path")).toHaveAttribute(
      "d",
      "M12 8v8M8 10l4-4 4 4M8 14l4 4 4-4",
    );
    expect(screen.getByLabelText("左右方向键").querySelector("svg path")).toHaveAttribute(
      "d",
      "M8 12h8M10 8l-4 4 4 4M14 8l4 4-4 4",
    );
    expect(screen.queryByText("↔")).not.toBeInTheDocument();
    expect(screen.queryByText("↕")).not.toBeInTheDocument();
  });

  it("debounces a transient missing frame before releasing an active claim", () => {
    const source = new ManualSource();
    setActiveImmersiveGamepadIndex(2);
    render(<ImmersiveShell source={source} help={[]} onAction={() => undefined}><h1>平台视图</h1></ImmersiveShell>);
    expect(screen.queryByText("等待手柄")).not.toBeInTheDocument();
    source.emit(pad(true), 0);
    expect(screen.queryByText("等待手柄")).not.toBeInTheDocument();
    source.emit(pad(false), 1);
    expect(screen.queryByText("等待手柄")).not.toBeInTheDocument();
    expect(getActiveImmersiveGamepadIndex()).toBe(2);
    source.emit(pad(false), 251);
    expect(screen.getByText("等待手柄")).toBeInTheDocument();
    expect(getActiveImmersiveGamepadIndex()).toBeNull();
  });

  it("preserves the active controller while polling is suspended", () => {
    const source = new ManualSource();
    setActiveImmersiveGamepadIndex(2);
    render(<ImmersiveShell source={source} help={[]} onAction={() => undefined}><h1>平台视图</h1></ImmersiveShell>);
    source.emit(pad(true), 0);
    source.emit(pad(true), 1, true);
    expect(screen.queryByText("等待手柄")).not.toBeInTheDocument();
    expect(getActiveImmersiveGamepadIndex()).toBe(2);
  });

  it("hydrates with a stable clock placeholder before reading browser time", async () => {
    vi.useFakeTimers();
    const source = new ManualSource();
    render(<ImmersiveShell source={source} help={[]} onAction={() => undefined}><h1>平台视图</h1></ImmersiveShell>);
    expect(screen.getByLabelText("正在读取当前时间")).toHaveTextContent("--:--");
    await act(() => vi.runOnlyPendingTimersAsync());
    expect(screen.queryByLabelText("正在读取当前时间")).not.toBeInTheDocument();
  });

  it("restores browser fullscreen with the F shortcut", async () => {
    vi.useFakeTimers();
    const requestFullscreen = vi.fn(() => Promise.resolve());
    Object.defineProperty(document.documentElement, "requestFullscreen", { configurable: true, value: requestFullscreen });
    const source = new ManualSource();
    render(<ImmersiveShell source={source} help={[]} onAction={() => undefined}><h1>平台视图</h1></ImmersiveShell>);
    await act(() => vi.advanceTimersByTimeAsync(1));
    fireEvent.keyDown(window, { key: "f" });
    expect(requestFullscreen).toHaveBeenCalledOnce();
    Reflect.deleteProperty(document.documentElement, "requestFullscreen");
  });
});
