import type { components } from "@/lib/api/generated/schema";

export type PegasusImportSummary = components["schemas"]["PegasusImportSummary"];
export type PegasusImportList = components["schemas"]["PegasusImportList"];
export type PegasusCollection = components["schemas"]["PegasusSourceCollection"];
export type PegasusItemList = components["schemas"]["PegasusItemList"];
export type PegasusItem = components["schemas"]["PegasusItem"];
export type PegasusDirectory = components["schemas"]["ServerImportDirectory"];
export type PegasusPlatformInstance = {
  id: string;
  name: string;
  platformName: string;
  defaultCoreId: string;
  defaultCoreName: string;
  enabled: boolean;
};

export const pegasusStateLabels: Record<PegasusImportSummary["state"], string> = {
  SCANNING: "正在扫描", AWAITING_MAPPING: "等待映射", QUEUED: "等待导入", RUNNING: "正在导入",
  PARTIAL_FAILURE: "部分需要处理", COMPLETED: "审核事项已生成", CANCEL_REQUESTED: "正在取消", CANCELLED: "已取消",
  FAILED: "任务失败", EXPIRED: "计划已过期",
};

export const pegasusPhaseLabels: Record<NonNullable<PegasusImportSummary["phase"]>, string> = {
  DISCOVERING_METADATA: "发现 metadata", PARSING_METADATA: "解析 metadata", RESOLVING_SOURCES: "核对源文件",
  COPYING_CONTENT: "复制内容", VALIDATING: "运行检查", PREPARING_REVIEWS: "生成审核事项",
};

export const pegasusOutcomeLabels: Record<PegasusItem["executionState"], string> = {
  PENDING: "等待处理", COPYING: "复制内容", VALIDATING: "运行检查", PUBLISHED: "已发布",
  REVIEW_PENDING: "待管理员审核", REVIEW_DISCARDED: "审核已丢弃",
  SKIPPED_EXISTING: "内容已存在", SKIPPED_MAPPING: "集合已跳过", BLOCKED_SOURCE: "源文件阻断", BLOCKED_CONTENT: "内容阻断",
  SOURCE_CHANGED: "源文件已变化", READ_FAILED: "读取失败", COMMIT_FAILED: "提交失败", CANCELLED: "已取消",
};

export function pegasusStateTone(state: PegasusImportSummary["state"]): "good" | "warn" | "bad" | "info" {
  if (state === "COMPLETED") {return "good";}
  if (state === "FAILED" || state === "PARTIAL_FAILURE") {return "bad";}
  if (state === "CANCELLED" || state === "CANCEL_REQUESTED" || state === "EXPIRED") {return "warn";}
  return "info";
}
