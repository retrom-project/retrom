"use client";

import Image from "next/image";
import { useState, type ChangeEvent, type FormEvent } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { StatusBadge } from "@/components/ui";
import { formatBytes, formatTime } from "@/lib/backend";
import { TagChips, TagPicker, type TagReference } from "@/components/tag-picker";
import { formatAdminGameTime, type runtimePresentation } from "./admin-game-library";
import { GameContentReplacementDialog } from "./game-content-replacement-dialog";
import type {
  AdminGame,
  Asset,
  ContentRevision,
  MetadataDraft,
  PendingMove,
  PlatformInstanceOption,
  ScrapeCandidate,
  Variant,
  VariantRevision,
} from "./admin-game-manager";

type ComparisonField = { key: "title" | "developer" | "publisher" | "genre" | "players" | "releaseYear"; label: string };

export type AdminGameManagerViewProps = {
  activeTags: TagReference[];
  busy: string | null;
  canonicalPlaylistSHA256: string;
  clientReady: boolean;
  comparison: ScrapeCandidate | null;
  comparisonCover: ScrapeCandidate["assets"][number] | null;
  comparisonFields: ComparisonField[];
  cover: Asset | undefined;
  currentContent: ContentRevision | undefined;
  currentDiscs: ContentRevision["files"];
  currentFile: string;
  currentInstance: PlatformInstanceOption | undefined;
  currentRuntime: VariantRevision | undefined;
  currentVariant: Variant | undefined;
  disabled: boolean;
  draft: MetadataDraft;
  error: string;
  game: AdminGame;
  gameTags: TagReference[];
  metadataComplete: boolean;
  metadataDirty: boolean;
  moveTarget: string;
  moveTargets: PlatformInstanceOption[];
  multiDiscReplacementLimits: { maxDiscs: number; maxTotalBytes: number } | null;
  notice: string;
  onApplyCandidate: (candidate: ScrapeCandidate) => void;
  onCloseComparison: () => void;
  onCloseMove: () => void;
  onConfirmMove: () => void;
  onDismissToast: () => void;
  onDraft: (field: keyof MetadataDraft, value: string) => void;
  onGameTags: (tags: TagReference[]) => void;
  onMoveTarget: (target: string) => void;
  onOpenComparison: (candidate: ScrapeCandidate) => void;
  onPreviewMove: (target: string) => void;
  onRemove: () => void;
  onRetryPayloadRelease: () => void;
  onRemoveVideo: () => void;
  onReplaceAsset: (file: File, kind: "COVER" | "VIDEO", ordinal: number) => void;
  onReplaceContent: (files: File[], mode: "STANDARD" | "MULTI_DISC_M3U_V1" | "RPG_MAKER_PROJECT_V1") => Promise<boolean>;
  onRescrape: () => void;
  onSaveMetadata: (event: FormEvent<HTMLFormElement>) => void;
  onSaveTags: () => void;
  pendingMove: PendingMove | null;
  runtime: ReturnType<typeof runtimePresentation>;
  scrapeCandidates: ScrapeCandidate[];
  tagsDirty: boolean;
  video: Asset | undefined;
};

function GameHero(props: Pick<AdminGameManagerViewProps, "cover" | "currentFile" | "currentInstance" | "currentRuntime" | "currentVariant" | "game" | "gameTags" | "metadataComplete" | "runtime">) {
  const { cover, currentInstance, currentVariant, game, gameTags, metadataComplete, runtime } = props;
  return <>
    <section className="admin-game-hero">
      <div className="admin-game-hero-cover">{cover ? <Image src={cover.url} alt={`${game.title} 封面`} fill sizes="102px" unoptimized /> : <span role="img" aria-label={`${game.title} 暂无封面`}>RETROM</span>}</div>
      <div className="admin-game-hero-copy"><h2>{game.title}</h2><p>{currentInstance?.platformName ?? game.platformId} · {game.platformInstance.name}{game.releaseYear ? ` · ${game.releaseYear}` : ""}{game.developer ? ` · ${game.developer}` : ""}</p><TagChips tags={gameTags} /><div><StatusBadge tone={game.status === "PUBLISHED" ? "good" : "bad"}>{game.status === "PUBLISHED" ? "用户可见" : "用户不可见"}</StatusBadge><StatusBadge tone={runtime.tone}>{runtime.label}</StatusBadge><StatusBadge tone={metadataComplete ? "info" : "warn"}>{metadataComplete ? "资料完整" : "资料待补充"}</StatusBadge></div></div>
      <div className="admin-game-hero-update"><span>最近更新</span><strong>{formatAdminGameTime(game.updatedAtMs, game.generatedAtMs)}</strong><small>{game.metadataRevisions.length} 个信息版本 · {game.contentRevisions.length} 个内容版本</small></div>
    </section>
    <section className="admin-game-overview" aria-label="游戏概览">
      <div><span>所属目录</span><strong>{game.platformInstance.name}</strong></div><div><span>推荐运行方式</span><strong>{currentInstance?.defaultCoreName ?? currentVariant?.coreName ?? "尚未配置"}</strong></div><div><span>当前游戏文件</span><strong>{props.currentFile}</strong></div><div><span>最后运行验证</span><strong>{formatTime(props.currentRuntime?.createdAtMs)}</strong></div><div><span>关联存档</span><strong>{game.deleteImpact.saveStateCount} 份</strong></div>
    </section>
  </>;
}

function GameTags(props: Pick<AdminGameManagerViewProps, "activeTags" | "busy" | "gameTags" | "onGameTags" | "onSaveTags" | "tagsDirty">) {
  return <section className="panel admin-game-tags" aria-labelledby="admin-game-tags-title"><div className="panel-head"><div><h2 id="admin-game-tags-title">游戏标签</h2><p>标签与发布信息分别保存；已删除游戏仍可调整标签。</p></div><button className="button" type="button" disabled={props.busy !== null || !props.tagsDirty} onClick={props.onSaveTags}>{props.busy === "tags" ? "正在更新…" : "更新标签"}</button></div><div className="panel-body"><TagPicker options={props.activeTags} selected={props.gameTags} onChange={props.onGameTags} disabled={props.busy !== null} /><div className="admin-game-empty-tags">{props.gameTags.length ? null : "未设置标签"}</div></div></section>;
}

function PublishInformation(props: Pick<AdminGameManagerViewProps, "currentInstance" | "disabled" | "draft" | "game" | "metadataDirty" | "onDraft" | "onSaveMetadata">) {
  const update = (field: keyof MetadataDraft) => (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => props.onDraft(field, event.target.value);
  return <section className="panel admin-game-publish" id="admin-game-basic"><div className="panel-head"><h2>发布信息</h2></div><form className="panel-body admin-game-publish-form" onSubmit={props.onSaveMetadata}>
    <label className="full">标题<input name="title" value={props.draft.title} onChange={update("title")} required maxLength={200} /></label>
    <label className="full">简介<textarea name="description" value={props.draft.description} onChange={update("description")} maxLength={10000} /></label>
    <label>开发商<input name="developer" value={props.draft.developer} onChange={update("developer")} maxLength={200} /></label><label>发行商<input name="publisher" value={props.draft.publisher} onChange={update("publisher")} maxLength={200} /></label>
    <label>类型<input name="genre" value={props.draft.genre} onChange={update("genre")} maxLength={200} /></label><label>玩家数<input name="players" type="number" min={1} max={64} value={props.draft.players} onChange={update("players")} /></label>
    <label>发行年份<input name="releaseYear" type="number" min={1950} value={props.draft.releaseYear} onChange={update("releaseYear")} /></label><label>平台<input value={props.currentInstance?.platformName ?? props.game.platformId} readOnly aria-readonly="true" /></label>
    <div className="admin-game-savebar full"><span>上次保存：{formatAdminGameTime(props.game.updatedAtMs, props.game.generatedAtMs)}</span><div><details><summary>查看版本历史</summary><div>{props.game.metadataRevisions.map((revision) => <small key={revision.id}>{revision.current ? "● 当前" : "○ 历史"} · {formatTime(revision.createdAtMs)} · {revision.sourceKind}</small>)}</div></details><button className="button" disabled={props.disabled || !props.metadataDirty}>保存新版本</button></div></div>
  </form></section>;
}

function MediaManager(props: Pick<AdminGameManagerViewProps, "clientReady" | "cover" | "disabled" | "game" | "onRemoveVideo" | "onReplaceAsset" | "video">) {
  const replace = (kind: "COVER" | "VIDEO") => (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (file) {props.onReplaceAsset(file, kind, 0);}
  };
  return <section className="panel admin-game-media" id="admin-game-media"><div className="panel-head"><h2>媒体</h2></div><div className="panel-body admin-game-media-grid">
    <article className="admin-game-cover-slot"><h3>封面</h3><div className="admin-game-cover-frame">{props.cover ? <Image src={props.cover.url} alt={`${props.game.title} 封面`} fill sizes="180px" unoptimized /> : <span>暂无封面</span>}</div><footer>{props.cover ? `${props.cover.widthPx}×${props.cover.heightPx}` : "建议使用 3:4 图片"}<input id="admin-cover-upload" hidden type="file" accept="image/png,image/jpeg,image/webp" disabled={props.disabled} onChange={replace("COVER")} /><label aria-disabled={props.disabled} htmlFor="admin-cover-upload">{props.cover ? "替换" : "添加"}</label></footer></article>
    <article className="admin-game-video-slot"><h3>视频预览</h3><div>{props.video ? <video src={props.video.url} controls playsInline preload="metadata" aria-label={`${props.game.title} 管理视频预览`} /> : <span><strong>暂无视频</strong><small>支持 MP4 / WebM，最大 256 MiB</small></span>}</div><footer><input id="admin-video-upload" hidden type="file" accept="video/mp4,video/webm" disabled={props.disabled || !props.clientReady} onChange={replace("VIDEO")} /><label aria-disabled={props.disabled || !props.clientReady} htmlFor="admin-video-upload">{props.video ? "替换" : "＋ 添加"}</label>{props.video ? <button type="button" disabled={props.disabled} onClick={props.onRemoveVideo}>移除</button> : null}</footer></article>
  </div></section>;
}

function DiscEvidence({ canonicalPlaylistSHA256, discs }: { canonicalPlaylistSHA256: string; discs: ContentRevision["files"] }) {
  if (!discs.length) {return null;}
  return <div className="admin-game-disc-evidence"><div><strong>当前盘序</strong><code title={canonicalPlaylistSHA256}>playlist SHA-256 · {canonicalPlaylistSHA256 || "不可用"}</code></div><ol>{discs.map((disc, index) => <li key={disc.sha256}><span>光盘 {index + 1}</span><strong>{disc.logicalName}</strong><small>{formatBytes(disc.sizeBytes)} · {disc.sha256.slice(0, 12)}…</small></li>)}</ol></div>;
}

function replacementContentPresentation(contentKind: string | undefined, multiDiscAvailable: boolean) {
  if (contentKind === "RPG_MAKER_PROJECT_V1") {
    return { key: "rpgmaker", label: "RPG Maker 项目", mode: "RPG_MAKER_PROJECT_V1" as const };
  }
  if (contentKind === "MULTI_DISC_M3U_V1") {
    return { key: "multi", label: "多盘 M3U", mode: "MULTI_DISC_M3U_V1" as const };
  }
  return { key: multiDiscAvailable ? "multi-capable" : "standard", label: "普通内容", mode: "STANDARD" as const };
}

function RuntimeManager(props: Pick<AdminGameManagerViewProps, "canonicalPlaylistSHA256" | "currentContent" | "currentDiscs" | "currentFile" | "currentInstance" | "currentRuntime" | "currentVariant" | "disabled" | "game" | "multiDiscReplacementLimits" | "onReplaceContent" | "runtime">) {
  const content = replacementContentPresentation(props.currentContent?.contentKind, props.multiDiscReplacementLimits !== null);
  return <section className="panel admin-game-runtime" id="admin-game-runtime"><div className="panel-head"><h2>游戏文件与运行环境</h2></div><div className="panel-body"><div className="admin-game-runtime-grid"><div><span>当前游戏文件</span><strong>{props.currentFile}</strong></div><div><span>内容类型</span><strong>{content.label}</strong></div><div><span>推荐运行方式</span><strong>{props.currentInstance?.defaultCoreName ?? props.currentVariant?.coreName ?? "尚未配置"}</strong></div><div><span>兼容状态</span><strong className={props.runtime.tone}>{props.runtime.label}</strong></div><div><span>最后验证</span><strong>{formatTime(props.currentRuntime?.createdAtMs)}</strong></div></div>
    <DiscEvidence canonicalPlaylistSHA256={props.canonicalPlaylistSHA256} discs={props.currentDiscs} />
    <div className="admin-game-runtime-note"><p>替换内容必须与当前 ROM 不同。新内容验证通过后才切换，并清理旧游戏文件、运行快照及其绑定存档；失败时不会改动当前内容。</p><GameContentReplacementDialog key={`${props.currentContent?.id ?? "none"}:${content.key}`} initialMode={content.mode} multiDiscLimits={props.multiDiscReplacementLimits} saveStateCount={props.game.deleteImpact.saveStateCount} disabled={props.disabled} onSubmit={props.onReplaceContent} /></div>
    <details className="admin-game-technical"><summary>技术详情</summary><div>{props.game.contentRevisions.map((revision) => <p key={revision.id}><strong>{revision.current ? "当前内容" : "历史内容"}</strong> · {revision.contentKind} · {formatTime(revision.createdAtMs)} · {revision.files.length ? revision.files.map((file) => file.logicalName).join("、") : "载荷已释放"}<code>{revision.id}</code></p>)}{props.game.variants.map((variant) => <p key={variant.id}><strong>{variant.coreName}</strong> · {variant.revisions.map((revision) => `${revision.current ? "当前" : "历史"} ${revision.status}`).join(" / ")}<code>{variant.id}</code></p>)}</div></details>
  </div></section>;
}

function ManagementActions(props: Pick<AdminGameManagerViewProps, "busy" | "disabled" | "moveTarget" | "moveTargets" | "onMoveTarget" | "onOpenComparison" | "onPreviewMove" | "onRescrape" | "scrapeCandidates">) {
  return <section className="panel admin-game-actions" id="admin-game-actions"><div className="panel-head"><h2>管理操作</h2></div><div className="panel-body admin-game-action-grid"><article><h3>重新获取游戏资料</h3><p>重新查询标题、简介与媒体候选；先并排比较基础信息、封面和完整说明，确认后才会应用。</p><button className="button secondary" type="button" disabled={props.disabled} onClick={props.onRescrape}>重新查找游戏信息</button>{props.scrapeCandidates.length ? <div className="admin-game-candidates">{props.scrapeCandidates.map((candidate) => <button type="button" key={candidate.candidateId} disabled={props.busy !== null} onClick={() => props.onOpenComparison(candidate)}><span><strong>{String(candidate.metadata.title ?? candidate.providerGameId)}</strong><small>{candidate.assets.some((asset) => asset.kind === "COVER" && asset.status === "READY") ? "包含封面候选" : "仅有文字候选"}</small></span><span>对比并选择</span></button>)}</div> : null}</article>
    <article><h3>移动到其他游戏目录</h3><p>移动前检查目标目录推荐运行方式与兼容性，不修改文件与存档。</p>{props.moveTargets.length ? <div><select aria-label="目标游戏目录" value={props.moveTarget} disabled={props.disabled} onChange={(event) => props.onMoveTarget(event.target.value)}><option value="">选择目标目录…</option>{props.moveTargets.map((item) => <option value={item.id} key={item.id}>{item.name} · 推荐 {item.defaultCoreName}</option>)}</select><button className="button secondary" type="button" disabled={props.disabled || !props.moveTarget} onClick={() => props.onPreviewMove(props.moveTarget)}>预览移动影响</button></div> : <small>没有其他同游戏平台目录可移动。</small>}</article></div></section>;
}

function RemoveGame({ disabled, game, onRemove }: Pick<AdminGameManagerViewProps, "disabled" | "game" | "onRemove">) {
  const [open, setOpen] = useState(false);
  const impact = game.deleteImpact;
  const sources = impact.sourceKinds.map((source) => ({ USER_UPLOAD: "用户上传", SERVER_SCAN: "本机扫描", ADMIN_REPLACE: "管理替换" })[source] ?? source).join("、") || "未知";
  return <section className="panel admin-game-remove"><div className="panel-head"><h2>危险操作</h2></div><div className="panel-body"><div><strong>永久删除游戏</strong><p>删除 ROM、媒体与存档 payload；原标题、审核及游玩历史保留为“已删除游戏”。</p></div><button className="button danger" type="button" disabled={disabled} onClick={() => setOpen(true)}>永久删除游戏</button></div><ConfirmDialog open={open} wide tone="danger" title={`永久删除“${game.title}”？`} description="此操作不可恢复；CAS 文件会在安全保留期后由存储回收任务清理。" confirmLabel="永久删除游戏" busy={disabled} onCancel={() => setOpen(false)} onConfirm={() => {onRemove(); setOpen(false);}}><ul><li>{impact.contentFileCount} 个内容文件、{impact.assetCount} 个媒体、{impact.saveStateCount} 份存档</li><li>预计独占回收 {formatBytes(Number(impact.exclusiveBytes))}；共享内容 {formatBytes(Number(impact.sharedBytes))} 继续保留</li><li>{impact.activeLaunchCount} 个活动游戏、{impact.activeNetplayCount} 个联机会话会立即终止</li><li>{impact.reviewEventCount} 条审核历史继续保留文字记录</li><li>来源：{sources}</li></ul></ConfirmDialog></section>;
}

function DeletedGameStatus({ game, onRetry }: { game: AdminGame; onRetry: () => void }) {
  const messages = { RELEASING: "游戏已删除，正在清理数据", RELEASED: "数据引用已清理，将由存储回收任务处理", FAILED: "数据清理失败", RETAINED: "等待数据清理" };
  return <section className="panel admin-game-remove"><div className="panel-head"><h2>已删除游戏</h2></div><div className="panel-body"><div><StatusBadge tone={game.payloadState === "FAILED" ? "bad" : game.payloadState === "RELEASED" ? "good" : "warn"}>{messages[game.payloadState]}</StatusBadge><p>游戏标题和结构化历史会继续保留；封面、视频、游戏内容和存档不可再访问。</p>{game.payloadLastErrorCode ? <code>{game.payloadLastErrorCode}</code> : null}</div>{game.payloadState === "FAILED" && game.payloadReleaseJobId ? <button className="button secondary" type="button" onClick={onRetry}>重试清理</button> : null}</div></section>;
}

function CandidateColumn({ candidate, comparisonCover, comparisonFields, cover, game }: { candidate: ScrapeCandidate; comparisonCover: AdminGameManagerViewProps["comparisonCover"]; comparisonFields: ComparisonField[]; cover: Asset | undefined; game: AdminGame }) {
  return <section className="metadata-compare-column" aria-label="最新候选"><header><strong>最新候选</strong><span>应用前预览</span></header><div className="metadata-compare-column-top"><div className="metadata-compare-fields">{comparisonFields.map((field) => { const currentValue = game[field.key]; const nextValue = candidate.metadata[field.key]; const same = String(currentValue ?? "") === String(nextValue ?? ""); return <div className={`compare-readonly ${same ? "is-same" : "is-changed"}`} key={field.key}><span>{field.label}</span><p>{String(nextValue ?? "") || "候选未提供，将保留当前值"}</p></div>; })}</div><div className={`metadata-compare-column-cover ${comparisonCover?.candidateAssetId === cover?.assetId ? "is-same" : "is-changed"}`}><span>封面</span>{comparisonCover ? <Image src={`/api/v1/admin/review-assets/${comparisonCover.candidateAssetId}`} alt="最新候选封面" width={comparisonCover.widthPx ?? 1} height={comparisonCover.heightPx ?? 1} unoptimized /> : <p>候选未提供，将保留当前封面</p>}</div></div><div className={`metadata-compare-column-description ${game.description === String(candidate.metadata.description ?? "") ? "is-same" : "is-changed"}`}><span>游戏说明</span><p>{String(candidate.metadata.description ?? "") || "候选未提供，将保留当前说明"}</p></div></section>;
}

function ComparisonDialog(props: Pick<AdminGameManagerViewProps, "busy" | "comparison" | "comparisonCover" | "comparisonFields" | "cover" | "game" | "onApplyCandidate" | "onCloseComparison">) {
  return <ConfirmDialog open={props.comparison !== null} wide title="对比最新游戏信息" description="左栏是当前信息，右栏是最新候选；每栏上方为基础信息与封面，下方为完整游戏说明。应用后会创建新的信息版本。" confirmLabel="应用这组信息" busy={props.busy === "candidate"} onCancel={props.onCloseComparison} onConfirm={() => { if (props.comparison) {props.onApplyCandidate(props.comparison);} }}>
    {props.comparison ? <div className="metadata-compare metadata-compare-columns"><section className="metadata-compare-column" aria-label="当前信息"><header><strong>当前信息</strong><span>只读</span></header><div className="metadata-compare-column-top"><div className="metadata-compare-fields">{props.comparisonFields.map((field) => <div className="compare-readonly" key={field.key}><span>{field.label}</span><p>{String(props.game[field.key] ?? "") || "未填写"}</p></div>)}</div><div className="metadata-compare-column-cover"><span>封面</span>{props.cover ? <Image src={props.cover.url} alt="当前游戏封面" width={props.cover.widthPx ?? 1} height={props.cover.heightPx ?? 1} unoptimized /> : <p>暂无封面</p>}</div></div><div className="metadata-compare-column-description"><span>游戏说明</span><p>{props.game.description || "未填写"}</p></div></section><CandidateColumn candidate={props.comparison} comparisonCover={props.comparisonCover} comparisonFields={props.comparisonFields} cover={props.cover} game={props.game} /></div> : null}
  </ConfirmDialog>;
}

function MoveDialog(props: Pick<AdminGameManagerViewProps, "busy" | "game" | "onCloseMove" | "onConfirmMove" | "pendingMove">) {
  const blocked = Boolean(props.pendingMove?.result.impact.blockerCodes.length);
  return <ConfirmDialog open={props.pendingMove !== null} title={`移动“${props.game.title}”到新目录？`} description={`目标目录：${props.pendingMove?.targetName ?? ""}`} confirmLabel="确认移动" tone={blocked ? "danger" : "default"} busy={props.busy !== null} onCancel={props.onCloseMove} onConfirm={props.onConfirmMove}><ul><li>新的推荐运行方式：{props.pendingMove?.result.impact.targetCoreId}</li><li>检查结果：{props.pendingMove?.result.impact.variantStatus}</li>{blocked ? <li>{props.pendingMove?.result.impact.blockerCodes.length} 项问题会暂时阻止运行</li> : <li>没有发现会阻止运行的问题</li>}<li>游戏文件、存档和历史版本不会移动或删除</li></ul></ConfirmDialog>;
}

export function AdminGameManagerView(props: AdminGameManagerViewProps) {
  if (props.game.status === "DELETED") {
    return <div className="admin-game-detail"><Toast toast={props.error ? { message: props.error, tone: "bad" } : props.notice ? { message: props.notice, tone: "good" } : null} onDismiss={props.onDismissToast} /><GameHero cover={undefined} currentFile="已清理" currentInstance={props.currentInstance} currentRuntime={props.currentRuntime} currentVariant={props.currentVariant} game={props.game} gameTags={props.gameTags} metadataComplete={props.metadataComplete} runtime={props.runtime} /><DeletedGameStatus game={props.game} onRetry={props.onRetryPayloadRelease} /></div>;
  }
  return <div className="admin-game-detail">
    <Toast toast={props.error ? { message: props.error, tone: "bad" } : props.notice ? { message: props.notice, tone: "good" } : null} onDismiss={props.onDismissToast} />
    <GameHero cover={props.cover} currentFile={props.currentFile} currentInstance={props.currentInstance} currentRuntime={props.currentRuntime} currentVariant={props.currentVariant} game={props.game} gameTags={props.gameTags} metadataComplete={props.metadataComplete} runtime={props.runtime} />
    <GameTags activeTags={props.activeTags} busy={props.busy} gameTags={props.gameTags} onGameTags={props.onGameTags} onSaveTags={props.onSaveTags} tagsDirty={props.tagsDirty} />
    <div className="admin-game-primary-grid"><PublishInformation currentInstance={props.currentInstance} disabled={props.disabled} draft={props.draft} game={props.game} metadataDirty={props.metadataDirty} onDraft={props.onDraft} onSaveMetadata={props.onSaveMetadata} /><MediaManager clientReady={props.clientReady} cover={props.cover} disabled={props.disabled} game={props.game} onRemoveVideo={props.onRemoveVideo} onReplaceAsset={props.onReplaceAsset} video={props.video} /></div>
    <RuntimeManager canonicalPlaylistSHA256={props.canonicalPlaylistSHA256} currentContent={props.currentContent} currentDiscs={props.currentDiscs} currentFile={props.currentFile} currentInstance={props.currentInstance} currentRuntime={props.currentRuntime} currentVariant={props.currentVariant} disabled={props.disabled} game={props.game} multiDiscReplacementLimits={props.multiDiscReplacementLimits} onReplaceContent={props.onReplaceContent} runtime={props.runtime} />
    <ManagementActions busy={props.busy} disabled={props.disabled} moveTarget={props.moveTarget} moveTargets={props.moveTargets} onMoveTarget={props.onMoveTarget} onOpenComparison={props.onOpenComparison} onPreviewMove={props.onPreviewMove} onRescrape={props.onRescrape} scrapeCandidates={props.scrapeCandidates} />
    <RemoveGame disabled={props.disabled} game={props.game} onRemove={props.onRemove} />
    <ComparisonDialog busy={props.busy} comparison={props.comparison} comparisonCover={props.comparisonCover} comparisonFields={props.comparisonFields} cover={props.cover} game={props.game} onApplyCandidate={props.onApplyCandidate} onCloseComparison={props.onCloseComparison} />
    <MoveDialog busy={props.busy} game={props.game} onCloseMove={props.onCloseMove} onConfirmMove={props.onConfirmMove} pendingMove={props.pendingMove} />
  </div>;
}
