import type { components } from "@/lib/api/generated/schema";

export type Platform = { id: string; name: string; enabled: boolean; cores: Array<{ id: string; name: string; enabled: boolean }> };

export type PlatformRecommendations = components["schemas"]["PlatformInstanceRecommendations"];
export type PlatformRecommendationsApplyResult = components["schemas"]["PlatformInstanceRecommendationsApplyResult"];

export type PlatformInstance = {
  id: string;
  platformId: string;
  platformName: string;
  defaultCoreId: string;
  defaultCoreName: string;
  name: string;
  slug: string;
  description: string;
  sortOrder: number;
  enabled: boolean;
  version: number;
  gameCount: number;
  supportedExtensions: string[];
};

export type PlatformDirectoryFilters = {
  query: string;
  platformId: string;
  status: "ALL" | "ENABLED" | "DISABLED";
  sort: "ORDER" | "NAME" | "GAME_COUNT";
};

function stableNameOrder(left: PlatformInstance, right: PlatformInstance) {
  return left.name.localeCompare(right.name, "zh-CN") || left.id.localeCompare(right.id);
}

export function filterPlatformDirectories(instances: PlatformInstance[], filters: PlatformDirectoryFilters) {
  const query = filters.query.trim().toLocaleLowerCase("zh-CN");
  return instances.filter((instance) => {
    if (filters.platformId && instance.platformId !== filters.platformId) {return false;}
    if (filters.status === "ENABLED" && !instance.enabled) {return false;}
    if (filters.status === "DISABLED" && instance.enabled) {return false;}
    if (!query) {return true;}
    return [instance.name, instance.platformName, instance.description]
      .some((value) => value.toLocaleLowerCase("zh-CN").includes(query));
  }).sort((left, right) => {
    if (filters.sort === "NAME") {return stableNameOrder(left, right);}
    if (filters.sort === "GAME_COUNT") {return right.gameCount - left.gameCount || stableNameOrder(left, right);}
    return left.sortOrder - right.sortOrder || left.id.localeCompare(right.id);
  });
}

export function platformDirectorySummary(instances: PlatformInstance[]) {
  return {
    total: instances.length,
    enabled: instances.filter((instance) => instance.enabled).length,
    disabled: instances.filter((instance) => !instance.enabled).length,
  };
}

export function canReorderPlatformDirectories(filters: PlatformDirectoryFilters) {
  return !filters.query.trim() && !filters.platformId && filters.status === "ALL" && filters.sort === "ORDER";
}

export function summarizeRecommendations(items: PlatformRecommendations["items"]): PlatformRecommendations["summary"] {
  const summary: PlatformRecommendations["summary"] = {
    totalCount: items.length,
    activeCount: 0,
    customizedCount: 0,
    coveredByEquivalentCount: 0,
    suppressedCount: 0,
    missingCount: 0,
  };
  for (const item of items) {
    if (item.state === "ACTIVE") {summary.activeCount++;}
    if (item.state === "CUSTOMIZED") {summary.customizedCount++;}
    if (item.state === "COVERED_BY_EQUIVALENT") {summary.coveredByEquivalentCount++;}
    if (item.state === "SUPPRESSED") {summary.suppressedCount++;}
    if (item.state === "MISSING") {summary.missingCount++;}
  }
  return summary;
}
