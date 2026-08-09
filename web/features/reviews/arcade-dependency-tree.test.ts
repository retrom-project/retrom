import { describe, expect, it } from "vitest";
import { buildArcadeDependencyRows, type ArcadeDependencies, type ArcadeDependencyNode } from "./arcade-dependency-tree";

function node(machine: string, requiredBy: string, depth: number, kind: ArcadeDependencyNode["kind"] = "PARENT"): ArcadeDependencyNode {
  return { kind, machine, requiredBy, depth, expectedLogicalName: `${machine}.zip`, state: "MISSING", requiredEntryCount: 1, canAttach: kind === "PARENT", attachment: null };
}

describe("buildArcadeDependencyRows", () => {
  it("uses requiredBy edges for stable visual and tab order", () => {
    const value: ArcadeDependencies = {
      machine: "a", status: "BLOCKED", compatibilityCode: "LAUNCH_PARENT_MISSING", activeAttachment: null,
      nodes: [node("bios-x", "c", 3, "BIOS_OR_BASE"), node("c", "b", 2), node("b", "a", 1), node("bios-a", "a", 1, "BIOS_OR_BASE")],
    };

    expect(buildArcadeDependencyRows(value).map(({ node: item, level }) => `${level}:${item.machine}`)).toEqual([
      "1:b", "2:c", "3:bios-x", "1:bios-a",
    ]);
  });

  it("keeps orphaned and cyclic server data visible without looping", () => {
    const value: ArcadeDependencies = {
      machine: "a", status: "INCOMPATIBLE", compatibilityCode: "ARCADE_DEPENDENCY_CYCLE", activeAttachment: null,
      nodes: [node("b", "c", 1), node("c", "b", 2), node("orphan", "missing", 4)],
    };

    expect(buildArcadeDependencyRows(value).map(({ node: item }) => item.machine)).toEqual(["b", "c", "orphan"]);
  });
});
