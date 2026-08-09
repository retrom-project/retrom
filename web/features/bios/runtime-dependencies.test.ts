import { describe, expect, it } from "vitest";
import { filterBIOS, filterDATVersions, isBIOSBlocking, summarizeBIOS, summarizeDAT, type BIOSRequirement, type DATVersion } from "./runtime-dependencies";

const bios = (id: string, overrides: Partial<BIOSRequirement> = {}): BIOSRequirement => ({
  id, coreId: "mgba", coreName: "mGBA", coreArtifactId: "artifact", logicalName: `${id}.bin`,
  requirementMode: "REQUIRED", enabled: true, version: 1, status: "MATCHED", ...overrides,
});

const dat = (id: string, overrides: Partial<DATVersion> = {}): DATVersion => ({
  id, coreId: "fbneo", coreName: "FinalBurn Neo", coreArtifactId: "artifact", source: "BUILTIN",
  compatibilityStatus: "MATCHED", parseStatus: "READY", active: false, machineCount: 1,
  romEntryCount: 2, diskEntryCount: 0, biosSetCount: 1, diffStatus: "READY", version: 1, updatedAtMs: 1, ...overrides,
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

  it("separates active, candidate, processing and failed DAT versions", () => {
    const items = [dat("active", { active: true }), dat("candidate", { source: "USER" }), dat("parsing", { source: "USER", parseStatus: "PARSING" }), dat("failed", { source: "USER", parseStatus: "FAILED" })];
    expect(summarizeDAT(items)).toEqual({ all: 4, active: 1, ready: 1, working: 1, attention: 1, history: 0 });
    expect(filterDATVersions(items, { query: "final", source: "USER", parseStatus: "", quick: "ATTENTION" }).map((item) => item.id)).toEqual(["failed"]);
  });
});
