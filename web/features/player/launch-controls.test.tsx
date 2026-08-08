import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LaunchControls, type CoreOption, type DOSEntry } from "./launch-controls";

const navigation = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));

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
    expect(window.localStorage.getItem("retrom:preferred-core:game-1")).toBe("gambatte");
    await user.click(screen.getByRole("button", { name: "开始游戏" }));

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
    expect(window.localStorage.getItem("retrom:preferred-core:remembered-game")).toBeNull();
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
    expect(window.localStorage.getItem("retrom:preferred-core:game-1")).toBeNull();
  });

  it("keeps the fullscreen document alive by using App Router navigation", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ launchId: "launch-1", playUrl: "/play/launch-1" }), { status: 201, headers: { "Content-Type": "application/json" } })));
    render(<LaunchControls gameId="game-1" coreOptions={cores.slice(0, 1)} dosEntries={[]} defaultDosEntry={null} />);

    await user.click(screen.getByRole("button", { name: "开始游戏" }));

    await waitFor(() => expect(navigation.replace).toHaveBeenCalledWith("/play/launch-1"));
  });

  it("exits fullscreen and exposes a repair entry when BIOS blocks launch", async () => {
    const user = userEvent.setup();
    const exitFullscreen = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => document.documentElement });
    Object.defineProperty(document, "exitFullscreen", { configurable: true, value: exitFullscreen });
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: { code: "LAUNCH_BLOCKED", message: "LAUNCH_BIOS_MISSING" } }), { status: 422, headers: { "Content-Type": "application/json" } })));
    render(<LaunchControls gameId="blocked-game" coreOptions={cores.slice(0, 1)} dosEntries={[]} defaultDosEntry={null} />);

    await user.click(screen.getByRole("button", { name: "开始游戏" }));

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
    await user.click(screen.getByRole("button", { name: "开始游戏" }));

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(JSON.parse(requests[0])).toMatchObject({ coreId: "dosbox_pure", dosEntry: null });
  });
});
