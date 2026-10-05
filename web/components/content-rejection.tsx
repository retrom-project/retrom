import {contentRequirementLabels} from "@/lib/content-requirements";
import {formatBytes} from "@/lib/backend";
import type {components} from "@/lib/api/generated/schema";

export type ContentRejection = components["schemas"]["ContentRejection"];

export const contentLimitLabels: Record<string, string> = {
  ...contentRequirementLabels,
  CONTENT_FILE_BYTES_EXCEEDED: "文件大小超过核心上限",
  MULTI_DISC_COUNT_EXCEEDED: "光盘数量超过上限",
  MULTI_DISC_TOTAL_BYTES_EXCEEDED: "光盘总大小超过上限",
  MULTI_DISC_PLAYLIST_BYTES_EXCEEDED: "播放列表大小超过上限",
  MULTI_DISC_REFERENCE_BYTES_EXCEEDED: "光盘引用文件名过长",
};

export function ContentRejectionEvidence({rejection}: {rejection: ContentRejection | null}) {
  const limit = rejection?.limit;
  if (!rejection) {return null;}
  if (!limit) {return <p className="content-rejection-limit">文件：{rejection.relativePath}</p>;}
  const count = limit.metric === "DISC_COUNT";
  const format = (value: number) => count ? `${value} 张` : `${formatBytes(value)}（${value.toLocaleString()} 字节）`;
  return <p className="content-rejection-limit">实际：{format(limit.actual)}；上限：{format(limit.maximum)}</p>;
}
