export type StatusTone = "good" | "warn" | "bad" | "info" | "neutral";

const GOOD = new Set(["READY", "AVAILABLE", "MATCHED", "COMPLETED", "PUBLISHED", "APPROVED", "ACTIVE", "ENABLED"]);
const WARN = new Set(["HASH_WARNING", "UNKNOWN", "WARNING"]);
const BAD = new Set(["INCOMPATIBLE", "DEPENDENCY_MISSING", "MISSING", "BLOCKED", "FAILED", "VALIDATION_FAILED", "DELETED", "DISCARDED", "ERROR", "LAUNCH_BIOS_MISSING", "LAUNCH_PARENT_MISSING", "UNSUPPORTED_MERGED_ROMSET", "UNSUPPORTED_CHD"]);
const INFO = new Set(["NEEDS_VALIDATION", "PENDING", "PARSING", "RUNNING", "QUEUED", "PROCESSING", "REVIEW_PENDING"]);

export function statusTone(status: string | null | undefined): StatusTone {
  const normalized = status?.trim().toUpperCase() ?? "";
  if (GOOD.has(normalized)) return "good";
  if (WARN.has(normalized)) return "warn";
  if (BAD.has(normalized)) return "bad";
  if (INFO.has(normalized)) return "info";
  return "neutral";
}
