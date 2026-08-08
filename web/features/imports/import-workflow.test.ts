import { describe, expect, it } from "vitest";
import { filterImportTasks, importTaskProgress, importTaskSummary, type ImportListItem } from "./import-workflow";

function task(overrides: Partial<ImportListItem> & Pick<ImportListItem, "id" | "state">): ImportListItem {
  return {
    platformInstanceName: "FBNeo 游戏",
    metadataProvider: "HASHEOUS",
    totalItemCount: 10,
    reviewPendingItemCount: 0,
    failedItemCount: 0,
    version: 1,
    createdAtMs: 1,
    updatedAtMs: 1,
    ...overrides,
  };
}

const tasks = [
  task({ id: "running", state: "RUNNING" }),
  task({ id: "review", state: "REVIEW_PENDING", platformInstanceName: "GBA 游戏", totalItemCount: 8, reviewPendingItemCount: 8 }),
  task({ id: "partial", state: "PARTIAL_FAILURE", failedItemCount: 3 }),
  task({ id: "done", state: "COMPLETED", metadataProvider: "NONE" }),
];

describe("import workflow presentation", () => {
  it("summarizes actionable task groups without mixing completed work", () => {
    expect(importTaskSummary(tasks)).toEqual({ total: 4, running: 1, attention: 1, review: 1, completed: 1 });
  });

  it("filters local task cards by directory, state group, and visible text", () => {
    expect(filterImportTasks(tasks, { query: "gba", directory: "", state: "" }).map((item) => item.id)).toEqual(["review"]);
    expect(filterImportTasks(tasks, { query: "", directory: "", state: "ATTENTION" }).map((item) => item.id)).toEqual(["partial"]);
    expect(filterImportTasks(tasks, { query: "不刮削", directory: "", state: "" }).map((item) => item.id)).toEqual(["done"]);
  });

  it("keeps progress bounded and final states complete", () => {
    expect(importTaskProgress(tasks[0])).toBeGreaterThan(0);
    expect(importTaskProgress(tasks[0])).toBeLessThan(100);
    expect(importTaskProgress(tasks[1])).toBe(100);
    expect(importTaskProgress(tasks[3])).toBe(100);
  });
});
