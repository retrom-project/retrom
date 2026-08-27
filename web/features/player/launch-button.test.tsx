import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LaunchButton } from "./launch-button";

const navigation = vi.hoisted(() => ({ replace: vi.fn(), replacePlayerDocument: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: navigation.replace }) }));
vi.mock("@/lib/player-document-navigation", () => ({
  replaceWithPlayerDocument: navigation.replacePlayerDocument,
}));
vi.mock("./orientation", () => ({
  requestFullscreenAndLandscape: vi.fn().mockResolvedValue({ fullscreen: "denied", orientation: "unsupported" }),
  unlockLandscape: vi.fn(),
}));

describe("LaunchButton thread capability guard", () => {
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); navigation.replace.mockReset(); navigation.replacePlayerDocument.mockReset(); });

  it("does not send an impossible threaded launch", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("isSecureContext", false);
    vi.stubGlobal("crossOriginIsolated", false);
    vi.stubGlobal("SharedArrayBuffer", undefined);
    render(<LaunchButton gameId="game-1" coreId="ppsspp" requiresThreads />);
    await user.click(screen.getByRole("button", { name: "开始游戏" }));

    expect(fetchMock).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("远程明文 HTTP 无法提供 SharedArrayBuffer");
  });

  it("replays a game through an ordinary single-player launch even when the recent session was netplay", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      launchId: "launch-single", playUrl: "/play/launch-single",
    }), { status: 201, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("isSecureContext", true);
    vi.stubGlobal("crossOriginIsolated", true);

    render(<LaunchButton gameId="game-from-netplay" returnTo="/" label="再玩一次" />);
    await user.click(screen.getByRole("button", { name: "再玩一次" }));

    await vi.waitFor(() => expect(navigation.replacePlayerDocument).toHaveBeenCalledWith("/play/launch-single", navigation.replace));
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/v1/launches");
    const request = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(request.method).toBe("POST");
    expect(JSON.parse(request.body as string)).toEqual({
      gameId: "game-from-netplay",
      coreId: null,
      saveStateId: null,
      dosEntry: null,
      returnTo: "/",
      clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true },
    });
  });
});
