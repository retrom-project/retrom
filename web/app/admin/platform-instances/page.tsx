import { EmptyState, PageHeader } from "@/components/ui";
import { PlatformManager, type Platform, type PlatformInstance } from "@/features/platforms/platform-manager";
import { backendJSON, type ListResponse } from "@/lib/backend";

export const metadata = { title: "平台目录" };

export default async function PlatformInstancesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const [instances, platforms, params] = await Promise.all([backendJSON<ListResponse<PlatformInstance>>("/api/v1/admin/platform-instances"), backendJSON<ListResponse<Platform>>("/api/v1/admin/platforms"), searchParams]);
  return <><PageHeader title="平台目录" description="同一基础平台可拥有多个目录，每个目录维护独立默认核心与游戏归属。" />{instances.items.length === 0 ? <EmptyState title="还没有平台目录" description="使用下方表单创建第一个导入目标。" /> : null}<PlatformManager instances={instances.items} platforms={platforms.items} createOpen={params.create === "1" || instances.items.length === 0} /></>;
}
