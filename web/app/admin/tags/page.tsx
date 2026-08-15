import { PageHeader } from "@/components/ui";
import { TagManager, type TagAdminPage } from "@/features/tags/tag-manager";
import { scalarSearchParams, withQuery } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "标签管理" };

export default async function TagsPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const raw = scalarSearchParams(await searchParams, ["q", "status", "sort"]);
  const filters = {
    q: raw.q ?? "",
    status: raw.status === "DELETED" || raw.status === "ALL" ? raw.status : "ACTIVE",
    sort: raw.sort === "UPDATED_DESC" ? raw.sort : "NAME_ASC",
  };
  const initial = await backendJSON<TagAdminPage>(withQuery("/api/v1/admin/tags", { ...filters, limit: "100" }));
  return <><PageHeader eyebrow="管理后台" title="标签管理" description="建立后才能用于游戏、导入和扫描；删除只隐藏活动引用并保留历史证据。" /><TagManager initial={initial} filters={filters} /></>;
}
