"use client";

import { type FormEvent, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useRouter } from "next/navigation";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { responseError, uploadFiles, uploadOne, waitForJob } from "@/lib/upload";
import { runtimePresentation } from "./admin-game-library";
import { type TagReference } from "@/components/tag-picker";
import { AdminGameManagerView } from "./admin-game-manager-view";

export type Revision = { id: string; sourceKind: string; sourceRefId: string | null; current: boolean; createdAtMs: number };
export type ContentRevision = Revision & { contentKind: string; files: Array<{ role: string; logicalName: string; sortOrder: number; sizeBytes: number; sha256: string }> };
export type VariantRevision = { id: string; contentRevisionId: string; coreArtifactId: string; datVersionId: string | null; status: string; compatibilityCode: string; dependencySnapshot?: { multiDisc?: { canonicalPlaylistSha256?: string } }; current: boolean; createdAtMs: number };
export type Variant = { id: string; coreId: string; coreName: string; currentRevisionId: string | null; version: number; revisions: VariantRevision[] };
export type Asset = { assetId: string; kind: string; ordinal: number; widthPx: number | null; heightPx: number | null; mediaType: string; url: string };

export type AdminGame = {
  gameId: string; status: string; title: string; description: string; developer: string; publisher: string; genre: string;
  players: number | null; releaseYear: number | null; platformId: string; platformInstance: { id: string; name: string };
  currentContentRevisionId: string; currentMetadataRevisionId: string; version: number; createdAtMs: number; updatedAtMs: number; generatedAtMs: number;
  deleteImpact: { saveStateCount: number; reviewEventCount: number; activeLaunchCount: number };
  metadataRevisions: Revision[]; assets: Asset[]; contentRevisions: ContentRevision[]; variants: Variant[];
  tags?: TagReference[];
};

export type PlatformInstanceOption = { id: string; platformId: string; platformName: string; name: string; defaultCoreId: string; defaultCoreName: string; enabled: boolean; importCapabilities?: { contentModes: string[]; multiDisc: { maxDiscs: number; maxTotalBytes: number } | null } };
type ScrapeCandidateAsset = { candidateAssetId: string; kind: string; status: string; widthPx: number | null; heightPx: number | null; mediaType: string | null };
export type ScrapeCandidate = { candidateId: string; providerGameId: string; metadata: Record<string, unknown>; hitCount: number; assets: ScrapeCandidateAsset[] };
export type MoveImpact = { impactDigest: string; impact: { targetCoreId: string; variantStatus: string; blockerCodes: string[] } };
export type PendingMove = { targetPlatformInstanceId: string; targetName: string; result: MoveImpact };

const metadataFields = ["title", "description", "developer", "publisher", "genre", "players", "releaseYear"];
const subscribeToClientReady = () => () => undefined;
const getClientReadySnapshot = () => true;
const getServerClientReadySnapshot = () => false;

export type MetadataDraft = {
  title: string;
  description: string;
  developer: string;
  publisher: string;
  genre: string;
  players: string;
  releaseYear: string;
};

function metadataDraft(game: AdminGame): MetadataDraft {
  return {
    title: game.title,
    description: game.description,
    developer: game.developer,
    publisher: game.publisher,
    genre: game.genre,
    players: game.players === null ? "" : String(game.players),
    releaseYear: game.releaseYear === null ? "" : String(game.releaseYear),
  };
}

function contentPresentation(game: AdminGame, instance: PlatformInstanceOption | undefined) {
  const current = game.contentRevisions.find((revision) => revision.current) ?? game.contentRevisions[0];
  const supportsMultiDisc = instance?.importCapabilities?.contentModes.includes("MULTI_DISC_M3U_V1") ?? false;
  const discs = current?.contentKind === "MULTI_DISC_M3U_V1"
    ? current.files.filter((file) => file.role === "DISC").sort((left, right) => left.sortOrder - right.sortOrder)
    : [];
  return {
    current,
    discs,
    file: current?.files[0]?.logicalName ?? "尚无游戏文件",
    replacementLimits: supportsMultiDisc ? instance?.importCapabilities?.multiDisc ?? null : null,
  };
}

function runtimeRevisionPresentation(game: AdminGame, instance: PlatformInstanceOption | undefined) {
  const variant = game.variants.find((item) => item.coreId === instance?.defaultCoreId) ?? game.variants[0];
  const revision = variant?.revisions.find((item) => item.current) ?? variant?.revisions[0];
  return {
    canonicalPlaylistSHA256: revision?.dependencySnapshot?.multiDisc?.canonicalPlaylistSha256 ?? "",
    revision,
    runtime: runtimePresentation(revision?.status ?? null),
    variant,
  };
}

function gameMetadataComplete(game: AdminGame) {
  return Boolean(game.description.trim()
    && game.developer.trim()
    && game.publisher.trim()
    && game.genre.trim()
    && game.players
    && game.releaseYear);
}

function readyComparisonCover(comparison: ScrapeCandidate | null) {
  return comparison?.assets.find((asset) => asset.kind === "COVER"
    && asset.status === "READY"
    && asset.widthPx
    && asset.heightPx) ?? null;
}

export function AdminGameManager({ game, platformInstances, candidates, activeTags = [] }: { game: AdminGame; platformInstances: PlatformInstanceOption[]; candidates: ScrapeCandidate[]; activeTags?: TagReference[] }) {
  const router = useRouter();
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [pendingMove, setPendingMove] = useState<PendingMove | null>(null);
  const [moveTarget, setMoveTarget] = useState("");
  const [scrapeCandidates, setScrapeCandidates] = useState(candidates);
  const [comparison, setComparison] = useState<ScrapeCandidate | null>(null);
  const clientReady = useSyncExternalStore(subscribeToClientReady, getClientReadySnapshot, getServerClientReadySnapshot);
  const [draft, setDraft] = useState<MetadataDraft>(() => metadataDraft(game));
  const [savedDraft, setSavedDraft] = useState<MetadataDraft>(() => metadataDraft(game));
  const [gameTags, setGameTags] = useState<TagReference[]>(game.tags ?? []);
  const [savedGameTags, setSavedGameTags] = useState<TagReference[]>(game.tags ?? []);
  const versionRef = useRef(game.version);
  const metadataRevisionRef = useRef(game.currentMetadataRevisionId);

  useEffect(() => {
    versionRef.current = game.version;
    if (metadataRevisionRef.current !== game.currentMetadataRevisionId) {
      const current = metadataDraft(game);
      metadataRevisionRef.current = game.currentMetadataRevisionId;
      setDraft(current);
      setSavedDraft(current);
    }
  }, [game]);

  async function action(name: string, callback: () => Promise<string>) {
    setBusy(name); setNotice(""); setError("");
    try { setNotice(await callback()); router.refresh(); return true; }
    catch (caught) { setError(caught instanceof Error ? caught.message : "操作失败"); return false; }
    finally { setBusy(null); }
  }

  function versionedHeaders(json = true) {
    return writeHeaders({ ...(json ? { "Content-Type": "application/json" } : {}), "If-Match": `"v${versionRef.current}"` });
  }

  async function saveMetadata(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const submitted = { ...draft };
    await action("metadata", async () => {
      const body: Record<string, string | number | null> = {
        title: submitted.title,
        description: submitted.description,
        developer: submitted.developer,
        publisher: submitted.publisher,
        genre: submitted.genre,
        players: submitted.players ? Number(submitted.players) : null,
        releaseYear: submitted.releaseYear ? Number(submitted.releaseYear) : null,
      };
      const response = await fetch(`/api/v1/admin/games/${game.gameId}`, { method: "PATCH", credentials: "same-origin", headers: await versionedHeaders(), body: JSON.stringify(body) });
      if (!response.ok) {throw new Error(await responseError(response, "发布信息保存失败"));}
      const result = await response.json() as { metadataRevisionId: string; version?: number };
      if (result.version) {versionRef.current = result.version;}
      setSavedDraft(submitted);
      return result.metadataRevisionId ? "发布信息已保存为新版本。" : "发布信息已保存。";
    });
  }

  async function saveTags() {
    await action("tags", async () => {
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/tags`, {
        method: "PUT", credentials: "same-origin",
        headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() },
        body: JSON.stringify({ tagIds: gameTags.map((tag) => tag.tagId) }),
      });
      if (!response.ok) {throw new Error(await responseError(response, "游戏标签保存失败"));}
      const result = await response.json() as { version: number; tags: TagReference[] };
      versionRef.current = result.version;
      setGameTags(result.tags); setSavedGameTags(result.tags);
      return "游戏标签已更新。";
    });
  }

  async function replaceAsset(file: File, kind: "COVER" | "VIDEO", ordinal: number) {
    await action("asset", async () => {
      const uploaded = await uploadOne(file, setNotice);
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/assets`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ uploadFileId: uploaded.uploadFileId, kind, ordinal }) });
      if (!response.ok) {throw new Error(await responseError(response, "媒体替换失败"));}
      const result = await response.json() as { assetId: string; metadataRevisionId: string };
      return result.assetId && result.metadataRevisionId ? `${kind === "VIDEO" ? "视频" : "图片"}已更新，旧版本仍会保留。` : "媒体已更新。";
    });
  }

  async function removeVideo() {
    await action("asset", async () => {
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/assets/VIDEO`, { method: "DELETE", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() } });
      if (!response.ok) {throw new Error(await responseError(response, "视频移除失败"));}
      const match = response.headers.get("ETag")?.match(/^"v(\d+)"$/);
      if (match) {versionRef.current = Number(match[1]);}
      return "视频已从当前媒体版本移除，历史版本仍会保留。";
    });
  }

  async function replaceContent(files: File[], mode: "STANDARD" | "MULTI_DISC_M3U_V1") {
    return action("content", async () => {
      const uploaded = await uploadFiles(files, setNotice);
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/content-revisions`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ uploadId: uploaded.uploadId, contentMode: mode }) });
      if (!response.ok) {throw new Error(await responseError(response, "内容替换任务创建失败"));}
      const result = await response.json() as { jobId: string };
      setNotice("正在安全校验新游戏文件…");
      await waitForJob(result.jobId, () => setNotice("正在检查新文件是否可以运行…"));
      return "游戏文件已更新，旧内容和存档仍会保留。";
    });
  }

  async function rescrape() {
    await action("scrape", async () => {
      const response = await fetch(`/api/v1/admin/games/${game.gameId}/scrape-candidates`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ metadataProvider: "HASHEOUS" }) });
      if (!response.ok) {throw new Error(await responseError(response, "重新刮削任务创建失败"));}
      const result = await response.json() as { scrapeRunId: string; jobId: string; version?: number };
      if (result.version) {versionRef.current = result.version;}
      setNotice("正在查找新的游戏信息候选…");
      await waitForJob(result.jobId, () => setNotice("正在整理候选信息…"));
      const latestResponse = await fetch(`/api/v1/admin/games/${game.gameId}/scrape-candidates`, { cache: "no-store" });
      if (!latestResponse.ok) {throw new Error(await responseError(latestResponse, "候选查询完成，但无法读取结果"));}
      const latest = await latestResponse.json() as { items: ScrapeCandidate[] };
      setScrapeCandidates(latest.items);
      if (latest.items[0]) {setComparison(latest.items[0]);}
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
      if (!response.ok) {throw new Error(await responseError(response, "候选采用失败"));}
      const result = await response.json() as { metadataRevisionId: string; version?: number };
      if (result.version) {versionRef.current = result.version;}
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
        if (!preview.ok) {throw new Error(await responseError(preview, "无法预览移动影响"));}
        if (preview.status === 202) {
          const pending = await preview.json() as { status: string; jobId: string };
          if (!pending.jobId) {throw new Error("目标核心验证响应无效");}
          await waitForJob(pending.jobId, (state) => setNotice(`移动预检 · ${state}`));
          continue;
        }
        impact = await preview.json() as MoveImpact;
        break;
      }
      if (!impact) {throw new Error("目标核心验证完成后仍无法生成移动影响");}
      const targetName = platformInstances.find((item) => item.id === targetPlatformInstanceId)?.name ?? "目标目录";
      setPendingMove({ targetPlatformInstanceId, targetName, result: impact });
      setNotice("移动影响已经准备好，请核对后确认。");
    } catch (caught) { setError(caught instanceof Error ? caught.message : "无法预览移动影响"); }
    finally { setBusy(null); }
  }

  async function confirmMove() {
    if (!pendingMove) {return;}
    const current = pendingMove;
    await action("move", async () => {
      const blocked = current.result.impact.blockerCodes.length > 0;
      const commit = await fetch(`/api/v1/admin/games/${game.gameId}/move`, { method: "POST", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ targetPlatformInstanceId: current.targetPlatformInstanceId, impactDigest: current.result.impactDigest, confirmBlocked: blocked }) });
      if (!commit.ok) {throw new Error(await responseError(commit, "游戏移动失败"));}
      setPendingMove(null);
      return "游戏已移动到目标目录；游戏文件、存档和历史版本均未改变。";
    });
  }

  async function remove(confirmTitle: string) {
    await action("delete", async () => {
      if (confirmTitle !== game.title) {throw new Error("请输入完整游戏标题确认删除");}
      const response = await fetch(`/api/v1/admin/games/${game.gameId}`, { method: "DELETE", credentials: "same-origin", headers: { ...await versionedHeaders(), "Idempotency-Key": newUuid() }, body: JSON.stringify({ confirmTitle }) });
      if (!response.ok) {throw new Error(await responseError(response, "游戏删除失败"));}
      return "游戏已从资料库移除；存档、审核记录和历史版本仍会保留。";
    });
  }

  const currentInstance = platformInstances.find((item) => item.id === game.platformInstance.id);
  const content = contentPresentation(game, currentInstance);
  const runtimeRevision = runtimeRevisionPresentation(game, currentInstance);
  const cover = game.assets.find((asset) => asset.kind === "COVER");
  const video = game.assets.find((asset) => asset.kind === "VIDEO");
  const metadataComplete = gameMetadataComplete(game);
  const moveTargets = platformInstances.filter((item) => item.enabled && item.platformId === game.platformId && item.id !== game.platformInstance.id);
  const disabled = busy !== null || game.status !== "PUBLISHED";
  const metadataDirty = JSON.stringify(draft) !== JSON.stringify(savedDraft);
  const tagsDirty = JSON.stringify(gameTags.map((tag) => tag.tagId).sort()) !== JSON.stringify(savedGameTags.map((tag) => tag.tagId).sort());
  const comparisonCover = readyComparisonCover(comparison);
  const comparisonFields: Array<{ key: "title" | "developer" | "publisher" | "genre" | "players" | "releaseYear"; label: string }> = [
    { key: "title", label: "标题" }, { key: "developer", label: "开发商" }, { key: "publisher", label: "发行商" },
    { key: "genre", label: "类型" }, { key: "players", label: "玩家数" }, { key: "releaseYear", label: "发行年份" },
  ];

  return <AdminGameManagerView
    activeTags={activeTags} busy={busy} canonicalPlaylistSHA256={runtimeRevision.canonicalPlaylistSHA256} clientReady={clientReady}
    comparison={comparison} comparisonCover={comparisonCover} comparisonFields={comparisonFields} cover={cover}
    currentContent={content.current} currentDiscs={content.discs} currentFile={content.file} currentInstance={currentInstance}
    currentRuntime={runtimeRevision.revision} currentVariant={runtimeRevision.variant} disabled={disabled} draft={draft} error={error}
    game={game} gameTags={gameTags} metadataComplete={metadataComplete} metadataDirty={metadataDirty}
    moveTarget={moveTarget} moveTargets={moveTargets} multiDiscReplacementLimits={content.replacementLimits} notice={notice}
    onApplyCandidate={(candidate) => void applyCandidate(candidate)} onCloseComparison={() => setComparison(null)}
    onCloseMove={() => setPendingMove(null)} onConfirmMove={() => void confirmMove()}
    onDismissToast={() => { setNotice(""); setError(""); }}
    onDraft={(field, value) => setDraft((current) => ({ ...current, [field]: value }))}
    onGameTags={setGameTags} onMoveTarget={setMoveTarget} onOpenComparison={setComparison}
    onPreviewMove={(target) => void previewMove(target)} onRemove={(title) => void remove(title)}
    onRemoveVideo={() => void removeVideo()} onReplaceAsset={(file, kind, ordinal) => void replaceAsset(file, kind, ordinal)}
    onReplaceContent={replaceContent} onRescrape={() => void rescrape()} onSaveMetadata={(event) => void saveMetadata(event)}
    onSaveTags={() => void saveTags()} pendingMove={pendingMove} runtime={runtimeRevision.runtime} scrapeCandidates={scrapeCandidates}
    tagsDirty={tagsDirty} video={video}
  />;
}
