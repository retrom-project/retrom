const labels = {
  HOME: "返回首页",
  LIBRARY: "返回游戏库",
  GAME: "返回游戏详情",
  RECENT: "返回最近游玩",
  SAVES: "返回我的存档",
  REVIEW: "返回审核页面",
  IMMERSIVE_HOME: "返回沉浸模式",
  IMMERSIVE_LIST: "返回游戏列表",
} as const;

export type PlayerReturnIntent = { kind: keyof typeof labels; href: string };

export function playerReturnLabel(intent: PlayerReturnIntent): string {
  return labels[intent.kind];
}

// Route ownership stays in Web. Views receive a destination, never classify URLs.
export function playerReturnIntent(href: string): PlayerReturnIntent {
  if (!href.startsWith("/") || href.startsWith("//") || href.includes("\\")) {
    throw new Error("PLAYER_RETURN_ROUTE_INVALID");
  }
  const path = href.split("?")[0];
  const exact: Record<string, PlayerReturnIntent["kind"]> = {
    "/": "HOME", "/library": "LIBRARY", "/recent": "RECENT", "/saves": "SAVES", "/immersive": "IMMERSIVE_HOME",
  };
  const kind = exact[path];
  if (kind) {return {kind, href};}
  if (/^\/games\/[a-zA-Z0-9-]+$/u.test(path)) {return {kind: "GAME", href};}
  if (/^\/admin\/reviews\/[a-zA-Z0-9-]+$/u.test(path)) {return {kind: "REVIEW", href};}
  if (/^\/immersive\/(?:library\/(?:all|recent|favorites|saves)|platforms\/[a-z0-9-]+)$/u.test(path)) {
    return {kind: "IMMERSIVE_LIST", href};
  }
  throw new Error("PLAYER_RETURN_ROUTE_INVALID");
}
