export function formatTime(
  value: number | null | undefined,
  timeZone: string,
): string {
  if (value === null || value === undefined) {
    return "—";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    timeZone,
    year: "numeric",
    month: "long",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(value);
}
