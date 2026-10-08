import { GameDetail } from "@/features/games/game-detail";
export default async function Page({
  params,
}: {
  params: Promise<{ gameId: string }>;
}) {
  const { gameId } = await params;
  return <GameDetail gameId={gameId} mode="admin" />;
}
