"use client";

import Image from "next/image";
import { useRouter } from "next/navigation";
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { queueFlashToast, Toast, type ToastMessage } from "@/components/flash-toast";
import { FeedbackBanner } from "@/components/ui";
import { newUuid } from "@/lib/crypto";
import { responseError, uploadOne, waitForJob } from "@/lib/upload";

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

export type ReviewWorkspace = {
  itemId: string;
  version: number;
  metadata: { title: string; description: string; developer: string; publisher: string; genre: string; players: number | null; releaseYear: number | null };
  validation: { id: string; status: string; current: boolean; compatibilityCode: string } | null;
  candidates: ReviewCandidate[];
  uploadedAssets?: UploadedReviewAsset[];
  scrapeRuns?: ReviewScrapeRun[];
  selectedCandidateId: string | null;
  selectedAssets: { coverCandidateAssetId: string | null; coverUploadedAssetId?: string | null; backgroundCandidateAssetId: string | null; screenshotCandidateAssetIds: string[] };
  defaultDosEntry: string | null;
  dosEntries: Array<{ path: string; originalPath: string; kind: string; enabled: boolean; directLaunchSafe: boolean }>;
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
  const validationStatus = review.validation?.status ?? null;
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
  const versionRef = useRef(review.version);
  const validationRefreshRequestedRef = useRef(false);
  const latestKeyRef = useRef("");
  const saveQueueRef = useRef<Promise<boolean>>(Promise.resolve(true));
  const serverPayload = toPayload(initialMetadata, review.selectedCandidateId, { candidateId: review.selectedAssets.coverCandidateAssetId, uploadedId: review.selectedAssets.coverUploadedAssetId ?? null }, review.selectedAssets.backgroundCandidateAssetId, review.selectedAssets.screenshotCandidateAssetIds, review.defaultDosEntry);
  const lastSavedKeyRef = useRef(JSON.stringify(serverPayload));
  const draftPayload = useMemo(() => toPayload(form, candidateId, cover, backgroundId, screenshotIds, defaultDosEntry), [form, candidateId, cover, backgroundId, screenshotIds, defaultDosEntry]);
  const draftKey = useMemo(() => JSON.stringify(draftPayload), [draftPayload]);
  const latestPayloadRef = useRef(draftPayload);

  const enqueueSave = useCallback((key: string, payload: DraftPayload, force = false) => {
    saveQueueRef.current = saveQueueRef.current.catch(() => false).then(async () => {
      if (!force && lastSavedKeyRef.current === key) return true;
      if (latestKeyRef.current === key) setSaveState("saving");
      try {
        const response = await fetch(`/api/v1/admin/reviews/${review.itemId}`, { method: "PATCH", credentials: "same-origin", keepalive: true, headers: { "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"` }, body: JSON.stringify(payload) });
        if (!response.ok) throw new Error(await responseError(response, "实时保存失败：字段、来源或版本已经变化"));
        const result = await response.json() as { version: number };
        versionRef.current = result.version;
        lastSavedKeyRef.current = key;
        if (validationStatus === "READY") setValidationCurrent(true);
        if (latestKeyRef.current === key) setSaveState("saved");
        return true;
      } catch (caught) {
        const message = caught instanceof Error ? caught.message : "实时保存失败";
        if (latestKeyRef.current === key) { setSaveState("error"); setToast({ message, tone: "bad" }); }
        return false;
      }
    });
    return saveQueueRef.current;
  }, [review.itemId, validationStatus]);

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
    void enqueueSave(draftKey, draftPayload, true);
  }, [draftKey, draftPayload, enqueueSave, validationStatus, validationWasCurrent]);

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
    finally { setBusy(null); setJobProgress(""); }
  }

  async function flushDraft() {
    latestKeyRef.current = draftKey;
    return enqueueSave(draftKey, draftPayload);
  }

  async function rescrape(metadataProvider: "HASHEOUS" | "NONE") {
    const label = metadataProvider === "HASHEOUS" ? "重新查询 Hasheous" : "停用元信息源";
    if (!await flushDraft()) return;
    await run(label, async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/scrape-candidates`, { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"`, "Idempotency-Key": newUuid() }, body: JSON.stringify({ metadataProvider }) });
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
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/assets`, { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"`, "Idempotency-Key": newUuid() }, body: JSON.stringify({ uploadFileId: uploaded.uploadFileId, kind: "COVER" }) });
      if (!response.ok) throw new Error(await responseError(response, "封面上传失败"));
      const asset = await response.json() as UploadedReviewAsset;
      setUploadedAssets((current) => current.some((entry) => entry.assetId === asset.assetId) ? current : [...current, asset]);
      if (target === "current") setCover({ candidateId: null, uploadedId: asset.assetId });
      else setComparison((current) => current ? { ...current, nextCover: { candidateId: null, uploadedId: asset.assetId } } : null);
      setNotice(target === "current" ? "新封面已上传，正在实时保存。" : "新封面已放入右侧对比结果，点击应用后生效。");
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
    sessionStorage.removeItem(`retrom:review-queue:${query}`);
  }

  async function approve() {
    if (!await flushDraft()) return;
    await run("发布", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/approve`, { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"`, "Idempotency-Key": newUuid() }, body: "{}" });
      if (!response.ok) throw new Error(await responseError(response, "发布失败：请确认实时保存和运行检查均已完成"));
      clearQueueCache(); queueFlashToast({ message: "游戏已成功发布，待审核队列已更新。", tone: "good" });
      router.replace(nextItemId ? `/admin/reviews/${nextItemId}?returnTo=${encodeURIComponent(returnTo)}` : returnTo);
    });
  }

  async function discard() {
    if (!await flushDraft()) return;
    await run("丢弃", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/discard`, { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"`, "Idempotency-Key": newUuid() }, body: "{}" });
      if (!response.ok) throw new Error(await responseError(response, "丢弃失败：审核状态或版本已经变化"));
      clearQueueCache(); queueFlashToast({ message: "条目已丢弃，待审核队列已更新。", tone: "good" });
      router.replace(nextItemId ? `/admin/reviews/${nextItemId}?returnTo=${encodeURIComponent(returnTo)}` : returnTo);
    });
  }

  const selectedCover = previewAsset(candidates, uploadedAssets, cover);
  const currentCompareCover = comparison ? previewAsset(candidates, uploadedAssets, comparison.currentCover) : null;
  const nextCompareCover = comparison ? previewAsset(candidates, uploadedAssets, comparison.nextCover) : null;
  const saveLabel = saveState === "saving" ? "正在实时保存…" : saveState === "pending" ? "等待保存…" : saveState === "error" ? "实时保存失败" : "已实时保存";

  const publishReady = validationStatus === "READY" && validationCurrent;

  return <div className="review-workflow-detail">
    <div className="review-workflow-top">
      <section className="review-workflow-summary-card"><StatusPill tone="info">来源：{sourceDisplayName}</StatusPill><h2>{form.title || sourceDisplayName}</h2><p>目标目录：{platformInstanceName}</p><div><StatusPill tone="info">已接收来源文件</StatusPill><StatusPill tone={publishReady ? "good" : "warn"}>{publishReady ? "运行检查通过" : validationStatus === "READY" ? "运行检查更新中" : "运行检查未通过"}</StatusPill><StatusPill tone={candidateId ? "info" : "warn"}>{candidateId ? "已找到游戏信息" : "未找到游戏信息"}</StatusPill></div></section>
      <aside className="review-workflow-decision"><h2>审核决定</h2><p>字段会实时保存；只有运行检查通过时才允许发布。</p><div className="review-workflow-save"><span>实时保存</span><strong className={`autosave-state ${saveState}`}><i aria-hidden="true" /><span>{saveLabel}</span></strong></div><div className="review-workflow-decision-actions"><button type="button" className="button secondary" disabled={busy !== null} onClick={() => void discard()}>{busy === "丢弃" ? "正在丢弃…" : "丢弃条目"}</button><button type="button" className="button" aria-busy={busy === "发布"} disabled={busy !== null || !publishReady || saveState === "error"} onClick={() => void approve()}>{busy === "发布" ? <><i className="button-spinner" aria-hidden="true" />正在发布…</> : "通过并发布"}</button></div></aside>
    </div>
    {notice ? <div className="review-workflow-feedback"><FeedbackBanner tone="info">{notice}</FeedbackBanner></div> : null}
    <div className="review-workflow-columns">
      <div className="review-workflow-left">{children}</div>
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
            <aside className="review-cover-panel review-workflow-cover-side"><span className="field-label">当前封面</span><label className="review-cover-upload" title="点击上传替换封面"><AssetPreview asset={selectedCover} label="当前选择的游戏封面" /><span>点击图片上传替换</span><input type="file" accept="image/png,image/jpeg,image/webp" disabled={busy !== null} onChange={(event) => { const file = event.target.files?.[0]; if (file) void uploadCover(file, "current"); event.currentTarget.value = ""; }} /></label>{cover.candidateId || cover.uploadedId ? <button type="button" className="button secondary compact" onClick={() => setCover({ candidateId: null, uploadedId: null })}>移除封面</button> : null}</aside>
          </div>
        </div>
      </section>
    </div>
    <ConfirmDialog open={comparison !== null} wide title="对比最新查询结果" description="左侧是当前信息，右侧是最新结果。红色表示内容不同，绿色表示一致；右侧可编辑并替换封面。" confirmLabel="应用" busy={busy !== null} onCancel={() => setComparison(null)} onConfirm={applyComparison}>
      {comparison ? <div className="metadata-compare"><div className="metadata-compare-head"><strong>当前信息（只读）</strong><strong>最新信息（可编辑）</strong></div>{compareFields.map((field) => { const same = comparison.current[field.key] === comparison.next[field.key]; return <div className="metadata-compare-row" key={field.key}><div className="compare-readonly"><span>{field.label}</span><p>{comparison.current[field.key] || "未填写"}</p></div><label className={`compare-field ${same ? "is-same" : "is-changed"}`}><span>{field.label}</span>{field.multiline ? <textarea aria-label={field.label} value={comparison.next[field.key]} onChange={(event) => setComparison((current) => current ? { ...current, next: { ...current.next, [field.key]: event.target.value } } : null)} /> : <input aria-label={field.label} type={field.type ?? "text"} value={comparison.next[field.key]} onChange={(event) => setComparison((current) => current ? { ...current, next: { ...current.next, [field.key]: event.target.value } } : null)} />}</label></div>; })}<div className="metadata-compare-row compare-cover-row"><div className="compare-readonly"><span>当前封面</span><AssetPreview asset={currentCompareCover} label="当前游戏封面" /></div><div className={`${comparison.currentCover.candidateId === comparison.nextCover.candidateId && comparison.currentCover.uploadedId === comparison.nextCover.uploadedId ? "is-same" : "is-changed"} compare-field`}><span>最新封面</span><label className="review-cover-upload"><AssetPreview asset={nextCompareCover} label="最新查询封面" /><span>点击图片上传替换</span><input type="file" accept="image/png,image/jpeg,image/webp" disabled={busy !== null} onChange={(event) => { const file = event.target.files?.[0]; if (file) void uploadCover(file, "comparison"); event.currentTarget.value = ""; }} /></label>{comparison.nextCover.candidateId || comparison.nextCover.uploadedId ? <button type="button" className="button secondary compact" onClick={() => setComparison((current) => current ? { ...current, nextCover: { candidateId: null, uploadedId: null } } : null)}>不使用新封面</button> : null}</div></div></div> : null}
    </ConfirmDialog>
    <Toast toast={toast} onDismiss={() => setToast(null)} />
  </div>;
}

function StatusPill({ tone, children }: { tone: "good" | "warn" | "info"; children: ReactNode }) {
  return <span className={`status ${tone}`}><i />{children}</span>;
}
