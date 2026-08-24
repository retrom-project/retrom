import type { ImmersiveGame } from "./api";

export function initialGameIndex(games: readonly ImmersiveGame[], requestedGameId?: string) {
  if (!games.length) {return -1;}
  const requested = requestedGameId ? games.findIndex((game) => game.gameId === requestedGameId) : -1;
  return requested >= 0 ? requested : 0;
}

export function moveGameIndex(current: number, direction: "up" | "down", count: number) {
  if (count <= 0) {return -1;}
  const delta = direction === "up" ? -1 : 1;
  return Math.max(0, Math.min(count - 1, current + delta));
}

export function mergeGamePage(current: readonly ImmersiveGame[], incoming: readonly ImmersiveGame[]) {
  const known = new Set(current.map((game) => game.gameId));
  return [...current, ...incoming.filter((game) => !known.has(game.gameId))];
}

export function shouldPrefetchGamePage(selectedIndex: number, loadedCount: number, nextCursor: string | null) {
  return nextCursor !== null && loadedCount > 0 && selectedIndex >= loadedCount - 10;
}

export function selectionAfterRemoval(previous: readonly ImmersiveGame[], next: readonly ImmersiveGame[], gameId: string) {
  const retained = next.findIndex((game) => game.gameId === gameId);
  if (retained >= 0) {return retained;}
  const previousIndex = previous.findIndex((game) => game.gameId === gameId);
  return next.length ? Math.min(Math.max(previousIndex, 0), next.length - 1) : -1;
}
