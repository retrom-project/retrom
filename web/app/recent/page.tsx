import { EmptyState, PageHeader } from "@/components/ui";
import { RecentHistory } from "@/features/recent/recent-history";
import type { RecentGame } from "@/features/recent/recent-games";
import { backendJSON } from "@/lib/backend";

export const metadata = { title: "最近游玩" };

export default async function RecentGamesPage() {
  const response = await backendJSON<{ generatedAtMs: number; items: RecentGame[] }>("/api/v1/recent-games");
  return <div className="page-layout page-layout-recent">
    <PageHeader eyebrow="我的游戏" title="最近游玩" description="按最近一次游玩时间排序，快速回到你刚刚放下的游戏。" />
    {response.items.length === 0
      ? <EmptyState title="还没有游玩记录" description="从游戏库启动一次游戏后，这里会显示最近玩过的内容。" />
      : <RecentHistory games={response.items} nowMs={response.generatedAtMs} />}
  </div>;
}
