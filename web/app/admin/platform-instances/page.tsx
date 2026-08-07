import { EmptyState, PageHeader } from "@/components/ui";
import { PlatformManager, type Platform, type PlatformInstance } from "@/features/platforms/platform-manager";
import { backendJSON, type ListResponse } from "@/lib/backend";

export const metadata = { title: "平台目录" };

export default async function PlatformInstancesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const [instances, platforms, params] = await Promise.all([backendJSON<ListResponse<PlatformInstance>>("/api/v1/admin/platform-instances"), backendJSON<ListResponse<Platform>>("/api/v1/admin/platforms"), searchParams]);
  return <div className="page-layout page-layout-admin"><PageHeader title="游戏目录" description="按游戏平台整理游戏，并为每个目录设置推荐运行方式。" />{instances.items.length === 0 ? <EmptyState title="还没有游戏目录" description="使用下方表单创建第一个导入目标。" /> : null}<PlatformManager instances={instances.items} platforms={platforms.items} createOpen={params.create === "1" || instances.items.length === 0} /></div>;
}
