import { describe, expect, it } from "vitest";
import { recentImportActivities, type PegasusImportSummary } from "./import-overview";
import type { ImportListItem } from "./import-workflow";

function ordinary(overrides: Partial<ImportListItem> = {}): ImportListItem {
  return {
    id: "01980000-0000-7000-8000-000000000001",
    state: "COMPLETED",
    platformInstanceName: "GBA 游戏",
    metadataProvider: "NONE",
    totalItemCount: 2,
    reviewPendingItemCount: 0,
    failedItemCount: 0,
    rejectedFileCount: 0,
    version: 1,
    createdAtMs: 10,
    updatedAtMs: 10,
    ...overrides,
  };
}

function pegasus(overrides: Partial<PegasusImportSummary> = {}): PegasusImportSummary {
  return {
    id: "01980000-0000-7000-8000-000000000002",
    root: { id: "games", label: "Pegasus ROM" },
    sourceRelativePath: "FBNeo",
    state: "COMPLETED",
    phase: null,
    scanJobId: "01980000-0000-7000-8000-000000000003",
    importJobId: "01980000-0000-7000-8000-000000000004",
    counts: {
      metadata: 1,
      invalidMetadata: 0,
      collections: 1,
      games: 109,
      estimatedSourceBytes: 1024,
      mappedCollections: 1,
      skippedCollections: 0,
      processable: 109,
      blocked: 0,
      reviewPending: 0,
      published: 107,
      reviewDiscarded: 2,
      existing: 0,
      failed: 0,
      cancelled: 0,
      mediaWarnings: 0,
      covers: 0,
      videos: 0,
    },
    mappingVersion: 2,
    version: 5,
    createdBy: { id: "01980000-0000-7000-8000-000000000005", displayName: "管理员" },
    lastErrorCode: null,
    retryable: false,
    createdAtMs: 20,
    updatedAtMs: 30,
    expiresAtMs: 100,
    completedAtMs: 30,
    ...overrides,
  };
}

describe("import overview activities", () => {
  it("represents one multi-game Pegasus operation as one recent batch", () => {
    const recent = recentImportActivities([], [pegasus()]);

    expect(recent).toHaveLength(1);
    expect(recent[0]).toMatchObject({
      kind: "PEGASUS_IMPORT",
      title: "Pegasus ROM / FBNeo",
      totalItemCount: 109,
      outcome: "107 个已发布 · 2 个已丢弃",
      actionHref: "/admin/imports/server/pegasus/01980000-0000-7000-8000-000000000002",
    });
  });

  it("merges top-level ordinary and Pegasus batches by creation time before limiting", () => {
    const recent = recentImportActivities(
      [ordinary({ createdAtMs: 40 }), ordinary({ id: "01980000-0000-7000-8000-000000000006", createdAtMs: 5 })],
      [pegasus({ createdAtMs: 30, counts: { ...pegasus().counts, reviewPending: 8, published: 0, reviewDiscarded: 0 } })],
      2,
    );

    expect(recent.map((item) => item.kind)).toEqual(["BROWSER_IMPORT", "PEGASUS_IMPORT"]);
    expect(recent[1]).toMatchObject({
      outcome: "8 个待审核",
      actionHref: "/admin/reviews?pegasusImportId=01980000-0000-7000-8000-000000000002",
      actionLabel: "审核",
    });
  });
});
