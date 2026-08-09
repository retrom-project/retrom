import { PageHeader } from "@/components/ui";
import { AdminGameBrowser } from "@/features/games/admin-game-browser";
import { collectAdminGamePages, type AdminGameFilters, type AdminGamePage } from "@/features/games/admin-game-library";
import { scalarSearchParams, withQuery } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "游戏管理" };

export default async function AdminGamesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "platformId", "platformInstanceId", "status", "runtime", "sort"]);
  const result = await collectAdminGamePages((cursor) => backendJSON<AdminGamePage>(withQuery("/api/v1/admin/games", { limit: "100", ...(cursor ? { cursor } : {}) })));
  const initialFilters: AdminGameFilters = {
    query: values.q ?? "",
    platformId: values.platformId ?? "",
    platformInstanceId: values.platformInstanceId ?? "",
    visibility: values.status === "PUBLISHED" || values.status === "DELETED" ? values.status : "ALL",
    runtime: values.runtime === "READY" || values.runtime === "ATTENTION" ? values.runtime : "ALL",
    sort: values.sort === "TITLE_ASC" || values.sort === "ADDED_DESC" ? values.sort : "UPDATED_DESC",
  };
  return <>
    <PageHeader eyebrow="管理后台" title="游戏管理" description="维护已发布游戏的信息、媒体和运行配置，快速定位需要处理的内容。" />
    <AdminGameBrowser games={result.items} nowMs={result.generatedAtMs} initialFilters={initialFilters} />
  </>;
}
