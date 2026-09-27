import type { components } from "@/lib/api/generated/schema";

export type StorageSnapshot = components["schemas"]["StorageAnalysis"];
export type StorageCategory = components["schemas"]["StorageAnalysisCategory"];
export type StorageCategoryCode = StorageCategory["code"];

export const categoryPresentation: Record<StorageCategoryCode, { label: string; description: string }> = {
  GAME_CONTENT: { label: "ROM 与游戏内容", description: "已发布游戏使用的 ROM、光盘、父集和运行内容包。" },
  BIOS: { label: "BIOS 与运行 bundle", description: "已安装 BIOS 及发布版本绑定的 BIOS bundle。" },
  SAVES: { label: "存档", description: "存档状态文件与对应截图。" },
  MEDIA: { label: "游戏媒体", description: "封面、背景、截图和视频。" },
  WORKFLOW: { label: "导入与审核工作区", description: "上传、导入、抓取和审核流程持有的文件。" },
  PENDING_DELETE: { label: "待删除文件", description: "所有者已移除，等待后台删除的文件。" },
};

export const excludedPresentation: Record<StorageSnapshot["excluded"][number], string> = {
  DATABASE_FILES: "SQLite 数据库、WAL 与 SHM",
  UPLOAD_PARTS: "未合并的上传分片",
  JOB_SCRATCH: "后台任务临时文件",
  DEPENDENCY_ROOT: "EmulatorJS、核心与 DAT 依赖",
  FILESYSTEM_OVERHEAD: "文件系统元数据和分配开销",
  UNREGISTERED_ORPHANS: "写入中或尚未登记的文件",
  VOLUME_FREE_SPACE: "磁盘总量与剩余空间",
};
