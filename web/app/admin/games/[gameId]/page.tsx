import { ButtonLink, PageHeader } from "@/components/ui";
import { AdminGameManager, type AdminGame, type PlatformInstanceOption, type ScrapeCandidate } from "@/features/games/admin-game-manager";
import type { ListResponse } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export default async function AdminGameDetail({ params }: { params: Promise<{ gameId: string }> }) {
  const { gameId } = await params;
  const [game, instances, scrape] = await Promise.all([
    backendJSON<AdminGame>(`/api/v1/admin/games/${gameId}`),
    backendJSON<ListResponse<PlatformInstanceOption>>("/api/v1/admin/platform-instances"),
    backendJSON<{ items: ScrapeCandidate[] }>(`/api/v1/admin/games/${gameId}/scrape-candidates`),
  ]);
  const platformName = instances.items.find((item) => item.id === game.platformInstance.id)?.platformName ?? game.platformId;
  return <>
    <PageHeader
      eyebrow={`游戏管理 / ${platformName}`}
      title={game.title}
      description="维护发布信息、媒体、游戏内容与运行环境；历史版本与存档引用都会保留。"
      actions={<ButtonLink href="/admin/games" secondary>← 返回游戏管理</ButtonLink>}
    />
    <AdminGameManager game={game} platformInstances={instances.items} candidates={scrape.items} />
  </>;
}
