import { PageHeader } from "@/components/ui";
import { AdminGameBrowser } from "@/features/games/admin-game-browser";
import { adminGameURL, type AdminGameFilters, type AdminGamePage } from "@/features/games/admin-game-library";
import { scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "游戏管理" };

export default async function AdminGamesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "tagId", "platformId", "platformInstanceId", "status", "runtime", "sort"]);
  const initialFilters: AdminGameFilters = {
    query: values.q ?? "",
    platformId: values.platformId ?? "",
    platformInstanceId: values.platformInstanceId ?? "",
    tagId: values.tagId ?? "",
    visibility: values.status === "PUBLISHED" || values.status === "DELETED" ? values.status : "ALL",
    runtime: values.runtime === "READY" || values.runtime === "ATTENTION" || values.runtime === "DELETED" ? values.runtime : "ALL",
    sort: values.sort === "TITLE_ASC" || values.sort === "ADDED_DESC" ? values.sort : "UPDATED_DESC",
  };
  const result = await backendJSON<AdminGamePage>(adminGameURL(initialFilters));
  return <>
    <PageHeader title="游戏管理" description="维护已发布游戏的信息、媒体和运行配置，快速定位需要处理的内容。" />
    <AdminGameBrowser initialPage={result} initialFilters={initialFilters} />
  </>;
}
