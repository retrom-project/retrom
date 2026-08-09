export const PLAYER_CONTROLS_REVEAL_EDGE_PX = 32;

export function shouldRevealPlayerControls(clientY: number) {
  return Number.isFinite(clientY) && clientY >= 0 && clientY <= PLAYER_CONTROLS_REVEAL_EDGE_PX;
}
