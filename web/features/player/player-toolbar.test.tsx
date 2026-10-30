import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { useState } from "react";
import type { ComponentProps } from "react";
import userEvent from "@testing-library/user-event";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { LaunchEnvelopeV1 } from "./runtime/contract";
import { PlayerToolbar } from "./player-toolbar";

afterEach(cleanup);

it.each([true, false])("hides the touch gamepad toggle on desktop when enabled is %s", (enabled) => {
  const toggle = vi.fn();
  render(<PlayerToolbar {...toolbarProps()} menu={false} state="RUNNING" desktopPointer touchGamepad={{enabled, toggle}} />);
  expect(screen.queryByRole("button", {name: /虚拟手柄/})).not.toBeInTheDocument();
});

it("keeps the touch gamepad toggle in the top toolbar in both preference states", () => {
  const toggle = vi.fn(), props = toolbarProps();
  const view = render(<PlayerToolbar {...props} menu={false} state="RUNNING" touchGamepad={{enabled: true, toggle}} />);
  const hide = screen.getByRole("button", {name: "隐藏虚拟手柄"});
  expect(hide.closest(".player-actions")).not.toBeNull();
  expect(hide).toHaveAttribute("aria-pressed", "true");
  const enabledIcon = hide.querySelector("svg")?.innerHTML;
  fireEvent.click(hide);
  expect(toggle).toHaveBeenCalledOnce();
  view.rerender(<PlayerToolbar {...props} menu={false} state="RUNNING" touchGamepad={{enabled: false, toggle}} />);
  const show = screen.getByRole("button", {name: "显示虚拟手柄"});
  expect(show).toHaveAttribute("aria-pressed", "false");
  expect(show.querySelector("svg")?.innerHTML).not.toBe(enabledIcon);
});

function toolbarProps(): ComponentProps<typeof PlayerToolbar> {
  const envelope = JSON.parse(
    readFileSync(
      resolve(
        process.cwd(),
        "../api/runtime-provider/v1/fixtures/valid/checkpoint-restore.json",
      ),
      "utf8",
    ),
  ) as LaunchEnvelopeV1;
  envelope.runtime.capabilities = {
    ...envelope.runtime.capabilities,
    pause: true,
    screenshot: true,
    nativeSettings: true,
    volume: true,
    videoModes: ["original"],
  };
  return {
    envelope,
    state: "MOUNTING",
    availability: { available: true, reason: null },
    busy: false,
    status: "",
    visible: true,
    menu: true,
    debugOpen: false,
    onDebug: vi.fn(),
    onReveal: vi.fn(),
    onHover: vi.fn(),
    onFocus: vi.fn(),
    onPause: vi.fn(),
    onSave: vi.fn(),
    onExit: vi.fn(),
    onSettings: vi.fn(),
    onUseCover: vi.fn(),

    onMenu: vi.fn(),
    onControlError: vi.fn(),
    onInteract: vi.fn().mockResolvedValue(undefined),
  };
}

it.each(["CREATED", "MOUNTING"] as const)(
  "keeps runtime actions disabled in %s, then enables running and paused controls",
  (state) => {
    const props = toolbarProps();
    const view = render(<PlayerToolbar {...props} state={state} />);
    const actionNames = [
      "暂停",
      "创建存档",

    ];
    for (const name of actionNames) {
      const button = screen.getByRole("button", { name });
      expect(button).toBeDisabled();
      fireEvent.click(button);
    }
    expect(screen.getByRole("menuitem", { name: "选用当前截图为封面" })).toBeDisabled();
    expect(screen.getByRole("menuitem", { name: "模拟器设置" })).toBeDisabled();
    expect(props.onPause).not.toHaveBeenCalled();
    expect(props.onSave).not.toHaveBeenCalled();
    expect(props.onUseCover).not.toHaveBeenCalled();
    expect(props.onSettings).not.toHaveBeenCalled();
    expect(screen.getByRole("menuitem", { name: "退出游戏" })).toBeEnabled();
    view.rerender(<PlayerToolbar {...props} state="RUNNING" />);
    for (const name of actionNames) {
      expect(screen.getByRole("button", { name })).toBeEnabled();
    }
    fireEvent.click(screen.getByRole("button", { name: "暂停" }));
    expect(props.onPause).toHaveBeenCalledOnce();
    view.rerender(<PlayerToolbar {...props} state="PAUSED" />);
    expect(screen.getByRole("button", { name: "继续" })).toBeEnabled();
  },
);

it("removes the reveal handle when controls are visible", () => {
  const props = toolbarProps();
  const view = render(<PlayerToolbar {...props} />);
  expect(
    screen.queryByRole("button", { name: "显示游戏工具栏" }),
  ).not.toBeInTheDocument();
  view.rerender(<PlayerToolbar {...props} visible={false} />);
  const handle = screen.getByRole("button", { name: "显示游戏工具栏" });
  fireEvent.pointerEnter(handle);
  expect(props.onReveal).not.toHaveBeenCalled();
  fireEvent.click(handle);
  expect(props.onReveal).toHaveBeenCalledOnce();
});

it.each([
  ["RUNNING", "返回并退出游戏"],
  ["MOUNTING", "返回并退出游戏"],
] as const)(
  "moves keyboard reveal focus to the first enabled control in %s",
  async (state, firstControl) => {
    const props = toolbarProps();
    function Toolbar() {
      const [visible, setVisible] = useState(false);
      return (
        <PlayerToolbar
          {...props}
          state={state}
          visible={visible}
          onReveal={() => setVisible(true)}
        />
      );
    }
    render(<Toolbar />);
    await userEvent.setup().tab();
    expect(
      screen.queryByRole("button", { name: "显示游戏工具栏" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: firstControl })).toHaveFocus();
    expect(props.onFocus).toHaveBeenCalledWith(true);
  },
);

it("describes GAME_SAVE as written game data, never an instant capture", () => {
  const props = toolbarProps();
  props.envelope.runtime.checkpoint = { semantics: "GAME_SAVE", writeFormat: "j2me-rms-bundle-v1-storage-v1", readFormats: ["j2me-rms-bundle-v1-storage-v1"], maxBytes: 1_048_576 };
  render(<PlayerToolbar {...props} state="RUNNING" />);
  expect(screen.getByRole("button", { name: "同步存档" })).toBeEnabled();
  expect(screen.queryByText("可创建存档")).not.toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("有待同步的游戏数据");
});

it.each([
  ["FAILED", "游戏运行失败"],
  ["EXITING", "正在退出…"],
  ["EXITED", "游戏已结束"],
] as const)("shows %s in both toolbar and menu instead of stale save status", (state, label) => {
  const props = toolbarProps();
  render(<PlayerToolbar {...props} state={state} availability={{available: false, reason: "CORE_NOT_READY"}} busy status="存档已同步" />);
  expect(screen.getByRole("status")).toHaveTextContent(label);
  expect(screen.getByRole("status")).not.toHaveClass("is-busy");
  expect(screen.getByRole("menu")).toHaveTextContent(label);
  expect(screen.queryByText("存档已同步")).not.toBeInTheDocument();
  expect(screen.queryByText("游戏运行中")).not.toBeInTheDocument();
});

it("reveals the whole desktop top edge on hover and pauses clicks in toolbar whitespace and controls", () => {
  const props = toolbarProps();
  const view = render(<PlayerToolbar {...props} menu={false} desktopPointer visible={false} state="RUNNING" />);
  const edge = screen.getByRole("button", {name: "显示游戏工具栏"});
  expect(edge).toHaveClass("is-desktop-pointer");
  fireEvent.pointerEnter(edge);
  expect(props.onHover).toHaveBeenCalledWith(true);
  expect(props.onInteract).not.toHaveBeenCalled();
  view.rerender(<PlayerToolbar {...props} menu={false} desktopPointer state="RUNNING" />);
  // Keep the same edge through the slide animation so leaving it still delivers pointerleave.
  expect(view.container.querySelector(".player-hud-handle")).toBe(edge);
  expect(edge).toHaveAttribute("tabindex", "-1");
  fireEvent.pointerLeave(edge);
  expect(props.onHover).toHaveBeenLastCalledWith(false);
  fireEvent.click(edge);
  fireEvent.click(view.container.querySelector(".player-toolbar")!);
  fireEvent.click(screen.getByRole("button", {name: "调试信息"}));
  expect(props.onInteract).toHaveBeenCalledTimes(3);
  expect(props.onDebug).toHaveBeenCalledOnce();
  view.rerender(<PlayerToolbar {...props} menu={false} desktopPointer state="PAUSED" />);
  fireEvent.click(screen.getByRole("button", {name: "暂停"}));
  expect(props.onInteract).toHaveBeenCalledTimes(4);
  expect(props.onPause).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", {name: "继续"})).not.toBeInTheDocument();
});

it("keeps only the back control at top left and exposes an available game editor through more", () => {
  const props = toolbarProps();
  const onGameEditor = vi.fn();
  const view = render(<PlayerToolbar {...props} state="RUNNING" onGameEditor={onGameEditor} />);
  expect(view.container.querySelector(".player-game-meta")).toBeNull();
  expect(screen.getByRole("button", { name: "返回并退出游戏" })).toBeVisible();
  expect(view.container.querySelector(".player-toolbar")?.firstElementChild).toBe(screen.getByRole("button", { name: "返回并退出游戏" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "游戏修改" }));
  expect(onGameEditor).toHaveBeenCalledOnce();
  view.rerender(<PlayerToolbar {...props} state="MOUNTING" onGameEditor={onGameEditor} />);
  expect(screen.getByRole("menuitem", { name: "游戏修改" })).toBeDisabled();
  view.rerender(<PlayerToolbar {...props} state="RUNNING" />);
  expect(screen.queryByRole("menuitem", { name: "游戏修改" })).not.toBeInTheDocument();
});

it("does not pin the touch toolbar through synthetic hover", () => {
  const props = toolbarProps();
  const view = render(<PlayerToolbar {...props} menu={false} desktopPointer={false} state="RUNNING" />);
  fireEvent.pointerEnter(view.container.querySelector(".player-toolbar")!);
  expect(props.onHover).not.toHaveBeenCalled();
});

it("keeps a touch handle mounted until its click instead of revealing on pointer focus", () => {
  const props = toolbarProps();
  render(<PlayerToolbar {...props} menu={false} visible={false} desktopPointer={false} state="RUNNING" />);
  const handle = screen.getByRole("button", {name: "显示游戏工具栏"});
  const matches = vi.spyOn(handle, "matches").mockReturnValue(true);
  try {
    fireEvent.pointerDown(handle);
    fireEvent.pointerUp(handle);
    fireEvent.focus(handle);
    expect(props.onReveal).not.toHaveBeenCalled();
    fireEvent.click(handle);
    expect(props.onReveal).toHaveBeenCalledOnce();
  } finally {matches.mockRestore();}
});

it.each([false, true])("keeps fullscreen only in the more menu (mobile: %s)", (mobile) => {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: mobile, media: query, onchange: null, addEventListener() {}, removeEventListener() {},
    addListener() {}, removeListener() {}, dispatchEvent: () => true,
  }));
  Object.defineProperty(document, "fullscreenElement", {configurable: true, value: null});
  try {
    const props = toolbarProps();
    const view = render(<PlayerToolbar {...props} menu={false} state="RUNNING" />);
    expect(screen.queryByRole("button", {name: /全屏/})).not.toBeInTheDocument();
    view.rerender(<PlayerToolbar {...props} menu state="RUNNING" />);
    expect(screen.getByRole("menuitem", {name: "在更多操作中进入全屏"})).toBeInTheDocument();
    expect(screen.queryByRole("button", {name: /全屏/})).not.toBeInTheDocument();
  } finally {vi.unstubAllGlobals(); Reflect.deleteProperty(document, "fullscreenElement");}
});
