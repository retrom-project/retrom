import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LaunchControls, type CoreOption, type DOSEntry } from "./launch-controls";

const navigation = vi.hoisted(() => ({ replace: vi.fn(), replacePlayerDocument: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: navigation.replace }) }));
vi.mock("@/lib/player-document-navigation", () => ({
  replaceWithPlayerDocument: navigation.replacePlayerDocument,
}));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "user-1" } } }) }));

const storagePrefix = "retrom:v2:user:user-1:player:";
const desktopLaunchButton = () => within(screen.getByRole("complementary", { name: "启动游戏" })).getByRole("button", { name: "开始游戏" });

const cores: CoreOption[] = [
  { coreId: "mgba", name: "mGBA", isDefault: true, status: "READY", reasons: [] },
  { coreId: "gambatte", name: "Gambatte", isDefault: false, status: "NEEDS_VALIDATION", reasons: [{ code: "VARIANT_VALIDATION_REQUIRED", level: "INFO" }] }
];

const dosEntries: DOSEntry[] = [
  { path: "GAMES/DOOM.EXE", originalPath: "GAMES/DOOM.EXE", kind: "EXE", rank: 0, enabled: true, directLaunchSafe: true },
  { path: "SETUP%.EXE", originalPath: "SETUP%.EXE", kind: "EXE", rank: 1, enabled: true, directLaunchSafe: false }
];

describe("LaunchControls", () => {
  const requests: string[] = [];

  beforeEach(() => {
    requests.length = 0;
    navigation.replace.mockReset();
    navigation.replacePlayerDocument.mockReset();
    window.localStorage.clear();
    Object.defineProperty(document.documentElement, "requestFullscreen", { configurable: true, value: vi.fn().mockResolvedValue(undefined) });
    vi.stubGlobal("fetch", vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      requests.push(String(init?.body));
      return new Response(JSON.stringify({ error: { message: "test stop" } }), { status: 422, headers: { "Content-Type": "application/json" } });
    }));
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("submits a selected core that still needs validation", async () => {
    const user = userEvent.setup();
    render(<LaunchControls gameId="game-1" coreOptions={cores} dosEntries={[]} defaultDosEntry={null} />);

    expect(screen.queryByLabelText("运行引擎")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /更换/ }));
    await user.selectOptions(screen.getByLabelText("运行引擎"), "gambatte");
    await user.click(screen.getByRole("button", { name: "应用" }));
    expect(screen.getByText("开始时会自动检查")).toBeInTheDocument();
    expect(screen.getByText("（未采用默认核心）")).toBeInTheDocument();
    expect(window.localStorage.getItem(`${storagePrefix}preferred-core:game-1`)).toBe("gambatte");
    await user.click(desktopLaunchButton());

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(JSON.parse(requests[0])).toMatchObject({ gameId: "game-1", coreId: "gambatte", dosEntry: null });
  });

  it("closes the core picker outside and restores the per-game choice on the next visit", async () => {
    const user = userEvent.setup();
    const first = render(<LaunchControls gameId="remembered-game" coreOptions={cores} dosEntries={[]} defaultDosEntry={null} />);

    await user.click(screen.getByRole("button", { name: /更换/ }));
    await user.selectOptions(screen.getByLabelText("运行引擎"), "gambatte");
    await user.click(screen.getByRole("button", { name: "应用" }));
    await user.click(screen.getByRole("button", { name: /更换/ }));
    await user.click(document.querySelector<HTMLElement>(".dialog-backdrop")!);
    expect(screen.queryByLabelText("运行引擎")).not.toBeInTheDocument();

    first.unmount();
    render(<LaunchControls gameId="remembered-game" coreOptions={cores} dosEntries={[]} defaultDosEntry={null} />);
    await waitFor(() => expect(screen.getByText("（未采用默认核心）")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /更换/ }));
    expect(screen.getByLabelText("运行引擎")).toHaveValue("gambatte");
    await user.selectOptions(screen.getByLabelText("运行引擎"), "mgba");
    await user.click(screen.getByRole("button", { name: "应用" }));
    expect(window.localStorage.getItem(`${storagePrefix}preferred-core:remembered-game`)).toBeNull();
    expect(screen.queryByText("（未采用默认核心）")).not.toBeInTheDocument();
  });

  it("closes the core picker with Escape or the cancel button without applying changes", async () => {
    const user = userEvent.setup();
    render(<LaunchControls gameId="game-1" coreOptions={cores} dosEntries={[]} defaultDosEntry={null} />);

    await user.click(screen.getByRole("button", { name: /更换/ }));
    await user.keyboard("{Escape}");
    expect(screen.queryByLabelText("运行引擎")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /更换/ }));
    await user.selectOptions(screen.getByLabelText("运行引擎"), "gambatte");
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.queryByLabelText("运行引擎")).not.toBeInTheDocument();
    expect(window.localStorage.getItem(`${storagePrefix}preferred-core:game-1`)).toBeNull();
  });

  it("keeps fullscreen while soft-routing to the Player", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ launchId: "launch-1", playUrl: "/play/launch-1" }), { status: 201, headers: { "Content-Type": "application/json" } })));
    render(<LaunchControls gameId="game-1" coreOptions={cores.slice(0, 1)} dosEntries={[]} defaultDosEntry={null} />);

    await user.click(desktopLaunchButton());

    await waitFor(() => expect(navigation.replacePlayerDocument).toHaveBeenCalledWith("/play/launch-1", navigation.replace));
  });

  it("exits fullscreen and exposes a repair entry when BIOS blocks launch", async () => {
    const user = userEvent.setup();
    const exitFullscreen = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => document.documentElement });
    Object.defineProperty(document, "exitFullscreen", { configurable: true, value: exitFullscreen });
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: { code: "LAUNCH_BLOCKED", message: "LAUNCH_BIOS_MISSING" } }), { status: 422, headers: { "Content-Type": "application/json" } })));
    render(<LaunchControls gameId="blocked-game" coreOptions={cores.slice(0, 1)} dosEntries={[]} defaultDosEntry={null} />);

    await user.click(desktopLaunchButton());

    expect(await screen.findByRole("alert")).toHaveTextContent("LAUNCH_BIOS_MISSING");
    expect(screen.getByRole("link", { name: "前往 BIOS 管理" })).toHaveAttribute("href", "/admin/bios?scope=REQUIRED_BY_LIBRARY");
    expect(exitFullscreen).toHaveBeenCalledOnce();
  });

  it("uses the reviewed DOS default and preserves the explicit program-menu choice", async () => {
    const user = userEvent.setup();
    const dosCore: CoreOption[] = [{ coreId: "dosbox_pure", name: "DOSBox Pure", isDefault: true, status: "READY", reasons: [] }];
    render(<LaunchControls gameId="dos-game" coreOptions={dosCore} dosEntries={dosEntries} defaultDosEntry="GAMES/DOOM.EXE" />);

    expect(screen.getByLabelText("启动程序")).toHaveValue("GAMES/DOOM.EXE");
    expect(screen.getByRole("option", { name: /SETUP%\.EXE/ })).toBeDisabled();
    await user.selectOptions(screen.getByLabelText("启动程序"), "");
    await user.click(desktopLaunchButton());

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(JSON.parse(requests[0])).toMatchObject({ coreId: "dosbox_pure", dosEntry: null });
    expect(window.localStorage.getItem(`${storagePrefix}preferred-dos-entry:dos-game`)).toBeNull();
  });

  it("remembers only a successfully launched DOS entry, including the explicit program menu", async () => {
    const user = userEvent.setup();
    const dosCore: CoreOption[] = [{ coreId: "dosbox_pure", name: "DOSBox Pure", isDefault: true, status: "READY", reasons: [] }];
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ launchId: "launch-1", playUrl: "/play/launch-1" }), { status: 201, headers: { "Content-Type": "application/json" } })));
    const first = render(<LaunchControls gameId="dos-game" coreOptions={dosCore} dosEntries={dosEntries} defaultDosEntry="GAMES/DOOM.EXE" />);

    await user.selectOptions(screen.getByLabelText("启动程序"), "");
    await user.click(desktopLaunchButton());
    await waitFor(() => expect(navigation.replacePlayerDocument).toHaveBeenCalledWith("/play/launch-1", navigation.replace));
    expect(window.localStorage.getItem(`${storagePrefix}preferred-dos-entry:dos-game`)).toBe('{"version":1,"entry":null}');

    first.unmount();
    render(<LaunchControls gameId="dos-game" coreOptions={dosCore} dosEntries={dosEntries} defaultDosEntry="GAMES/DOOM.EXE" />);
    expect(screen.getByLabelText("启动程序")).toHaveValue("");
  });

  it("falls back to the program menu when the reviewed DOS default is not direct-launch safe", () => {
    const dosCore: CoreOption[] = [{ coreId: "dosbox_pure", name: "DOSBox Pure", isDefault: true, status: "READY", reasons: [] }];
    render(<LaunchControls gameId="menu-only-game" coreOptions={dosCore} dosEntries={dosEntries} defaultDosEntry="SETUP%.EXE" />);

    expect(screen.getByLabelText("启动程序")).toHaveValue("");
  });

  it("explains the fresh start when no resumable save is available", () => {
    render(<LaunchControls gameId="no-save" coreOptions={cores.slice(0, 1)} dosEntries={[]} defaultDosEntry={null} />);
    expect(screen.getByText("还没有可继续的存档")).toBeVisible();
    expect(screen.getByText("本次将从游戏开头启动。可用存档会显示在这里，方便下次继续。")).toBeVisible();
    expect(screen.queryByRole("button", { name: "从存档继续" })).not.toBeInTheDocument();
    expect(desktopLaunchButton()).toBeEnabled();
  });

  it("keeps a resumable save visible when no screenshot was captured", () => {
    render(<LaunchControls
      gameId="game-without-shot"
      coreOptions={cores.slice(0, 1)}
      dosEntries={[]}
      defaultDosEntry={null}
      latestSave={{
        saveStateId: "save-without-shot",
        sizeBytes: 2048,
        screenshotUrl: null,
        createdAtMs: Date.parse("2026-08-30T12:00:00+08:00"),
        coreId: "mgba",
        coreName: "mGBA",
      }}
    />);

    expect(screen.queryByText("还没有可继续的存档")).not.toBeInTheDocument();
    expect(screen.getByRole("img", { name: "最近存档无预览图" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "从存档继续" })).toHaveLength(1);
    expect(screen.queryByAltText("最近存档")).not.toBeInTheDocument();
  });
  it("keeps phone save recovery primary and offers a separate fresh launch with usable settings", async () => {
    vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
    const user = userEvent.setup();
    render(<LaunchControls gameId="phone-game" coreOptions={cores} dosEntries={[]} defaultDosEntry={null} latestSave={{ saveStateId: "phone-save", coreId: "mgba", coreName: "mGBA", screenshotUrl: null, sizeBytes: 512, createdAtMs: 1000 }} />);
    expect(screen.queryByRole("complementary", { name: "启动游戏" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "从存档继续" }));
    await waitFor(() => expect(requests).toHaveLength(1));
    expect(JSON.parse(requests[0])).toMatchObject({ gameId: "phone-game", saveStateId: "phone-save" });
    const trigger = screen.getByRole("button", { name: "启动选项" });
    await user.click(trigger);
    const sheet = screen.getByRole("dialog", { name: "启动选项" });
    await user.selectOptions(within(sheet).getByRole("combobox", { name: "运行方式" }), "gambatte");
    await user.click(within(sheet).getByRole("button", { name: "从头开始" }));
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(JSON.parse(requests[1])).toMatchObject({ gameId: "phone-game", coreId: "gambatte", saveStateId: null });
    await user.keyboard("{Escape}");
    expect(trigger).toHaveFocus();
  });

  it("keeps DOS program selection reachable on a phone", async () => {
    vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
    const user = userEvent.setup();
    render(<LaunchControls gameId="phone-dos" coreOptions={[{ coreId: "dosbox_pure", name: "DOSBox Pure", isDefault: true, status: "READY", reasons: [] }]} dosEntries={dosEntries} defaultDosEntry="GAMES/DOOM.EXE" />);
    await user.click(screen.getByRole("button", { name: "启动选项" }));
    const sheet = screen.getByRole("dialog", { name: "启动选项" });
    expect(within(sheet).getByRole("combobox", { name: "启动程序" })).toHaveValue("GAMES/DOOM.EXE");
    await user.selectOptions(within(sheet).getByRole("combobox", { name: "启动程序" }), "");
    await user.click(within(sheet).getByRole("button", { name: "开始游戏" }));
    await waitFor(() => expect(requests).toHaveLength(1));
    expect(JSON.parse(requests[0])).toMatchObject({ gameId: "phone-dos", coreId: "dosbox_pure", dosEntry: null });
  });

});
