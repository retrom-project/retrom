"use client";

import Image from "next/image";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { type Dispatch, type ReactNode, type SetStateAction, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast, type ToastMessage } from "@/components/flash-toast";
import { FeedbackBanner } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { formatBytes } from "@/lib/backend";
import { responseError } from "@/lib/upload";
import { ArcadeDependencyCard } from "./arcade-dependencies";
import type { ArcadeDependencies } from "./arcade-dependency-tree";
import { MultiDiscAttachmentDrawer } from "./multi-disc-attachment-drawer";
import { TagChips, TagPicker, type TagReference } from "@/components/tag-picker";
import { useReviewAttachments } from "./review-attachments";
import { useReviewCommands } from "./review-commands";
import { RPGValidationCard, useRPGReviewValidation } from "./review-rpg-validation";
import {
  activeAttachmentJobId, compareFields, initialDraftState, initialRuntimeState, reviewCoverPresentation,
  reviewReadiness, saveStateLabel, toPayload, withRPGMakerDraft,
  type Comparison, type CoverSelection, type DraftPayload, type MetadataForm, type PreviewAsset,
  type ReviewMultiDisc, type ReviewMultiDiscAttachment, type ReviewWorkspace,
} from "./review-actions-model";
export type { ReviewAsset, ReviewCandidate, ReviewMultiDisc, ReviewMultiDiscAttachment, ReviewScrapeRun, ReviewWorkspace, UploadedReviewAsset } from "./review-actions-model";

function AssetPreview({ asset, label }: { asset: PreviewAsset | null; label: string }) {
  return asset ? <Image src={asset.url} alt={label} width={asset.width} height={asset.height} unoptimized /> : <div className="asset-placeholder">暂无封面</div>;
}

export function ReviewActions({ review, activeTags = [], returnTo = "/admin/reviews", nextItemId = null, sourceDisplayName = "游戏文件", platformInstanceName = "游戏目录", children }: { review: ReviewWorkspace; activeTags?: TagReference[]; returnTo?: string; nextItemId?: string | null; sourceDisplayName?: string; platformInstanceName?: string; children?: ReactNode }) {
  const router = useRouter();
  const initial = initialDraftState(review);
  const initialRuntime = initialRuntimeState(review);
  const validationWasCurrent = initialRuntime.validationWasCurrent;
  const [form, setForm] = useState<MetadataForm>(initial.form);
  const [candidateId, setCandidateId] = useState<string | null>(initial.candidateId);
  const [cover, setCover] = useState<CoverSelection>(initial.cover);
  const [backgroundId, setBackgroundId] = useState<string | null>(review.selectedAssets.backgroundCandidateAssetId);
  const [screenshotIds, setScreenshotIds] = useState(review.selectedAssets.screenshotCandidateAssetIds);
  const [defaultDosEntry, setDefaultDosEntry] = useState<string | null>(review.defaultDosEntry);
  const [tags, setTags] = useState<TagReference[]>(initial.tags);
  const [candidates, setCandidates] = useState(initial.candidates);
  const [uploadedAssets, setUploadedAssets] = useState(initial.uploadedAssets);
  const [comparison, setComparison] = useState<Comparison | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [saveState, setSaveState] = useState<"saved" | "pending" | "saving" | "error">(initial.saveState);
  const [notice, setNotice] = useState(initial.notice);
  const [jobProgress, setJobProgress] = useState("");
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const [validationCurrent, setValidationCurrent] = useState(validationWasCurrent);
  const [validationStale, setValidationStale] = useState(initialRuntime.validationStale);
  const [currentValidation, setCurrentValidation] = useState(initialRuntime.validation);
  const [effectiveSourceSnapshotId, setEffectiveSourceSnapshotId] = useState(initialRuntime.effectiveSourceSnapshotId);
  const [arcadeDependencies, setArcadeDependencies] = useState(initialRuntime.arcadeDependencies);
  const [multiDisc, setMultiDisc] = useState(initialRuntime.multiDisc);
  const [serverCanApprove, setServerCanApprove] = useState(initialRuntime.canApprove);
  const [runtimeScreenshot, setRuntimeScreenshot] = useState(initialRuntime.runtimeScreenshot);
  const [rpgMaker, setRPGMaker] = useState(review.rpgMaker ?? null);
  const versionRef = useRef(review.version);
  const validationRefreshRequestedRef = useRef(false);
  const latestKeyRef = useRef("");
  const saveQueueRef = useRef<Promise<boolean>>(Promise.resolve(true));
  const serverPayload = withRPGMakerDraft(toPayload(initial.baseMetadata, review.selectedCandidateId, { candidateId: review.selectedAssets.coverCandidateAssetId, uploadedId: initial.cover.uploadedId }, review.selectedAssets.backgroundCandidateAssetId, review.selectedAssets.screenshotCandidateAssetIds, review.defaultDosEntry, initial.tags), review.rpgMaker);
  const lastSavedKeyRef = useRef(JSON.stringify(serverPayload));
  const draftPayload = useMemo(() => withRPGMakerDraft(toPayload(form, candidateId, cover, backgroundId, screenshotIds, defaultDosEntry, tags), rpgMaker), [form, candidateId, cover, backgroundId, screenshotIds, defaultDosEntry, tags, rpgMaker]);
  const draftKey = useMemo(() => JSON.stringify(draftPayload), [draftPayload]);
  const latestPayloadRef = useRef(draftPayload);
  const validationStatus = currentValidation ? currentValidation.status : null;

  const refreshReview = useCallback(async () => {
    const response = await fetch(`/api/v1/admin/reviews/${review.itemId}`, { cache: "no-store" });
    if (!response.ok) {throw new Error(await responseError(response, "校验完成，但无法读取最新审核状态"));}
    const updated = await response.json() as ReviewWorkspace;
    versionRef.current = updated.version;
    setCurrentValidation(updated.validation);
    setValidationCurrent(updated.validation?.current ?? false);
    setValidationStale(updated.validationStale ?? false);
    setEffectiveSourceSnapshotId(updated.effectiveSourceSnapshotId ?? "");
    setArcadeDependencies(updated.arcadeDependencies ?? null);
    setMultiDisc(updated.multiDisc ?? null);
    setServerCanApprove(updated.canApprove ?? (updated.validation?.current === true && updated.validation.status === "READY"));
    setCandidates(updated.candidates);
    setUploadedAssets(updated.uploadedAssets ?? []);
    setRuntimeScreenshot(updated.runtimeScreenshot ?? null);
    setRPGMaker(updated.rpgMaker ?? null);
    setTags(updated.tags ?? []);
    router.refresh();
    return updated;
  }, [review.itemId, router, setArcadeDependencies, setCandidates, setCurrentValidation, setEffectiveSourceSnapshotId, setMultiDisc, setRPGMaker, setRuntimeScreenshot, setServerCanApprove, setTags, setUploadedAssets, setValidationCurrent, setValidationStale]);

  useEffect(() => {
    const onPreviewMessage = (event: MessageEvent<unknown>) => {
      if (event.origin !== window.location.origin || !event.data || typeof event.data !== "object") {return;}
      const message = event.data as { type?: string; importItemId?: string };
      if (message.type !== "retrom-review-screenshot" || message.importItemId !== review.itemId) {return;}
      void refreshReview().then(() => {
        setToast({ message: "已更新第 5 秒运行截图", tone: "good" });
      }).catch((error: unknown) => {
        setToast({ message: error instanceof Error ? error.message : "截图已生成，但审核页刷新失败", tone: "warn" });
      });
    };
    window.addEventListener("message", onPreviewMessage);
    return () => window.removeEventListener("message", onPreviewMessage);
  }, [refreshReview, review.itemId]);

  const enqueueSave = useCallback((key: string, payload: DraftPayload, force = false) => {
    saveQueueRef.current = saveQueueRef.current.catch(() => false).then(async () => {
      if (!force && lastSavedKeyRef.current === key) {return true;}
      if (latestKeyRef.current === key) {setSaveState("saving");}
      try {
        const response = await fetch(`/api/v1/admin/reviews/${review.itemId}`, { method: "PATCH", credentials: "same-origin", keepalive: true, headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${versionRef.current}"` }), body: JSON.stringify(payload) });
        if (!response.ok) {throw new Error(await responseError(response, "实时保存失败：字段、来源或版本已经变化"));}
        const result = await response.json() as { version: number };
        versionRef.current = result.version;
        lastSavedKeyRef.current = key;
        if (latestKeyRef.current === key) {setSaveState("saved");}
        return true;
      } catch (caught) {
        const message = caught instanceof Error ? caught.message : "实时保存失败";
        if (latestKeyRef.current === key) { setSaveState("error"); setToast({ message, tone: "bad" }); }
        return false;
      }
    });
    return saveQueueRef.current;
  }, [review.itemId, setSaveState, setToast]);

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
    if (validationStatus !== "READY" || validationWasCurrent || validationRefreshRequestedRef.current) {return;}
    validationRefreshRequestedRef.current = true;
    setSaveState("pending");
    void enqueueSave(draftKey, draftPayload, true).then(async (saved) => {
      if (!saved) {return;}
      try { await refreshReview(); }
      catch (caught) { setToast({ message: caught instanceof Error ? caught.message : "无法读取更新后的运行检查", tone: "bad" }); }
    });
  }, [draftKey, draftPayload, enqueueSave, refreshReview, validationStatus, validationWasCurrent]);

  useEffect(() => {
    if (!notice) {return;}
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

  const rpgValidation = useRPGReviewValidation({
    reviewId: review.itemId, versionRef, rpgMaker, setRPGMaker, flushDraft, run, setNotice, setToast,
  });

  const attachments = useReviewAttachments({
    reviewId: review.itemId,
    versionRef,
    currentValidationId: currentValidation?.id,
    effectiveSourceSnapshotId,
    activeParentJobId: activeAttachmentJobId(arcadeDependencies),
    multiDisc,
    setMultiDisc,
    refreshReview,
    flushDraft,
    run,
    setToast,
  });

  const commands = useReviewCommands({
    review, returnTo, nextItemId, versionRef, latestPayloadRef, validationRefreshRequestedRef,
    draftKey, draftPayload, form, cover, uploadedAssets, comparison, refreshReview, flushDraft,
    enqueueSave, run, setJobProgress, setNotice, setToast, setCandidates, setUploadedAssets,
    setComparison, setForm, setCandidateId, setCover, setBackgroundId, setScreenshotIds,
  });

  const covers = reviewCoverPresentation(review, candidates, uploadedAssets, cover, comparison);
  const readiness = reviewReadiness(validationStatus, validationCurrent, runtimeScreenshot, effectiveCanApprove(rpgMaker, serverCanApprove), arcadeDependencies?.activeAttachment?.state, multiDisc?.activeAttachment?.state, Boolean(rpgMaker));

  return <ReviewActionsView model={{ review, activeTags, sourceDisplayName, platformInstanceName, children, form, updateField, candidateId, cover, setCover, defaultDosEntry, setDefaultDosEntry, tags, setTags, busy, saveState, notice, jobProgress, validationStatus, validationStale, runtimeScreenshot, rpgMaker, setRPGMaker, rpgValidation, sourceCover: covers.source, selectedCover: covers.selected, currentCompareCover: covers.currentComparison, nextCompareCover: covers.nextComparison, comparison, setComparison, arcadeDependencies, multiDisc, ...readiness, saveLabel: saveStateLabel(saveState), attachments, commands, toast, setToast }} />;
}

function effectiveCanApprove(rpgMaker: ReviewWorkspace["rpgMaker"], serverCanApprove: boolean) {
  if (!rpgMaker) {return serverCanApprove;}
  return rpgMaker.runtimeValidationCurrent && Boolean(rpgMaker.runtimeValidation?.launchId);
}

type ReviewViewModel = {
  review: ReviewWorkspace; activeTags: TagReference[]; sourceDisplayName: string; platformInstanceName: string; children?: ReactNode;
  form: MetadataForm; updateField: (key: keyof MetadataForm, value: string) => void; candidateId: string | null;
  cover: CoverSelection; setCover: Dispatch<SetStateAction<CoverSelection>>; defaultDosEntry: string | null; setDefaultDosEntry: Dispatch<SetStateAction<string | null>>;
  tags: TagReference[]; setTags: Dispatch<SetStateAction<TagReference[]>>; busy: string | null; saveState: "saved" | "pending" | "saving" | "error";
  notice: string; jobProgress: string; validationStatus: string | null; validationStale: boolean; runtimeScreenshot: ReviewWorkspace["runtimeScreenshot"];
  rpgMaker: NonNullable<ReviewWorkspace["rpgMaker"]> | null; setRPGMaker: Dispatch<SetStateAction<NonNullable<ReviewWorkspace["rpgMaker"]> | null>>; rpgValidation: ReturnType<typeof useRPGReviewValidation>;
  sourceCover: PreviewAsset | null; selectedCover: PreviewAsset | null; currentCompareCover: PreviewAsset | null; nextCompareCover: PreviewAsset | null;
  comparison: Comparison | null; setComparison: Dispatch<SetStateAction<Comparison | null>>; arcadeDependencies: ArcadeDependencies | null; multiDisc: ReviewMultiDisc | null;
  parentAttachmentActive: boolean; multiDiscAttachmentActive: boolean; validationReady: boolean; screenshotOverride: boolean; publishReady: boolean; saveLabel: string;
  attachments: ReturnType<typeof useReviewAttachments>; commands: ReturnType<typeof useReviewCommands>; toast: ToastMessage | null; setToast: Dispatch<SetStateAction<ToastMessage | null>>;
};

function ReviewActionsView({ model }: { model: ReviewViewModel }) {
  return <div className="review-workflow-detail">
    <ReviewStepper />
    <div className="review-workflow-top"><ReviewSummary model={model} /><ReviewDecision model={model} /></div>
    <ReviewFeedback model={model} />
    <ReviewColumns model={model} />
    <ComparisonDialog model={model} />
    <DuplicateDialog model={model} />
    <Toast toast={model.toast} onDismiss={() => model.setToast(null)} />
  </div>;
}

function ReviewStepper() {
  return <nav className="review-mobile-stepper" aria-label="审核步骤"><a href="#review-step-source"><span>1</span>来源与依赖</a><a href="#review-step-runtime"><span>2</span>运行检查</a><a href="#review-step-publish"><span>3</span>发布信息</a><a href="#review-step-decision"><span>4</span>审核决定</a></nav>;
}

function serverSourceName(model: ReviewViewModel) {
  return model.review.sourceMedia?.sourceKind === "EMULATIONSTATION" ? "EmulationStation" : "Pegasus";
}

function reviewMetadataLabel(model: ReviewViewModel) {
  if (model.candidateId) {return "已找到游戏信息";}
  if (model.review.sourceMedia?.sourceKind === "EMULATIONSTATION") {return "已读取 Gamelist 信息";}
  if (model.review.sourceMedia) {return "已读取 Pegasus 信息";}
  return "未找到游戏信息";
}

function ReviewSummary({ model }: { model: ReviewViewModel }) {
  const sourceName = serverSourceName(model);
  const source = model.review.sourceMedia ? `${sourceName} · ${model.review.sourceMedia.sourceLabel ?? model.sourceDisplayName}` : model.sourceDisplayName;
  const validationLabel = reviewValidationLabel(model);
  const metadataLabel = reviewMetadataLabel(model);
  return <section id="review-step-source" className="review-workflow-summary-card">
    <div className="review-workflow-summary-copy"><StatusPill tone="info">来源：{source}</StatusPill><h2>{model.form.title || model.sourceDisplayName}</h2><p>目标目录：{model.platformInstanceName}</p><TagChips tags={model.tags} /><div className="review-workflow-summary-pills"><StatusPill tone="info">已接收来源文件</StatusPill><StatusPill tone={model.publishReady ? "good" : "warn"}>{validationLabel}</StatusPill><StatusPill tone={model.candidateId || model.review.sourceMedia ? "info" : "warn"}>{metadataLabel}</StatusPill></div></div>
    <RuntimeScreenshot model={model} />
  </section>;
}

function reviewValidationLabel(model: ReviewViewModel) {
  if (model.rpgMaker) {return effectiveCanApprove(model.rpgMaker, false) ? "已启动游戏，可发布" : "等待启动游戏";}
  if (model.validationStale) {return "Runtime 待重检";}
  if (model.validationReady) {return "运行检查通过";}
  if (model.screenshotOverride) {return "已取得运行截图";}
  return model.validationStatus === "READY" ? "运行检查更新中" : "运行检查未通过";
}

function RuntimeScreenshot({ model }: { model: ReviewViewModel }) {
  if (model.rpgMaker) {
    const evidence = model.rpgMaker.runtimeValidation?.checkpointRoundTrip;
    if (!evidence?.screenshotUrl) {return <aside className="review-runtime-screenshot" aria-label="恢复位置证据截图"><span>恢复位置证据</span><div><strong>等待不同 Launch 恢复</strong><small>截图不能代替地图、坐标和状态的逐字段核对</small></div></aside>;}
    return <aside className="review-runtime-screenshot" aria-label="恢复位置证据截图"><span>恢复位置证据</span><Image src={evidence.screenshotUrl} alt={`${model.form.title || model.sourceDisplayName} 恢复到保存位置 B 的证据`} width={640} height={480} unoptimized /></aside>;
  }
  if (!model.runtimeScreenshot) {return <aside className="review-runtime-screenshot" aria-label="第 5 秒运行截图"><span>第 5 秒运行截图</span><div><strong>{model.validationReady ? "等待生成" : "等待运行截图"}</strong><small>运行游戏后在第 5 秒自动截取，可作为管理员放行依据</small></div></aside>;}
  return <aside className="review-runtime-screenshot" aria-label="第 5 秒运行截图"><span>第 5 秒运行截图</span><Image src={model.runtimeScreenshot.url} alt={`${model.form.title || model.sourceDisplayName} 的第 5 秒运行截图`} width={model.runtimeScreenshot.widthPx} height={model.runtimeScreenshot.heightPx} unoptimized /></aside>;
}

function ReviewDecision({ model }: { model: ReviewViewModel }) {
  if (model.rpgMaker) {return <RPGReviewDecision model={model} />;}
  const message = reviewDecisionMessage(model);
  const descriptionId = model.validationStale ? "review-runtime-refresh-required" : undefined;
  return <aside id="review-step-decision" className="review-workflow-decision"><h2>审核决定</h2><p id={descriptionId} className={model.validationStale ? "is-runtime-refresh-required" : undefined} title={model.validationStale ? message : undefined}>{message}</p><div className="review-workflow-save"><span>实时保存</span><strong className={`autosave-state ${model.saveState}`}><i aria-hidden="true" /><span>{model.saveLabel}</span></strong></div><div className="review-workflow-preview-actions"><button type="button" className="button secondary review-revalidate" aria-busy={model.busy === "重新运行检查"} disabled={model.busy !== null || model.saveState === "error"} onClick={() => void model.commands.revalidate()}>{model.busy === "重新运行检查" ? "正在检查…" : "重新运行检查"}</button><button type="button" className="button secondary review-launch-preview" aria-busy={model.busy === "运行游戏"} aria-describedby={descriptionId} title={model.validationStale ? message : undefined} disabled={model.validationStale || model.busy !== null || model.saveState === "error"} onClick={() => void model.commands.launchPreview()}>{model.busy === "运行游戏" ? "正在准备…" : "运行游戏"}</button></div><div className="review-workflow-decision-actions"><button type="button" className="button secondary" disabled={model.busy !== null} onClick={() => void model.commands.discard()}>{model.busy === "丢弃" ? "正在丢弃…" : "丢弃条目"}</button><button type="button" className="button" aria-busy={model.busy === "发布"} disabled={model.busy !== null || !model.publishReady || model.saveState === "error"} onClick={() => void model.commands.approve()}>{model.busy === "发布" ? <><i className="button-spinner" aria-hidden="true" />正在发布…</> : "通过并发布"}</button></div></aside>;
}

function reviewDecisionMessage(model: ReviewViewModel) {
  if (model.validationStale) {return "Runtime 已更新，请先重新运行检查。";}
  if (model.validationReady) {return "运行检查已经通过，可以发布。";}
  if (model.screenshotOverride) {return "已取得第 5 秒运行截图，可由管理员确认后发布。";}
  return "可先运行游戏；取得第 5 秒截图后允许人工放行。";
}

function RPGReviewDecision({ model }: { model: ReviewViewModel }) {
  const validation = model.rpgValidation.validation;
  return <aside id="review-step-decision" className="review-workflow-decision"><h2>审核决定</h2><p>{rpgDecisionMessage(model.rpgMaker, validation)}</p><div className="review-workflow-save"><span>实时保存</span><strong className={`autosave-state ${model.saveState}`}><i aria-hidden="true" /><span>{model.saveLabel}</span></strong></div><RPGValidationActions model={model} /><div className="review-workflow-decision-actions"><button type="button" className="button secondary" disabled={model.busy !== null} onClick={() => void model.commands.discard()}>{model.busy === "丢弃" ? "正在丢弃…" : "丢弃条目"}</button><button type="button" className="button" disabled={model.busy !== null || !model.publishReady || model.saveState === "error"} onClick={() => void model.commands.approve()}>{model.busy === "发布" ? "正在发布…" : "通过并发布"}</button></div></aside>;
}

function rpgDecisionMessage(rpgMaker: ReviewViewModel["rpgMaker"], validation: ReviewViewModel["rpgValidation"]["validation"]) {
  if (validation && !rpgMaker?.runtimeValidationCurrent) {return "当前绑定已变化；历史验证不能发布，请重新运行游戏。";}
  if (validation?.launchId) {return "游戏 Launch 已创建；确认可运行后即可发布，高级恢复验证为可选。";}
  return "请先运行一次游戏，随后即可通过并发布。";
}

function RPGValidationActions({ model }: { model: ReviewViewModel }) {
  return <div className="review-workflow-preview-actions">
    {model.rpgValidation.canRestore ? <button type="button" className="button secondary review-launch-preview" disabled={model.busy !== null} onClick={() => void model.rpgValidation.restore()}>验证恢复</button> : null}
    {model.rpgValidation.canCreate ? <button type="button" className="button secondary review-launch-preview" disabled={model.busy !== null || model.saveState === "error"} onClick={() => void model.rpgValidation.create()}>运行游戏</button> : null}
    {model.rpgValidation.canDecide ? <><button type="button" className="button secondary" disabled={model.busy !== null} onClick={() => void model.rpgValidation.decide("FAIL")}>判定失败</button><button type="button" className="button" disabled={model.busy !== null} onClick={() => void model.rpgValidation.decide("PASS")}>确认验证通过</button></> : null}
  </div>;
}

function ReviewFeedback({ model }: { model: ReviewViewModel }) {
  return <>{model.notice ? <div className="review-workflow-feedback"><FeedbackBanner tone="info">{model.notice}</FeedbackBanner></div> : null}{model.review.duplicateGames?.length ? <div className="review-workflow-feedback"><FeedbackBanner tone="info">相同游戏文件已经关联到 {model.review.duplicateGames.map((game, index) => <span key={game.gameId}>{index ? "、" : ""}<Link href={`/games/${game.gameId}`}>{game.title}</Link></span>)}。仍可发布为新游戏，但发布时需要二次确认。</FeedbackBanner></div> : null}</>;
}

function ReviewColumns({ model }: { model: ReviewViewModel }) {
  return <div className="review-workflow-columns"><RuntimeDependencies model={model} /><MetadataEditor model={model} /></div>;
}

function RuntimeDependencies({ model }: { model: ReviewViewModel }) {
  return <div id="review-step-runtime" className="review-workflow-left">{model.rpgMaker ? <RPGValidationCard value={model.rpgMaker} disabled={model.busy !== null} onChange={(next) => model.setRPGMaker(next)} /> : null}{model.children}{model.multiDisc ? <MultiDiscReviewCard value={model.multiDisc} disabled={model.busy !== null || model.multiDiscAttachmentActive} progress={model.attachments.multiDiscProgress} onAttach={model.attachments.attachMissingDiscs} onRetry={model.attachments.retryMultiDisc} /> : null}{model.arcadeDependencies ? <ArcadeDependencyCard value={model.arcadeDependencies} disabled={model.busy !== null || model.parentAttachmentActive} progress={model.attachments.parentProgress} onAttach={model.attachments.attachParent} onRetry={model.attachments.retryParent} /> : null}</div>;
}

function MetadataEditor({ model }: { model: ReviewViewModel }) {
  return <section id="review-step-publish" className="panel review-workflow-metadata"><MetadataHeader model={model} /><div className="panel-body review-workflow-editor"><div className="review-workflow-publish-layout"><MetadataFields model={model} /><CoverEditor model={model} /></div></div></section>;
}

function MetadataHeader({ model }: { model: ReviewViewModel }) {
  return <div className="panel-head"><div><h2>② 发布成什么？</h2><p>核对标题、简介、封面和标签；修改会实时保存。</p></div><div className="review-workflow-query-actions">{model.jobProgress ? <p className="scrape-live" role="status"><i className="button-spinner" aria-hidden="true" />正在查询游戏信息：{model.jobProgress}</p> : null}<button type="button" className="button secondary" disabled={model.busy !== null} aria-busy={model.busy === "重新查询 Hasheous"} onClick={() => void model.commands.rescrape("HASHEOUS")}>{model.busy === "重新查询 Hasheous" ? <><i className="button-spinner" aria-hidden="true" />查询中…</> : "重新查询游戏信息"}</button></div></div>;
}

function MetadataFields({ model }: { model: ReviewViewModel }) {
  return <div className="form-grid review-workflow-metadata-fields"><label className="field full">标题<input value={model.form.title} onChange={(event) => model.updateField("title", event.target.value)} maxLength={200} /></label><label className="field full">简介<textarea value={model.form.description} onChange={(event) => model.updateField("description", event.target.value)} maxLength={10000} /></label><label className="field review-workflow-field-half">开发商<input value={model.form.developer} onChange={(event) => model.updateField("developer", event.target.value)} maxLength={200} /></label><label className="field review-workflow-field-half">发行商<input value={model.form.publisher} onChange={(event) => model.updateField("publisher", event.target.value)} maxLength={200} /></label><label className="field review-workflow-field-third">类型<input value={model.form.genre} onChange={(event) => model.updateField("genre", event.target.value)} maxLength={200} /></label><label className="field review-workflow-field-third">玩家数<input type="number" min={1} max={64} value={model.form.players} onChange={(event) => model.updateField("players", event.target.value)} /></label><label className="field review-workflow-field-third">发行年份<input type="number" min={1950} value={model.form.releaseYear} onChange={(event) => model.updateField("releaseYear", event.target.value)} /></label>{model.review.dosEntries.length ? <label className="field full">DOS 默认程序<select value={model.defaultDosEntry ?? ""} onChange={(event) => model.setDefaultDosEntry(event.target.value || null)}><option value="">打开 DOSBox 程序菜单</option>{model.review.dosEntries.map((entry) => <option key={entry.path} value={entry.path} disabled={!entry.enabled}>{entry.originalPath}{entry.directLaunchSafe ? "" : " · 仅程序菜单"}</option>)}</select></label> : null}<div className="field full review-tag-editor"><TagPicker label="游戏标签" options={model.activeTags} selected={model.tags} onChange={model.setTags} disabled={model.busy !== null} description="与其他发布信息一起实时保存；通过审核后会原子复制到游戏。" /></div></div>;
}

function CoverEditor({ model }: { model: ReviewViewModel }) {
  const upload = (file: File | undefined) => {if (file) {void model.commands.uploadCover(file, "current");}};
  const sourceName = model.review.sourceMedia?.sourceKind === "EMULATIONSTATION" ? "EmulationStation" : "Pegasus";
  return <aside className="review-cover-panel review-workflow-cover-side"><span className="field-label">当前封面</span><label className="review-cover-upload" title="点击上传替换封面"><AssetPreview asset={model.selectedCover} label="当前选择的游戏封面" /><span>点击图片上传替换</span><input type="file" accept="image/png,image/jpeg,image/webp" disabled={model.busy !== null} onChange={(event) => {upload(event.target.files?.[0]); event.currentTarget.value = "";}} /></label>{model.cover.candidateId || model.cover.uploadedId ? <button type="button" className="button secondary compact" onClick={() => model.setCover({ candidateId: null, uploadedId: null })}>{model.sourceCover ? `恢复 ${sourceName} 封面` : "移除封面"}</button> : null}{model.review.sourceMedia?.videoUrl ? <div className="review-source-video"><span className="field-label">{sourceName} 视频预览</span><video controls preload="metadata" src={model.review.sourceMedia.videoUrl}>浏览器无法播放这段视频。</video><small>通过审核后会随游戏一并发布。</small></div> : null}</aside>;
}

function ComparisonDialog({ model }: { model: ReviewViewModel }) {
  return <ConfirmDialog open={model.comparison !== null} wide title="对比最新查询结果" description="左栏是当前信息，右栏是最新结果；每栏上方为基础信息与封面，下方为完整简介。红色表示内容不同，绿色表示一致；右栏可编辑。" confirmLabel="应用" busy={model.busy !== null} onCancel={() => model.setComparison(null)} onConfirm={model.commands.applyComparison}>{model.comparison ? <ComparisonColumns model={model} comparison={model.comparison} /> : null}</ConfirmDialog>;
}

function ComparisonColumns({ model, comparison }: { model: ReviewViewModel; comparison: Comparison }) {
  return <div className="metadata-compare metadata-compare-columns"><CurrentComparison comparison={comparison} cover={model.currentCompareCover} /><NextComparison model={model} comparison={comparison} /></div>;
}

function CurrentComparison({ comparison, cover }: { comparison: Comparison; cover: PreviewAsset | null }) {
  return <section className="metadata-compare-column" aria-label="当前信息"><header><strong>当前信息</strong><span>只读</span></header><div className="metadata-compare-column-top"><div className="metadata-compare-fields">{compareFields.filter((field) => !field.multiline).map((field) => <div className="compare-readonly" key={field.key}><span>{field.label}</span><p>{comparison.current[field.key] || "未填写"}</p></div>)}</div><div className="metadata-compare-column-cover"><span>封面</span><AssetPreview asset={cover} label="当前游戏封面" /></div></div><div className="metadata-compare-column-description"><span>游戏说明</span><p>{comparison.current.description || "未填写"}</p></div></section>;
}

function NextComparison({ model, comparison }: { model: ReviewViewModel; comparison: Comparison }) {
  const setNext = (key: keyof MetadataForm, value: string) => model.setComparison((current) => current ? { ...current, next: { ...current.next, [key]: value } } : null);
  const upload = (file: File | undefined) => {if (file) {void model.commands.uploadCover(file, "comparison");}};
  const sameCover = comparison.currentCover.candidateId === comparison.nextCover.candidateId && comparison.currentCover.uploadedId === comparison.nextCover.uploadedId;
  return <section className="metadata-compare-column" aria-label="最新信息"><header><strong>最新信息</strong><span>可编辑</span></header><div className="metadata-compare-column-top"><div className="metadata-compare-fields">{compareFields.filter((field) => !field.multiline).map((field) => <label className={`compare-field ${comparison.current[field.key] === comparison.next[field.key] ? "is-same" : "is-changed"}`} key={field.key}><span>{field.label}</span><input aria-label={field.label} type={field.type ?? "text"} value={comparison.next[field.key]} onChange={(event) => setNext(field.key, event.target.value)} /></label>)}</div><div className={`metadata-compare-column-cover ${sameCover ? "is-same" : "is-changed"}`}><span>封面</span><label className="review-cover-upload"><AssetPreview asset={model.nextCompareCover} label="最新查询封面" /><span>点击图片上传替换</span><input type="file" accept="image/png,image/jpeg,image/webp" disabled={model.busy !== null} onChange={(event) => {upload(event.target.files?.[0]); event.currentTarget.value = "";}} /></label>{comparison.nextCover.candidateId || comparison.nextCover.uploadedId ? <button type="button" className="button secondary compact" onClick={() => model.setComparison((current) => current ? { ...current, nextCover: { candidateId: null, uploadedId: null } } : null)}>不使用新封面</button> : null}</div></div><label className={`metadata-compare-column-description ${comparison.current.description === comparison.next.description ? "is-same" : "is-changed"}`}><span>游戏说明（可编辑）</span><textarea aria-label="简介" value={comparison.next.description} onChange={(event) => setNext("description", event.target.value)} /></label></section>;
}

function DuplicateDialog({ model }: { model: ReviewViewModel }) {
  const duplicates = model.commands.duplicateConfirmation;
  return <ConfirmDialog open={duplicates !== null} title="仍然发布为新游戏？" description="相同游戏文件已经存在。继续发布会创建另一个游戏条目，可能造成重复游戏。" confirmLabel="仍然发布为新游戏" tone="danger" busy={model.busy === "发布"} onCancel={() => model.commands.setDuplicateConfirmation(null)} onConfirm={() => void model.commands.confirmDuplicatePublish()}>{duplicates ? <ul>{duplicates.map((game) => <li key={game.gameId}><Link href={`/games/${game.gameId}`}>{game.title}</Link><span> · {game.platformInstanceName}</span></li>)}</ul> : null}</ConfirmDialog>;
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
    <MultiDiscHeader value={value} maxDiscs={maxDiscs} maxTotalBytes={maxTotalBytes} />
    <div className="panel-body">
      <ol className="review-multidisc-list">{value.entries.map((entry) => <MultiDiscEntry key={entry.discIndex} entry={entry} />)}</ol>
      <MultiDiscFeedback value={value} validating={validating} progress={progress} />
      <MultiDiscActions value={value} latest={latest} disabled={disabled} onRetry={onRetry} onOpen={() => setDrawerOpen(true)} />
    </div>
    <MultiDiscAttachmentDrawer open={drawerOpen} missingReferences={value.missingReferences} presentBytes={value.totalPresentBytes} maxTotalBytes={maxTotalBytes} busy={disabled} progress={progress} onClose={() => setDrawerOpen(false)} onSubmit={onAttach} />
  </section>;
}

function MultiDiscHeader({ value, maxDiscs, maxTotalBytes }: { value: ReviewMultiDisc; maxDiscs: number; maxTotalBytes: number }) {
  return <div className="panel-head"><div><h2 id="review-multidisc-title">多盘内容</h2><p>{value.playlist.name} · {formatBytes(value.playlist.sizeBytes)} · SHA-256 {value.playlist.sha256.slice(0, 12)}…</p><small>{value.discCount} / {maxDiscs} 张光盘 · 已接收 {formatBytes(value.totalPresentBytes)} / 上限 {formatBytes(maxTotalBytes)}</small></div><StatusPill tone={value.missingDiscCount ? "warn" : "good"}>{value.missingDiscCount ? `缺少 ${value.missingDiscCount} 张` : "盘序完整"}</StatusPill></div>;
}

function MultiDiscEntry({ entry }: { entry: ReviewMultiDisc["entries"][number] }) {
  const stateLabel = entry.state === "PRESENT" ? entry.sizeBytes === null ? "已接收" : formatBytes(entry.sizeBytes) : "待补齐";
  return <li><span><strong>{entry.label} · {entry.sourceReference}</strong><small>规范文件名：{entry.canonicalName}</small>{entry.sha256 ? <small>SHA-256 {entry.sha256.slice(0, 12)}…</small> : null}</span><span className={`status ${entry.state === "PRESENT" ? "good" : "warn"}`}><i />{stateLabel}</span></li>;
}

function MultiDiscFeedback({ value, validating, progress }: { value: ReviewMultiDisc; validating: boolean; progress: string }) {
  const latest = value.latestAttachment;
  if (validating) {return <><FeedbackBanner tone="info">正在校验补充光盘。{value.activeAttachment?.jobId ? `Job ${value.activeAttachment.jobId}` : progress}</FeedbackBanner>{progress ? <p className="scrape-live" role="status"><i className="button-spinner" aria-hidden="true" />正在校验补充光盘：{progress}</p> : null}</>;}
  if (!value.missingDiscCount) {return <FeedbackBanner tone="good">多盘内容完整，运行检查结果已更新。</FeedbackBanner>;}
  if (latest?.state === "FAILED_RETRYABLE") {return <FeedbackBanner tone="bad">补盘校验服务暂时不可用；可以复用已上传文件重试。错误码：{latest.errorCode ?? "REVIEW_MULTI_DISC_VALIDATION_UNAVAILABLE"}</FeedbackBanner>;}
  if (latest?.state === "REJECTED") {return <FeedbackBanner tone="bad">上次补盘未通过：{latest.errorCode ?? "REVIEW_MULTI_DISC_CONTENT_INVALID"}</FeedbackBanner>;}
  return <FeedbackBanner tone="bad">多盘内容不完整，发布已阻止。请一次上传当前全部缺失光盘。</FeedbackBanner>;
}

function MultiDiscActions({ value, latest, disabled, onRetry, onOpen }: { value: ReviewMultiDisc; latest: ReviewMultiDiscAttachment | null; disabled: boolean; onRetry: (attachment: ReviewMultiDiscAttachment) => Promise<void>; onOpen: () => void }) {
  if (!value.missingDiscCount) {return null;}
  const retryable = latest?.state === "FAILED_RETRYABLE" && latest.canRetry;
  return <div className="review-multidisc-actions">{retryable ? <button className="button secondary" type="button" disabled={disabled} onClick={() => void onRetry(latest)}>重试校验</button> : null}<button className="button secondary" type="button" disabled={disabled || !value.canAttachMissingDiscs} onClick={onOpen}>{latest?.state === "REJECTED" ? "重新上传全部缺失光盘" : "上传全部缺失光盘"}</button></div>;
}
