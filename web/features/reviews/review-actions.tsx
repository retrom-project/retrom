"use client";

import Image from "next/image";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { queueFlashToast, Toast, type ToastMessage } from "@/components/flash-toast";
import { formatTime } from "@/lib/backend";
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

function textValue(value: unknown): string | null {
  return typeof value === "string" ? value : null;
}

function numberValue(value: unknown): string | null {
  return typeof value === "number" && Number.isInteger(value) ? String(value) : null;
}

function runResult(run: ReviewScrapeRun) {
  if (run.provider === "NONE") return "已明确跳过元信息查询";
  if (run.state === "RUNNING" || run.jobState === "QUEUED" || run.jobState === "RUNNING") return "查询进行中";
  if (run.state === "FAILED" || run.jobState === "FAILED") return `查询失败${run.errorCode ? `：${run.errorCode}` : ""}`;
  if (run.candidateCount > 0) return `命中 ${run.outcomes.hit} 次，生成 ${run.candidateCount} 个候选`;
  if (run.outcomes.invalidResponse > 0) return `${run.outcomes.invalidResponse} 份上游响应无法解析`;
  if (run.outcomes.rateLimited + run.outcomes.timeout + run.outcomes.networkError > 0) return "上游限流、超时或网络异常";
  if (run.outcomes.miss > 0) return `${run.outcomes.miss} 次精确 hash 查询均未命中`;
  return run.evidenceCount === 0 ? "没有可查询的内容 hash" : "查询完成，未生成候选";
}

function ScrapeRunRow({ run }: { run: ReviewScrapeRun }) {
  return <article className="scrape-run"><div><strong>{run.provider} · {run.state}</strong><span>{runResult(run)}</span></div><small title={run.jobId}>{formatTime(run.createdAtMs)} · Job {run.jobId.slice(0, 12)}… · {run.attemptCount} attempts</small></article>;
}

export function ReviewActions({ review, returnTo = "/admin/reviews", nextItemId = null }: { review: ReviewWorkspace; returnTo?: string; nextItemId?: string | null }) {
  const router = useRouter();
  const [version, setVersion] = useState(review.version);
  const [title, setTitle] = useState(review.metadata.title);
  const [description, setDescription] = useState(review.metadata.description);
  const [developer, setDeveloper] = useState(review.metadata.developer);
  const [publisher, setPublisher] = useState(review.metadata.publisher);
  const [genre, setGenre] = useState(review.metadata.genre);
  const [players, setPlayers] = useState(review.metadata.players === null ? "" : String(review.metadata.players));
  const [releaseYear, setReleaseYear] = useState(review.metadata.releaseYear === null ? "" : String(review.metadata.releaseYear));
  const [candidateId, setCandidateId] = useState<string | null>(review.selectedCandidateId);
  const [coverId, setCoverId] = useState<string | null>(review.selectedAssets.coverCandidateAssetId);
  const [backgroundId, setBackgroundId] = useState<string | null>(review.selectedAssets.backgroundCandidateAssetId);
  const [screenshotIds, setScreenshotIds] = useState(review.selectedAssets.screenshotCandidateAssetIds);
  const [defaultDosEntry, setDefaultDosEntry] = useState<string | null>(review.defaultDosEntry);
  const [approvalReason, setApprovalReason] = useState("");
  const [discardReason, setDiscardReason] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [dirty, setDirty] = useState(false);
  const [candidates, setCandidates] = useState(review.candidates);
  const [scrapeRuns, setScrapeRuns] = useState(review.scrapeRuns ?? []);
  const [jobProgress, setJobProgress] = useState("");
  const [toast, setToast] = useState<ToastMessage | null>(null);

  useEffect(() => {
    if (!dirty) return;
    const unload = (event: BeforeUnloadEvent) => event.preventDefault();
    const navigate = (event: MouseEvent) => {
      const link = (event.target as Element | null)?.closest("a[href]");
      if (!link || window.confirm("草稿尚未保存。要放弃本次修改并离开吗？")) return;
      event.preventDefault();
      event.stopPropagation();
    };
    window.addEventListener("beforeunload", unload);
    document.addEventListener("click", navigate, true);
    return () => { window.removeEventListener("beforeunload", unload); document.removeEventListener("click", navigate, true); };
  }, [dirty]);

  function adoptCandidate(candidate: ReviewCandidate) {
    setDirty(true);
    setCandidateId(candidate.candidateId);
    setTitle(textValue(candidate.metadata.title) ?? title);
    setDescription(textValue(candidate.metadata.description) ?? description);
    setDeveloper(textValue(candidate.metadata.developer) ?? developer);
    setPublisher(textValue(candidate.metadata.publisher) ?? publisher);
    setGenre(textValue(candidate.metadata.genre) ?? genre);
    setPlayers(numberValue(candidate.metadata.players) ?? players);
    setReleaseYear(numberValue(candidate.metadata.releaseYear) ?? releaseYear);
    setNotice(`已将候选 ${candidate.providerGameId} 合并到草稿；保存后才会持久化。`);
  }

  function selectAsset(asset: ReviewAsset) {
    if (asset.status !== "READY") return;
    setDirty(true);
    if (asset.kind === "COVER") setCoverId((current) => current === asset.candidateAssetId ? null : asset.candidateAssetId);
    if (asset.kind === "BACKGROUND") setBackgroundId((current) => current === asset.candidateAssetId ? null : asset.candidateAssetId);
    if (asset.kind === "SCREENSHOT") setScreenshotIds((current) => current.includes(asset.candidateAssetId) ? current.filter((id) => id !== asset.candidateAssetId) : [...current, asset.candidateAssetId].slice(0, 32));
  }

  async function run(label: string, operation: () => Promise<void>) {
    setBusy(label); setError(""); setNotice("");
    try { await operation(); }
    catch (caught) {
      const message = caught instanceof Error ? caught.message : `${label}失败`;
      setError(message); setToast({ message, tone: "bad" });
    }
    finally { setBusy(null); setJobProgress(""); }
  }

  async function save() {
    await run("保存草稿", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}`, {
        method: "PATCH", credentials: "same-origin",
        headers: { "Content-Type": "application/json", "If-Match": `"v${version}"` },
        body: JSON.stringify({
          metadata: { title, description, developer, publisher, genre, players: players === "" ? null : Number(players), releaseYear: releaseYear === "" ? null : Number(releaseYear) },
          selectedCandidateId: candidateId,
          selectedAssets: { coverCandidateAssetId: coverId, backgroundCandidateAssetId: backgroundId, screenshotCandidateAssetIds: screenshotIds },
          defaultDosEntry,
        }),
      });
      if (!response.ok) throw new Error("草稿保存失败：字段、候选、Validation 或版本已经变化");
      const result = await response.json() as { version: number };
      setVersion(result.version); setDirty(false); setNotice("草稿及来源选择已保存");
    });
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
      setVersion(result.version);
      setJobProgress(`${result.state} · Job ${result.jobId.slice(0, 8)}…`);
      await waitForJob(result.jobId, setJobProgress);

      const updatedResponse = await fetch(`/api/v1/admin/reviews/${review.itemId}`, { cache: "no-store" });
      if (!updatedResponse.ok) throw new Error(await responseError(updatedResponse, "查询完成，但无法刷新审核元信息"));
      const updated = await updatedResponse.json() as ReviewWorkspace;
      const updatedRuns = updated.scrapeRuns ?? [];
      const completed = updatedRuns.find((entry) => entry.scrapeRunId === result.scrapeRunId);
      setCandidates(updated.candidates);
      setScrapeRuns(updatedRuns);
      if (metadataProvider === "NONE") {
        const message = "已记录不使用元信息源";
        setNotice(message); setToast({ message, tone: "good" });
      } else if (!completed) {
        throw new Error("Hasheous 查询完成，但服务器没有返回对应批次结果");
      } else if (completed.candidateCount > 0) {
        const message = `Hasheous 查询完成，已刷新 ${completed.candidateCount} 个候选；明确采用后才会更新草稿。`;
        setNotice(message); setToast({ message, tone: "good" });
      } else if (completed.outcomes.invalidResponse + completed.outcomes.rateLimited + completed.outcomes.timeout + completed.outcomes.networkError > 0) {
        const message = `Hasheous 查询已结束，但未得到可用候选：${runResult(completed)}`;
        setNotice(message); setToast({ message, tone: "warn" });
      } else {
        const message = `Hasheous 查询完成：${runResult(completed)}`;
        setNotice(message); setToast({ message, tone: "warn" });
      }
      router.refresh();
    });
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
      clearQueueCache();
      queueFlashToast({ message: "游戏已成功发布，待审核队列已更新。", tone: "good" });
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
      clearQueueCache();
      queueFlashToast({ message: "条目已丢弃，待审核队列已更新。", tone: "good" });
      router.replace(nextItemId ? `/admin/reviews/${nextItemId}?returnTo=${encodeURIComponent(returnTo)}` : returnTo);
    });
  }

  return <div className="stack" data-dirty={dirty ? "true" : "false"}>
    <div className="form-grid">
      <label className="field full">标题<input value={title} onChange={(event) => { setTitle(event.target.value); setDirty(true); }} maxLength={200} /></label>
      <label className="field full">简介<textarea value={description} onChange={(event) => { setDescription(event.target.value); setDirty(true); }} maxLength={10000} /></label>
      <label className="field">开发商<input value={developer} onChange={(event) => { setDeveloper(event.target.value); setDirty(true); }} maxLength={200} /></label>
      <label className="field">发行商<input value={publisher} onChange={(event) => { setPublisher(event.target.value); setDirty(true); }} maxLength={200} /></label>
      <label className="field">类型<input value={genre} onChange={(event) => { setGenre(event.target.value); setDirty(true); }} maxLength={200} /></label>
      <label className="field">玩家数<input type="number" min={1} max={64} value={players} onChange={(event) => { setPlayers(event.target.value); setDirty(true); }} /></label>
      <label className="field">发行年份<input type="number" min={1950} value={releaseYear} onChange={(event) => { setReleaseYear(event.target.value); setDirty(true); }} /></label>
      {review.dosEntries.length ? <label className="field">DOS 默认程序<select value={defaultDosEntry ?? ""} onChange={(event) => { setDefaultDosEntry(event.target.value || null); setDirty(true); }}><option value="">打开 DOSBox 程序菜单</option>{review.dosEntries.map((entry) => <option key={entry.path} value={entry.path} disabled={!entry.enabled}>{entry.originalPath}{entry.directLaunchSafe ? "" : " · 仅程序菜单"}</option>)}</select></label> : null}
    </div>

    <section className="stack" aria-label="游戏信息候选">
      <div className="header-actions"><button type="button" className="button secondary" disabled={busy !== null} aria-busy={busy === "重新查询 Hasheous"} onClick={() => void rescrape("HASHEOUS")}>{busy === "重新查询 Hasheous" ? <><i className="button-spinner" aria-hidden="true" />查询中…</> : "重新查询游戏信息"}</button><button type="button" className="button secondary" disabled={busy !== null} onClick={() => void rescrape("NONE")}>{busy === "停用元信息源" ? "正在记录…" : "不使用在线游戏信息"}</button>{candidateId ? <button type="button" className="button secondary" disabled={busy !== null} onClick={() => { setCandidateId(null); setDirty(true); }}>清除文本来源</button> : null}</div>
      {jobProgress ? <p className="scrape-live" role="status"><i className="button-spinner" aria-hidden="true" />正在查询游戏信息：{jobProgress}</p> : null}
      {scrapeRuns.length ? <div className="stack scrape-batches"><div><strong>最近查询批次</strong><ScrapeRunRow run={scrapeRuns[0]} /></div>{scrapeRuns.length > 1 ? <details className="scrape-history"><summary>查看更早 {scrapeRuns.length - 1} 次查询</summary><div className="stack">{scrapeRuns.slice(1).map((entry) => <ScrapeRunRow run={entry} key={entry.scrapeRunId} />)}</div></details> : null}</div> : <p>尚无元信息查询批次。</p>}
      {candidates.length === 0 ? <p>当前没有可用的游戏信息候选。你仍可人工填写；兼容性检查通过后即可发布。</p> : candidates.map((candidate) => <article className="candidate" key={candidate.candidateId}>
        <div className="panel-head"><div><strong>{textValue(candidate.metadata.title) ?? candidate.providerGameId}</strong><p>在线游戏信息候选</p><details className="technical-details"><summary>技术详情</summary><code>{candidate.providerGameId} · {candidate.scrapeRunId}</code></details></div><button type="button" className="button secondary" disabled={busy !== null} onClick={() => adoptCandidate(candidate)}>{candidateId === candidate.candidateId ? "已选文本来源" : "采用候选文本"}</button></div>
        <div className="candidate-metadata">
          <div><span>发行商</span><strong>{textValue(candidate.metadata.publisher) || "未提供"}</strong></div>
          <div><span>年份</span><strong>{numberValue(candidate.metadata.releaseYear) || "未提供"}</strong></div>
          <div><span>开发商</span><strong>{textValue(candidate.metadata.developer) || "未提供"}</strong></div>
          <div><span>类型</span><strong>{textValue(candidate.metadata.genre) || "未提供"}</strong></div>
          {textValue(candidate.metadata.description) ? <p>{textValue(candidate.metadata.description)}</p> : null}
        </div>
        {candidate.assets.length ? <div className="asset-grid">{candidate.assets.map((asset) => {
          const selected = coverId === asset.candidateAssetId || backgroundId === asset.candidateAssetId || screenshotIds.includes(asset.candidateAssetId);
          const kind = asset.kind === "COVER" ? "封面" : asset.kind === "BACKGROUND" ? "背景" : "游戏截图";
          return <figure key={asset.candidateAssetId}>{asset.status === "READY" && asset.widthPx && asset.heightPx ? <Image src={`/api/v1/admin/review-assets/${asset.candidateAssetId}`} alt={`${kind}候选`} width={asset.widthPx} height={asset.heightPx} unoptimized /> : <div className="asset-placeholder">图片暂不可用</div>}<figcaption>{kind} {asset.ordinal + 1}</figcaption><button type="button" className="button secondary" disabled={busy !== null || asset.status !== "READY"} onClick={() => selectAsset(asset)}>{selected ? "取消选择" : "选择媒体"}</button></figure>;
        })}</div> : null}
      </article>)}
    </section>

    <div className="form-grid">
      <label className="field full">发布说明（可空）<input value={approvalReason} onChange={(event) => setApprovalReason(event.target.value)} maxLength={500} /></label>
      <label className="field full">丢弃原因<input value={discardReason} onChange={(event) => setDiscardReason(event.target.value)} maxLength={500} placeholder="丢弃时必填" /></label>
      <div className="field full"><div className="header-actions"><button type="button" className="button secondary" disabled={busy !== null} onClick={() => void save()}>{busy === "保存草稿" ? "正在保存…" : "保存草稿"}</button><button type="button" className="button secondary" disabled={busy !== null || !discardReason.trim()} onClick={() => void discard()}>{busy === "丢弃" ? "正在丢弃…" : "丢弃条目"}</button><button type="button" className="button" aria-busy={busy === "发布"} disabled={dirty || busy !== null || review.validation?.status !== "READY"} onClick={() => void approve()}>{busy === "发布" ? <><i className="button-spinner" aria-hidden="true" />正在发布…</> : "通过并发布"}</button></div>{notice ? <p role="status" className="status good">{notice}</p> : null}{error ? <p role="alert" className="status bad">{error}</p> : null}</div>
    </div>
    <Toast toast={toast} onDismiss={() => setToast(null)} />
  </div>;
}
