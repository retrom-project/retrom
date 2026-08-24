import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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
import { IMMERSIVE_AUDIO_PREFERENCES_STORAGE_KEY } from "./immersive-audio-preferences";

vi.mock("next/link", () => ({ default: ({ children, href }: { children: ReactNode; href: string }) => <a href={href}>{children}</a> }));
const routerSpies = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: routerSpies.replace }) }));

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
  localStorage.clear();
  routerSpies.replace.mockReset();
  vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
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
  vi.restoreAllMocks();
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

  it("opens the system menu with Select, consumes page actions and closes with B", () => {
    const source = new ManualSource();
    const onAction = vi.fn();
    setActiveImmersiveGamepadIndex(2);
    render(<ImmersiveShell source={source} help={[]} onAction={onAction}><h1>平台视图</h1></ImmersiveShell>);
    source.emit(pad(true), 0);
    source.emit(pad(true), 120);
    const select = { ...pad(true), buttons: pad(true).buttons.map((button, index) => index === 8 ? { pressed: true, value: 1 } : button) };
    source.emit(select, 121);
    expect(screen.getByRole("dialog", { name: "系统菜单" })).toBeInTheDocument();
    expect(onAction).not.toHaveBeenCalled();
    source.emit(pad(true), 200);
    source.emit(pad(true), 320);
    const cancel = { ...pad(true), buttons: pad(true).buttons.map((button, index) => index === 1 ? { pressed: true, value: 1 } : button) };
    source.emit(cancel, 321);
    expect(screen.queryByRole("dialog", { name: "系统菜单" })).not.toBeInTheDocument();
    expect(onAction).not.toHaveBeenCalled();
    const confirm = { ...pad(true), buttons: pad(true).buttons.map((button, index) => index === 0 ? { pressed: true, value: 1 } : button) };
    source.emit(confirm, 322);
    expect(onAction).not.toHaveBeenCalled();
    source.emit(pad(true), 400);
    source.emit(pad(true), 520);
    source.emit(confirm, 521);
    expect(onAction).toHaveBeenCalledWith("confirm");
  });

  it("adjusts BGM volume in the menu and persists the strict v1 payload", () => {
    const source = new ManualSource();
    setActiveImmersiveGamepadIndex(2);
    render(<ImmersiveShell source={source} help={[]} onAction={() => undefined}><h1>平台视图</h1></ImmersiveShell>);
    fireEvent.keyDown(window, { key: "s" });
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByRole("meter", { name: "背景音乐音量" })).toHaveAttribute("aria-valuenow", "50");
    expect(JSON.parse(localStorage.getItem(IMMERSIVE_AUDIO_PREFERENCES_STORAGE_KEY) ?? "null")).toEqual({
      bgmVolume: 0.5,
      bgmMuted: false,
      gameVolume: 1,
      gameMuted: false,
    });
  });

  it("applies same-frame menu direction before A", () => {
    const source = new ManualSource();
    setActiveImmersiveGamepadIndex(2);
    render(<ImmersiveShell source={source} help={[]} onAction={() => undefined}><h1>平台视图</h1></ImmersiveShell>);
    fireEvent.keyDown(window, { key: "s" });
    source.emit(pad(true), 0);
    source.emit(pad(true), 120);
    const downAndConfirm = {
      ...pad(true),
      buttons: pad(true).buttons.map((button, index) => index === 0 || index === 13 ? { pressed: true, value: 1 } : button),
    };
    source.emit(downAndConfirm, 121);
    expect(screen.getByRole("button", { name: "背景音乐静音" })).toHaveFocus();
    expect(JSON.parse(localStorage.getItem(IMMERSIVE_AUDIO_PREFERENCES_STORAGE_KEY) ?? "null")).toMatchObject({ bgmMuted: true });
  });

  it("executes the fullscreen menu row and reports the result", async () => {
    vi.useFakeTimers();
    const requestFullscreen = vi.fn(() => Promise.resolve());
    Object.defineProperty(document.documentElement, "requestFullscreen", { configurable: true, value: requestFullscreen });
    const source = new ManualSource();
    setActiveImmersiveGamepadIndex(2);
    render(<ImmersiveShell source={source} help={[]} onAction={() => undefined}><h1>平台视图</h1></ImmersiveShell>);
    await act(() => vi.advanceTimersByTimeAsync(1));
    fireEvent.keyDown(window, { key: "s" });
    for (let index = 0; index < 4; index += 1) {fireEvent.keyDown(window, { key: "ArrowDown" });}
    fireEvent.keyDown(window, { key: "Enter" });
    expect(requestFullscreen).toHaveBeenCalledOnce();
    await act(() => Promise.resolve());
    expect(screen.getByRole("status")).toHaveTextContent("已进入全屏模式");
    Reflect.deleteProperty(document.documentElement, "requestFullscreen");
  });

  it("exposes autoplay refusal as a retriable accessible status", async () => {
    vi.mocked(HTMLMediaElement.prototype.play).mockRejectedValueOnce(new DOMException("blocked"));
    const source = new ManualSource();
    setActiveImmersiveGamepadIndex(2);
    render(<ImmersiveShell source={source} help={[]} onAction={() => undefined}><h1>平台视图</h1></ImmersiveShell>);
    const retry = await screen.findByRole("button", { name: "启用背景音乐" });
    expect(retry.closest('[role="status"]')).toHaveTextContent("背景音乐等待播放");
    vi.mocked(HTMLMediaElement.prototype.play).mockResolvedValueOnce();
    fireEvent.click(retry);
    await waitFor(() => expect(screen.queryByRole("button", { name: "启用背景音乐" })).not.toBeInTheDocument());
  });

  it("forwards Y only outside the system menu and exits from its final row", () => {
    const source = new ManualSource();
    const onAction = vi.fn();
    setActiveImmersiveGamepadIndex(2);
    render(<ImmersiveShell source={source} help={[]} onAction={onAction}><h1>平台视图</h1></ImmersiveShell>);
    fireEvent.keyDown(window, { key: "y" });
    expect(onAction).toHaveBeenCalledWith("favorite");
    fireEvent.keyDown(window, { key: "s" });
    fireEvent.keyDown(window, { key: "y" });
    expect(onAction).toHaveBeenCalledTimes(1);
    for (let index = 0; index < 5; index += 1) {fireEvent.keyDown(window, { key: "ArrowDown" });}
    fireEvent.keyDown(window, { key: "Enter" });
    expect(routerSpies.replace).toHaveBeenCalledWith("/");
    expect(getActiveImmersiveGamepadIndex()).toBeNull();
  });
});
