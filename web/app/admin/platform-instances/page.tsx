import { PlatformManager, type Platform, type PlatformInstance, type PlatformRecommendations } from "@/features/platforms/platform-manager";
import type { ListResponse } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "游戏目录" };

export default async function PlatformInstancesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const [instances, platforms, recommendations, params] = await Promise.all([
    backendJSON<ListResponse<PlatformInstance>>("/api/v1/admin/platform-instances"),
    backendJSON<ListResponse<Platform>>("/api/v1/admin/platforms"),
    backendJSON<PlatformRecommendations>("/api/v1/admin/platform-instances/recommendations").catch(() => null),
    searchParams,
  ]);
  return <PlatformManager instances={instances.items} platforms={platforms.items} recommendations={recommendations} createOpen={params.create === "1"} />;
}
