import { describe, expect, it } from "vitest";
import { validateMultiDiscAttachmentSelection } from "./multi-disc-attachment-drawer";

describe("validateMultiDiscAttachmentSelection", () => {
  it("requires the exact missing set while allowing unique ASCII case-fold matching", () => {
    const result = validateMultiDiscAttachmentSelection(
      [new File(["two"], "TWO.CHD"), new File(["three"], "three.chd")],
      ["two.chd", "three.chd"],
      4,
      1024,
    );
    expect(result).toMatchObject({ missing: [], unexpected: [], duplicates: [], selectedBytes: 8, complete: true });
  });

  it("reports missing, unexpected, duplicate, and total-size failures separately", () => {
    const result = validateMultiDiscAttachmentSelection(
      [new File(["large"], "TWO.CHD"), new File(["duplicate"], "two.chd"), new File(["x"], "extra.chd")],
      ["two.chd", "three.chd"],
      100,
      105,
    );
    expect(result.missing).toContain("three.chd");
    expect(result.unexpected).toEqual(expect.arrayContaining(["TWO.CHD", "extra.chd"]));
    expect(result.duplicates).toEqual(["TWO.CHD", "two.chd"]);
    expect(result.complete).toBe(false);
  });
});
