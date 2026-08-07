import { ButtonLink, PageHeader } from "@/components/ui";
import { AdminGameManager, type AdminGame, type PlatformInstanceOption, type ScrapeCandidate } from "@/features/games/admin-game-manager";
import { backendJSON, type ListResponse } from "@/lib/backend";

export default async function AdminGameDetail({ params }: { params: Promise<{ gameId: string }> }) {
  const { gameId } = await params;
  const [game, instances, scrape] = await Promise.all([backendJSON<AdminGame>(`/api/v1/admin/games/${gameId}`), backendJSON<ListResponse<PlatformInstanceOption>>("/api/v1/admin/platform-instances"), backendJSON<{ items: ScrapeCandidate[] }>(`/api/v1/admin/games/${gameId}/scrape-candidates`)]);
  return <><PageHeader title={game.title} description="编辑发布信息和媒体，维护游戏文件与运行环境。所有历史版本都会保留。" actions={<ButtonLink href={`/games/${gameId}`} secondary>查看用户详情</ButtonLink>} /><AdminGameManager game={game} platformInstances={instances.items} candidates={scrape.items} /></>;
}
