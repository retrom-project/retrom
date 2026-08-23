export const PLAYER_CONTROLS_REVEAL_EDGE_PX = 32;

export function shouldRevealPlayerControls(clientY: number) {
  return Number.isFinite(clientY) && clientY >= 0 && clientY <= PLAYER_CONTROLS_REVEAL_EDGE_PX;
}

export function shouldRevealPlayerControlsForKey(key: string) {
  return key === "Tab";
}

export function shouldAutoHidePlayerControls(state: "loading" | "running" | "error", paused: boolean, pinned: boolean) {
  return state === "running" && !paused && !pinned;
}
