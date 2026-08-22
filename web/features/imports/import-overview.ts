import type { components } from "@/lib/api/generated/schema";
import type { ImportListItem } from "./import-workflow";
import { importProviderLabels, importStateLabels, importTaskIssueCount, importTaskPhase } from "./import-workflow";

export type ImportOverviewSummary = components["schemas"]["ImportOverviewSummary"];
export type PegasusImportSummary = components["schemas"]["PegasusImportSummary"];

export type ImportOverviewActivity = {
  id: string;
  kind: "BROWSER_IMPORT" | "PEGASUS_IMPORT";
  title: string;
  sourceLabel: string;
  stateLabel: string;
  tone: "good" | "warn" | "bad" | "info";
  phase: string;
  outcome: string;
  totalItemCount: number;
  createdAtMs: number;
  updatedAtMs: number;
  actionHref: string;
  actionLabel: string;
};

const pegasusStateLabels: Record<string, string> = {
  SCANNING: "正在扫描",
  AWAITING_MAPPING: "等待映射",
  QUEUED: "等待开始",
  RUNNING: "正在准备",
  PARTIAL_FAILURE: "需要处理",
  COMPLETED: "准备完成",
  CANCEL_REQUESTED: "正在取消",
  CANCELLED: "已取消",
  FAILED: "任务失败",
  EXPIRED: "计划已过期",
};

const pegasusPhaseLabels: Record<string, string> = {
  DISCOVERING_METADATA: "发现元数据",
  PARSING_METADATA: "解析元数据",
  RESOLVING_SOURCES: "解析游戏来源",
  COPYING_CONTENT: "复制游戏内容",
  VALIDATING: "运行检查",
  PREPARING_REVIEWS: "准备审核事项",
  PUBLISHING: "更新审核结果",
};

function pegasusTone(state: string): ImportOverviewActivity["tone"] {
  if (state === "COMPLETED") {return "good";}
  if (state === "PARTIAL_FAILURE" || state === "FAILED") {return "bad";}
  if (state === "CANCEL_REQUESTED" || state === "CANCELLED" || state === "EXPIRED") {return "warn";}
  return "info";
}

function ordinaryTone(state: string): ImportOverviewActivity["tone"] {
  if (state === "COMPLETED") {return "good";}
  if (state === "PARTIAL_FAILURE" || state === "FAILED") {return "bad";}
  if (state === "CANCEL_REQUESTED" || state === "CANCELLED") {return "warn";}
  return "info";
}

function ordinaryActivity(item: ImportListItem): ImportOverviewActivity {
  const issues = importTaskIssueCount(item);
  const hasReviews = item.reviewPendingItemCount > 0;
  return {
    id: item.id,
    kind: "BROWSER_IMPORT",
    title: item.platformInstanceName,
    sourceLabel: importProviderLabels[item.metadataProvider] ?? item.metadataProvider,
    stateLabel: importStateLabels[item.state] ?? item.state,
    tone: ordinaryTone(item.state),
    phase: importTaskPhase(item),
    outcome: hasReviews ? `${item.reviewPendingItemCount} 个待审核` : issues ? `${issues} 个异常` : item.state === "COMPLETED" ? "批次已完成" : "后台处理中",
    totalItemCount: item.totalItemCount,
    createdAtMs: item.createdAtMs,
    updatedAtMs: item.updatedAtMs,
    actionHref: hasReviews ? `/admin/reviews?importJobId=${item.id}` : "/admin/imports/tasks",
    actionLabel: hasReviews ? "审核" : "查看",
  };
}

function pegasusActivity(item: PegasusImportSummary): ImportOverviewActivity {
  const issues = item.counts.blocked + item.counts.failed;
  const hasReviews = item.counts.reviewPending > 0;
  const terminalOutcome = `${item.counts.published} 个已发布 · ${item.counts.reviewDiscarded} 个已丢弃`;
  return {
    id: item.id,
    kind: "PEGASUS_IMPORT",
    title: `${item.root.label}${item.sourceRelativePath ? ` / ${item.sourceRelativePath}` : " / 根目录"}`,
    sourceLabel: "Pegasus 目录",
    stateLabel: pegasusStateLabels[item.state] ?? item.state,
    tone: pegasusTone(item.state),
    phase: item.phase ? pegasusPhaseLabels[item.phase] ?? item.phase : pegasusStateLabels[item.state] ?? item.state,
    outcome: hasReviews ? `${item.counts.reviewPending} 个待审核` : issues ? `${issues} 个异常` : item.state === "COMPLETED" ? terminalOutcome : "后台处理中",
    totalItemCount: item.counts.games,
    createdAtMs: item.createdAtMs,
    updatedAtMs: item.updatedAtMs,
    actionHref: hasReviews ? `/admin/reviews?pegasusImportId=${item.id}` : `/admin/imports/server/pegasus/${item.id}`,
    actionLabel: hasReviews ? "审核" : "查看",
  };
}

export function recentImportActivities(
  imports: ImportListItem[],
  pegasusImports: PegasusImportSummary[],
  limit = 3,
): ImportOverviewActivity[] {
  return [
    ...imports.map(ordinaryActivity),
    ...pegasusImports.map(pegasusActivity),
  ].sort((left, right) => {
    if (left.createdAtMs !== right.createdAtMs) {return right.createdAtMs - left.createdAtMs;}
    if (left.id === right.id) {return 0;}
    return left.id < right.id ? 1 : -1;
  }).slice(0, limit);
}
