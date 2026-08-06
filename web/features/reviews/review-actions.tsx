"use client";

import Image from "next/image";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

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

export type ReviewWorkspace = {
  itemId: string;
  version: number;
  metadata: { title: string; description: string; developer: string; publisher: string; genre: string; players: number | null; releaseYear: number | null };
  validation: { id: string; status: string; compatibilityCode: string } | null;
  candidates: ReviewCandidate[];
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
    catch (caught) { setError(caught instanceof Error ? caught.message : `${label}失败`); }
    finally { setBusy(null); }
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
    await run("重新刮削", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/scrape-candidates`, {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/json", "If-Match": `"v${version}"`, "Idempotency-Key": crypto.randomUUID() },
        body: JSON.stringify({ metadataProvider }),
      });
      if (!response.ok) throw new Error("重新刮削失败：条目或版本已经变化");
      const result = await response.json() as { version: number; state: string };
      setVersion(result.version); setNotice(metadataProvider === "NONE" ? "已记录不使用元信息源" : `Hasheous 任务已进入 ${result.state}`); router.refresh();
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
        headers: { "Content-Type": "application/json", "If-Match": `"v${version}"`, "Idempotency-Key": crypto.randomUUID() },
        body: JSON.stringify({ reason: approvalReason.trim() || null }),
      });
      if (!response.ok) throw new Error("发布失败：必须先保存草稿并选择当前 READY Validation");
      clearQueueCache();
      router.push(nextItemId ? `/admin/reviews/${nextItemId}?returnTo=${encodeURIComponent(returnTo)}` : returnTo); router.refresh();
    });
  }

  async function discard() {
    await run("丢弃", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${review.itemId}/discard`, {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/json", "If-Match": `"v${version}"`, "Idempotency-Key": crypto.randomUUID() },
        body: JSON.stringify({ reason: discardReason }),
      });
      if (!response.ok) throw new Error("丢弃失败：请填写原因并刷新当前版本");
      clearQueueCache();
      router.push(nextItemId ? `/admin/reviews/${nextItemId}?returnTo=${encodeURIComponent(returnTo)}` : returnTo); router.refresh();
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

    <section className="stack" aria-label="Hasheous 元信息候选">
      <div className="header-actions"><button type="button" className="button secondary" disabled={busy !== null} onClick={() => void rescrape("HASHEOUS")}>重新查询 Hasheous</button><button type="button" className="button secondary" disabled={busy !== null} onClick={() => void rescrape("NONE")}>不使用元信息源</button>{candidateId ? <button type="button" className="button secondary" disabled={busy !== null} onClick={() => { setCandidateId(null); setDirty(true); }}>清除文本来源</button> : null}</div>
      {review.candidates.length === 0 ? <p>当前没有可用元信息候选；仍可人工填写并发布 READY 条目。</p> : review.candidates.map((candidate) => <article className="candidate" key={candidate.candidateId}>
        <div className="panel-head"><div><strong>{textValue(candidate.metadata.title) ?? candidate.providerGameId}</strong><p>Hasheous {candidate.providerGameId} · Run {candidate.scrapeRunId.slice(0, 8)}</p></div><button type="button" className="button secondary" disabled={busy !== null} onClick={() => adoptCandidate(candidate)}>{candidateId === candidate.candidateId ? "已选文本来源" : "采用候选文本"}</button></div>
        <pre>{JSON.stringify(candidate.metadata, null, 2)}</pre>
        {candidate.assets.length ? <div className="asset-grid">{candidate.assets.map((asset) => {
          const selected = coverId === asset.candidateAssetId || backgroundId === asset.candidateAssetId || screenshotIds.includes(asset.candidateAssetId);
          return <figure key={asset.candidateAssetId}>{asset.status === "READY" && asset.widthPx && asset.heightPx ? <Image src={`/api/v1/admin/review-assets/${asset.candidateAssetId}`} alt={`${asset.kind} 候选`} width={asset.widthPx} height={asset.heightPx} unoptimized /> : <div className="asset-placeholder">{asset.errorCode ?? asset.status}</div>}<figcaption>{asset.kind} #{asset.ordinal + 1}</figcaption><button type="button" className="button secondary" disabled={busy !== null || asset.status !== "READY"} onClick={() => selectAsset(asset)}>{selected ? "取消选择" : "选择媒体"}</button></figure>;
        })}</div> : null}
      </article>)}
    </section>

    <div className="form-grid">
      <label className="field full">发布说明（可空）<input value={approvalReason} onChange={(event) => setApprovalReason(event.target.value)} maxLength={500} /></label>
      <label className="field full">丢弃原因<input value={discardReason} onChange={(event) => setDiscardReason(event.target.value)} maxLength={500} placeholder="丢弃时必填" /></label>
      <div className="field full"><div className="header-actions"><button type="button" className="button secondary" disabled={busy !== null} onClick={() => void save()}>保存草稿</button><button type="button" className="button secondary" disabled={busy !== null || !discardReason.trim()} onClick={() => void discard()}>丢弃条目</button><button type="button" className="button" disabled={dirty || busy !== null || review.validation?.status !== "READY"} onClick={() => void approve()}>{busy ?? "通过并发布"}</button></div>{notice ? <p role="status" className="status good">{notice}</p> : null}{error ? <p role="alert" className="status bad">{error}</p> : null}</div>
    </div>
  </div>;
}
