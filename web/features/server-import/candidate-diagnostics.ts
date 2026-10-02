const failureLabels: Record<string, string> = {
  DAT_UNAVAILABLE: "DAT 尚未就绪，无法校验内容",
  DAT_MACHINE_UNDEFINED: "DAT 未定义该依赖，无法校验内容",
  DAT_ENTRIES_UNVERIFIABLE: "DAT 缺少可校验条目",
  BIOS_CATALOG_INVALID: "固件目录缺少校验依据",
  SERVER_IMPORT_SOURCE_UNREADABLE: "无法读取候选文件",
  SERVER_IMPORT_SOURCE_CHANGED: "候选文件在读取期间发生变化",
  BIOS_ARCHIVE_INVALID: "压缩包损坏或格式无效",
  ARCHIVE_UNSAFE: "压缩包未通过安全检查",
  SERVER_IMPORT_VALIDATION_FAILED: "校验服务失败，请重试",
};

export function candidateFailureLabel(code: unknown): string {
  return failureLabels[String(code)] ?? `内容校验未完成（${String(code)}）`;
}
