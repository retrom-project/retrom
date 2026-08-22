const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"] as const;

export function parseStorageBytes(value: string): bigint {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) {throw new Error("INVALID_STORAGE_BYTES");}
  return BigInt(value);
}

export function formatStorageBytes(value: string): string {
  const bytes = parseStorageBytes(value);
  let divisor = 1n;
  let unit = 0;
  while (unit < units.length - 1 && bytes >= divisor * 1024n) {
    divisor *= 1024n;
    unit += 1;
  }
  if (unit === 0) {return `${bytes} B`;}
  const tenths = (bytes * 10n + divisor / 2n) / divisor;
  return tenths % 10n === 0n
    ? `${tenths / 10n} ${units[unit]}`
    : `${tenths / 10n}.${tenths % 10n} ${units[unit]}`;
}

export function storagePercentage(value: string, total: string): string {
  const bytes = parseStorageBytes(value);
  const totalBytes = parseStorageBytes(total);
  if (totalBytes === 0n) {return "0%";}
  const tenths = (bytes * 1000n + totalBytes / 2n) / totalBytes;
  return tenths % 10n === 0n ? `${tenths / 10n}%` : `${tenths / 10n}.${tenths % 10n}%`;
}

export function storageBarWidth(value: string, total: string): string {
  const bytes = parseStorageBytes(value);
  const totalBytes = parseStorageBytes(total);
  if (bytes === 0n || totalBytes === 0n) {return "0%";}
  const basisPoints = (bytes * 10_000n + totalBytes / 2n) / totalBytes;
  return `${basisPoints / 100n}.${(basisPoints % 100n).toString().padStart(2, "0")}%`;
}
