import { ListFilters } from "@/components/list-filters";
import { PageHeader } from "@/components/ui";
import { GameGrid, type GameSummary } from "@/features/library/game-grid";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";

export const metadata = { title: "游戏库" };

type PlatformInstance = { id: string; name: string; enabled: boolean; platformId: string };

export default async function LibraryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "platformId", "platformInstanceId", "sort"]);
  const [games, instances] = await Promise.all([
    backendJSON<ListResponse<GameSummary>>(withQuery("/api/v1/games", values)),
    backendJSON<ListResponse<PlatformInstance>>("/api/v1/admin/platform-instances?enabled=true&limit=100"),
  ]);
  return (
    <>
      <PageHeader title="游戏库" description="浏览已发布游戏，查看兼容性和存档，再选择本次运行核心。" />
      <ListFilters action="/library" placeholder="搜索游戏标题…" values={values} filters={[{ name: "platformId", label: "基础平台", options: [{ value: "", label: "所有平台" }, { value: "arcade", label: "Arcade" }, { value: "gba", label: "Game Boy Advance" }, { value: "nes", label: "NES" }, { value: "snes", label: "SNES" }, { value: "gbc", label: "Game Boy / Color" }, { value: "dos", label: "DOS" }] }, { name: "platformInstanceId", label: "平台目录", dependsOn: "platformId", options: [{ value: "", label: "所有目录" }, ...instances.items.filter((item) => item.enabled).map((item) => ({ value: item.id, label: item.name, parentValue: item.platformId }))] }]} />
      <GameGrid games={games.items} filtered={Object.values(values).some(Boolean)} />
    </>
  );
}
