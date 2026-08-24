import { fetchImmersiveGames, type ImmersiveGameList } from "./api";
import { mergeGamePage } from "./game-list-state";

type GamePageFetcher = typeof fetchImmersiveGames;

export async function fetchInitialGameList(
  platformId: string,
  requestedGameId: string | undefined,
  signal: AbortSignal,
  fetchPage: GamePageFetcher = fetchImmersiveGames,
): Promise<ImmersiveGameList> {
  const first = await fetchPage(platformId, null, signal);
  if (!requestedGameId || first.items.some((game) => game.gameId === requestedGameId)) {
    return first;
  }

  let items = first.items;
  let nextCursor = first.nextCursor;
  while (nextCursor) {
    const next = await fetchPage(platformId, nextCursor, signal);
    items = mergeGamePage(items, next.items);
    nextCursor = next.nextCursor;
    if (items.some((game) => game.gameId === requestedGameId)) {break;}
  }
  return { ...first, items, nextCursor };
}
