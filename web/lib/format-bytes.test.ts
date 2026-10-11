import { expect, it } from "vitest";
import { formatBytes } from "./format-bytes";

it.each([
  [0, "0B"], [999, "999B"], [1000, "1KB"], [27322, "27.3KB"],
  [999949, "999.9KB"], [999950, "1MB"], [4706100, "4.7MB"],
  [4819057, "4.8MB"], [1_000_000_000, "1GB"], [1_000_000_000_000, "1TB"],
  [-1, "—"], [NaN, "—"], [Infinity, "—"], [1.5, "—"],
])("formats %s bytes as %s", (bytes, expected) => {
  expect(formatBytes(bytes)).toBe(expected);
});
