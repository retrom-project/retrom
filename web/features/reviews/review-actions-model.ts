import type { TagReference } from "@/components/tag-picker";
import type { ArcadeDependencies } from "./arcade-dependency-tree";

export type ReviewAsset = { candidateAssetId: string; kind: "COVER" | "BACKGROUND" | "SCREENSHOT" | "UNKNOWN"; ordinal: number; status: string; widthPx: number | null; heightPx: number | null; mediaType: string | null; errorCode: string | null };
export type UploadedReviewAsset = { assetId: string; kind: "COVER"; widthPx: number; heightPx: number; mediaType: string; url: string; createdAtMs: number };
export type ReviewCandidate = { candidateId: string; scrapeRunId: string; providerGameId: string; metadata: Record<string, unknown>; evidence: unknown; assets: ReviewAsset[] };
export type ReviewScrapeRun = { scrapeRunId: string; jobId: string; provider: "HASHEOUS" | "NONE"; state: string; jobState: string; createdAtMs: number; completedAtMs: number | null; errorCode: string | null; evidenceCount: number; attemptCount: number; candidateCount: number; outcomes: { hit: number; miss: number; rateLimited: number; timeout: number; invalidResponse: number; networkError: number } };
export type ReviewMultiDiscAttachment = { attachmentId: string; state: string; errorCode: string | null; diagnostics?: unknown; jobId: string; jobState: string; version?: number; jobVersion?: number; canRetry?: boolean };
export type ReviewMultiDisc = {
  contentKind: "MULTI_DISC_M3U_V1"; playlist: { name: string; sizeBytes: number; sha256: string };
  discCount: number; presentDiscCount: number; missingDiscCount: number; totalPresentBytes: number; maxDiscs?: number; maxTotalBytes?: number;
  entries: Array<{ index: number; discIndex: number; label: string; sourceReference: string; canonicalName: string; state: "PRESENT" | "MISSING"; logicalName: string | null; sizeBytes: number | null; sha256: string | null }>;
  missingReferences: string[]; canAttachMissingDiscs: boolean; latestAttachment: ReviewMultiDiscAttachment | null; activeAttachment: ReviewMultiDiscAttachment | null;
};
export type DuplicateGame = { gameId: string; title: string; platformInstanceId: string; platformInstanceName: string };
export type ReviewWorkspace = {
  itemId: string; version: number; platformInstance?: { id: string; name: string }; effectiveSourceSnapshotId?: string; canApprove?: boolean; validationStale?: boolean;
  arcadeDependencies?: ArcadeDependencies | null; multiDisc?: ReviewMultiDisc | null;
  metadata: { title: string; description: string; developer: string; publisher: string; genre: string; players: number | null; releaseYear: number | null };
  validation: { id: string; status: string; current: boolean; compatibilityCode: string } | null;
  candidates: ReviewCandidate[]; uploadedAssets?: UploadedReviewAsset[];
  sourceMedia?: { sourceKind: "PEGASUS"; sourceRefId: string; pegasusImportId: string; sourceLabel: string | null; coverUrl: string | null; coverWidthPx: number | null; coverHeightPx: number | null; videoUrl: string | null } | null;
  runtimeScreenshot?: { screenshotId: string; validationId: string; coreArtifactId: string; widthPx: number; heightPx: number; capturedAfterMs: 5000; capturedAtMs: number; url: string } | null;
  scrapeRuns?: ReviewScrapeRun[]; selectedCandidateId: string | null;
  selectedAssets: { coverCandidateAssetId: string | null; coverUploadedAssetId?: string | null; backgroundCandidateAssetId: string | null; screenshotCandidateAssetIds: string[] };
  defaultDosEntry: string | null; dosEntries: Array<{ path: string; originalPath: string; kind: string; enabled: boolean; directLaunchSafe: boolean }>;
  duplicateGames?: DuplicateGame[]; contentIdentityDigest?: string; tags?: TagReference[];
};

export type MetadataForm = { title: string; description: string; developer: string; publisher: string; genre: string; players: string; releaseYear: string };
export type CoverSelection = { candidateId: string | null; uploadedId: string | null };
export type PreviewAsset = { id: string; url: string; width: number; height: number };
export type DraftPayload = { metadata: { title: string; description: string; developer: string; publisher: string; genre: string; players: number | null; releaseYear: number | null }; selectedCandidateId: string | null; selectedAssets: { coverCandidateAssetId: string | null; coverUploadedAssetId: string | null; backgroundCandidateAssetId: string | null; screenshotCandidateAssetIds: string[] }; defaultDosEntry: string | null; tagIds: string[] };
export type Comparison = { candidate: ReviewCandidate; current: MetadataForm; next: MetadataForm; currentCover: CoverSelection; nextCover: CoverSelection };

export const compareFields: Array<{ key: keyof MetadataForm; label: string; multiline?: boolean; type?: "number" }> = [
  { key: "title", label: "标题" }, { key: "description", label: "简介", multiline: true }, { key: "developer", label: "开发商" }, { key: "publisher", label: "发行商" }, { key: "genre", label: "类型" }, { key: "players", label: "玩家数", type: "number" }, { key: "releaseYear", label: "发行年份", type: "number" },
];

export function metadataForm(review: ReviewWorkspace): MetadataForm {
  return { ...review.metadata, players: review.metadata.players === null ? "" : String(review.metadata.players), releaseYear: review.metadata.releaseYear === null ? "" : String(review.metadata.releaseYear) };
}

export function candidateForm(candidate: ReviewCandidate, fallback: MetadataForm): MetadataForm {
  const stringField = (key: keyof MetadataForm) => typeof candidate.metadata[key] === "string" ? String(candidate.metadata[key]) : fallback[key];
  const numberField = (key: "players" | "releaseYear") => typeof candidate.metadata[key] === "number" && Number.isInteger(candidate.metadata[key]) ? String(candidate.metadata[key]) : fallback[key];
  return { title: stringField("title"), description: stringField("description"), developer: stringField("developer"), publisher: stringField("publisher"), genre: stringField("genre"), players: numberField("players"), releaseYear: numberField("releaseYear") };
}

export function readyCover(candidate: ReviewCandidate | null) {return candidate?.assets.find((asset) => asset.kind === "COVER" && asset.status === "READY") ?? null;}

export function toPayload(form: MetadataForm, candidateId: string | null, cover: CoverSelection, backgroundId: string | null, screenshotIds: string[], defaultDosEntry: string | null, tags: TagReference[]): DraftPayload {
  return { metadata: { ...form, players: form.players === "" ? null : Number(form.players), releaseYear: form.releaseYear === "" ? null : Number(form.releaseYear) }, selectedCandidateId: candidateId, selectedAssets: { coverCandidateAssetId: cover.candidateId, coverUploadedAssetId: cover.uploadedId, backgroundCandidateAssetId: backgroundId, screenshotCandidateAssetIds: screenshotIds }, defaultDosEntry, tagIds: tags.map((tag) => tag.tagId) };
}

export function workspaceDraftPayload(review: ReviewWorkspace) {
  return toPayload(metadataForm(review), review.selectedCandidateId, { candidateId: review.selectedAssets.coverCandidateAssetId, uploadedId: review.selectedAssets.coverUploadedAssetId ?? null }, review.selectedAssets.backgroundCandidateAssetId, review.selectedAssets.screenshotCandidateAssetIds, review.defaultDosEntry, review.tags ?? []);
}

export function sameDraftPayload(left: DraftPayload, right: DraftPayload) {
  const canonical = (value: DraftPayload) => JSON.stringify({ ...value, tagIds: [...value.tagIds].sort() });
  return canonical(left) === canonical(right);
}

export function reviewReadyForPublish(review: ReviewWorkspace) {
  const parentActive = review.arcadeDependencies?.activeAttachment?.state;
  const multiDiscActive = review.multiDisc?.activeAttachment?.state;
  const attachmentActive = [parentActive, multiDiscActive].some((state) => state === "QUEUED" || state === "RUNNING");
  return (review.canApprove ?? (review.validation?.current === true && review.validation.status === "READY")) && !attachmentActive;
}

export function previewAsset(candidates: ReviewCandidate[], uploaded: UploadedReviewAsset[], cover: CoverSelection): PreviewAsset | null {
  if (cover.uploadedId) {
    const asset = uploaded.find((entry) => entry.assetId === cover.uploadedId);
    return asset ? { id: asset.assetId, url: asset.url, width: asset.widthPx, height: asset.heightPx } : null;
  }
  if (!cover.candidateId) {return null;}
  const asset = candidates.flatMap((candidate) => candidate.assets).find((entry) => entry.candidateAssetId === cover.candidateId);
  return asset?.status === "READY" && asset.widthPx && asset.heightPx ? { id: asset.candidateAssetId, url: `/api/v1/admin/review-assets/${asset.candidateAssetId}`, width: asset.widthPx, height: asset.heightPx } : null;
}

export function scrapeResult(run: ReviewScrapeRun) {
  if (run.candidateCount > 0) {return `找到 ${run.candidateCount} 组可用信息`;}
  if (run.outcomes.invalidResponse > 0) {return "上游响应无法解析";}
  if (run.outcomes.rateLimited + run.outcomes.timeout + run.outcomes.networkError > 0) {return "上游限流、超时或网络异常";}
  if (run.outcomes.miss > 0) {return "精确文件特征查询未命中";}
  return run.evidenceCount === 0 ? "没有可查询的文件特征" : "没有找到可用信息";
}

export function initialDraftState(review: ReviewWorkspace) {
  const baseMetadata = metadataForm(review);
  const automaticCandidate = review.selectedCandidateId ? null : review.candidates[0] ?? null;
  const automaticCover = readyCover(automaticCandidate);
  const cover = {
    candidateId: review.selectedAssets.coverCandidateAssetId ?? automaticCover?.candidateAssetId ?? null,
    uploadedId: review.selectedAssets.coverUploadedAssetId ?? null,
  };
  return {
    baseMetadata,
    form: automaticCandidate ? candidateForm(automaticCandidate, baseMetadata) : baseMetadata,
    candidateId: review.selectedCandidateId ?? automaticCandidate?.candidateId ?? null,
    cover,
    tags: review.tags ?? [],
    candidates: review.candidates,
    uploadedAssets: review.uploadedAssets ?? [],
    saveState: automaticCandidate ? "pending" as const : "saved" as const,
    notice: automaticCandidate ? "首次查询到的信息已自动填入，系统会实时保存。" : "",
  };
}

export function initialRuntimeState(review: ReviewWorkspace) {
  const validationWasCurrent = review.validation?.current ?? false;
  return {
    validationWasCurrent,
    validation: review.validation,
    effectiveSourceSnapshotId: review.effectiveSourceSnapshotId ?? "",
    arcadeDependencies: review.arcadeDependencies ?? null,
    multiDisc: review.multiDisc ?? null,
    canApprove: review.canApprove ?? (validationWasCurrent && review.validation?.status === "READY"),
    runtimeScreenshot: review.runtimeScreenshot ?? null,
  };
}

export function reviewCoverPresentation(review: ReviewWorkspace, candidates: ReviewCandidate[], uploaded: UploadedReviewAsset[], cover: CoverSelection, comparison: Comparison | null) {
  const source = review.sourceMedia?.coverUrl ? {
    id: review.sourceMedia.sourceRefId,
    url: review.sourceMedia.coverUrl,
    width: review.sourceMedia.coverWidthPx ?? 600,
    height: review.sourceMedia.coverHeightPx ?? 800,
  } : null;
  return {
    source,
    selected: previewAsset(candidates, uploaded, cover) ?? source,
    currentComparison: comparison ? previewAsset(candidates, uploaded, comparison.currentCover) ?? source : null,
    nextComparison: comparison ? previewAsset(candidates, uploaded, comparison.nextCover) ?? source : null,
  };
}

export function saveStateLabel(state: "saved" | "pending" | "saving" | "error") {
  if (state === "saving") {return "正在实时保存…";}
  if (state === "pending") {return "等待保存…";}
  if (state === "error") {return "实时保存失败";}
  return "已实时保存";
}

export function reviewReadiness(validationStatus: string | null, validationCurrent: boolean, runtimeScreenshot: ReviewWorkspace["runtimeScreenshot"], serverCanApprove: boolean, parentState: string | undefined, multiDiscState: string | undefined) {
  const active = (state: string | undefined) => state === "QUEUED" || state === "RUNNING";
  const parentAttachmentActive = active(parentState);
  const multiDiscAttachmentActive = active(multiDiscState);
  const validationReady = validationStatus === "READY" && validationCurrent;
  return {
    parentAttachmentActive,
    multiDiscAttachmentActive,
    validationReady,
    screenshotOverride: Boolean(runtimeScreenshot) && !validationReady,
    publishReady: serverCanApprove && !parentAttachmentActive && !multiDiscAttachmentActive,
  };
}

export function activeAttachmentJobId(value: ArcadeDependencies | null) {
  return value?.activeAttachment?.jobId ?? "";
}
