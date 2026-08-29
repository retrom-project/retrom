import { describe, expect, it } from "vitest";
import { boundedReviewSourcePreview, reviewWorkspaceWithoutSourceEvidence } from "./review-source-preview";

describe("review source preview", () => {
  it("bounds rendered rows while preserving the exact total", () => {
    const result = boundedReviewSourcePreview(Array.from({ length: 4033 }, (_, index) => index));
    expect(result.visible).toHaveLength(200);
    expect(result.visible.at(-1)).toBe(199);
    expect(result.total).toBe(4033);
    expect(result.hidden).toBe(3833);
  });

  it("removes large source evidence from the client workspace", () => {
    const workspace = reviewWorkspaceWithoutSourceEvidence({
      itemId: "item", importJobId: "job", version: 1, effectiveSourceSnapshotId: "snapshot",
      metadata: { title: "Game", description: "", developer: "", publisher: "", genre: "", players: 1, releaseYear: 2000 },
      validation: null, candidates: [], selectedCandidateId: null,
      selectedAssets: { coverCandidateAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: [] },
      defaultDosEntry: null, dosEntries: [],
      sourceFiles: [{ archiveEntries: Array.from({ length: 4033 }, (_, index) => index) }],
      sourceManifest: { files: Array.from({ length: 3253 }, (_, index) => index) },
    });
    expect(workspace).not.toHaveProperty("sourceFiles");
    expect(workspace).not.toHaveProperty("sourceManifest");
    expect(workspace).not.toHaveProperty("importJobId");
    expect(workspace.itemId).toBe("item");
  });
});
