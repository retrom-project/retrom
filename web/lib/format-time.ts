export type TimeFormat = "full" | "compact";

export function formatTime(
  value: number | null | undefined,
  timeZone: string,
  format: TimeFormat = "full",
): string {
  if (value === null || value === undefined) {
    return "—";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    timeZone,
    year: format === "full" ? "numeric" : undefined,
    month: format === "full" ? "long" : "2-digit",
    day: format === "full" ? "numeric" : "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(value);
}
