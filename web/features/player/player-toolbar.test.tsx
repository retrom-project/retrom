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
