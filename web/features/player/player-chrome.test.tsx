import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PlayerChrome } from "./player-chrome";

afterEach(cleanup);

function props(overrides: Partial<Parameters<typeof PlayerChrome>[0]> = {}): Parameters<typeof PlayerChrome>[0] {
  return {
    controlsVisible: true,
    running: true,
    paused: false,
    fullscreen: false,
    gameTitle: "1943: The Battle of Midway",
    coreName: "FinalBurn Neo",
    platformName: "Arcade",
    syncText: "已同步",
    syncTone: "synced",
    toast: "",
    warnings: [],
    hasPersistentConflict: false,
    onHoldControls: vi.fn(),
    onReleaseControls: vi.fn(),
    onSave: vi.fn(),
    onPauseForToolbarInteraction: vi.fn(),
    onToggleFullscreen: vi.fn(),
    onOpenEmulatorSettings: vi.fn(),
    onExit: vi.fn(),
    onDownloadConflict: vi.fn(),
    ...overrides,
  };
}

describe("PlayerChrome", () => {
  it("renders compact game context and routes secondary controls through the more menu", async () => {
    const user = userEvent.setup();
    const values = props();
    render(<PlayerChrome {...values} />);

    expect(screen.getByText("1943: The Battle of Midway")).toBeVisible();
    expect(screen.getByText("FinalBurn Neo · Arcade")).toBeVisible();
    expect(screen.getByRole("button", { name: "暂停" })).toBeVisible();
    expect(screen.getByRole("button", { name: "全屏" })).toBeVisible();

    await user.click(screen.getByText("1943: The Battle of Midway"));
    expect(values.onPauseForToolbarInteraction).toHaveBeenCalledOnce();

    await user.click(screen.getByRole("button", { name: "更多操作" }));
    expect(screen.getByRole("button", { name: "更多操作" })).toHaveAttribute("aria-expanded", "true");
    await user.click(screen.getByRole("menuitem", { name: "模拟器设置" }));
    expect(values.onOpenEmulatorSettings).toHaveBeenCalledOnce();
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("keeps the toolbar paused until the user returns to the game surface", async () => {
    const user = userEvent.setup();
    const values = props({ paused: true });
    render(<PlayerChrome {...values} />);

    expect(screen.getByText("已暂停")).toBeVisible();
    expect(screen.getByText("点击游戏画面继续")).toBeVisible();
    expect(screen.getByRole("button", { name: "已暂停，点击游戏画面继续" })).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "创建存档" }));
    expect(values.onPauseForToolbarInteraction).toHaveBeenCalledOnce();
    expect(values.onSave).toHaveBeenCalledOnce();
  });

  it("requires exit confirmation and preserves the local-save conflict actions", async () => {
    const user = userEvent.setup();
    const values = props({ hasPersistentConflict: true, syncText: "存档需要处理", syncTone: "warning" });
    render(<PlayerChrome {...values} />);

    expect(screen.getByRole("alert")).toHaveTextContent("另一游戏会话更新了服务器存档");
    await user.click(screen.getByRole("button", { name: "下载本地存档" }));
    expect(values.onDownloadConflict).toHaveBeenCalledOnce();

    await user.click(screen.getByRole("button", { name: "返回并退出游戏" }));
    expect(screen.getByRole("alertdialog", { name: "退出游戏？" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "下载本地存档并退出" }));
    expect(values.onDownloadConflict).toHaveBeenCalledTimes(2);
    expect(values.onExit).toHaveBeenCalledOnce();
  });

  it("closes the more menu with Escape without treating it as game exit", async () => {
    const user = userEvent.setup();
    const values = props({ warnings: ["BIOS_HASH_WARNING"] });
    render(<PlayerChrome {...values} />);
    await user.click(screen.getByRole("button", { name: "更多操作" }));
    expect(screen.getByRole("menuitem", { name: "查看运行提醒" })).toBeVisible();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(values.onExit).not.toHaveBeenCalled();
  });

  it("keeps a requested runtime warning visible over the automatic pause message", async () => {
    const user = userEvent.setup();
    render(<PlayerChrome {...props({ warnings: ["BIOS_HASH_WARNING"], toast: "游戏已暂停，点击游戏画面继续" })} />);

    await user.click(screen.getByRole("button", { name: "查看运行提醒" }));
    expect(screen.getByText("BIOS 校验值与目录期望不同，但当前允许运行。")).toBeVisible();
    expect(screen.queryByText("游戏已暂停，点击游戏画面继续")).not.toBeInTheDocument();
  });
});
