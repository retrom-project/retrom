"use client";

import Image from "next/image";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { queueFlashToast, Toast, type ToastMessage } from "@/components/flash-toast";
import { FeedbackBanner } from "@/components/ui";
import { newUuid } from "@/lib/crypto";
import { responseError, waitForJob } from "@/lib/upload";

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
  validation: { id: string; status: string; compatibilityCode: string } | null;
  candidates: ReviewCandidate[];
  scrapeRuns?: ReviewScrapeRun[];
  selectedCandidateId: string | null;
  selectedAssets: { coverCandidateAssetId: string | null; backgroundCandidateAssetId: string | null; screenshotCandidateAssetIds: string[] };
  defaultDosEntry: string | null;
  dosEntries: Array<{ path: string; originalPath: string; kind: string; enabled: boolean; directLaunchSafe: boolean }>;
};

type MetadataForm = {
  title: string;
  description: string;
  developer: string;
  publisher: string;
  genre: string;
  players: string;
  releaseYear: string;
};

type Comparison = {
  candidate: ReviewCandidate;
  current: MetadataForm;
  next: MetadataForm;
  currentCoverId: string | null;
  nextCoverId: string | null;
};

const compareFields: Array<{ key: keyof MetadataForm; label: string; multiline?: boolean; type?: "number" }> = [
  { key: "title", label: "标题" },
  { key: "description", label: "简介", multiline: true },
  { key: "developer", label: "开发商" },
  { key: "publisher", label: "发行商" },
  { key: "genre", label: "类型" },
  { key: "players", label: "玩家数", type: "number" },
  { key: "releaseYear", label: "发行年份", type: "number" },
];

function metadataForm(review: ReviewWorkspace): MetadataForm {
  return {
    title: review.metadata.title,
    description: review.metadata.description,
    developer: review.metadata.developer,
    publisher: review.metadata.publisher,
    genre: review.metadata.genre,
    players: review.metadata.players === null ? "" : String(review.metadata.players),
    releaseYear: review.metadata.releaseYear === null ? "" : String(review.metadata.releaseYear),
  };
}

function candidateForm(candidate: ReviewCandidate, fallback: MetadataForm): MetadataForm {
  const stringField = (key: keyof MetadataForm) => typeof candidate.metadata[key] === "string" ? String(candidate.metadata[key]) : fallback[key];
  const numberField = (key: "players" | "releaseYear") => typeof candidate.metadata[key] === "number" && Number.isInteger(candidate.metadata[key]) ? String(candidate.metadata[key]) : fallback[key];
  return {
    title: stringField("title"),
    description: stringField("description"),
    developer: stringField("developer"),
    publisher: stringField("publisher"),
    genre: stringField("genre"),
    players: numberField("players"),
    releaseYear: numberField("releaseYear"),
  };
}

function readyCover(candidate: ReviewCandidate | null) {
  return candidate?.assets.find((asset) => asset.kind === "COVER" && asset.status === "READY") ?? null;
}

function assetById(candidates: ReviewCandidate[], id: string | null) {
  if (!id) return null;
  return candidates.flatMap((candidate) => candidate.assets).find((asset) => asset.candidateAssetId === id) ?? null;
}

function scrapeResult(run: ReviewScrapeRun) {
  if (run.candidateCount > 0) return `找到 ${run.candidateCount} 组可用信息`;
  if (run.outcomes.invalidResponse > 0) return "上游响应无法解析";
  if (run.outcomes.rateLimited + run.outcomes.timeout + run.outcomes.networkError > 0) return "上游限流、超时或网络异常";
  if (run.outcomes.miss > 0) return "精确文件特征查询未命中";
  return run.evidenceCount === 0 ? "没有可查询的文件特征" : "没有找到可用信息";
}

function AssetPreview({ asset, label }: { asset: ReviewAsset | null; label: string }) {
  return asset?.status === "READY" && asset.widthPx && asset.heightPx
    ? <Image src={`/api/v1/admin/review-assets/${asset.candidateAssetId}`} alt={label} width={asset.widthPx} height={asset.heightPx} unoptimized />
    : <div className="asset-placeholder">暂无封面</div>;
}

export function ReviewActions({ review, returnTo = "/admin/reviews", nextItemId = null }: { review: ReviewWorkspace; returnTo?: string; nextItemId?: string | null }) {
  const router = useRouter();
  const initialMetadata = metadataForm(review);
  const automaticCandidate = review.selectedCandidateId ? null : review.candidates[0] ?? null;
  const automaticCover = readyCover(automaticCandidate);
  const [version, setVersion] = useState(review.version);
  const [form, setForm] = useState<MetadataForm>(() => automaticCandidate ? candidateForm(automaticCandidate, initialMetadata) : initialMetadata);
  const [candidateId, setCandidateId] = useState<string | null>(review.selectedCandidateId ?? automaticCandidate?.candidateId ?? null);
  const [coverId, setCoverId] = useState<string | null>(review.selectedAssets.coverCandidateAssetId ?? automaticCover?.candidateAssetId ?? null);
  const [backgroundId, setBackgroundId] = useState<string | null>(review.selectedAssets.backgroundCandidateAssetId);
  const [screenshotIds, setScreenshotIds] = useState(review.selectedAssets.screenshotCandidateAssetIds);
  const [defaultDosEntry, setDefaultDosEntry] = useState<string | null>(review.defaultDosEntry);
  const [approvalReason, setApprovalReason] = useState("");
  const [discardReason, setDiscardReason] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState(automaticCandidate ? "首次查询到的游戏信息和封面已填入草稿，请核对后保存。" : "");
  const [dirty, setDirty] = useState(automaticCandidate !== null);
  const [candidates, setCandidates] = useState(review.candidates);
  const [jobProgress, setJobProgress] = useState("");
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const [pendingNavigation, setPendingNavigation] = useState<string | null>(null);
  const [comparison, setComparison] = useState<Comparison | null>(null);

  useEffect(() => {
    if (!dirty) return;
    const unload = (event: BeforeUnloadEvent) => event.preventDefault();
    const navigate = (event: MouseEvent) => {
      const link = (event.target as Element | null)?.closest("a[href]");
      if (!link || link.getAttribute("target") === "_blank") return;
      const href = link.getAttribute("href");
      if (!href || href.startsWith("#")) return;
      event.preventDefault(); event.stopPropagation(); setPendingNavigation(href);
    };
    window.addEventListener("beforeunload", unload);
    document.addEventListener("click", navigate, true);
    return () => { window.removeEventListener("beforeunload", unload); document.removeEventListener("click", navigate, true); };
  }, [dirty]);

  function updateField(key: keyof MetadataForm, value: string) {
    setForm((current) => ({ ...current, [key]: value }));
    setDirty(true);
  }

  async function run(label: string, operation: () => Promise<void>) {
    setBusy(label); setError(""); setNotice("");
    try { await operation(); return true; }
    catch (caught) {
      const message = caught instanceof Error ? caught.message : `${label}失败`;
      setError(message); setToast({ message, tone: "bad" }); return false;
    } finally { setBusy(null); setJobProgress(""); }
  }

  async function save() {
    return run("保存草稿", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}`, {
        method: "PATCH", credentials: "same-origin",
        headers: { "Content-Type": "application/json", "If-Match": `"v${version}"` },
        body: JSON.stringify({
          metadata: { ...form, players: form.players === "" ? null : Number(form.players), releaseYear: form.releaseYear === "" ? null : Number(form.releaseYear) },
          selectedCandidateId: candidateId,
          selectedAssets: { coverCandidateAssetId: coverId, backgroundCandidateAssetId: backgroundId, screenshotCandidateAssetIds: screenshotIds },
          defaultDosEntry,
        }),
      });
      if (!response.ok) throw new Error(await responseError(response, "草稿保存失败：字段、候选、运行检查或版本已经变化"));
      const result = await response.json() as { version: number };
      setVersion(result.version); setDirty(false); setNotice("草稿及来源选择已保存");
    });
  }

  async function saveAndNavigate() {
    const href = pendingNavigation;
    if (!href || !await save()) return;
    setPendingNavigation(null); router.push(href);
  }

  function discardAndNavigate() {
    if (!pendingNavigation) return;
    const href = pendingNavigation;
    setDirty(false); setPendingNavigation(null); router.push(href);
  }

  async function rescrape(metadataProvider: "HASHEOUS" | "NONE") {
    const label = metadataProvider === "HASHEOUS" ? "重新查询 Hasheous" : "停用元信息源";
    await run(label, async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/scrape-candidates`, {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/json", "If-Match": `"v${version}"`, "Idempotency-Key": newUuid() },
        body: JSON.stringify({ metadataProvider }),
      });
      if (!response.ok) throw new Error(await responseError(response, "重新查询失败：条目或版本已经变化"));
      const result = await response.json() as { version: number; state: string; scrapeRunId: string; jobId: string };
      setVersion(result.version); setJobProgress(`${result.state} · Job ${result.jobId.slice(0, 8)}…`);
      await waitForJob(result.jobId, setJobProgress);
      const updatedResponse = await fetch(`/api/v1/admin/reviews/${review.itemId}`, { cache: "no-store" });
      if (!updatedResponse.ok) throw new Error(await responseError(updatedResponse, "查询完成，但无法读取新游戏信息"));
      const updated = await updatedResponse.json() as ReviewWorkspace;
      setCandidates(updated.candidates);
      if (metadataProvider === "NONE") {
        setNotice("已记录不使用在线游戏信息"); return;
      }
      const completed = (updated.scrapeRuns ?? []).find((entry) => entry.scrapeRunId === result.scrapeRunId);
      const latest = updated.candidates.find((entry) => entry.scrapeRunId === result.scrapeRunId);
      if (!completed) throw new Error("查询完成，但服务器没有返回对应结果");
      if (!latest) {
        const message = `查询完成，但${scrapeResult(completed)}`;
        setNotice(message); setToast({ message, tone: "warn" }); return;
      }
      setComparison({ candidate: latest, current: { ...form }, next: candidateForm(latest, form), currentCoverId: coverId, nextCoverId: readyCover(latest)?.candidateAssetId ?? null });
    });
  }

  function applyComparison() {
    if (!comparison) return;
    setForm(comparison.next);
    setCandidateId(comparison.candidate.candidateId);
    setCoverId(comparison.nextCoverId);
    setBackgroundId(null);
    setScreenshotIds([]);
    setDirty(true);
    setComparison(null);
    setNotice("新查询结果已应用到草稿；保存草稿后才会持久化。旧候选素材选择已清除。");
  }

  function clearQueueCache() {
    const query = new URL(returnTo, window.location.origin).searchParams.toString();
    sessionStorage.removeItem(`retrom:review-queue:${query}`);
  }

  async function approve() {
    await run("发布", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/approve`, {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/json", "If-Match": `"v${version}"`, "Idempotency-Key": newUuid() },
        body: JSON.stringify({ reason: approvalReason.trim() || null }),
      });
      if (!response.ok) throw new Error("发布失败：请先保存草稿，并确认当前兼容性检查已经通过");
      clearQueueCache(); queueFlashToast({ message: "游戏已成功发布，待审核队列已更新。", tone: "good" });
      router.replace(nextItemId ? `/admin/reviews/${nextItemId}?returnTo=${encodeURIComponent(returnTo)}` : returnTo);
    });
  }

  async function discard() {
    await run("丢弃", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/discard`, {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/json", "If-Match": `"v${version}"`, "Idempotency-Key": newUuid() },
        body: JSON.stringify({ reason: discardReason }),
      });
      if (!response.ok) throw new Error("丢弃失败：请填写原因并刷新当前版本");
      clearQueueCache(); queueFlashToast({ message: "条目已丢弃，待审核队列已更新。", tone: "good" });
      router.replace(nextItemId ? `/admin/reviews/${nextItemId}?returnTo=${encodeURIComponent(returnTo)}` : returnTo);
    });
  }

  const selectedCover = assetById(candidates, coverId);
  const currentCompareCover = comparison ? assetById(candidates, comparison.currentCoverId) : null;
  const nextCompareCover = comparison ? assetById(candidates, comparison.nextCoverId) : null;

  return <div className="stack review-editor" data-dirty={dirty ? "true" : "false"}>
    {notice ? <FeedbackBanner tone="good">{notice}</FeedbackBanner> : null}
    {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
    <div className="review-editor-grid">
      <div className="form-grid">
        <label className="field full">标题<input value={form.title} onChange={(event) => updateField("title", event.target.value)} maxLength={200} /></label>
        <label className="field full">简介<textarea value={form.description} onChange={(event) => updateField("description", event.target.value)} maxLength={10000} /></label>
        <label className="field">开发商<input value={form.developer} onChange={(event) => updateField("developer", event.target.value)} maxLength={200} /></label>
        <label className="field">发行商<input value={form.publisher} onChange={(event) => updateField("publisher", event.target.value)} maxLength={200} /></label>
        <label className="field">类型<input value={form.genre} onChange={(event) => updateField("genre", event.target.value)} maxLength={200} /></label>
        <label className="field">玩家数<input type="number" min={1} max={64} value={form.players} onChange={(event) => updateField("players", event.target.value)} /></label>
        <label className="field">发行年份<input type="number" min={1950} value={form.releaseYear} onChange={(event) => updateField("releaseYear", event.target.value)} /></label>
        {review.dosEntries.length ? <label className="field">DOS 默认程序<select value={defaultDosEntry ?? ""} onChange={(event) => { setDefaultDosEntry(event.target.value || null); setDirty(true); }}><option value="">打开 DOSBox 程序菜单</option>{review.dosEntries.map((entry) => <option key={entry.path} value={entry.path} disabled={!entry.enabled}>{entry.originalPath}{entry.directLaunchSafe ? "" : " · 仅程序菜单"}</option>)}</select></label> : null}
      </div>
      <aside className="review-cover-panel"><span className="field-label">当前封面</span><AssetPreview asset={selectedCover} label="当前选择的游戏封面" />{coverId ? <button type="button" className="button secondary compact" onClick={() => { setCoverId(null); setDirty(true); }}>移除封面</button> : null}<small>{candidateId ? "封面与文字来源已关联到当前候选" : "当前使用人工填写的信息"}</small></aside>
    </div>

    <section className="review-query-bar" aria-label="在线游戏信息">
      <div><strong>在线游戏信息</strong><p>重新查询后先对比当前草稿与最新结果，不会在页面下方累积候选卡片。</p></div>
      <div className="header-actions"><button type="button" className="button secondary" disabled={busy !== null} aria-busy={busy === "重新查询 Hasheous"} onClick={() => void rescrape("HASHEOUS")}>{busy === "重新查询 Hasheous" ? <><i className="button-spinner" aria-hidden="true" />查询中…</> : "重新查询游戏信息"}</button><button type="button" className="button secondary" disabled={busy !== null} onClick={() => void rescrape("NONE")}>{busy === "停用元信息源" ? "正在记录…" : "不使用在线游戏信息"}</button></div>
    </section>
    {jobProgress ? <p className="scrape-live" role="status"><i className="button-spinner" aria-hidden="true" />正在查询游戏信息：{jobProgress}</p> : null}

    <div className="form-grid review-decision-fields">
      <label className="field full">发布说明（可空）<input value={approvalReason} onChange={(event) => setApprovalReason(event.target.value)} maxLength={500} /></label>
      <label className="field full">丢弃原因<input value={discardReason} onChange={(event) => setDiscardReason(event.target.value)} maxLength={500} placeholder="丢弃时必填" /></label>
      <div className="field full"><div className="header-actions"><button type="button" className="button secondary" disabled={busy !== null} onClick={() => void save()}>{busy === "保存草稿" ? "正在保存…" : "保存草稿"}</button><button type="button" className="button secondary" disabled={busy !== null || !discardReason.trim()} onClick={() => void discard()}>{busy === "丢弃" ? "正在丢弃…" : "丢弃条目"}</button><button type="button" className="button" aria-busy={busy === "发布"} disabled={dirty || busy !== null || review.validation?.status !== "READY"} onClick={() => void approve()}>{busy === "发布" ? <><i className="button-spinner" aria-hidden="true" />正在发布…</> : "通过并发布"}</button></div></div>
    </div>

    <ConfirmDialog open={comparison !== null} wide title="对比最新查询结果" description="左侧是当前草稿，右侧是最新查询结果。红色表示内容不同，绿色表示一致；右侧内容可继续修改。" confirmLabel="应用到草稿" busy={busy !== null} onCancel={() => setComparison(null)} onConfirm={applyComparison}>
      {comparison ? <div className="metadata-compare">
        <div className="metadata-compare-head"><strong>当前信息（只读）</strong><strong>最新信息（可编辑）</strong></div>
        {compareFields.map((field) => {
          const same = comparison.current[field.key] === comparison.next[field.key];
          return <div className="metadata-compare-row" key={field.key}><div className="compare-readonly"><span>{field.label}</span><p>{comparison.current[field.key] || "未填写"}</p></div><label className={`compare-field ${same ? "is-same" : "is-changed"}`}><span>{field.label}</span>{field.multiline ? <textarea value={comparison.next[field.key]} onChange={(event) => setComparison((current) => current ? { ...current, next: { ...current.next, [field.key]: event.target.value } } : null)} /> : <input type={field.type ?? "text"} value={comparison.next[field.key]} onChange={(event) => setComparison((current) => current ? { ...current, next: { ...current.next, [field.key]: event.target.value } } : null)} />}</label></div>;
        })}
        <div className="metadata-compare-row compare-cover-row"><div className="compare-readonly"><span>当前封面</span><AssetPreview asset={currentCompareCover} label="当前游戏封面" /></div><div className={`compare-field ${comparison.currentCoverId === comparison.nextCoverId ? "is-same" : "is-changed"}`}><span>最新封面</span><AssetPreview asset={nextCompareCover} label="最新查询封面" />{comparison.nextCoverId ? <button type="button" className="button secondary compact" onClick={() => setComparison((current) => current ? { ...current, nextCoverId: null } : null)}>不使用新封面</button> : null}</div></div>
      </div> : null}
    </ConfirmDialog>
    <ConfirmDialog open={pendingNavigation !== null} title="草稿还没有保存" description="离开前请选择如何处理本页修改。" confirmLabel="保存并离开" secondaryLabel="放弃修改" cancelLabel="留在页面" busy={busy !== null} onCancel={() => setPendingNavigation(null)} onSecondary={discardAndNavigate} onConfirm={() => void saveAndNavigate()}><ul><li>保存后，当前字段和候选来源会写入草稿</li><li>放弃修改会恢复到最近一次已保存版本</li></ul></ConfirmDialog>
    <Toast toast={toast} onDismiss={() => setToast(null)} />
  </div>;
}
