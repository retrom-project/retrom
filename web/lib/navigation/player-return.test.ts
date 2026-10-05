import {describe, expect, it} from "vitest";
import {playerReturnIntent, playerReturnLabel} from "./player-return";

describe("player return destination", () => {
  it.each([
    ["/games/game-1", "返回游戏详情"],
    ["/library", "返回游戏库"],
    ["/recent", "返回最近游玩"],
    ["/saves", "返回我的存档"],
    ["/admin/reviews/item-1?returnTo=%2Fadmin%2Freviews", "返回审核页面"],
    ["/immersive/library/favorites?gameId=game-1&folderId=folder-1", "返回游戏列表"],
    ["/immersive/platforms/saturn?gameId=game-1", "返回游戏列表"],
  ])("keeps label and complete context together: %s", (href, label) => {
    const intent = playerReturnIntent(href);
    expect(intent.href).toBe(href);
    expect(playerReturnLabel(intent)).toBe(label);
  });

  it.each(["//external.example", "https://external.example", "/unknown", "/games/game-1/unknown"])("rejects unrecognized route %s", href => {
    expect(() => playerReturnIntent(href)).toThrow("PLAYER_RETURN_ROUTE_INVALID");
  });
});
