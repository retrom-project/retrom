"use client";

import { FormEvent, useState } from "react";
import Image from "next/image";
import { useRouter } from "next/navigation";
import { StatusBadge } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { formatTime } from "@/lib/backend";
import { newUuid } from "@/lib/crypto";
import { responseError, uploadFiles, uploadOne, waitForJob } from "@/lib/upload";

type Revision = { id: string; sourceKind: string; sourceRefId: string | null; current: boolean; createdAtMs: number };
type ContentRevision = Revision & { files: Array<{ role: string; logicalName: string; sortOrder: number }> };
type VariantRevision = { id: string; contentRevisionId: string; coreArtifactId: string; datVersionId: string | null; status: string; compatibilityCode: string; current: boolean; createdAtMs: number };
type Variant = { id: string; coreId: string; coreName: string; currentRevisionId: string | null; version: number; revisions: VariantRevision[] };
type Asset = { assetId: string; kind: string; ordinal: number; widthPx: number; heightPx: number; mediaType: string; url: string };

export type AdminGame = {
  gameId: string; status: string; title: string; description: string; developer: string; publisher: string; genre: string;
  players: number | null; releaseYear: number | null; platformId: string; platformInstance: { id: string; name: string };
  currentContentRevisionId: string; currentMetadataRevisionId: string; version: number; updatedAtMs: number;
  deleteImpact: { saveStateCount: number; reviewEventCount: number; activeLaunchCount: number };
  metadataRevisions: Revision[]; assets: Asset[]; contentRevisions: ContentRevision[]; variants: Variant[];
};

export type PlatformInstanceOption = { id: string; platformId: string; name: string; defaultCoreName: string; enabled: boolean };
export type ScrapeCandidate = { candidateId: string; providerGameId: string; metadata: Record<string, unknown>; hitCount: number };

const metadataFields = ["title", "description", "developer", "publisher", "genre", "players", "releaseYear"];

export function AdminGameManager({ game, platformInstances, candidates }: { game: AdminGame; platformInstances: PlatformInstanceOption[]; candidates: ScrapeCandidate[] }) {
  const router = useRouter();
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  async function action(name: string, callback: () => Promise<string>) {
    setBusy(name); setNotice(""); setError("");
    try { setNotice(await callback()); router.refresh(); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "操作失败"); }
    finally { setBusy(null); }
  }

  function versionedHeaders(json = true) {
    return writeHeaders({ ...(json ? { "Content-Type": "application/json" } : {}), "If-Match": `"v${game.version}"` });
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

  async function replaceAsset(file: File, kind: string) {
    await action("asset", async () => {
      const uploaded = await uploadOne(file, setNotice);
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/assets`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ uploadFileId: uploaded.uploadFileId, kind, ordinal: 0 }) });
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
      const result = await response.json() as { scrapeRunId: string; jobId: string };
      setNotice("正在查找新的游戏信息候选…");
      await waitForJob(result.jobId, () => setNotice("正在整理候选信息…"));
      return result.scrapeRunId ? "候选已准备好，采用前不会覆盖当前信息。" : "候选查询已完成。";
    });
  }

  async function applyCandidate(candidate: ScrapeCandidate) {
    await action("candidate", async () => {
      const fields = metadataFields.filter((field) => Object.hasOwn(candidate.metadata, field));
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/scrape-candidates/${candidate.candidateId}/apply`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ fields, selectedAssets: { coverCandidateAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: [] } }) });
      if (!response.ok) throw new Error(await responseError(response, "候选采用失败"));
      const result = await response.json() as { metadataRevisionId: string };
      return result.metadataRevisionId ? "已采用候选并保存为新的信息版本。" : "已采用候选。";
    });
  }

  async function move(targetPlatformInstanceId: string) {
    await action("move", async () => {
      type MoveImpact = { impactDigest: string; impact: { targetCoreId: string; variantStatus: string; blockerCodes: string[] } };
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
      const blocked = impact.impact.blockerCodes.length > 0;
      if (!window.confirm(`移动到新目录？目标默认核心 ${impact.impact.targetCoreId}，状态 ${impact.impact.variantStatus}${blocked ? `，阻断：${impact.impact.blockerCodes.join("、")}` : ""}。`)) return "已取消移动，游戏归属未改变。";
      const commit = await fetch(`/api/v1/admin/games/${game.gameId}/move`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ targetPlatformInstanceId, impactDigest: impact.impactDigest, confirmBlocked: blocked }) });
      if (!commit.ok) throw new Error(await responseError(commit, "游戏移动失败"));
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

  const moveTargets = platformInstances.filter((item) => item.enabled && item.platformId === game.platformId && item.id !== game.platformInstance.id);
  return <div className="stack">
    {notice ? <p role="status" className="status good">{notice}</p> : null}
    {error ? <p role="alert" className="status bad">{error}</p> : null}
    <div className="admin-workbench">
      <section className="panel"><div className="panel-head"><div><StatusBadge tone="info">信息版本 · {game.metadataRevisions.length}</StatusBadge><h2>发布信息</h2></div></div><form className="panel-body form-grid" onSubmit={(event) => void saveMetadata(event)}><div className="field full"><label htmlFor="game-title">标题</label><input id="game-title" name="title" defaultValue={game.title} required maxLength={200} /></div><div className="field full"><label htmlFor="game-description">简介</label><textarea id="game-description" name="description" defaultValue={game.description} maxLength={10000} /></div><div className="field"><label htmlFor="game-developer">开发商</label><input id="game-developer" name="developer" defaultValue={game.developer} maxLength={200} /></div><div className="field"><label htmlFor="game-publisher">发行商</label><input id="game-publisher" name="publisher" defaultValue={game.publisher} maxLength={200} /></div><div className="field"><label htmlFor="game-genre">类型</label><input id="game-genre" name="genre" defaultValue={game.genre} maxLength={200} /></div><div className="field"><label htmlFor="game-players">玩家数</label><input id="game-players" name="players" type="number" min={1} max={64} defaultValue={game.players ?? ""} /></div><div className="field"><label htmlFor="game-year">发行年份</label><input id="game-year" name="releaseYear" type="number" min={1950} defaultValue={game.releaseYear ?? ""} /></div><div className="field"><button className="button" disabled={busy !== null || game.status !== "PUBLISHED"}>保存并创建新版本</button></div></form><details className="revision-list technical-details"><summary>查看版本历史</summary>{game.metadataRevisions.map((revision) => <small key={revision.id}>{revision.current ? "● 当前" : "○ 历史"} · {formatTime(revision.createdAtMs)} · {revision.id}</small>)}</details></section>
      <section className="panel"><div className="panel-head"><div><StatusBadge tone="info">媒体资源</StatusBadge><h2>媒体</h2></div></div><div className="panel-body"><div className="asset-grid">{game.assets.map((asset) => <figure key={asset.assetId}><Image src={asset.url} alt={`${game.title} ${asset.kind}`} width={asset.widthPx} height={asset.heightPx} unoptimized /><figcaption>{asset.kind === "COVER" ? "封面" : asset.kind === "BACKGROUND" ? "背景" : "游戏截图"} · {asset.widthPx}×{asset.heightPx}</figcaption></figure>)}</div><div className="form-grid"><label className="field">媒体类型<select id="asset-kind" defaultValue="COVER"><option value="COVER">封面</option><option value="BACKGROUND">背景</option><option value="SCREENSHOT">游戏截图</option></select></label><div className="field"><span className="field-label">替换图片</span><input id="game-asset-file" hidden type="file" accept="image/png,image/jpeg,image/webp" disabled={busy !== null || game.status !== "PUBLISHED"} onChange={(event) => { const file = event.target.files?.[0]; const kind = (document.getElementById("asset-kind") as HTMLSelectElement | null)?.value ?? "COVER"; if (file) void replaceAsset(file, kind); }} /><label className="button secondary" aria-disabled={busy !== null || game.status !== "PUBLISHED"} htmlFor="game-asset-file">选择图片</label></div></div></div></section>
      <section className="panel"><div className="panel-head"><div><StatusBadge tone="good">运行快照</StatusBadge><h2>游戏内容与运行环境</h2></div></div><div className="panel-body"><div className="field"><span className="field-label">替换游戏文件（DOS 游戏可多选）</span><input id="game-content-files" hidden type="file" multiple disabled={busy !== null || game.status !== "PUBLISHED"} onChange={(event) => { const files = Array.from(event.target.files ?? []); if (files.length) void replaceContent(files); }} /><label className="button secondary" aria-disabled={busy !== null || game.status !== "PUBLISHED"} htmlFor="game-content-files">选择游戏文件</label></div>{game.contentRevisions.map((revision) => <details key={revision.id} open={revision.current}><summary>{revision.current ? "● 当前内容" : "○ 历史内容"} · {formatTime(revision.createdAtMs)}</summary><ul>{revision.files.map((file) => <li key={`${file.role}-${file.logicalName}`}>{file.logicalName}</li>)}</ul><details className="technical-details"><summary>技术详情</summary><code>{revision.id}</code></details></details>)}{game.variants.map((variant) => <details key={variant.id} open><summary>{variant.coreName} · 运行记录</summary>{variant.revisions.map((revision) => <div key={revision.id}><StatusBadge tone={revision.status === "READY" ? "good" : "bad"}>{revision.current ? "当前 · " : "历史 · "}{revision.status === "READY" ? "可以运行" : "需要处理"}</StatusBadge><details className="technical-details"><summary>技术详情</summary><code>{revision.id} · {revision.coreArtifactId} · {revision.datVersionId ?? "无 DAT"}</code></details></div>)}</details>)}</div></section>
      <section className="panel"><div className="panel-head"><div><StatusBadge tone="warn">需确认</StatusBadge><h2>管理操作</h2></div></div><div className="panel-body stack"><button className="button secondary" type="button" disabled={busy !== null || game.status !== "PUBLISHED"} onClick={() => void rescrape()}>重新刮削并生成预览</button>{candidates.map((candidate) => <div className="candidate" key={candidate.candidateId}><strong>{String(candidate.metadata.title ?? candidate.providerGameId)}</strong><p>Hasheous {candidate.providerGameId} · {candidate.hitCount} 条证据命中</p><pre>{JSON.stringify(candidate.metadata, null, 2)}</pre><button className="button secondary" disabled={busy !== null} onClick={() => void applyCandidate(candidate)}>明确采用全部可用文本字段</button></div>)}{moveTargets.length ? <label className="field">移动到同平台目录<select disabled={busy !== null || game.status !== "PUBLISHED"} defaultValue="" onChange={(event) => { if (event.target.value) void move(event.target.value); }}><option value="" disabled>选择目录并预览影响…</option>{moveTargets.map((item) => <option value={item.id} key={item.id}>{item.name} · 默认 {item.defaultCoreName}</option>)}</select></label> : <p>没有其他同基础平台目录可移动。</p>}<form onSubmit={(event) => { event.preventDefault(); void remove(String(new FormData(event.currentTarget).get("confirmTitle") ?? "")); }}><label className="field">输入“{game.title}”确认软删除<input name="confirmTitle" autoComplete="off" disabled={busy !== null || game.status !== "PUBLISHED"} /></label><p>影响：{game.deleteImpact.saveStateCount} 个存档、{game.deleteImpact.reviewEventCount} 条审核历史、{game.deleteImpact.activeLaunchCount} 个活动启动会话。</p><button className="button danger" disabled={busy !== null || game.status !== "PUBLISHED"}>软删除游戏</button></form></div></section>
    </div>
  </div>;
}
