import { PlatformManager, type Platform, type PlatformInstance } from "@/features/platforms/platform-manager";
import type { ListResponse } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "游戏目录" };

export default async function PlatformInstancesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const [instances, platforms, params] = await Promise.all([backendJSON<ListResponse<PlatformInstance>>("/api/v1/admin/platform-instances"), backendJSON<ListResponse<Platform>>("/api/v1/admin/platforms"), searchParams]);
  return <PlatformManager instances={instances.items} platforms={platforms.items} createOpen={params.create === "1" || instances.items.length === 0} />;
}
