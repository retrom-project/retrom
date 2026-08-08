export type Platform = { id: string; name: string; enabled: boolean; cores: Array<{ id: string; name: string; enabled: boolean }> };

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
    if (filters.platformId && instance.platformId !== filters.platformId) return false;
    if (filters.status === "ENABLED" && !instance.enabled) return false;
    if (filters.status === "DISABLED" && instance.enabled) return false;
    if (!query) return true;
    return [instance.name, instance.platformName, instance.description]
      .some((value) => value.toLocaleLowerCase("zh-CN").includes(query));
  }).sort((left, right) => {
    if (filters.sort === "NAME") return stableNameOrder(left, right);
    if (filters.sort === "GAME_COUNT") return right.gameCount - left.gameCount || stableNameOrder(left, right);
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
