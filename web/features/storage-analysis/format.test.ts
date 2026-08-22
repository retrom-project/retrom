import { describe, expect, it } from "vitest";
import { formatStorageBytes, parseStorageBytes, storageBarWidth, storagePercentage } from "./format";

describe("storage capacity formatting", () => {
  it("uses IEC units without converting decimal byte strings through Number", () => {
    expect(formatStorageBytes("0")).toBe("0 B");
    expect(formatStorageBytes("1023")).toBe("1023 B");
    expect(formatStorageBytes("1024")).toBe("1 KiB");
    expect(formatStorageBytes("1536")).toBe("1.5 KiB");
    expect(formatStorageBytes("9223372036854775807")).toBe("8192 PiB");
  });

  it("rejects signed, padded, and non-decimal byte values", () => {
    for (const value of ["", "01", "-1", "1.5", "Infinity"]) {
      expect(() => parseStorageBytes(value)).toThrow("INVALID_STORAGE_BYTES");
    }
  });

  it("calculates readable percentages and exact CSS widths with BigInt", () => {
    expect(storagePercentage("1", "3")).toBe("33.3%");
    expect(storagePercentage("0", "0")).toBe("0%");
    expect(storageBarWidth("1", "3")).toBe("33.33%");
    expect(storageBarWidth("0", "3")).toBe("0%");
  });
});
