import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NetplayGame, NetplayRoom } from "./client";
import { NetplayRoomLobby } from "./room-lobby";

const auth = vi.hoisted(() => ({ fetch: vi.fn() }));
const navigation = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ authenticatedFetch: auth.fetch }) }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: navigation.replace }) }));
vi.mock("next/image", () => ({ default: ({ alt }: { alt: string }) => <span role="img" aria-label={alt} /> }));

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  onerror: (() => void) | null = null;
  close = vi.fn();
  addEventListener = vi.fn();
  constructor(public readonly url: string) { FakeEventSource.instances.push(this); }
}

const roomId = "01980000-0000-7000-8000-000000000001";
const hostMemberId = "01980000-0000-7000-8000-000000000002";

function game(): NetplayGame {
  return {
    gameId: "01980000-0000-7000-8000-000000000003",
    title: "F1 Race",
    coverUrl: null,
    platformId: "nes",
    platformName: "Nintendo Entertainment System",
    platformInstanceId: "01980000-0000-7000-8000-000000000004",
    platformInstanceName: "NES 游戏",
    lastPlayedAtMs: null,
    addedAtMs: 100,
    availability: "SUPPORTED",
    blockerCode: null,
    netplayProfiles: [
      { id: "fceumm-423-f1race-v1", coreId: "fceumm", coreName: "FCEUmm", emulatorjsVersion: "4.2.3", maxPlayers: 2 },
      { id: "fceumm-423-f1race-alt", coreId: "fceumm", coreName: "FCEUmm 严格", emulatorjsVersion: "4.2.3", maxPlayers: 2 },
    ],
  };
}

function room(state: NetplayRoom["state"] = "DRAFT"): NetplayRoom {
  const selected = game();
  return {
    roomId,
    state,
    version: 3,
    game: state === "DRAFT" ? null : {
      gameId: selected.gameId, title: selected.title, platformName: selected.platformName,
      profileId: selected.netplayProfiles[0]!.id, coreName: "FCEUmm", emulatorjsVersion: "4.2.3", maxPlayers: 2,
    },
    members: [{ memberId: hostMemberId, playerNo: 1, role: "HOST", displayName: "Host", avatarRef: null, ready: false, connectionState: "NOT_CONNECTED" }],
    currentSession: null,
    permissions: {
      host: true, member: true, canSelectGame: true, canJoin: false,
      canReady: state === "WAITING", canStart: false, canClose: true,
    },
    selfMemberId: hostMemberId,
    expiresAtMs: 2_000,
    serverNowMs: 1_000,
    endedAtMs: null,
    endReason: null,
  };
}

describe("NetplayRoomLobby", () => {
  beforeEach(() => {
    auth.fetch.mockReset();
    navigation.replace.mockReset();
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    window.history.replaceState({}, "", `/netplay/rooms/${roomId}`);
  });
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

  it("traps focus in the profile dialog, closes with Escape, and restores the trigger", async () => {
    const user = userEvent.setup();
    render(<NetplayRoomLobby initialRoom={room()} games={[game()]} />);
    const trigger = screen.getByRole("button", { name: "选择" });
    await user.click(trigger);
    const dialog = screen.getByRole("dialog", { name: "选择 F1 Race 的联机配置" });
    const firstProfile = within(dialog).getByRole("button", { name: /FCEUmmEmulatorJS/ });
    expect(firstProfile).toHaveFocus();
    await user.keyboard("{Shift>}{Tab}{/Shift}");
    expect(within(dialog).getByRole("button", { name: "取消" })).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it("copies the room link and requires confirmation before closing", async () => {
    auth.fetch.mockResolvedValue(new Response(null, { status: 204 }));
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
    render(<NetplayRoomLobby initialRoom={room("WAITING")} games={[game()]} />);

    await user.click(screen.getByRole("button", { name: "复制房间链接" }));
    expect(writeText).toHaveBeenCalledWith(window.location.href);
    expect(screen.getByRole("button", { name: "已复制链接" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "关闭房间" }));
    const confirmation = screen.getByRole("alertdialog", { name: "关闭联机房间？" });
    expect(auth.fetch).not.toHaveBeenCalled();
    await user.click(within(confirmation).getByRole("button", { name: "关闭房间" }));
    await waitFor(() => expect(auth.fetch).toHaveBeenCalledOnce());
    expect(auth.fetch.mock.calls[0]?.[0]).toBe(`/api/v1/netplay/rooms/${roomId}`);
    expect(auth.fetch.mock.calls[0]?.[1]?.method).toBe("DELETE");
    await waitFor(() => expect(navigation.replace).toHaveBeenCalledWith("/netplay"));
  });

  it("shows unsupported P3 and P4 without issuing a seat claim", () => {
    const guestRoom = room("WAITING");
    guestRoom.permissions = { ...guestRoom.permissions, host: false, member: false, canJoin: true };
    guestRoom.selfMemberId = null;
    render(<NetplayRoomLobby initialRoom={guestRoom} games={[game()]} />);

    expect(screen.getAllByText("当前游戏不支持")).toHaveLength(2);
    expect(screen.queryByRole("button", { name: "选择 P3" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "选择 P4" })).not.toBeInTheDocument();
    expect(auth.fetch).not.toHaveBeenCalled();
  });
});
