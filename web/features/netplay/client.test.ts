import { describe, expect, it } from "vitest";
import { applyRoomSnapshot, netplayBlocker, type NetplayRoom } from "./client";

function room(roomId: string, version: number): NetplayRoom {
  return {
    roomId,
    state: "DRAFT",
    version,
    game: null,
    members: [],
    currentSession: null,
    permissions: { host: true, member: true, canSelectGame: true, canJoin: false, canReady: false, canStart: false, canClose: true },
    selfMemberId: "01980000-0000-7000-8000-000000000001",
    expiresAtMs: 1,
    serverNowMs: 1,
    endedAtMs: null,
    endReason: null,
  };
}

describe("netplay room snapshots", () => {
  it("ignores stale versions and requests refresh for gaps or another room", () => {
    const current = room("01980000-0000-7000-8000-000000000001", 4);
    expect(applyRoomSnapshot(current, room(current.roomId, 3))).toEqual({ room: current, gap: false });
    const next = room(current.roomId, 5);
    expect(applyRoomSnapshot(current, next)).toEqual({ room: next, gap: false });
    expect(applyRoomSnapshot(current, room(current.roomId, 7)).gap).toBe(true);
    expect(applyRoomSnapshot(current, room("01980000-0000-7000-8000-000000000002", 5))).toEqual({ room: current, gap: true });
  });

  it("maps every closed eligibility blocker to user-facing copy", () => {
    for (const code of ["CONTENT_NOT_ALLOWLISTED", "CORE_NOT_ALLOWLISTED", "DEPENDENCY_STALE", "GAME_UNAVAILABLE"] as const) {
      expect(netplayBlocker(code)).toBeTruthy();
    }
    expect(netplayBlocker("CONTENT_NOT_ALLOWLISTED")).toBe("当前内容类型尚未支持联机");
    expect(netplayBlocker("CORE_NOT_ALLOWLISTED")).toBe("当前平台与核心组合尚未验证联机");
  });
});
