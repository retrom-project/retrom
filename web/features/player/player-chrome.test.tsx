import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
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
    syncText: "可创建存档",
    syncTone: "synced",
    saveUploadProgress: null,
    saveAvailable: true,
    toast: "",
    warnings: [],
    emulatorToolbarOpen: false,
    emulatorVolume: 0.72,
    emulatorMuted: false,
    videoRenderingMode: "pixel",
    discSet: null,
    discState: null,
    netplayPlayerNo: null,
    netplayPaused: false,
    debugOpen: false,
    debugMetrics: null,
    debugRuntime: {
      runtimeFamily: "EMULATORJS",
      coreId: "fbneo", coreArtifactId: "artifact-1", emulatorJSVersion: "4.2.3",
      playerAdapterId: "ejs-4.2.3-v3", inputMode: "STANDARD",
      crossOriginIsolated: true, sharedArrayBuffer: true,
    },
    runtimeState: "running",
    onHoldControls: vi.fn(),
    onReleaseControls: vi.fn(),
    onToggleControls: vi.fn(),
    onSave: vi.fn().mockResolvedValue(true),
    onPauseForToolbarInteraction: vi.fn(),
    onToggleFullscreen: vi.fn(),
    onOpenEmulatorSettings: vi.fn(),
    onCloseEmulatorSettings: vi.fn(),
    onOpenEmulatorPanel: vi.fn(),
    onChangeEmulatorVolume: vi.fn(),
    onToggleEmulatorMute: vi.fn(),
    onChangeVideoRenderingMode: vi.fn(),
    onSelectDisc: vi.fn().mockResolvedValue(true),
    onToggleNetplayPause: vi.fn(),
    onToggleDebug: vi.fn(),
    onGameSurface: vi.fn(),
    onExit: vi.fn(),
    ...overrides,
  };
}

describe("PlayerChrome", () => {
  it("keeps the running toolbar hidden until the reveal state is active", () => {
    const values = props({ controlsVisible: false });
    const { container, rerender } = render(<PlayerChrome {...values} />);
    const toolbar = container.querySelector(".player-toolbar");
    expect(toolbar).not.toHaveClass("is-visible");
    fireEvent.pointerEnter(screen.getByRole("button", { name: "显示 Player 控制栏" }));
    expect(values.onToggleControls).toHaveBeenCalledOnce();

    rerender(<PlayerChrome {...props({ controlsVisible: true })} />);
    expect(toolbar).toHaveClass("is-visible");
  });

  it("keeps a determinate save upload progress bar visible until completion", () => {
    const { rerender } = render(<PlayerChrome {...props({
      syncText: "正在上传存档 47%", syncTone: "busy", saveUploadProgress: 47,
    })} />);
    const progress = screen.getByRole("progressbar", { name: "存档上传进度" });
    expect(progress).toHaveValue(47);
    expect(progress.closest("[role='status']")).toHaveTextContent("正在上传存档47%");
    rerender(<PlayerChrome {...props({ saveUploadProgress: null })} />);
    expect(screen.queryByRole("progressbar", { name: "存档上传进度" })).not.toBeInTheDocument();
  });

  it("does not create an unrestorable save from the DOS program menu", async () => {
    const user = userEvent.setup();
    const values = props({ saveAvailable: false });
    render(<PlayerChrome {...values} />);

    expect(screen.getByRole("button", { name: "创建存档" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "返回并退出游戏" }));
    const dialog = screen.getByRole("alertdialog", { name: "退出游戏？" });
    expect(values.onPauseForToolbarInteraction).toHaveBeenCalledOnce();
    expect(dialog).toHaveTextContent("当前从 DOS 程序菜单启动，无法创建可恢复存档");
    expect(within(dialog).getByRole("button", { name: "创建存档" })).toBeDisabled();
    expect(dialog).toHaveTextContent("选择一个具体 DOS 程序再开始");
    expect(values.onSave).not.toHaveBeenCalled();
  });

  it("locks local save, pause, disc and emulator settings controls in netplay mode", async () => {
    const user = userEvent.setup();
    const values = props({ netplayPlayerNo: 1, netplayPaused: false });
    render(<PlayerChrome {...values} />);
    expect(screen.getByText("FinalBurn Neo · Arcade · 联机 · P1")).toBeVisible();
    expect(screen.queryByRole("button", { name: "创建存档" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "暂停" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "全局暂停" }));
    expect(values.onToggleNetplayPause).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "更多操作" }));
    expect(screen.queryByRole("menuitem", { name: "模拟器设置" })).not.toBeInTheDocument();
  });

  it("shows the locked disc set and changes discs without exposing server paths", async () => {
    const user = userEvent.setup();
    const values = props({
      paused: true,
      discSet: {
        contentKind: "MULTI_DISC_M3U_V1", count: 2, initialDiscIndex: 0,
        entries: [
          { index: 0, label: "光盘 1", virtualPath: "/disc-001.chd" },
          { index: 1, label: "光盘 2", virtualPath: "/disc-002.chd" },
        ]
      },
      discState: { count: 2, currentIndex: 0 },
    });
    render(<PlayerChrome {...values} />);

    await user.click(screen.getByRole("button", { name: "光盘 1 / 2" }));
    expect(screen.getByRole("menu", { name: "选择光盘" })).toBeVisible();
    expect(screen.getByRole("menuitemradio", { name: "光盘 1 · 当前" })).toHaveAttribute("aria-checked", "true");
    expect(screen.queryByText("/disc-001.chd")).not.toBeInTheDocument();
    await user.click(screen.getByRole("menuitemradio", { name: "光盘 2" }));
    expect(values.onSelectDisc).toHaveBeenCalledWith(1);
    expect(values.onPauseForToolbarInteraction).not.toHaveBeenCalled();
  });

  it("supports arrow, Home, End, and Escape keyboard navigation in the disc menu", async () => {
    const user = userEvent.setup();
    const values = props({
      paused: true,
      discSet: {
        contentKind: "MULTI_DISC_M3U_V1", count: 3, initialDiscIndex: 1,
        entries: [
          { index: 0, label: "光盘 1", virtualPath: "/disc-001.chd" },
          { index: 1, label: "光盘 2", virtualPath: "/disc-002.chd" },
          { index: 2, label: "光盘 3", virtualPath: "/disc-003.chd" },
        ]
      },
      discState: { count: 3, currentIndex: 1 },
    });
    render(<PlayerChrome {...values} />);

    const trigger = screen.getByRole("button", { name: "光盘 2 / 3" });
    await user.click(trigger);
    const first = screen.getByRole("menuitemradio", { name: "光盘 1" });
    const current = screen.getByRole("menuitemradio", { name: "光盘 2 · 当前" });
    const last = screen.getByRole("menuitemradio", { name: "光盘 3" });
    expect(current).toHaveFocus();
    await user.keyboard("{End}");
    expect(last).toHaveFocus();
    await user.keyboard("{ArrowUp}");
    expect(current).toHaveFocus();
    await user.keyboard("{Home}");
    expect(first).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(trigger).toHaveFocus();
    expect(screen.queryByRole("menu", { name: "选择光盘" })).not.toBeInTheDocument();
  });

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
    expect(screen.queryByRole("menuitem", { name: /创建存档/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "创建存档" })).toBeVisible();
    await user.click(screen.getByRole("menuitem", { name: "模拟器设置" }));
    expect(values.onOpenEmulatorSettings).toHaveBeenCalledOnce();
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("opens the right-side diagnostics without pausing the running game", async () => {
    const user = userEvent.setup();
    const values = props({
      debugMetrics: {
        fps: 59.9, frameCount: 4_210, canvasWidth: 384, canvasHeight: 224,
        viewportWidth: 1440, viewportHeight: 900, devicePixelRatio: 2,
      },
    });
    const { rerender } = render(<PlayerChrome {...values} />);

    const trigger = screen.getByRole("button", { name: "调试信息" });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    await user.click(trigger);
    expect(values.onToggleDebug).toHaveBeenCalledOnce();
    expect(values.onPauseForToolbarInteraction).not.toHaveBeenCalled();

    rerender(<PlayerChrome {...values} debugOpen />);
    const panel = screen.getByRole("complementary", { name: "运行调试信息" });
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(within(panel).getByText("59.9 FPS")).toBeVisible();
    expect(within(panel).getByText("4,210")).toBeVisible();
    expect(within(panel).getByText("384 × 224")).toBeVisible();
    expect(within(panel).getByText("COOP/COEP + SAB")).toBeVisible();
    await user.click(within(panel).getByRole("button", { name: "关闭调试信息面板" }));
    expect(values.onToggleDebug).toHaveBeenCalledTimes(2);
  });

  it("keeps RPG runtime implementation details out of ordinary diagnostics", () => {
    const internalValues = [
      "RPGXP_MKXP", "mkxp-libretro-web", "artifact-rpg-xp",
    ];
    render(<PlayerChrome {...props({
      coreName: "RPG Maker XP",
      debugOpen: true,
      debugRuntime: {
        runtimeFamily: "RPGMAKER",
        coreId: "rpgmaker_xp",
        coreArtifactId: internalValues[2],
        emulatorJSVersion: internalValues[0],
        playerAdapterId: internalValues[1],
        inputMode: "STANDARD",
        crossOriginIsolated: true,
        sharedArrayBuffer: true,
      },
    })} />);

    const panel = screen.getByRole("complementary", { name: "运行调试信息" });
    expect(within(panel).getByText("RPG Maker XP")).toBeVisible();
    expect(within(panel).getByText("RPG Maker")).toBeVisible();
    expect(within(panel).queryByText("EmulatorJS")).not.toBeInTheDocument();
    expect(within(panel).queryByText("Player adapter")).not.toBeInTheDocument();
    for (const value of internalValues) {
      expect(within(panel).queryByText(value)).not.toBeInTheDocument();
    }
  });

  it("identifies the standalone ONS runtime without labeling it EmulatorJS", () => {
    render(<PlayerChrome {...props({
      coreName: "ONScripterYuri",
      debugOpen: true,
      debugRuntime: {
        runtimeFamily: "ONS", coreId: "onscripter_yuri", coreArtifactId: "artifact-ons",
        emulatorJSVersion: "v0.3.2", playerAdapterId: "ons-yuri-web", inputMode: "STANDARD",
        crossOriginIsolated: true, sharedArrayBuffer: true,
      },
    })} />);
    const panel = screen.getByRole("complementary", { name: "运行调试信息" });
    expect(within(panel).getByText("ONScripter")).toBeVisible();
    expect(within(panel).getByText("v0.3.2")).toBeVisible();
    expect(within(panel).getByText("ons-yuri-web")).toBeVisible();
    expect(within(panel).queryByText("EmulatorJS")).not.toBeInTheDocument();
  });

  it("keeps the toolbar paused until the user returns to the game surface", async () => {
    const user = userEvent.setup();
    const calls: string[] = [];
    const values = props({ paused: true, onPauseForToolbarInteraction: vi.fn(() => { calls.push("pause"); }), onSave: vi.fn(async () => { calls.push("save"); return true; }) });
    render(<PlayerChrome {...values} />);

    expect(screen.getByText("已暂停")).toBeVisible();
    expect(screen.getByText("点击游戏画面继续")).toBeVisible();
    expect(screen.getByRole("button", { name: "已暂停，点击游戏画面继续" })).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "继续游戏" }));
    expect(values.onGameSurface).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "创建存档" }));
    expect(values.onPauseForToolbarInteraction).toHaveBeenCalledOnce();
    expect(values.onSave).toHaveBeenCalledOnce();
    expect(calls).toEqual(["pause", "save"]);
  });

  it("renders the Retrom emulator toolbar without a native exit action", async () => {
    const user = userEvent.setup();
    const values = props({ emulatorToolbarOpen: true });
    render(<PlayerChrome {...values} />);

    const toolbar = screen.getByRole("region", { name: "模拟器设置工具栏" });
    expect(toolbar).toBeVisible();
    expect(screen.getByRole("button", { name: "控制" })).toBeVisible();
    expect(screen.getByRole("button", { name: "显示" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Core 设置" })).toBeVisible();
    expect(screen.getByRole("combobox", { name: "画面模式" })).toHaveValue("pixel");
    expect(screen.getByRole("slider", { name: "模拟器音量" })).toHaveValue("72");
    expect(within(toolbar).queryByRole("button", { name: /退出/ })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "显示" }));
    expect(values.onOpenEmulatorPanel).toHaveBeenCalledWith("display");
    await user.selectOptions(screen.getByRole("combobox", { name: "画面模式" }), "sharpen");
    expect(values.onChangeVideoRenderingMode).toHaveBeenCalledWith("sharpen");
    await user.click(screen.getByRole("button", { name: "静音" }));
    expect(values.onToggleEmulatorMute).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "收起" }));
    expect(values.onCloseEmulatorSettings).toHaveBeenCalledOnce();
  });

  it("exits without creating a save unless the user chooses the explicit save action", async () => {
    const user = userEvent.setup();
    const values = props();
    render(<PlayerChrome {...values} />);

    await user.click(screen.getByRole("button", { name: "返回并退出游戏" }));
    const dialog = screen.getByRole("alertdialog", { name: "退出游戏？" });
    expect(dialog).toHaveTextContent("直接退出不会创建存档");
    expect(dialog).toHaveTextContent("只有点击“创建存档”才会保存当前位置");
    await user.click(within(dialog).getByRole("button", { name: "退出游戏" }));
    expect(values.onSave).not.toHaveBeenCalled();
    expect(values.onExit).toHaveBeenCalledOnce();
  });

  it("creates a save from the leftmost exit action and keeps the decision open", async () => {
    const user = userEvent.setup();
    let resolveSave: (saved: boolean) => void = () => undefined;
    const values = props({ onSave: vi.fn(() => new Promise<boolean>((resolve) => { resolveSave = resolve; })) });
    render(<PlayerChrome {...values} />);

    await user.click(screen.getByRole("button", { name: "返回并退出游戏" }));
    const dialog = screen.getByRole("alertdialog", { name: "退出游戏？" });
    expect(within(dialog).getAllByRole("button").map((button) => button.textContent)).toEqual(["创建存档", "取消", "退出游戏"]);

    vi.mocked(values.onPauseForToolbarInteraction).mockClear();
    await user.click(within(dialog).getByRole("button", { name: "创建存档" }));
    expect(values.onPauseForToolbarInteraction).toHaveBeenCalledOnce();
    expect(values.onSave).toHaveBeenCalledOnce();
    expect(within(dialog).getByRole("button", { name: "正在创建…" })).toBeDisabled();
    expect(within(dialog).getByRole("button", { name: "取消" })).toBeDisabled();
    expect(within(dialog).getByRole("button", { name: "退出游戏" })).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(dialog).toBeVisible();

    await act(async () => { resolveSave(true); });
    expect(await within(dialog).findByRole("button", { name: "已创建存档" })).toBeDisabled();
    expect(within(dialog).getByText("退出前存档已创建，可以安全退出。")).toBeVisible();
    expect(values.onExit).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: "退出游戏" }));
    expect(values.onExit).toHaveBeenCalledOnce();
  });

  it("keeps the exit dialog actionable when creating a save fails", async () => {
    const user = userEvent.setup();
    const values = props({ onSave: vi.fn().mockResolvedValue(false) });
    render(<PlayerChrome {...values} />);

    await user.click(screen.getByRole("button", { name: "返回并退出游戏" }));
    const dialog = screen.getByRole("alertdialog", { name: "退出游戏？" });
    await user.click(within(dialog).getByRole("button", { name: "创建存档" }));

    expect(await within(dialog).findByRole("button", { name: "重试创建存档" })).toBeEnabled();
    expect(within(dialog).getByText("创建存档失败，未生成不完整记录。可以重试或取消后继续游戏。")).toBeVisible();
    expect(within(dialog).getByRole("button", { name: "取消" })).toBeEnabled();
    expect(values.onExit).not.toHaveBeenCalled();
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
