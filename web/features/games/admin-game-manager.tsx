"use client";

import { type FormEvent, useRef, useState } from "react";
import Image from "next/image";
import { useRouter } from "next/navigation";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { StatusBadge } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { formatTime } from "@/lib/backend";
import { newUuid } from "@/lib/crypto";
import { responseError, uploadFiles, uploadOne, waitForJob } from "@/lib/upload";
import { formatAdminGameTime, runtimePresentation } from "./admin-game-library";

type Revision = { id: string; sourceKind: string; sourceRefId: string | null; current: boolean; createdAtMs: number };
type ContentRevision = Revision & { files: Array<{ role: string; logicalName: string; sortOrder: number }> };
type VariantRevision = { id: string; contentRevisionId: string; coreArtifactId: string; datVersionId: string | null; status: string; compatibilityCode: string; current: boolean; createdAtMs: number };
type Variant = { id: string; coreId: string; coreName: string; currentRevisionId: string | null; version: number; revisions: VariantRevision[] };
type Asset = { assetId: string; kind: string; ordinal: number; widthPx: number; heightPx: number; mediaType: string; url: string };

export type AdminGame = {
  gameId: string; status: string; title: string; description: string; developer: string; publisher: string; genre: string;
  players: number | null; releaseYear: number | null; platformId: string; platformInstance: { id: string; name: string };
  currentContentRevisionId: string; currentMetadataRevisionId: string; version: number; createdAtMs: number; updatedAtMs: number; generatedAtMs: number;
  deleteImpact: { saveStateCount: number; reviewEventCount: number; activeLaunchCount: number };
  metadataRevisions: Revision[]; assets: Asset[]; contentRevisions: ContentRevision[]; variants: Variant[];
};

export type PlatformInstanceOption = { id: string; platformId: string; platformName: string; name: string; defaultCoreId: string; defaultCoreName: string; enabled: boolean };
type ScrapeCandidateAsset = { candidateAssetId: string; kind: string; status: string; widthPx: number | null; heightPx: number | null; mediaType: string | null };
export type ScrapeCandidate = { candidateId: string; providerGameId: string; metadata: Record<string, unknown>; hitCount: number; assets: ScrapeCandidateAsset[] };
type MoveImpact = { impactDigest: string; impact: { targetCoreId: string; variantStatus: string; blockerCodes: string[] } };
type PendingMove = { targetPlatformInstanceId: string; targetName: string; result: MoveImpact };

const metadataFields = ["title", "description", "developer", "publisher", "genre", "players", "releaseYear"];

export function AdminGameManager({ game, platformInstances, candidates }: { game: AdminGame; platformInstances: PlatformInstanceOption[]; candidates: ScrapeCandidate[] }) {
  const router = useRouter();
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [pendingMove, setPendingMove] = useState<PendingMove | null>(null);
  const [moveTarget, setMoveTarget] = useState("");
  const [scrapeCandidates, setScrapeCandidates] = useState(candidates);
  const [comparison, setComparison] = useState<ScrapeCandidate | null>(null);
  const versionRef = useRef(game.version);

  async function action(name: string, callback: () => Promise<string>) {
    setBusy(name); setNotice(""); setError("");
    try { setNotice(await callback()); router.refresh(); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "操作失败"); }
    finally { setBusy(null); }
  }

  function versionedHeaders(json = true) {
    return writeHeaders({ ...(json ? { "Content-Type": "application/json" } : {}), "If-Match": `"v${versionRef.current}"` });
  }

  async function saveMetadata(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    await action("metadata", async () => {
      const body: Record<string, string | number> = { title: String(data.get("title") ?? ""), description: String(data.get("description") ?? ""), developer: String(data.get("developer") ?? ""), publisher: String(data.get("publisher") ?? ""), genre: String(data.get("genre") ?? "") };
      for (const name of ["players", "releaseYear"]) { const value = String(data.get(name) ?? ""); if (value) body[name] = Number(value); }
      const response = await fetch(`/api/v1/admin/games/${game.gameId}`, { method: "PATCH", credentials: "same-origin", headers: await versionedHeaders(), body: JSON.stringify(body) });
      if (!response.ok) throw new Error(await responseError(response, "发布信息保存失败"));
      const result = await response.json() as { metadataRevisionId: string };
      return result.metadataRevisionId ? "发布信息已保存为新版本。" : "发布信息已保存。";
    });
  }

  async function replaceAsset(file: File, kind: string, ordinal: number) {
    await action("asset", async () => {
      const uploaded = await uploadOne(file, setNotice);
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/assets`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ uploadFileId: uploaded.uploadFileId, kind, ordinal }) });
      if (!response.ok) throw new Error(await responseError(response, "媒体替换失败"));
      const result = await response.json() as { assetId: string; metadataRevisionId: string };
      return result.assetId && result.metadataRevisionId ? "图片已更新，旧版本仍会保留。" : "图片已更新。";
    });
  }

  async function replaceContent(files: File[]) {
    await action("content", async () => {
      const uploaded = await uploadFiles(files, setNotice);
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/content-revisions`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ uploadId: uploaded.uploadId }) });
      if (!response.ok) throw new Error(await responseError(response, "内容替换任务创建失败"));
      const result = await response.json() as { jobId: string };
      setNotice("正在安全校验新游戏文件…");
      await waitForJob(result.jobId, () => setNotice("正在检查新文件是否可以运行…"));
      return "游戏文件已更新，旧内容和存档仍会保留。";
    });
  }

  async function rescrape() {
    await action("scrape", async () => {
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/scrape-candidates`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ metadataProvider: "HASHEOUS" }) });
      if (!response.ok) throw new Error(await responseError(response, "重新刮削任务创建失败"));
      const result = await response.json() as { scrapeRunId: string; jobId: string; version?: number };
      if (result.version) versionRef.current = result.version;
      setNotice("正在查找新的游戏信息候选…");
      await waitForJob(result.jobId, () => setNotice("正在整理候选信息…"));
      const latestResponse = await fetch(`/api/v1/admin/games/${game.gameId}/scrape-candidates`, { cache: "no-store" });
      if (!latestResponse.ok) throw new Error(await responseError(latestResponse, "候选查询完成，但无法读取结果"));
      const latest = await latestResponse.json() as { items: ScrapeCandidate[] };
      setScrapeCandidates(latest.items);
      if (latest.items[0]) setComparison(latest.items[0]);
      return latest.items.length ? "候选已准备好，请在对比窗口中确认。" : "查询完成，但没有找到可用候选。";
    });
  }

  async function applyCandidate(candidate: ScrapeCandidate) {
    await action("candidate", async () => {
      const fields = metadataFields.filter((field) => {
        const value = candidate.metadata[field];
        return typeof value === "number" || typeof value === "string" && value.trim() !== "";
      });
      const candidateCover = candidate.assets.find((asset) => asset.kind === "COVER" && asset.status === "READY");
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/scrape-candidates/${candidate.candidateId}/apply`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ fields, selectedAssets: { coverCandidateAssetId: candidateCover?.candidateAssetId ?? null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: [] } }) });
      if (!response.ok) throw new Error(await responseError(response, "候选采用失败"));
      const result = await response.json() as { metadataRevisionId: string; version?: number };
      if (result.version) versionRef.current = result.version;
      setComparison(null);
      return result.metadataRevisionId ? "已采用候选并保存为新的信息版本。" : "已采用候选。";
    });
  }

  async function previewMove(targetPlatformInstanceId: string) {
    setBusy("move"); setNotice(""); setError("");
    try {
      let impact: MoveImpact | null = null;
      for (let attempt = 0; attempt < 2; attempt += 1) {
        const preview = await fetch(`/api/v1/admin/games/${game.gameId}/move-preview`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ targetPlatformInstanceId }) });
        if (!preview.ok) throw new Error(await responseError(preview, "无法预览移动影响"));
        if (preview.status === 202) {
          const pending = await preview.json() as { status: string; jobId: string };
          if (!pending.jobId) throw new Error("目标核心验证响应无效");
          await waitForJob(pending.jobId, (state) => setNotice(`移动预检 · ${state}`));
          continue;
        }
        impact = await preview.json() as MoveImpact;
        break;
      }
      if (!impact) throw new Error("目标核心验证完成后仍无法生成移动影响");
      const targetName = platformInstances.find((item) => item.id === targetPlatformInstanceId)?.name ?? "目标目录";
      setPendingMove({ targetPlatformInstanceId, targetName, result: impact });
      setNotice("移动影响已经准备好，请核对后确认。");
    } catch (caught) { setError(caught instanceof Error ? caught.message : "无法预览移动影响"); }
    finally { setBusy(null); }
  }

  async function confirmMove() {
    if (!pendingMove) return;
    const current = pendingMove;
    await action("move", async () => {
      const blocked = current.result.impact.blockerCodes.length > 0;
      const commit = await fetch(`/api/v1/admin/games/${game.gameId}/move`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ targetPlatformInstanceId: current.targetPlatformInstanceId, impactDigest: current.result.impactDigest, confirmBlocked: blocked }) });
      if (!commit.ok) throw new Error(await responseError(commit, "游戏移动失败"));
      setPendingMove(null);
      return "游戏已移动到目标目录；游戏文件、存档和历史版本均未改变。";
    });
  }

  async function remove(confirmTitle: string) {
    await action("delete", async () => {
      if (confirmTitle !== game.title) throw new Error("请输入完整游戏标题确认删除");
      const response = await fetch(`/api/v1/admin/games/${game.gameId}`, { method: "DELETE", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ confirmTitle }) });
      if (!response.ok) throw new Error(await responseError(response, "游戏删除失败"));
      return "游戏已从资料库移除；存档、审核记录和历史版本仍会保留。";
    });
  }

  const currentInstance = platformInstances.find((item) => item.id === game.platformInstance.id);
  const currentContent = game.contentRevisions.find((revision) => revision.current) ?? game.contentRevisions[0];
  const currentFile = currentContent?.files[0]?.logicalName ?? "尚无游戏文件";
  const currentVariant = game.variants.find((variant) => variant.coreId === currentInstance?.defaultCoreId) ?? game.variants[0];
  const currentRuntime = currentVariant?.revisions.find((revision) => revision.current) ?? currentVariant?.revisions[0];
  const runtime = runtimePresentation(currentRuntime?.status ?? null);
  const cover = game.assets.find((asset) => asset.kind === "COVER");
  const background = game.assets.find((asset) => asset.kind === "BACKGROUND");
  const screenshots = game.assets.filter((asset) => asset.kind === "SCREENSHOT").sort((left, right) => left.ordinal - right.ordinal);
  const nextScreenshotOrdinal = Math.min(31, Math.max(-1, ...screenshots.map((asset) => asset.ordinal)) + 1);
  const metadataComplete = Boolean(game.description.trim() && game.developer.trim() && game.publisher.trim() && game.genre.trim() && game.players && game.releaseYear);
  const moveTargets = platformInstances.filter((item) => item.enabled && item.platformId === game.platformId && item.id !== game.platformInstance.id);
  const disabled = busy !== null || game.status !== "PUBLISHED";
  const comparisonCover = comparison?.assets.find((asset) => asset.kind === "COVER" && asset.status === "READY" && asset.widthPx && asset.heightPx) ?? null;
  const comparisonFields: Array<{ key: "title" | "developer" | "publisher" | "genre" | "players" | "releaseYear"; label: string }> = [
    { key: "title", label: "标题" }, { key: "developer", label: "开发商" }, { key: "publisher", label: "发行商" },
    { key: "genre", label: "类型" }, { key: "players", label: "玩家数" }, { key: "releaseYear", label: "发行年份" },
  ];

  return <div className="admin-game-detail">
    <Toast toast={error ? { message: error, tone: "bad" } : notice ? { message: notice, tone: "good" } : null} onDismiss={() => { setNotice(""); setError(""); }} />
    <section className="admin-game-hero">
      <div className="admin-game-hero-cover">{cover ? <Image src={cover.url} alt={`${game.title} 封面`} fill sizes="102px" unoptimized /> : <span role="img" aria-label={`${game.title} 暂无封面`}>RETROM</span>}</div>
      <div className="admin-game-hero-copy"><h2>{game.title}</h2><p>{currentInstance?.platformName ?? game.platformId} · {game.platformInstance.name}{game.releaseYear ? ` · ${game.releaseYear}` : ""}{game.developer ? ` · ${game.developer}` : ""}</p><div><StatusBadge tone={game.status === "PUBLISHED" ? "good" : "bad"}>{game.status === "PUBLISHED" ? "用户可见" : "用户不可见"}</StatusBadge><StatusBadge tone={runtime.tone}>{runtime.label}</StatusBadge><StatusBadge tone={metadataComplete ? "info" : "warn"}>{metadataComplete ? "资料完整" : "资料待补充"}</StatusBadge></div></div>
      <div className="admin-game-hero-update"><span>最近更新</span><strong>{formatAdminGameTime(game.updatedAtMs, game.generatedAtMs)}</strong><small>{game.metadataRevisions.length} 个信息版本 · {game.contentRevisions.length} 个内容版本</small></div>
    </section>

    <section className="admin-game-overview" aria-label="游戏概览">
      <div><span>所属目录</span><strong>{game.platformInstance.name}</strong></div><div><span>推荐运行方式</span><strong>{currentInstance?.defaultCoreName ?? currentVariant?.coreName ?? "尚未配置"}</strong></div><div><span>当前游戏文件</span><strong>{currentFile}</strong></div><div><span>最后运行验证</span><strong>{formatTime(currentRuntime?.createdAtMs)}</strong></div><div><span>关联存档</span><strong>{game.deleteImpact.saveStateCount} 份</strong></div>
    </section>

    <div className="admin-game-primary-grid">
      <section className="panel admin-game-publish" id="admin-game-basic"><div className="panel-head"><h2>发布信息</h2></div><form className="panel-body admin-game-publish-form" onSubmit={(event) => void saveMetadata(event)}>
        <label className="full">标题<input name="title" defaultValue={game.title} required maxLength={200} /></label>
        <label className="full">简介<textarea name="description" defaultValue={game.description} maxLength={10000} /></label>
        <label>开发商<input name="developer" defaultValue={game.developer} maxLength={200} /></label><label>发行商<input name="publisher" defaultValue={game.publisher} maxLength={200} /></label>
        <label>类型<input name="genre" defaultValue={game.genre} maxLength={200} /></label><label>玩家数<input name="players" type="number" min={1} max={64} defaultValue={game.players ?? ""} /></label>
        <label>发行年份<input name="releaseYear" type="number" min={1950} defaultValue={game.releaseYear ?? ""} /></label><label>平台<input value={currentInstance?.platformName ?? game.platformId} readOnly aria-readonly="true" /></label>
        <div className="admin-game-savebar full"><span>上次保存：{formatAdminGameTime(game.updatedAtMs, game.generatedAtMs)}</span><div><details><summary>查看版本历史</summary><div>{game.metadataRevisions.map((revision) => <small key={revision.id}>{revision.current ? "● 当前" : "○ 历史"} · {formatTime(revision.createdAtMs)} · {revision.sourceKind}</small>)}</div></details><button className="button" disabled={disabled}>保存新版本</button></div></div>
      </form></section>

      <section className="panel admin-game-media" id="admin-game-media"><div className="panel-head"><h2>媒体</h2></div><div className="panel-body admin-game-media-grid">
        <article className="admin-game-cover-slot"><h3>封面</h3><div className="admin-game-cover-frame">{cover ? <Image src={cover.url} alt={`${game.title} 封面`} fill sizes="180px" unoptimized /> : <span>暂无封面</span>}</div><footer>{cover ? `${cover.widthPx}×${cover.heightPx}` : "建议使用 3:4 图片"}<input id="admin-cover-upload" hidden type="file" accept="image/png,image/jpeg,image/webp" disabled={disabled} onChange={(event) => { const file = event.target.files?.[0]; if (file) void replaceAsset(file, "COVER", 0); }} /><label aria-disabled={disabled} htmlFor="admin-cover-upload">{cover ? "替换" : "添加"}</label></footer></article>
        <div className="admin-game-other-media"><article className="admin-game-background-slot"><h3>背景图</h3><div>{background ? <Image src={background.url} alt={`${game.title} 背景图`} fill sizes="360px" unoptimized /> : <span><strong>暂无背景图</strong><small>添加一张用于用户详情页</small></span>}</div><input id="admin-background-upload" hidden type="file" accept="image/png,image/jpeg,image/webp" disabled={disabled} onChange={(event) => { const file = event.target.files?.[0]; if (file) void replaceAsset(file, "BACKGROUND", 0); }} /><label aria-disabled={disabled} htmlFor="admin-background-upload">{background ? "替换背景" : "＋ 添加背景"}</label></article>
          <div className="admin-game-screenshots"><h3>游戏截图</h3><div>{screenshots.slice(0, 2).map((asset) => <article key={asset.assetId}><Image src={asset.url} alt={`${game.title} 游戏截图 ${asset.ordinal + 1}`} fill sizes="130px" unoptimized /><input id={`admin-shot-${asset.ordinal}`} hidden type="file" accept="image/png,image/jpeg,image/webp" disabled={disabled} onChange={(event) => { const file = event.target.files?.[0]; if (file) void replaceAsset(file, "SCREENSHOT", asset.ordinal); }} /><label aria-disabled={disabled} htmlFor={`admin-shot-${asset.ordinal}`}>替换截图 {asset.ordinal + 1}</label></article>)}<article className="add"><input id="admin-screenshot-upload" hidden type="file" accept="image/png,image/jpeg,image/webp" disabled={disabled || screenshots.length >= 32} onChange={(event) => { const file = event.target.files?.[0]; if (file) void replaceAsset(file, "SCREENSHOT", nextScreenshotOrdinal); }} /><label aria-disabled={disabled || screenshots.length >= 32} htmlFor="admin-screenshot-upload">＋ 添加截图</label></article></div></div>
        </div>
      </div></section>
    </div>

    <section className="panel admin-game-runtime" id="admin-game-runtime"><div className="panel-head"><h2>游戏文件与运行环境</h2></div><div className="panel-body"><div className="admin-game-runtime-grid"><div><span>当前游戏文件</span><strong>{currentFile}</strong></div><div><span>推荐运行方式</span><strong>{currentInstance?.defaultCoreName ?? currentVariant?.coreName ?? "尚未配置"}</strong></div><div><span>兼容状态</span><strong className={runtime.tone}>{runtime.label}</strong></div><div><span>最后验证</span><strong>{formatTime(currentRuntime?.createdAtMs)}</strong></div></div>
      <div className="admin-game-runtime-note"><p>替换游戏文件后会创建新的内容版本并执行兼容性验证；验证通过后才切换当前版本。原文件、历史版本和已有存档不会删除。</p><input id="admin-game-content-upload" hidden type="file" multiple disabled={disabled} onChange={(event) => { const files = Array.from(event.target.files ?? []); if (files.length) void replaceContent(files); }} /><label className="button secondary" aria-disabled={disabled} htmlFor="admin-game-content-upload">替换游戏文件</label></div>
      <details className="admin-game-technical"><summary>技术详情</summary><div>{game.contentRevisions.map((revision) => <p key={revision.id}><strong>{revision.current ? "当前内容" : "历史内容"}</strong> · {formatTime(revision.createdAtMs)} · {revision.files.map((file) => file.logicalName).join("、")}<code>{revision.id}</code></p>)}{game.variants.map((variant) => <p key={variant.id}><strong>{variant.coreName}</strong> · {variant.revisions.map((revision) => `${revision.current ? "当前" : "历史"} ${revision.status}`).join(" / ")}<code>{variant.id}</code></p>)}</div></details>
    </div></section>

    <section className="panel admin-game-actions" id="admin-game-actions"><div className="panel-head"><h2>管理操作</h2></div><div className="panel-body admin-game-action-grid"><article><h3>重新获取游戏资料</h3><p>重新查询标题、简介与媒体候选；先并排比较基础信息、封面和完整说明，确认后才会应用。</p><button className="button secondary" type="button" disabled={disabled} onClick={() => void rescrape()}>重新查找游戏信息</button>{scrapeCandidates.length ? <div className="admin-game-candidates">{scrapeCandidates.map((candidate) => <button type="button" key={candidate.candidateId} disabled={busy !== null} onClick={() => setComparison(candidate)}><span><strong>{String(candidate.metadata.title ?? candidate.providerGameId)}</strong><small>{candidate.assets.some((asset) => asset.kind === "COVER" && asset.status === "READY") ? "包含封面候选" : "仅有文字候选"}</small></span><span>对比并选择</span></button>)}</div> : null}</article>
      <article><h3>移动到其他游戏目录</h3><p>移动前检查目标目录推荐运行方式与兼容性，不修改文件与存档。</p>{moveTargets.length ? <div><select aria-label="目标游戏目录" value={moveTarget} disabled={disabled} onChange={(event) => setMoveTarget(event.target.value)}><option value="">选择目标目录…</option>{moveTargets.map((item) => <option value={item.id} key={item.id}>{item.name} · 推荐 {item.defaultCoreName}</option>)}</select><button className="button secondary" type="button" disabled={disabled || !moveTarget} onClick={() => void previewMove(moveTarget)}>预览移动影响</button></div> : <small>没有其他同游戏平台目录可移动。</small>}</article></div></section>

    <details className="panel admin-game-remove"><summary className="panel-head"><h2>从游戏库移除</h2><span>展开危险操作</span></summary><form className="panel-body" onSubmit={(event) => { event.preventDefault(); void remove(String(new FormData(event.currentTarget).get("confirmTitle") ?? "")); }}><div><strong>游戏将不再对用户可见。</strong><p>已有 {game.deleteImpact.saveStateCount} 份存档、{game.deleteImpact.reviewEventCount} 条审核历史及历史版本会继续保留；当前 {game.deleteImpact.activeLaunchCount} 个活动游戏会话。</p></div><label>输入完整游戏标题确认<input name="confirmTitle" placeholder={game.title} autoComplete="off" disabled={disabled} /><button className="button danger" disabled={disabled}>移除游戏</button></label></form></details>

    <ConfirmDialog open={comparison !== null} wide title="对比最新游戏信息" description="左栏是当前信息，右栏是最新候选；每栏上方为基础信息与封面，下方为完整游戏说明。应用后会创建新的信息版本。" confirmLabel="应用这组信息" busy={busy === "candidate"} onCancel={() => setComparison(null)} onConfirm={() => { if (comparison) void applyCandidate(comparison); }}>
      {comparison ? <div className="metadata-compare metadata-compare-columns">
        <section className="metadata-compare-column" aria-label="当前信息"><header><strong>当前信息</strong><span>只读</span></header><div className="metadata-compare-column-top"><div className="metadata-compare-fields">{comparisonFields.map((field) => <div className="compare-readonly" key={field.key}><span>{field.label}</span><p>{String(game[field.key] ?? "") || "未填写"}</p></div>)}</div><div className="metadata-compare-column-cover"><span>封面</span>{cover ? <Image src={cover.url} alt="当前游戏封面" width={cover.widthPx} height={cover.heightPx} unoptimized /> : <p>暂无封面</p>}</div></div><div className="metadata-compare-column-description"><span>游戏说明</span><p>{game.description || "未填写"}</p></div></section>
        <section className="metadata-compare-column" aria-label="最新候选"><header><strong>最新候选</strong><span>应用前预览</span></header><div className="metadata-compare-column-top"><div className="metadata-compare-fields">{comparisonFields.map((field) => { const currentValue = game[field.key]; const nextValue = comparison.metadata[field.key]; const same = String(currentValue ?? "") === String(nextValue ?? ""); return <div className={`compare-readonly ${same ? "is-same" : "is-changed"}`} key={field.key}><span>{field.label}</span><p>{String(nextValue ?? "") || "候选未提供，将保留当前值"}</p></div>; })}</div><div className={`metadata-compare-column-cover ${comparisonCover?.candidateAssetId === cover?.assetId ? "is-same" : "is-changed"}`}><span>封面</span>{comparisonCover ? <Image src={`/api/v1/admin/review-assets/${comparisonCover.candidateAssetId}`} alt="最新候选封面" width={comparisonCover.widthPx ?? 1} height={comparisonCover.heightPx ?? 1} unoptimized /> : <p>候选未提供，将保留当前封面</p>}</div></div><div className={`metadata-compare-column-description ${game.description === String(comparison.metadata.description ?? "") ? "is-same" : "is-changed"}`}><span>游戏说明</span><p>{String(comparison.metadata.description ?? "") || "候选未提供，将保留当前说明"}</p></div></section>
      </div> : null}
    </ConfirmDialog>
    <ConfirmDialog open={pendingMove !== null} title={`移动“${game.title}”到新目录？`} description={`目标目录：${pendingMove?.targetName ?? ""}`} confirmLabel="确认移动" tone={pendingMove?.result.impact.blockerCodes.length ? "danger" : "default"} busy={busy !== null} onCancel={() => setPendingMove(null)} onConfirm={() => void confirmMove()}><ul><li>新的推荐运行方式：{pendingMove?.result.impact.targetCoreId}</li><li>检查结果：{pendingMove?.result.impact.variantStatus}</li>{pendingMove?.result.impact.blockerCodes.length ? <li>{pendingMove.result.impact.blockerCodes.length} 项问题会暂时阻止运行</li> : <li>没有发现会阻止运行的问题</li>}<li>游戏文件、存档和历史版本不会移动或删除</li></ul></ConfirmDialog>
  </div>;
}
