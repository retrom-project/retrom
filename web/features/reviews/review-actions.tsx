"use client";

import Image from "next/image";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { queueFlashToast, Toast, type ToastMessage } from "@/components/flash-toast";
import { FeedbackBanner } from "@/components/ui";
import { newUuid } from "@/lib/crypto";
import { writeHeaders } from "@/lib/api/client";
import { formatBytes } from "@/lib/backend";
import { useAuth } from "@/features/auth/auth-provider";
import { userStorageKey } from "@/features/auth/storage";
import { responseError, uploadFiles, uploadOne, waitForJob, waitForJobEvents } from "@/lib/upload";
import { ArcadeDependencyCard } from "./arcade-dependencies";
import { type ArcadeDependencies, type ArcadeDependencyNode, type ArcadeParentAttachment } from "./arcade-dependency-tree";
import { MultiDiscAttachmentDrawer } from "./multi-disc-attachment-drawer";

export type ReviewAsset = {
  candidateAssetId: string;
  kind: "COVER" | "BACKGROUND" | "SCREENSHOT" | "UNKNOWN";
  ordinal: number;
  status: string;
  widthPx: number | null;
  heightPx: number | null;
  mediaType: string | null;
  errorCode: string | null;
};

export type UploadedReviewAsset = {
  assetId: string;
  kind: "COVER";
  widthPx: number;
  heightPx: number;
  mediaType: string;
  url: string;
  createdAtMs: number;
};

export type ReviewCandidate = {
  candidateId: string;
  scrapeRunId: string;
  providerGameId: string;
  metadata: Record<string, unknown>;
  evidence: unknown;
  assets: ReviewAsset[];
};

export type ReviewScrapeRun = {
  scrapeRunId: string;
  jobId: string;
  provider: "HASHEOUS" | "NONE";
  state: string;
  jobState: string;
  createdAtMs: number;
  completedAtMs: number | null;
  errorCode: string | null;
  evidenceCount: number;
  attemptCount: number;
  candidateCount: number;
  outcomes: { hit: number; miss: number; rateLimited: number; timeout: number; invalidResponse: number; networkError: number };
};

export type ReviewMultiDiscAttachment = {
  attachmentId: string;
  state: string;
  errorCode: string | null;
  diagnostics?: unknown;
  jobId: string;
  jobState: string;
  version?: number;
  jobVersion?: number;
  canRetry?: boolean;
};

export type ReviewMultiDisc = {
  contentKind: "MULTI_DISC_M3U_V1";
  playlist: { name: string; sizeBytes: number; sha256: string };
  discCount: number;
  presentDiscCount: number;
  missingDiscCount: number;
  totalPresentBytes: number;
  maxDiscs?: number;
  maxTotalBytes?: number;
  entries: Array<{
    index: number; discIndex: number; label: string; sourceReference: string;
    canonicalName: string; state: "PRESENT" | "MISSING"; logicalName: string | null;
    sizeBytes: number | null; sha256: string | null;
  }>;
  missingReferences: string[];
  canAttachMissingDiscs: boolean;
  latestAttachment: ReviewMultiDiscAttachment | null;
  activeAttachment: ReviewMultiDiscAttachment | null;
};

export type ReviewWorkspace = {
  itemId: string;
  version: number;
  effectiveSourceSnapshotId?: string;
  canApprove?: boolean;
  validationStale?: boolean;
  arcadeDependencies?: ArcadeDependencies | null;
  multiDisc?: ReviewMultiDisc | null;
  metadata: { title: string; description: string; developer: string; publisher: string; genre: string; players: number | null; releaseYear: number | null };
  validation: { id: string; status: string; current: boolean; compatibilityCode: string } | null;
  candidates: ReviewCandidate[];
  uploadedAssets?: UploadedReviewAsset[];
  sourceMedia?: {
    sourceKind: "PEGASUS";
    sourceRefId: string;
    pegasusImportId: string;
    sourceLabel: string | null;
    coverUrl: string | null;
    coverWidthPx: number | null;
    coverHeightPx: number | null;
    videoUrl: string | null;
  } | null;
  scrapeRuns?: ReviewScrapeRun[];
  selectedCandidateId: string | null;
  selectedAssets: { coverCandidateAssetId: string | null; coverUploadedAssetId?: string | null; backgroundCandidateAssetId: string | null; screenshotCandidateAssetIds: string[] };
  defaultDosEntry: string | null;
  dosEntries: Array<{ path: string; originalPath: string; kind: string; enabled: boolean; directLaunchSafe: boolean }>;
  duplicateGames?: DuplicateGame[];
  contentIdentityDigest?: string;
};

export type DuplicateGame = {
  gameId: string;
  title: string;
  platformInstanceId: string;
  platformInstanceName: string;
};

type MetadataForm = { title: string; description: string; developer: string; publisher: string; genre: string; players: string; releaseYear: string };
type CoverSelection = { candidateId: string | null; uploadedId: string | null };
type PreviewAsset = { id: string; url: string; width: number; height: number };
type DraftPayload = {
  metadata: { title: string; description: string; developer: string; publisher: string; genre: string; players: number | null; releaseYear: number | null };
  selectedCandidateId: string | null;
  selectedAssets: { coverCandidateAssetId: string | null; coverUploadedAssetId: string | null; backgroundCandidateAssetId: string | null; screenshotCandidateAssetIds: string[] };
  defaultDosEntry: string | null;
};
type Comparison = { candidate: ReviewCandidate; current: MetadataForm; next: MetadataForm; currentCover: CoverSelection; nextCover: CoverSelection };

const compareFields: Array<{ key: keyof MetadataForm; label: string; multiline?: boolean; type?: "number" }> = [
  { key: "title", label: "标题" }, { key: "description", label: "简介", multiline: true },
  { key: "developer", label: "开发商" }, { key: "publisher", label: "发行商" },
  { key: "genre", label: "类型" }, { key: "players", label: "玩家数", type: "number" },
  { key: "releaseYear", label: "发行年份", type: "number" },
];

function metadataForm(review: ReviewWorkspace): MetadataForm {
  return { ...review.metadata, players: review.metadata.players === null ? "" : String(review.metadata.players), releaseYear: review.metadata.releaseYear === null ? "" : String(review.metadata.releaseYear) };
}

function candidateForm(candidate: ReviewCandidate, fallback: MetadataForm): MetadataForm {
  const stringField = (key: keyof MetadataForm) => typeof candidate.metadata[key] === "string" ? String(candidate.metadata[key]) : fallback[key];
  const numberField = (key: "players" | "releaseYear") => typeof candidate.metadata[key] === "number" && Number.isInteger(candidate.metadata[key]) ? String(candidate.metadata[key]) : fallback[key];
  return { title: stringField("title"), description: stringField("description"), developer: stringField("developer"), publisher: stringField("publisher"), genre: stringField("genre"), players: numberField("players"), releaseYear: numberField("releaseYear") };
}

function readyCover(candidate: ReviewCandidate | null) {
  return candidate?.assets.find((asset) => asset.kind === "COVER" && asset.status === "READY") ?? null;
}

function toPayload(form: MetadataForm, candidateId: string | null, cover: CoverSelection, backgroundId: string | null, screenshotIds: string[], defaultDosEntry: string | null): DraftPayload {
  return {
    metadata: { ...form, players: form.players === "" ? null : Number(form.players), releaseYear: form.releaseYear === "" ? null : Number(form.releaseYear) },
    selectedCandidateId: candidateId,
    selectedAssets: { coverCandidateAssetId: cover.candidateId, coverUploadedAssetId: cover.uploadedId, backgroundCandidateAssetId: backgroundId, screenshotCandidateAssetIds: screenshotIds },
    defaultDosEntry,
  };
}

function previewAsset(candidates: ReviewCandidate[], uploaded: UploadedReviewAsset[], cover: CoverSelection): PreviewAsset | null {
  if (cover.uploadedId) {
    const asset = uploaded.find((entry) => entry.assetId === cover.uploadedId);
    return asset ? { id: asset.assetId, url: asset.url, width: asset.widthPx, height: asset.heightPx } : null;
  }
  if (!cover.candidateId) return null;
  const asset = candidates.flatMap((candidate) => candidate.assets).find((entry) => entry.candidateAssetId === cover.candidateId);
  return asset?.status === "READY" && asset.widthPx && asset.heightPx ? { id: asset.candidateAssetId, url: `/api/v1/admin/review-assets/${asset.candidateAssetId}`, width: asset.widthPx, height: asset.heightPx } : null;
}

function AssetPreview({ asset, label }: { asset: PreviewAsset | null; label: string }) {
  return asset ? <Image src={asset.url} alt={label} width={asset.width} height={asset.height} unoptimized /> : <div className="asset-placeholder">暂无封面</div>;
}

function scrapeResult(run: ReviewScrapeRun) {
  if (run.candidateCount > 0) return `找到 ${run.candidateCount} 组可用信息`;
  if (run.outcomes.invalidResponse > 0) return "上游响应无法解析";
  if (run.outcomes.rateLimited + run.outcomes.timeout + run.outcomes.networkError > 0) return "上游限流、超时或网络异常";
  if (run.outcomes.miss > 0) return "精确文件特征查询未命中";
  return run.evidenceCount === 0 ? "没有可查询的文件特征" : "没有找到可用信息";
}

export function ReviewActions({ review, returnTo = "/admin/reviews", nextItemId = null, sourceDisplayName = "游戏文件", platformInstanceName = "游戏目录", children }: { review: ReviewWorkspace; returnTo?: string; nextItemId?: string | null; sourceDisplayName?: string; platformInstanceName?: string; children?: ReactNode }) {
  const router = useRouter();
  const { context } = useAuth();
  const validationWasCurrent = review.validation?.current ?? false;
  const initialMetadata = metadataForm(review);
  const automaticCandidate = review.selectedCandidateId ? null : review.candidates[0] ?? null;
  const automaticCover = readyCover(automaticCandidate);
  const initialCover = { candidateId: review.selectedAssets.coverCandidateAssetId ?? automaticCover?.candidateAssetId ?? null, uploadedId: review.selectedAssets.coverUploadedAssetId ?? null };
  const [form, setForm] = useState<MetadataForm>(() => automaticCandidate ? candidateForm(automaticCandidate, initialMetadata) : initialMetadata);
  const [candidateId, setCandidateId] = useState<string | null>(review.selectedCandidateId ?? automaticCandidate?.candidateId ?? null);
  const [cover, setCover] = useState<CoverSelection>(initialCover);
  const [backgroundId, setBackgroundId] = useState<string | null>(review.selectedAssets.backgroundCandidateAssetId);
  const [screenshotIds, setScreenshotIds] = useState(review.selectedAssets.screenshotCandidateAssetIds);
  const [defaultDosEntry, setDefaultDosEntry] = useState<string | null>(review.defaultDosEntry);
  const [candidates, setCandidates] = useState(review.candidates);
  const [uploadedAssets, setUploadedAssets] = useState(review.uploadedAssets ?? []);
  const [comparison, setComparison] = useState<Comparison | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [saveState, setSaveState] = useState<"saved" | "pending" | "saving" | "error">(automaticCandidate ? "pending" : "saved");
  const [notice, setNotice] = useState(automaticCandidate ? "首次查询到的信息已自动填入，系统会实时保存。" : "");
  const [jobProgress, setJobProgress] = useState("");
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const [validationCurrent, setValidationCurrent] = useState(validationWasCurrent);
  const [currentValidation, setCurrentValidation] = useState(review.validation);
  const [effectiveSourceSnapshotId, setEffectiveSourceSnapshotId] = useState(review.effectiveSourceSnapshotId ?? "");
  const [arcadeDependencies, setArcadeDependencies] = useState(review.arcadeDependencies ?? null);
  const [multiDisc, setMultiDisc] = useState(review.multiDisc ?? null);
  const [serverCanApprove, setServerCanApprove] = useState(review.canApprove ?? (validationWasCurrent && review.validation?.status === "READY"));
  const [parentProgress, setParentProgress] = useState("");
  const [multiDiscProgress, setMultiDiscProgress] = useState("");
  const [duplicateConfirmation, setDuplicateConfirmation] = useState<DuplicateGame[] | null>(null);
  const versionRef = useRef(review.version);
  const watchedParentJobRef = useRef("");
  const watchedMultiDiscJobRef = useRef("");
  const validationRefreshRequestedRef = useRef(false);
  const latestKeyRef = useRef("");
  const saveQueueRef = useRef<Promise<boolean>>(Promise.resolve(true));
  const serverPayload = toPayload(initialMetadata, review.selectedCandidateId, { candidateId: review.selectedAssets.coverCandidateAssetId, uploadedId: review.selectedAssets.coverUploadedAssetId ?? null }, review.selectedAssets.backgroundCandidateAssetId, review.selectedAssets.screenshotCandidateAssetIds, review.defaultDosEntry);
  const lastSavedKeyRef = useRef(JSON.stringify(serverPayload));
  const draftPayload = useMemo(() => toPayload(form, candidateId, cover, backgroundId, screenshotIds, defaultDosEntry), [form, candidateId, cover, backgroundId, screenshotIds, defaultDosEntry]);
  const draftKey = useMemo(() => JSON.stringify(draftPayload), [draftPayload]);
  const latestPayloadRef = useRef(draftPayload);
  const validationStatus = currentValidation?.status ?? null;

  const refreshReview = useCallback(async () => {
    const response = await fetch(`/api/v1/admin/reviews/${review.itemId}`, { cache: "no-store" });
    if (!response.ok) throw new Error(await responseError(response, "校验完成，但无法读取最新审核状态"));
    const updated = await response.json() as ReviewWorkspace;
    versionRef.current = updated.version;
    setCurrentValidation(updated.validation);
    setValidationCurrent(updated.validation?.current ?? false);
    setEffectiveSourceSnapshotId(updated.effectiveSourceSnapshotId ?? "");
    setArcadeDependencies(updated.arcadeDependencies ?? null);
    setMultiDisc(updated.multiDisc ?? null);
    setServerCanApprove(updated.canApprove ?? (updated.validation?.current === true && updated.validation.status === "READY"));
    setCandidates(updated.candidates);
    setUploadedAssets(updated.uploadedAssets ?? []);
    router.refresh();
    return updated;
  }, [review.itemId, router]);

  const enqueueSave = useCallback((key: string, payload: DraftPayload, force = false) => {
    saveQueueRef.current = saveQueueRef.current.catch(() => false).then(async () => {
      if (!force && lastSavedKeyRef.current === key) return true;
      if (latestKeyRef.current === key) setSaveState("saving");
      try {
        const response = await fetch(`/api/v1/admin/reviews/${review.itemId}`, { method: "PATCH", credentials: "same-origin", keepalive: true, headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"` }), body: JSON.stringify(payload) });
        if (!response.ok) throw new Error(await responseError(response, "实时保存失败：字段、来源或版本已经变化"));
        const result = await response.json() as { version: number };
        versionRef.current = result.version;
        lastSavedKeyRef.current = key;
        if (latestKeyRef.current === key) setSaveState("saved");
        return true;
      } catch (caught) {
        const message = caught instanceof Error ? caught.message : "实时保存失败";
        if (latestKeyRef.current === key) { setSaveState("error"); setToast({ message, tone: "bad" }); }
        return false;
      }
    });
    return saveQueueRef.current;
  }, [review.itemId]);

  useEffect(() => {
    latestKeyRef.current = draftKey;
    latestPayloadRef.current = draftPayload;
    if (lastSavedKeyRef.current === draftKey) { setSaveState("saved"); return; }
    setSaveState("pending");
    const timer = window.setTimeout(() => { void enqueueSave(draftKey, draftPayload); }, 450);
    return () => window.clearTimeout(timer);
  }, [draftKey, draftPayload, enqueueSave]);

  useEffect(() => () => {
    if (lastSavedKeyRef.current !== latestKeyRef.current) {
      void enqueueSave(latestKeyRef.current, latestPayloadRef.current);
    }
  }, [enqueueSave]);

  useEffect(() => {
    if (validationStatus !== "READY" || validationWasCurrent || validationRefreshRequestedRef.current) return;
    validationRefreshRequestedRef.current = true;
    setSaveState("pending");
    void enqueueSave(draftKey, draftPayload, true).then(async (saved) => {
      if (!saved) return;
      try { await refreshReview(); }
      catch (caught) { setToast({ message: caught instanceof Error ? caught.message : "无法读取更新后的运行检查", tone: "bad" }); }
    });
  }, [draftKey, draftPayload, enqueueSave, refreshReview, validationStatus, validationWasCurrent]);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(""), 2_000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  function updateField(key: keyof MetadataForm, value: string) { setForm((current) => ({ ...current, [key]: value })); }

  async function run(label: string, operation: () => Promise<void>) {
    setBusy(label); setNotice("");
    try { await operation(); return true; }
    catch (caught) { const message = caught instanceof Error ? caught.message : `${label}失败`; setToast({ message, tone: "bad" }); return false; }
    finally { setBusy(null); setJobProgress(""); setParentProgress(""); setMultiDiscProgress(""); }
  }

  async function flushDraft() {
    latestKeyRef.current = draftKey;
    return enqueueSave(draftKey, draftPayload);
  }

  async function rescrape(metadataProvider: "HASHEOUS" | "NONE") {
    const label = metadataProvider === "HASHEOUS" ? "重新查询 Hasheous" : "停用元信息源";
    if (!await flushDraft()) return;
    await run(label, async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/scrape-candidates`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"`, "Idempotency-Key": newUuid() }), body: JSON.stringify({ metadataProvider }) });
      if (!response.ok) throw new Error(await responseError(response, "重新查询失败：条目或版本已经变化"));
      const result = await response.json() as { version: number; state: string; scrapeRunId: string; jobId: string };
      versionRef.current = result.version;
      setJobProgress(`${result.state} · Job ${result.jobId.slice(0, 8)}…`);
      await waitForJob(result.jobId, setJobProgress);
      const updatedResponse = await fetch(`/api/v1/admin/reviews/${review.itemId}`, { cache: "no-store" });
      if (!updatedResponse.ok) throw new Error(await responseError(updatedResponse, "查询完成，但无法读取新游戏信息"));
      const updated = await updatedResponse.json() as ReviewWorkspace;
      setCandidates(updated.candidates); setUploadedAssets(updated.uploadedAssets ?? uploadedAssets);
      if (metadataProvider === "NONE") { setNotice("已记录不使用在线游戏信息"); return; }
      const completed = (updated.scrapeRuns ?? []).find((entry) => entry.scrapeRunId === result.scrapeRunId);
      const latest = updated.candidates.find((entry) => entry.scrapeRunId === result.scrapeRunId);
      if (!completed) throw new Error("查询完成，但服务器没有返回对应结果");
      if (!latest) { const message = `查询完成，但${scrapeResult(completed)}`; setNotice(message); setToast({ message, tone: "warn" }); return; }
      setComparison({ candidate: latest, current: { ...form }, next: candidateForm(latest, form), currentCover: cover, nextCover: { candidateId: readyCover(latest)?.candidateAssetId ?? null, uploadedId: null } });
    });
  }

  async function uploadCover(file: File, target: "current" | "comparison") {
    await run("上传封面", async () => {
      const uploaded = await uploadOne(file, setNotice);
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/assets`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"`, "Idempotency-Key": newUuid() }), body: JSON.stringify({ uploadFileId: uploaded.uploadFileId, kind: "COVER" }) });
      if (!response.ok) throw new Error(await responseError(response, "封面上传失败"));
      const asset = await response.json() as UploadedReviewAsset;
      setUploadedAssets((current) => current.some((entry) => entry.assetId === asset.assetId) ? current : [...current, asset]);
      if (target === "current") setCover({ candidateId: null, uploadedId: asset.assetId });
      else setComparison((current) => current ? { ...current, nextCover: { candidateId: null, uploadedId: asset.assetId } } : null);
      setNotice(target === "current" ? "新封面已上传，正在实时保存。" : "新封面已放入右侧对比结果，点击应用后生效。");
    });
  }

  const watchParentJob = useCallback(async (jobId: string) => {
    watchedParentJobRef.current = jobId;
    let terminalError: Error | null = null;
    try {
      await waitForJobEvents(jobId, setParentProgress);
    } catch (caught) {
      terminalError = caught instanceof Error ? caught : new Error("Parent ROM 校验失败");
    }
    const updated = await refreshReview();
    if (terminalError) setToast({ message: terminalError.message, tone: "bad" });
    else if (updated.validation?.status === "READY") setToast({ message: "Parent ROM 已匹配，运行检查已通过", tone: "good" });
    else setToast({ message: "Parent ROM 已匹配，仍有依赖需要处理", tone: "warn" });
  }, [refreshReview]);

  const activeParentJobId = arcadeDependencies?.activeAttachment?.jobId ?? "";
  useEffect(() => {
    if (!activeParentJobId || watchedParentJobRef.current === activeParentJobId) return;
    setParentProgress("恢复 Parent ROM 校验进度…");
    void watchParentJob(activeParentJobId).catch((caught: unknown) => {
      setToast({ message: caught instanceof Error ? caught.message : "无法恢复 Parent ROM 校验状态", tone: "bad" });
    }).finally(() => setParentProgress(""));
  }, [activeParentJobId, watchParentJob]);

  async function attachParent(node: ArcadeDependencyNode, file: File) {
    if (!await flushDraft()) return false;
    return run("补充 Parent ROM", async () => {
      setParentProgress("正在上传 Parent ZIP…");
      const uploaded = await uploadOne(file, setParentProgress);
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/arcade-parent-attachments`, {
        method: "POST",
        credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"`, "Idempotency-Key": newUuid() }),
        body: JSON.stringify({
          validationId: currentValidation?.id,
          baseSourceSnapshotId: effectiveSourceSnapshotId,
          dependencyMachine: node.machine,
          uploadFileId: uploaded.uploadFileId,
        }),
      });
      if (!response.ok) throw new Error(await responseError(response, "无法创建 Parent ROM 校验任务"));
      const result = await response.json() as { jobId: string };
      const version = response.headers.get("ETag")?.match(/^"v(\d+)"$/)?.[1];
      if (version) versionRef.current = Number(version);
      await watchParentJob(result.jobId);
    });
  }

  async function retryParent(attachment: ArcadeParentAttachment) {
    await run("重试 Parent ROM 校验", async () => {
      const snapshot = await fetch(`/api/v1/admin/jobs/${attachment.jobId}`, { cache: "no-store" });
      if (!snapshot.ok) throw new Error(await responseError(snapshot, "无法读取待重试任务"));
      const job = await snapshot.json() as { version: number };
      const response = await fetch(`/api/v1/admin/jobs/${attachment.jobId}/retry`, {
        method: "POST", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${job.version}"`, "Idempotency-Key": newUuid() }), body: "{}",
      });
      if (!response.ok) throw new Error(await responseError(response, "Parent ROM 校验无法重试"));
      await watchParentJob(attachment.jobId);
    });
  }

  const watchMultiDiscJob = useCallback(async (jobId: string) => {
    watchedMultiDiscJobRef.current = jobId;
    let terminalError: Error | null = null;
    try {
      await waitForJob(jobId, setMultiDiscProgress);
    } catch (caught) {
      terminalError = caught instanceof Error ? caught : new Error("补盘校验失败");
    }
    const updated = await refreshReview();
    if (terminalError) throw terminalError;
    if (updated.multiDisc?.missingDiscCount) throw new Error("补盘未通过：所选文件与当前缺失盘不一致");
    setToast({ message: "缺失光盘已补齐，正在更新审核结果", tone: "good" });
  }, [refreshReview]);

  const activeMultiDiscJobId = multiDisc?.activeAttachment?.jobId ?? "";
  useEffect(() => {
    if (!activeMultiDiscJobId || watchedMultiDiscJobRef.current === activeMultiDiscJobId) return;
    setMultiDiscProgress("恢复补盘校验进度…");
    void watchMultiDiscJob(activeMultiDiscJobId).catch((caught: unknown) => {
      setToast({ message: caught instanceof Error ? caught.message : "无法恢复补盘校验状态", tone: "bad" });
    }).finally(() => setMultiDiscProgress(""));
  }, [activeMultiDiscJobId, watchMultiDiscJob]);

  async function attachMissingDiscs(files: File[], onQueued: () => void) {
    if (!multiDisc || !await flushDraft()) return;
    await run("补充缺失光盘", async () => {
      const uploaded = await uploadFiles(files, setMultiDiscProgress);
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/multi-disc-attachments`, {
        method: "POST", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"`, "Idempotency-Key": newUuid() }),
        body: JSON.stringify({ uploadId: uploaded.uploadId }),
      });
      if (!response.ok) {
        const payload = await response.json().catch(() => null) as { error?: { code?: string; message?: string } } | null;
        const code = payload?.error?.code ?? "";
        if (["REVIEW_VERSION_CONFLICT", "REVIEW_MULTI_DISC_INPUT_STALE", "REVIEW_MULTI_DISC_ATTACHMENT_SET_MISMATCH", "REVIEW_MULTI_DISC_CONTENT_INVALID", "REVIEW_MULTI_DISC_ATTACHMENT_IN_PROGRESS", "REVIEW_MULTI_DISC_ATTACHMENT_RETRY_REQUIRED"].includes(code)) {
          await refreshReview();
        }
        throw new Error(payload?.error?.message ?? "无法创建补盘校验任务");
      }
      const result = await response.json() as { attachmentId: string; state: string; jobId: string; reviewVersion?: number };
      const responseVersion = response.headers.get("ETag")?.match(/^"v(\d+)"$/)?.[1];
      if (responseVersion) versionRef.current = Number(responseVersion);
      else if (result.reviewVersion) versionRef.current = result.reviewVersion;
      const queuedAttachment: ReviewMultiDiscAttachment = { attachmentId: result.attachmentId, state: result.state, errorCode: null, jobId: result.jobId, jobState: "QUEUED", canRetry: false };
      setMultiDisc((current) => current ? { ...current, canAttachMissingDiscs: false, latestAttachment: queuedAttachment, activeAttachment: queuedAttachment } : current);
      setMultiDiscProgress(`正在校验补充光盘 · Job ${result.jobId.slice(0, 8)}…`);
      onQueued();
      await watchMultiDiscJob(result.jobId);
    });
  }

  async function retryMultiDisc(attachment: ReviewMultiDiscAttachment) {
    await run("重试补盘校验", async () => {
      const snapshot = await fetch(`/api/v1/admin/jobs/${attachment.jobId}`, { cache: "no-store" });
      if (!snapshot.ok) throw new Error(await responseError(snapshot, "无法读取待重试补盘任务"));
      const job = await snapshot.json() as { version: number };
      const response = await fetch(`/api/v1/admin/jobs/${attachment.jobId}/retry`, {
        method: "POST", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${job.version}"`, "Idempotency-Key": newUuid() }), body: "{}",
      });
      if (!response.ok) throw new Error(await responseError(response, "补盘校验无法重试"));
      setMultiDiscProgress(`正在重试补盘校验 · Job ${attachment.jobId.slice(0, 8)}…`);
      await watchMultiDiscJob(attachment.jobId);
    });
  }

  function applyComparison() {
    if (!comparison) return;
    setForm(comparison.next); setCandidateId(comparison.candidate.candidateId); setCover(comparison.nextCover);
    setBackgroundId(null); setScreenshotIds([]); setComparison(null);
    setNotice("新查询结果已应用，系统会实时保存；旧候选素材选择已清除。");
  }

  function clearQueueCache() {
    const query = new URL(returnTo, window.location.origin).searchParams.toString();
    const key = userStorageKey(context.user?.userId, "reviews", `queue:${query}`);
    if (key) sessionStorage.removeItem(key);
  }

  async function publish(duplicateGames: DuplicateGame[] = []) {
    await run("发布", async () => {
      const body = duplicateGames.length ? { duplicatePolicy: "ALLOW_NEW", acknowledgedGameIds: duplicateGames.map((game) => game.gameId) } : {};
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/approve`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"`, "Idempotency-Key": newUuid() }), body: JSON.stringify(body) });
      if (!response.ok) {
        const payload = await response.json().catch(() => null) as { error?: { code?: string; message?: string; details?: { games?: DuplicateGame[] } } } | null;
        if (payload?.error?.code === "DUPLICATE_GAME_CONFIRMATION_REQUIRED" && payload.error.details?.games?.length) {
          setDuplicateConfirmation(payload.error.details.games);
          return;
        }
        throw new Error(payload?.error?.message ?? "发布失败：请确认实时保存和运行检查均已完成");
      }
      setDuplicateConfirmation(null);
      clearQueueCache(); queueFlashToast({ message: "游戏已成功发布，待审核队列已更新。", tone: "good" });
      router.replace(nextItemId ? `/admin/reviews/${nextItemId}?returnTo=${encodeURIComponent(returnTo)}` : returnTo);
    });
  }

  async function approve() {
    if (!await flushDraft()) return;
    const duplicates = review.duplicateGames ?? [];
    if (duplicates.length) {
      setDuplicateConfirmation(duplicates);
      return;
    }
    await publish();
  }

  async function revalidate() {
    await run("重新运行检查", async () => {
      if (!await enqueueSave(draftKey, draftPayload, true)) throw new Error("无法保存当前审核内容");
      validationRefreshRequestedRef.current = true;
      const updated = await refreshReview();
      const ready = updated.canApprove ?? (updated.validation?.current === true && updated.validation.status === "READY");
      setNotice(ready ? "运行检查已通过，可以发布。" : "运行检查已更新，请按最新提示继续处理。");
    });
  }

  async function confirmDuplicatePublish() {
    if (!duplicateConfirmation || !await flushDraft()) return;
    await publish(duplicateConfirmation);
  }

  async function discard() {
    if (!await flushDraft()) return;
    await run("丢弃", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/discard`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"`, "Idempotency-Key": newUuid() }), body: "{}" });
      if (!response.ok) throw new Error(await responseError(response, "丢弃失败：审核状态或版本已经变化"));
      clearQueueCache(); queueFlashToast({ message: "条目已丢弃，待审核队列已更新。", tone: "good" });
      router.replace(nextItemId ? `/admin/reviews/${nextItemId}?returnTo=${encodeURIComponent(returnTo)}` : returnTo);
    });
  }

  const sourceCover = review.sourceMedia?.coverUrl ? {
    id: review.sourceMedia.sourceRefId,
    url: review.sourceMedia.coverUrl,
    width: review.sourceMedia.coverWidthPx ?? 600,
    height: review.sourceMedia.coverHeightPx ?? 800,
  } : null;
  const selectedCover = previewAsset(candidates, uploadedAssets, cover) ?? sourceCover;
  const currentCompareCover = comparison ? previewAsset(candidates, uploadedAssets, comparison.currentCover) ?? sourceCover : null;
  const nextCompareCover = comparison ? previewAsset(candidates, uploadedAssets, comparison.nextCover) ?? sourceCover : null;
  const saveLabel = saveState === "saving" ? "正在实时保存…" : saveState === "pending" ? "等待保存…" : saveState === "error" ? "实时保存失败" : "已实时保存";

  const parentAttachmentActive = Boolean(arcadeDependencies?.activeAttachment?.state === "QUEUED" || arcadeDependencies?.activeAttachment?.state === "RUNNING");
  const multiDiscAttachmentActive = Boolean(multiDisc?.activeAttachment?.state === "QUEUED" || multiDisc?.activeAttachment?.state === "RUNNING");
  const publishReady = serverCanApprove && validationStatus === "READY" && validationCurrent && !parentAttachmentActive && !multiDiscAttachmentActive && !multiDisc?.missingDiscCount;

  return <div className="review-workflow-detail">
    <div className="review-workflow-top">
      <section className="review-workflow-summary-card"><StatusPill tone="info">来源：{review.sourceMedia ? `Pegasus · ${review.sourceMedia.sourceLabel ?? sourceDisplayName}` : sourceDisplayName}</StatusPill><h2>{form.title || sourceDisplayName}</h2><p>目标目录：{platformInstanceName}</p><div><StatusPill tone="info">已接收来源文件</StatusPill><StatusPill tone={publishReady ? "good" : "warn"}>{publishReady ? "运行检查通过" : validationStatus === "READY" ? "运行检查更新中" : "运行检查未通过"}</StatusPill><StatusPill tone={candidateId || review.sourceMedia ? "info" : "warn"}>{candidateId ? "已找到游戏信息" : review.sourceMedia ? "已读取 Pegasus 信息" : "未找到游戏信息"}</StatusPill></div></section>
      <aside className="review-workflow-decision"><h2>审核决定</h2><p>{publishReady ? "运行检查已经通过，可以发布。" : "先按左侧提示处理问题，再重新运行检查。"}</p><div className="review-workflow-save"><span>实时保存</span><strong className={`autosave-state ${saveState}`}><i aria-hidden="true" /><span>{saveLabel}</span></strong></div>{!publishReady ? <button type="button" className="button secondary review-revalidate" aria-busy={busy === "重新运行检查"} disabled={busy !== null || saveState === "error"} onClick={() => void revalidate()}>{busy === "重新运行检查" ? "正在检查…" : "重新运行检查"}</button> : null}<div className="review-workflow-decision-actions"><button type="button" className="button secondary" disabled={busy !== null} onClick={() => void discard()}>{busy === "丢弃" ? "正在丢弃…" : "丢弃条目"}</button><button type="button" className="button" aria-busy={busy === "发布"} disabled={busy !== null || !publishReady || saveState === "error"} onClick={() => void approve()}>{busy === "发布" ? <><i className="button-spinner" aria-hidden="true" />正在发布…</> : "通过并发布"}</button></div></aside>
    </div>
    {notice ? <div className="review-workflow-feedback"><FeedbackBanner tone="info">{notice}</FeedbackBanner></div> : null}
    {review.duplicateGames?.length ? <div className="review-workflow-feedback"><FeedbackBanner tone="info">相同游戏文件已经关联到 {review.duplicateGames.map((game, index) => <span key={game.gameId}>{index ? "、" : ""}<Link href={`/games/${game.gameId}`}>{game.title}</Link></span>)}。仍可发布为新游戏，但发布时需要二次确认。</FeedbackBanner></div> : null}
    <div className="review-workflow-columns">
      <div className="review-workflow-left">{children}{multiDisc ? <MultiDiscReviewCard value={multiDisc} disabled={busy !== null || multiDiscAttachmentActive} progress={multiDiscProgress} onAttach={attachMissingDiscs} onRetry={retryMultiDisc} /> : null}{arcadeDependencies ? <ArcadeDependencyCard value={arcadeDependencies} disabled={busy !== null || parentAttachmentActive} progress={parentProgress} onAttach={attachParent} onRetry={retryParent} /> : null}</div>
      <section className="panel review-workflow-metadata">
        <div className="panel-head"><div><h2>② 发布成什么？</h2><p>核对标题、简介和封面；修改会实时保存。</p></div><div className="review-workflow-query-actions">{jobProgress ? <p className="scrape-live" role="status"><i className="button-spinner" aria-hidden="true" />正在查询游戏信息：{jobProgress}</p> : null}<button type="button" className="button secondary" disabled={busy !== null} aria-busy={busy === "重新查询 Hasheous"} onClick={() => void rescrape("HASHEOUS")}>{busy === "重新查询 Hasheous" ? <><i className="button-spinner" aria-hidden="true" />查询中…</> : "重新查询游戏信息"}</button></div></div>
        <div className="panel-body review-workflow-editor">
          <div className="review-workflow-publish-layout">
            <div className="form-grid review-workflow-metadata-fields">
              <label className="field full">标题<input value={form.title} onChange={(event) => updateField("title", event.target.value)} maxLength={200} /></label>
              <label className="field full">简介<textarea value={form.description} onChange={(event) => updateField("description", event.target.value)} maxLength={10000} /></label>
              <label className="field review-workflow-field-half">开发商<input value={form.developer} onChange={(event) => updateField("developer", event.target.value)} maxLength={200} /></label>
              <label className="field review-workflow-field-half">发行商<input value={form.publisher} onChange={(event) => updateField("publisher", event.target.value)} maxLength={200} /></label>
              <label className="field review-workflow-field-third">类型<input value={form.genre} onChange={(event) => updateField("genre", event.target.value)} maxLength={200} /></label>
              <label className="field review-workflow-field-third">玩家数<input type="number" min={1} max={64} value={form.players} onChange={(event) => updateField("players", event.target.value)} /></label>
              <label className="field review-workflow-field-third">发行年份<input type="number" min={1950} value={form.releaseYear} onChange={(event) => updateField("releaseYear", event.target.value)} /></label>
              {review.dosEntries.length ? <label className="field full">DOS 默认程序<select value={defaultDosEntry ?? ""} onChange={(event) => setDefaultDosEntry(event.target.value || null)}><option value="">打开 DOSBox 程序菜单</option>{review.dosEntries.map((entry) => <option key={entry.path} value={entry.path} disabled={!entry.enabled}>{entry.originalPath}{entry.directLaunchSafe ? "" : " · 仅程序菜单"}</option>)}</select></label> : null}
            </div>
            <aside className="review-cover-panel review-workflow-cover-side"><span className="field-label">当前封面</span><label className="review-cover-upload" title="点击上传替换封面"><AssetPreview asset={selectedCover} label="当前选择的游戏封面" /><span>点击图片上传替换</span><input type="file" accept="image/png,image/jpeg,image/webp" disabled={busy !== null} onChange={(event) => { const file = event.target.files?.[0]; if (file) void uploadCover(file, "current"); event.currentTarget.value = ""; }} /></label>{cover.candidateId || cover.uploadedId ? <button type="button" className="button secondary compact" onClick={() => setCover({ candidateId: null, uploadedId: null })}>{sourceCover ? "恢复 Pegasus 封面" : "移除封面"}</button> : null}{review.sourceMedia?.videoUrl ? <div className="review-source-video"><span className="field-label">Pegasus 视频预览</span><video controls preload="metadata" src={review.sourceMedia.videoUrl}>浏览器无法播放这段视频。</video><small>通过审核后会随游戏一并发布。</small></div> : null}</aside>
          </div>
        </div>
      </section>
    </div>
    <ConfirmDialog open={comparison !== null} wide title="对比最新查询结果" description="左栏是当前信息，右栏是最新结果；每栏上方为基础信息与封面，下方为完整简介。红色表示内容不同，绿色表示一致；右栏可编辑。" confirmLabel="应用" busy={busy !== null} onCancel={() => setComparison(null)} onConfirm={applyComparison}>
      {comparison ? <div className="metadata-compare metadata-compare-columns">
        <section className="metadata-compare-column" aria-label="当前信息"><header><strong>当前信息</strong><span>只读</span></header><div className="metadata-compare-column-top"><div className="metadata-compare-fields">{compareFields.filter((field) => !field.multiline).map((field) => <div className="compare-readonly" key={field.key}><span>{field.label}</span><p>{comparison.current[field.key] || "未填写"}</p></div>)}</div><div className="metadata-compare-column-cover"><span>封面</span><AssetPreview asset={currentCompareCover} label="当前游戏封面" /></div></div><div className="metadata-compare-column-description"><span>游戏说明</span><p>{comparison.current.description || "未填写"}</p></div></section>
        <section className="metadata-compare-column" aria-label="最新信息"><header><strong>最新信息</strong><span>可编辑</span></header><div className="metadata-compare-column-top"><div className="metadata-compare-fields">{compareFields.filter((field) => !field.multiline).map((field) => { const same = comparison.current[field.key] === comparison.next[field.key]; return <label className={`compare-field ${same ? "is-same" : "is-changed"}`} key={field.key}><span>{field.label}</span><input aria-label={field.label} type={field.type ?? "text"} value={comparison.next[field.key]} onChange={(event) => setComparison((current) => current ? { ...current, next: { ...current.next, [field.key]: event.target.value } } : null)} /></label>; })}</div><div className={`metadata-compare-column-cover ${comparison.currentCover.candidateId === comparison.nextCover.candidateId && comparison.currentCover.uploadedId === comparison.nextCover.uploadedId ? "is-same" : "is-changed"}`}><span>封面</span><label className="review-cover-upload"><AssetPreview asset={nextCompareCover} label="最新查询封面" /><span>点击图片上传替换</span><input type="file" accept="image/png,image/jpeg,image/webp" disabled={busy !== null} onChange={(event) => { const file = event.target.files?.[0]; if (file) void uploadCover(file, "comparison"); event.currentTarget.value = ""; }} /></label>{comparison.nextCover.candidateId || comparison.nextCover.uploadedId ? <button type="button" className="button secondary compact" onClick={() => setComparison((current) => current ? { ...current, nextCover: { candidateId: null, uploadedId: null } } : null)}>不使用新封面</button> : null}</div></div><label className={`metadata-compare-column-description ${comparison.current.description === comparison.next.description ? "is-same" : "is-changed"}`}><span>游戏说明（可编辑）</span><textarea aria-label="简介" value={comparison.next.description} onChange={(event) => setComparison((current) => current ? { ...current, next: { ...current.next, description: event.target.value } } : null)} /></label></section>
      </div> : null}
    </ConfirmDialog>
    <ConfirmDialog open={duplicateConfirmation !== null} title="仍然发布为新游戏？" description="相同游戏文件已经存在。继续发布会创建另一个游戏条目，可能造成重复游戏。" confirmLabel="仍然发布为新游戏" tone="danger" busy={busy === "发布"} onCancel={() => setDuplicateConfirmation(null)} onConfirm={() => void confirmDuplicatePublish()}>
      {duplicateConfirmation ? <ul>{duplicateConfirmation.map((game) => <li key={game.gameId}><Link href={`/games/${game.gameId}`}>{game.title}</Link><span> · {game.platformInstanceName}</span></li>)}</ul> : null}
    </ConfirmDialog>
    <Toast toast={toast} onDismiss={() => setToast(null)} />
  </div>;
}

function StatusPill({ tone, children }: { tone: "good" | "warn" | "info"; children: ReactNode }) {
  return <span className={`status ${tone}`}><i />{children}</span>;
}

function MultiDiscReviewCard({ value, disabled, progress, onAttach, onRetry }: { value: ReviewMultiDisc; disabled: boolean; progress: string; onAttach: (files: File[], onQueued: () => void) => Promise<void>; onRetry: (attachment: ReviewMultiDiscAttachment) => Promise<void> }) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const maxDiscs = value.maxDiscs ?? 8;
  const maxTotalBytes = value.maxTotalBytes ?? 1_073_741_824;
  const latest = value.latestAttachment;
  const validating = Boolean(progress || value.activeAttachment);
  return <section className="panel review-multidisc-card" aria-labelledby="review-multidisc-title">
    <div className="panel-head"><div><h2 id="review-multidisc-title">多盘内容</h2><p>{value.playlist.name} · {formatBytes(value.playlist.sizeBytes)} · SHA-256 {value.playlist.sha256.slice(0, 12)}…</p><small>{value.discCount} / {maxDiscs} 张光盘 · 已接收 {formatBytes(value.totalPresentBytes)} / 上限 {formatBytes(maxTotalBytes)}</small></div><StatusPill tone={value.missingDiscCount ? "warn" : "good"}>{value.missingDiscCount ? `缺少 ${value.missingDiscCount} 张` : "盘序完整"}</StatusPill></div>
    <div className="panel-body">
      <ol className="review-multidisc-list">{value.entries.map((entry) => <li key={entry.discIndex}><span><strong>{entry.label} · {entry.sourceReference}</strong><small>规范文件名：{entry.canonicalName}</small>{entry.sha256 ? <small>SHA-256 {entry.sha256.slice(0, 12)}…</small> : null}</span><span className={`status ${entry.state === "PRESENT" ? "good" : "warn"}`}><i />{entry.state === "PRESENT" ? entry.sizeBytes === null ? "已接收" : formatBytes(entry.sizeBytes) : "待补齐"}</span></li>)}</ol>
      {validating ? <FeedbackBanner tone="info">正在校验补充光盘。{value.activeAttachment?.jobId ? `Job ${value.activeAttachment.jobId}` : progress}</FeedbackBanner> : value.missingDiscCount ? latest?.state === "FAILED_RETRYABLE" ? <FeedbackBanner tone="bad">补盘校验服务暂时不可用；可以复用已上传文件重试。错误码：{latest.errorCode ?? "REVIEW_MULTI_DISC_VALIDATION_UNAVAILABLE"}</FeedbackBanner> : latest?.state === "REJECTED" ? <FeedbackBanner tone="bad">上次补盘未通过：{latest.errorCode ?? "REVIEW_MULTI_DISC_CONTENT_INVALID"}</FeedbackBanner> : <FeedbackBanner tone="bad">多盘内容不完整，发布已阻止。请一次上传当前全部缺失光盘。</FeedbackBanner> : <FeedbackBanner tone="good">多盘内容完整，运行检查结果已更新。</FeedbackBanner>}
      {progress ? <p className="scrape-live" role="status"><i className="button-spinner" aria-hidden="true" />正在校验补充光盘：{progress}</p> : null}
      {value.missingDiscCount ? <div className="review-multidisc-actions">{latest?.state === "FAILED_RETRYABLE" && latest.canRetry ? <button className="button secondary" type="button" disabled={disabled} onClick={() => void onRetry(latest)}>重试校验</button> : null}<button className="button secondary" type="button" disabled={disabled || !value.canAttachMissingDiscs} onClick={() => setDrawerOpen(true)}>{latest?.state === "REJECTED" ? "重新上传全部缺失光盘" : "上传全部缺失光盘"}</button></div> : null}
    </div>
    <MultiDiscAttachmentDrawer open={drawerOpen} missingReferences={value.missingReferences} presentBytes={value.totalPresentBytes} maxTotalBytes={maxTotalBytes} busy={disabled} progress={progress} onClose={() => setDrawerOpen(false)} onSubmit={onAttach} />
  </section>;
}
