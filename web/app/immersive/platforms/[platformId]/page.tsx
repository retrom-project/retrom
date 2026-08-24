import { GameListView } from "@/features/immersive/game-list-view";

export const metadata = { title: "选择游戏 · 沉浸模式" };

export default async function ImmersivePlatformPage({ params, searchParams }: {
  params: Promise<{ platformId: string }>;
  searchParams: Promise<{ gameId?: string | string[] }>;
}) {
  const [{ platformId }, query] = await Promise.all([params, searchParams]);
  const initialGameId = Array.isArray(query.gameId) ? query.gameId[0] : query.gameId;
  return <GameListView platformId={platformId} initialGameId={initialGameId} />;
}
