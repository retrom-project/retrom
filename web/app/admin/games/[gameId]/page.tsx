import { ButtonLink, PageHeader } from "@/components/ui";
import { AdminGameManager, type AdminGame, type PlatformInstanceOption, type ScrapeCandidate } from "@/features/games/admin-game-manager";
import { backendJSON, type ListResponse } from "@/lib/backend";

export default async function AdminGameDetail({ params }: { params: Promise<{ gameId: string }> }) {
  const { gameId } = await params;
  const [game, instances, scrape] = await Promise.all([backendJSON<AdminGame>(`/api/v1/admin/games/${gameId}`), backendJSON<ListResponse<PlatformInstanceOption>>("/api/v1/admin/platform-instances"), backendJSON<{ items: ScrapeCandidate[] }>(`/api/v1/admin/games/${gameId}/scrape-candidates`)]);
  return <><PageHeader title="游戏管理详情" description={`${game.title} 的管理面只创建不可变 revision，不直接改写已发布历史。`} actions={<ButtonLink href={`/games/${gameId}`} secondary>查看用户详情</ButtonLink>} /><AdminGameManager game={game} platformInstances={instances.items} candidates={scrape.items} /></>;
}
