import { describe, expect, it } from "vitest";
import { filterBIOS, isBIOSBlocking, summarizeBIOS, type BIOSRequirement } from "./runtime-dependencies";

const bios = (id: string, overrides: Partial<BIOSRequirement> = {}): BIOSRequirement => ({
  id, coreId: "mgba", coreName: "mGBA", providerId: "emulatorjs", targetId: "mgba",
  logicalName: `${id}.bin`,
  sourceKind: "STATIC", requirementMode: "REQUIRED", enabled: true, version: 1, status: "MATCHED", ...overrides,
});

describe("runtime dependency presentation", () => {
  it("only counts required missing BIOS files as blockers", () => {
    const items = [bios("ready"), bios("missing", { status: "MISSING" }), bios("optional", { requirementMode: "OPTIONAL", status: "OPTIONAL_MISSING" }), bios("warning", { status: "HASH_WARNING" })];
    expect(isBIOSBlocking(items[2])).toBe(false);
    expect(summarizeBIOS(items)).toEqual({ total: 4, blocking: 1, warnings: 1, ready: 1 });
    expect(filterBIOS(items, { query: "mGBA", coreId: "", status: "", quick: "ATTENTION" }).map((item) => item.id)).toEqual(["missing", "warning"]);
  });

  it("filters BIOS by filename, core and requirement mode without changing the source list", () => {
    const items = [bios("gba"), bios("gb", { coreId: "gambatte", coreName: "Gambatte", requirementMode: "OPTIONAL" })];
    expect(filterBIOS(items, { query: "gb.bin", coreId: "gambatte", status: "", quick: "OPTIONAL" })).toEqual([items[1]]);
    expect(items).toHaveLength(2);
  });
});
