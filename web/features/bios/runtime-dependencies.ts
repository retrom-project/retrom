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
    if (query && !`${item.logicalName} ${item.coreName}`.toLocaleLowerCase("zh-CN").includes(query)) return false;
    if (filters.coreId && item.coreId !== filters.coreId) return false;
    if (filters.status && item.status !== filters.status) return false;
    if (filters.quick === "ATTENTION" && !isBIOSAttention(item)) return false;
    if (filters.quick === "REQUIRED" && item.requirementMode !== "REQUIRED") return false;
    if (filters.quick === "OPTIONAL" && item.requirementMode !== "OPTIONAL") return false;
    return true;
  });
}

export type DATVersion = {
  id: string;
  coreId: string;
  coreName: string;
  coreArtifactId: string;
  source: string;
  compatibilityStatus: string;
  parseStatus: string;
  active: boolean;
  machineCount: number | null;
  romEntryCount: number | null;
  diskEntryCount: number | null;
  biosSetCount: number | null;
  version: number;
  updatedAtMs: number;
  jobId?: string | null;
  jobState?: string | null;
  jobVersion?: number | null;
  diffJobId?: string | null;
  diffStatus: "NOT_READY" | "NOT_APPLICABLE" | "NOT_RUN" | "PENDING" | "RUNNING" | "READY" | "STALE" | "FAILED";
  diffErrorCode?: string | null;
  diffVersion?: number | null;
};

export type CoreArtifact = {
  id: string;
  coreId: string;
  coreName: string;
  emulatorjsVersion: string;
  bundleVersion: string;
  enabled: boolean;
  version: number;
};

export type DATQuickFilter = "ALL" | "READY" | "WORKING" | "ATTENTION" | "HISTORY";

export type DATFilters = {
  query: string;
  source: string;
  parseStatus: string;
  quick: DATQuickFilter;
};

export function filterDATVersions(items: DATVersion[], filters: DATFilters) {
  const query = filters.query.trim().toLocaleLowerCase("zh-CN");
  return items.filter((item) => {
    if (query && !item.coreName.toLocaleLowerCase("zh-CN").includes(query)) return false;
    if (filters.source && item.source !== filters.source) return false;
    if (filters.parseStatus && item.parseStatus !== filters.parseStatus) return false;
    if (filters.quick === "READY" && (item.active || item.parseStatus !== "READY" || item.diffStatus !== "READY")) return false;
    if (filters.quick === "WORKING" && !["PENDING", "PARSING"].includes(item.parseStatus) && !["PENDING", "RUNNING"].includes(item.diffStatus)) return false;
    if (filters.quick === "ATTENTION" && !["FAILED", "CANCELLED"].includes(item.parseStatus) && item.diffStatus !== "FAILED") return false;
    if (filters.quick === "HISTORY" && (item.active || item.parseStatus !== "READY" || item.source !== "BUILTIN")) return false;
    return true;
  });
}

export function summarizeDAT(items: DATVersion[]) {
  return {
    all: items.length,
    active: items.filter((item) => item.active).length,
    ready: items.filter((item) => !item.active && item.parseStatus === "READY" && item.diffStatus === "READY").length,
    working: items.filter((item) => ["PENDING", "PARSING"].includes(item.parseStatus) || ["PENDING", "RUNNING"].includes(item.diffStatus)).length,
    attention: items.filter((item) => ["FAILED", "CANCELLED"].includes(item.parseStatus) || item.diffStatus === "FAILED").length,
    history: items.filter((item) => !item.active && item.parseStatus === "READY" && item.source === "BUILTIN").length,
  };
}
