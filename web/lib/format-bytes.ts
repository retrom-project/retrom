const units = ["B", "KB", "MB", "GB", "TB"];

/** Decimal units: 1 KB = 1,000 bytes. Keep the exact byte count in tooltips. */
export function formatBytes(bytes: number): string {
  if (!Number.isSafeInteger(bytes) || bytes < 0) { return "—"; }
  let amount = bytes;
  let unit = 0;
  while (amount >= 1000 && unit < units.length - 1) {
    amount /= 1000;
    unit += 1;
  }
  amount = Number(amount.toFixed(1));
  if (amount >= 1000 && unit < units.length - 1) {
    amount /= 1000;
    unit += 1;
  }
  return `${amount}${units[unit]}`;
}
