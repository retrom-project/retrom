export type ImportListItem = {
  id: string;
  state: string;
  platformInstanceName: string;
  metadataProvider: string;
  totalItemCount: number;
  reviewPendingItemCount: number;
  failedItemCount: number;
  rejectedFileCount: number;
  unresolvedRejectedFileCount?: number;
  alreadyImportedItemCount?: number;
  alreadyImportedFileCount?: number;
  version: number;
  createdAtMs: number;
  updatedAtMs: number;
};

export type ImportTaskFilters = { query: string; directory: string; state: string };

export type ImportFileOutcome = {
  uploadFileId: string;
  name: string;
  sizeBytes: number;
  disposition: "SOURCE" | "IGNORED" | "REJECTED" | "ALREADY_IMPORTED";
  reasonCode: string | null;
  resolution: null | {
    action: "RECONFIGURED";
    replacementImportJobId: string;
    resolvedAtMs: number;
  };
};

export type ImportDetail = {
  importJobId: string;
  metadataProvider: string;
  targetPlatformInstance: { id: string; name: string };
  fileOutcomes: ImportFileOutcome[];
  alreadyImportedMatches?: Array<{
    importItemId: string;
    contentIdentityDigest: string;
    existingGame: { id: string; title: string; platformInstanceId: string; platformInstanceName: string };
  }>;
  version: number;
};

export const importStateLabels: Record<string, string> = {
  QUEUED: "排队中",
  RUNNING: "运行中",
  REVIEW_PENDING: "等待审核",
  PARTIAL_FAILURE: "需要处理",
  FAILED: "需要处理",
  COMPLETED: "已完成",
  CANCEL_REQUESTED: "正在取消",
  CANCELLED: "已取消",
};

export const importProviderLabels: Record<string, string> = {
  HASHEOUS: "Hasheous",
  NONE: "不刮削",
};

export function importTaskSummary(items: ImportListItem[]) {
  return {
    total: items.length,
    running: items.filter((item) => item.state === "RUNNING" || item.state === "QUEUED").length,
    attention: items.filter((item) => item.state === "PARTIAL_FAILURE" || item.state === "FAILED").length,
    review: items.filter((item) => item.state === "REVIEW_PENDING").length,
    completed: items.filter((item) => item.state === "COMPLETED").length,
  };
}

export function filterImportTasks(items: ImportListItem[], filters: ImportTaskFilters) {
  const query = filters.query.trim().toLocaleLowerCase("zh-CN");
  return items.filter((item) => {
    if (filters.directory && item.platformInstanceName !== filters.directory) return false;
    if (filters.state === "ATTENTION" && item.state !== "PARTIAL_FAILURE" && item.state !== "FAILED") return false;
    if (filters.state && filters.state !== "ATTENTION" && item.state !== filters.state) return false;
    if (!query) return true;
    return [item.platformInstanceName, importStateLabels[item.state] ?? item.state, importProviderLabels[item.metadataProvider] ?? item.metadataProvider]
      .some((value) => value.toLocaleLowerCase("zh-CN").includes(query));
  });
}

export function importTaskProgress(item: ImportListItem) {
  if (item.state === "QUEUED") return 5;
  if (["REVIEW_PENDING", "COMPLETED", "CANCELLED", "FAILED"].includes(item.state)) return 100;
  const known = item.reviewPendingItemCount + item.failedItemCount;
  const measured = item.totalItemCount > 0 ? Math.round(known / item.totalItemCount * 100) : 0;
  if (item.state === "PARTIAL_FAILURE") return Math.max(72, measured);
  if (item.state === "CANCEL_REQUESTED") return Math.max(20, Math.min(95, measured || 60));
  return Math.max(18, Math.min(92, measured || 48));
}

export function importTaskIssueCount(item: ImportListItem) {
  return item.failedItemCount + (item.unresolvedRejectedFileCount ?? item.rejectedFileCount ?? 0);
}

export function importTaskIssueSummary(item: ImportListItem) {
  const parts = [];
  if (item.failedItemCount) parts.push(`${item.failedItemCount} 个条目失败`);
  const rejected = item.unresolvedRejectedFileCount ?? item.rejectedFileCount ?? 0;
  if (rejected) parts.push(`${rejected} 个文件未被接受`);
  return parts.length ? parts.join("；") : "没有可报告的异常";
}

export function importTaskPhase(item: ImportListItem) {
  if (item.state === "QUEUED") return "等待识别";
  if (item.state === "RUNNING") return "识别与运行检查";
  if (item.state === "REVIEW_PENDING") return "人工审核";
  if (item.state === "PARTIAL_FAILURE" || item.state === "FAILED") return "处理异常";
  if (item.state === "COMPLETED") return "发布";
  if (item.state === "CANCEL_REQUESTED") return "正在取消";
  if (item.state === "CANCELLED") return "已取消";
  return "后台处理";
}

export function importStageIndex(item: ImportListItem) {
  if (item.state === "QUEUED") return 0;
  if (item.state === "RUNNING") return 2;
  if (item.state === "REVIEW_PENDING") return 4;
  if (item.state === "COMPLETED") return 5;
  if (item.state === "PARTIAL_FAILURE" || item.state === "FAILED") return 2;
  return 0;
}
