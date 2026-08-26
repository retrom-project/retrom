import type { ReviewWorkspace } from "./review-actions-model";

export const reviewSourcePreviewLimit = 200;

export function boundedReviewSourcePreview<T>(items: readonly T[]) {
  const visible = items.slice(0, reviewSourcePreviewLimit);
  return { visible, total: items.length, hidden: items.length - visible.length };
}

export function reviewWorkspaceWithoutSourceEvidence<T extends ReviewWorkspace & {
  importJobId: string;
  sourceFiles: unknown;
  sourceManifest: unknown;
}>(review: T): ReviewWorkspace {
  const { importJobId, sourceFiles, sourceManifest, ...workspace } = review;
  void importJobId;
  void sourceFiles;
  void sourceManifest;
  return workspace;
}
