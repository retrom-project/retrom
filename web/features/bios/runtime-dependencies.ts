export type BIOSInstallation = {
  id: string;
  md5: string;
  sha1: string;
  sha256: string;
  validatedRequirementVersion: number;
  createdAtMs: number;
};

export type BIOSRequirement = {
  id: string;
  coreId: string;
  coreName: string;
  coreArtifactId: string;
  logicalName: string;
  sourceKind: string;
  requirementMode: string;
  conditionCode?: string | null;
  enabled: boolean;
  version: number;
  status: string;
  expectedMd5?: string | null;
  activeInstallation?: BIOSInstallation | null;
};

export type BIOSQuickFilter = "ALL" | "ATTENTION" | "REQUIRED" | "OPTIONAL";

export type BIOSFilters = {
  query: string;
  coreId: string;
  status: string;
  quick: BIOSQuickFilter;
};

const BIOS_BLOCKING = new Set(["MISSING", "MISSING_ENTRY", "INVALID"]);

export function isBIOSBlocking(item: BIOSRequirement) {
  return item.requirementMode !== "OPTIONAL" && BIOS_BLOCKING.has(item.status);
}

export function isBIOSAttention(item: BIOSRequirement) {
  return isBIOSBlocking(item) || item.status === "HASH_WARNING";
}

export function summarizeBIOS(items: BIOSRequirement[]) {
  return {
    total: items.length,
    blocking: items.filter(isBIOSBlocking).length,
    warnings: items.filter((item) => item.status === "HASH_WARNING").length,
    ready: items.filter((item) => item.status === "MATCHED" || item.status === "SATISFIED_BY_CONTENT").length,
  };
}
export function filterBIOS(items: BIOSRequirement[], filters: BIOSFilters) {
  const query = filters.query.trim().toLocaleLowerCase("zh-CN");
  return items.filter((item) => {
    if (query && !`${item.logicalName} ${item.coreName}`.toLocaleLowerCase("zh-CN").includes(query)) {return false;}
    if (filters.coreId && item.coreId !== filters.coreId) {return false;}
    if (filters.status && item.status !== filters.status) {return false;}
    if (filters.quick === "ATTENTION" && !isBIOSAttention(item)) {return false;}
    if (filters.quick === "REQUIRED" && item.requirementMode !== "REQUIRED") {return false;}
    if (filters.quick === "OPTIONAL" && item.requirementMode !== "OPTIONAL") {return false;}
    return true;
  });
}
