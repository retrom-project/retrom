import { ListFilters } from "@/components/list-filters";
import { EmptyState, PageHeader } from "@/components/ui";
import { SaveManager, type SaveItem } from "@/features/saves/save-manager";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";

export const metadata = { title: "我的存档" };

export default async function SavesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "gameId", "availability"]);
  const saves = await backendJSON<ListResponse<SaveItem>>(withQuery("/api/v1/saves", values));
  return (
    <>
      <PageHeader title="我的存档" description="查看保存画面、整理名称，并随时回到当时的游戏进度。" />
      <ListFilters action="/saves" placeholder="输入存档或游戏名称" values={values} resultCount={saves.items.length} fixedFilters={values.gameId ? [{ name: "gameId", value: values.gameId }] : []} filters={[{ name: "availability", label: "存档状态", options: [{ value: "AVAILABLE", label: "可以继续" }, { value: "ALL", label: "全部存档" }, { value: "BLOCKED", label: "当前不可用" }] }]} />
      {saves.items.length === 0 ? <EmptyState title="还没有手动存档" description="游玩时使用工具栏的“保存进度”，存档会安全地出现在这里。" /> : <SaveManager saves={saves.items} />}
    </>
  );
}
