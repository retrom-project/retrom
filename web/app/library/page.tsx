import { ListFilters } from "@/components/list-filters";
import { PageHeader } from "@/components/ui";
import { GameGrid, type GameSummary } from "@/features/library/game-grid";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";

export const metadata = { title: "游戏库" };

type PlatformInstance = { id: string; name: string; enabled: boolean; platformId: string };

export default async function LibraryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "platformId", "platformInstanceId"]);
  const [games, instances] = await Promise.all([
    backendJSON<ListResponse<GameSummary>>(withQuery("/api/v1/games", values)),
    backendJSON<ListResponse<PlatformInstance>>("/api/v1/admin/platform-instances?enabled=true&limit=100"),
  ]);
  return (
    <div className="page-layout page-layout-library">
      <PageHeader title="游戏库" description="按名称或平台找到游戏，打开详情后即可使用推荐配置开始游玩。" />
      <ListFilters action="/library" placeholder="输入游戏名称" values={values} resultCount={games.items.length} filters={[{ name: "platformId", label: "游戏平台", options: [{ value: "", label: "所有平台" }, { value: "arcade", label: "街机" }, { value: "gba", label: "Game Boy Advance" }, { value: "nes", label: "NES" }, { value: "snes", label: "SNES" }, { value: "gbc", label: "Game Boy / Color" }, { value: "dos", label: "DOS" }] }, { name: "platformInstanceId", label: "游戏目录", dependsOn: "platformId", options: [{ value: "", label: "所有目录" }, ...instances.items.filter((item) => item.enabled).map((item) => ({ value: item.id, label: item.name, parentValue: item.platformId }))] }]} />
      <GameGrid games={games.items} filtered={Object.values(values).some(Boolean)} />
    </div>
  );
}
