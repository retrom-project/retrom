import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LaunchControls, type CoreOption, type DOSEntry } from "./launch-controls";

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

    await user.selectOptions(screen.getByLabelText("本次运行核心"), "gambatte");
    expect(screen.getByText("启动时验证此核心")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "开始游戏" }));

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(JSON.parse(requests[0])).toMatchObject({ gameId: "game-1", coreId: "gambatte", dosEntry: null });
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
