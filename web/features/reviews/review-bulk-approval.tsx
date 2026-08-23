"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { useAuth } from "@/features/auth/auth-provider";
import { userStoragePrefix } from "@/features/auth/storage";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";

type ReviewBulkScope = {
  q?: string;
  tagId?: string;
  importJobId?: string;
  pegasusImportId?: string;
  emulationStationImportId?: string;
  platformInstanceId?: string;
  blockerCode?: string;
};

type ReviewBulkCounts = {
  matched: number;
  strictReady: number;
  screenshotOnly: number;
  duplicate: number;
  attachmentActive: number;
  sourceFlagged: number;
  notReadyOrStale: number;
};

type ReviewBulkProgress = {
  candidate: number;
  processed: number;
  published: number;
  skippedDuplicate: number;
  skippedChanged: number;
  skippedNotReady: number;
  failed: number;
  cancelled: number;
};

type ReviewBulkSummary = {
  bulkApprovalId: string;
  jobId: string;
  state: string;
  version: number;
  scope: ReviewBulkScope;
  initialCounts: ReviewBulkCounts;
  counts: ReviewBulkProgress;
  lastErrorCode: string | null;
};

type ReviewBulkPreview = {
  scope: ReviewBulkScope;
  scopeDigest: string;
  candidateManifestDigest: string;
  counts: ReviewBulkCounts;
  activeBulkApproval: ReviewBulkSummary | null;
};

type ReviewBulkItem = {
  importItemId: string;
  title: string;
  platformName: string;
  state: string;
  gameId: string | null;
  outcomeCode: string | null;
};

type APIError = { error?: { code?: string; message?: string } };

const terminalStates = new Set(["COMPLETED", "PARTIAL_FAILURE", "CANCELLED", "FAILED"]);

function bulkScope(values: Record<string, string>): ReviewBulkScope {
  const scope: ReviewBulkScope = {};
  for (const key of ["q", "tagId", "importJobId", "pegasusImportId", "emulationStationImportId", "platformInstanceId", "blockerCode"] as const) {
    if (values[key]) {scope[key] = values[key];}
  }
  return scope;
}

function previewURL(scope: ReviewBulkScope) {
  return `/api/v1/admin/review-bulk-approval-preview?${new URLSearchParams(Object.entries(scope))}`;
}

function scopeLabel(scope: ReviewBulkScope) {
  if (scope.pegasusImportId) {return `Pegasus 批次 ${scope.pegasusImportId.slice(0, 8)}… 的全部分页结果`;}
  if (scope.emulationStationImportId) {return `EmulationStation 批次 ${scope.emulationStationImportId.slice(0, 8)}… 的全部分页结果`;}
  if (scope.importJobId) {return `导入批次 ${scope.importJobId.slice(0, 8)}… 的全部分页结果`;}
  if (Object.keys(scope).length) {return "当前筛选范围的全部分页结果";}
  return "全部待审核条目";
}

async function responseError(response: Response, fallback: string) {
  const body = await response.json().catch(() => ({})) as APIError;
  return { code: body.error?.code ?? "", message: body.error?.message ?? fallback };
}

function clearReviewQueueSnapshots(userId: string | null | undefined) {
  if (!userId) {return;}
  const expected = `${userStoragePrefix(userId)}reviews:`;
  const keys = Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.key(index))
    .filter((key): key is string => Boolean(key?.startsWith(expected)));
  for (const key of keys) {sessionStorage.removeItem(key);}
}

function BulkResultItems({ items }: { items: ReviewBulkItem[] }) {
  if (!items.length) {return null;}
  return <div className="review-bulk-results"><strong>审批结果（前 {items.length} 项）</strong>{items.map((item) => {
    const href = item.state === "PUBLISHED" && item.gameId
      ? `/admin/games/${item.gameId}`
      : `/admin/reviews/${item.importItemId}?returnTo=${encodeURIComponent(window.location.pathname + window.location.search)}`;
    return <Link key={item.importItemId} href={href}><span>{item.title}</span><small>{item.outcomeCode ?? item.state}</small></Link>;
  })}</div>;
}

function BulkStatus({ cancelling, error, onCancel, onRetry, resultItems, starting, summary }: {
  cancelling: boolean;
  error: string;
  onCancel: () => void;
  onRetry: () => void;
  resultItems: ReviewBulkItem[];
  starting: boolean;
  summary: ReviewBulkSummary;
}) {
  const terminal = terminalStates.has(summary.state);
  const retryable = summary.state === "FAILED" && summary.lastErrorCode === "REVIEW_BULK_WORKER_UNAVAILABLE";
  const processedPercent = summary.counts.candidate ? summary.counts.processed / summary.counts.candidate * 100 : 0;
  return <section className={`review-bulk-status ${terminal ? "is-terminal" : ""}`} aria-live="polite">
    <div className="review-bulk-status-head">
      <div><h2>{terminal ? "快速审批结果" : "正在快速审批"}</h2><p>{scopeLabel(summary.scope)} · 已处理 {summary.counts.processed} / {summary.counts.candidate}</p></div>
      {!terminal ? <button className="button secondary" type="button" disabled={cancelling} onClick={onCancel}>{cancelling ? "正在停止…" : "停止未处理项目"}</button> : null}
      {retryable ? <button className="button secondary" type="button" disabled={starting} onClick={onRetry}>{starting ? "正在重试…" : "重试未处理项目"}</button> : null}
    </div>
    <div className="review-bulk-meter" aria-label={`快速审批进度 ${summary.counts.processed} / ${summary.counts.candidate}`}><i style={{ width: `${processedPercent}%` }} /></div>
    <div className="review-bulk-stats">
      <span><small>已发布</small><strong>{summary.counts.published}</strong></span>
      <span><small>需要人工处理</small><strong>{summary.counts.skippedDuplicate + summary.counts.skippedChanged + summary.counts.skippedNotReady}</strong></span>
      <span><small>失败</small><strong>{summary.counts.failed}</strong></span>
      <span><small>未处理/取消</small><strong>{summary.counts.candidate - summary.counts.processed + summary.counts.cancelled}</strong></span>
    </div>
    <BulkResultItems items={resultItems} />
    {error ? <p className="review-bulk-error" role="alert">{error}</p> : null}
  </section>;
}

function BulkPreview({ error, preview }: { error: string; preview: ReviewBulkPreview | null }) {
  if (!preview) {return null;}
  return <div className="review-bulk-preview">
    <p><strong>审批范围</strong><span>{scopeLabel(preview.scope)}</span></p>
    <div className="review-bulk-preview-counts"><span><small>可自动发布</small><strong>{preview.counts.strictReady}</strong></span><span><small>匹配待审核</small><strong>{preview.counts.matched}</strong></span></div>
    <ul><li>检查未通过或证据已过期 <strong>{preview.counts.notReadyOrStale}</strong></li><li>发现重复内容 <strong>{preview.counts.duplicate}</strong></li><li>仅有运行截图人工放行 <strong>{preview.counts.screenshotOnly}</strong></li><li>附件仍在处理 <strong>{preview.counts.attachmentActive}</strong></li><li>hidden/adult 来源标记需逐项核对 <strong>{preview.counts.sourceFlagged}</strong></li></ul>
    <p className="review-bulk-warning">将使用当前已保存的信息。执行中发生变化的项目会被跳过；取消不会回滚已经发布的游戏。</p>
    {error ? <p className="review-bulk-error" role="alert">{error}</p> : null}
  </div>;
}

function ReviewBulkControls({ cancelDialogOpen, cancelling, dialogOpen, error, loadingPreview, onCancel, onCancelDialogClose, onOpen, onPreviewClose, onStart, preview, starting, summary }: {
  cancelDialogOpen: boolean;
  cancelling: boolean;
  dialogOpen: boolean;
  error: string;
  loadingPreview: boolean;
  onCancel: () => void;
  onCancelDialogClose: () => void;
  onOpen: () => void;
  onPreviewClose: () => void;
  onStart: () => void;
  preview: ReviewBulkPreview | null;
  starting: boolean;
  summary: ReviewBulkSummary | null;
}) {
  const active = Boolean(summary && !terminalStates.has(summary.state));
  const buttonLabel = loadingPreview ? "正在检查…" : active ? "查看快速审批进度" : "快速审批";
  return <>
    <button className="button" type="button" disabled={loadingPreview} onClick={onOpen}>{buttonLabel}</button>
    <ConfirmDialog open={dialogOpen && Boolean(preview)} title="快速审批可直接发布的游戏" description="系统会冻结当前筛选范围，并在后台逐项重新验证后发布。" confirmLabel={`确认快速发布 ${preview?.counts.strictReady ?? 0} 个游戏`} confirmDisabled={!preview?.counts.strictReady} busy={starting} portalToBody onCancel={onPreviewClose} onConfirm={onStart}>
      <BulkPreview error={error} preview={preview} />
    </ConfirmDialog>
    <ConfirmDialog open={cancelDialogOpen && active} title="停止快速审批？" description="只会停止尚未提交的项目；已经发布的游戏不会被撤销。" confirmLabel="停止未处理项目" busy={cancelling} portalToBody onCancel={onCancelDialogClose} onConfirm={onCancel} />
  </>;
}

export function ReviewBulkApproval({ values, restoreBulkApprovalId }: {
  values: Record<string, string>;
  restoreBulkApprovalId?: string;
}) {
  const router = useRouter();
  const { context } = useAuth();
  const scope = useMemo(() => bulkScope(values), [values]);
  const [preview, setPreview] = useState<ReviewBulkPreview | null>(null);
  const [summary, setSummary] = useState<ReviewBulkSummary | null>(null);
  const [resultItems, setResultItems] = useState<ReviewBulkItem[]>([]);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [cancelDialogOpen, setCancelDialogOpen] = useState(false);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [starting, setStarting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [error, setError] = useState("");
  const refreshedBulk = useRef<string | null>(null);
  const portalRoot = typeof document === "undefined" ? null : document.querySelector<HTMLElement>("#review-bulk-status-root");

  const loadSummary = useCallback(async (bulkApprovalId: string) => {
    const response = await fetch(`/api/v1/admin/review-bulk-approvals/${bulkApprovalId}`, { cache: "no-store" });
    if (!response.ok) {throw new Error("无法读取快速审批进度");}
    const next = await response.json() as ReviewBulkSummary;
    setSummary(next);
    return next;
  }, []);

  useEffect(() => {
    if (!restoreBulkApprovalId) {return;}
    const timer = window.setTimeout(() => {
      void loadSummary(restoreBulkApprovalId).catch(() => setError("无法恢复快速审批进度，请稍后重试。"));
    }, 0);
    return () => window.clearTimeout(timer);
  }, [loadSummary, restoreBulkApprovalId]);

  useEffect(() => {
    if (!summary || terminalStates.has(summary.state)) {return;}
    const timer = window.setTimeout(() => {
      void loadSummary(summary.bulkApprovalId).catch(() => setError("连接中断，正在重试快速审批进度。"));
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [loadSummary, summary]);

  useEffect(() => {
    if (!summary || !terminalStates.has(summary.state) || refreshedBulk.current === summary.bulkApprovalId) {return;}
    refreshedBulk.current = summary.bulkApprovalId;
    clearReviewQueueSnapshots(context.user?.userId);
    router.refresh();
    void fetch(`/api/v1/admin/review-bulk-approvals/${summary.bulkApprovalId}/items?limit=50`, { cache: "no-store" })
      .then(async (response) => response.ok ? response.json() as Promise<{ items: ReviewBulkItem[] }> : { items: [] })
      .then((page) => setResultItems(page.items));
  }, [context.user?.userId, router, summary]);

  function rememberBulkApproval(bulkApprovalId: string) {
    const current = new URL(window.location.href);
    current.searchParams.set("bulkApprovalId", bulkApprovalId);
    window.history.replaceState(window.history.state, "", `${current.pathname}${current.search}${current.hash}`);
  }

  async function openPreview() {
    if (summary && !terminalStates.has(summary.state)) {return;}
    setLoadingPreview(true);
    setError("");
    try {
      const response = await fetch(previewURL(scope), { cache: "no-store" });
      if (!response.ok) {throw new Error("无法计算快速审批范围");}
      const next = await response.json() as ReviewBulkPreview;
      setPreview(next);
      if (next.activeBulkApproval) {
        setSummary(next.activeBulkApproval);
        rememberBulkApproval(next.activeBulkApproval.bulkApprovalId);
        return;
      }
      setDialogOpen(true);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "无法计算快速审批范围");
    } finally {
      setLoadingPreview(false);
    }
  }

  async function start() {
    if (!preview || preview.counts.strictReady === 0) {return;}
    setStarting(true);
    setError("");
    try {
      const response = await fetch("/api/v1/admin/review-bulk-approvals", {
        method: "POST",
        credentials: "same-origin",
        headers: writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
        body: JSON.stringify({
          scope: preview.scope,
          scopeDigest: preview.scopeDigest,
          candidateManifestDigest: preview.candidateManifestDigest,
        }),
      });
      if (!response.ok) {
        const failure = await responseError(response, "无法开始快速审批");
        if (failure.code === "REVIEW_BULK_PREVIEW_STALE") {
          const refreshed = await fetch(previewURL(scope), { cache: "no-store" });
          if (refreshed.ok) {setPreview(await refreshed.json() as ReviewBulkPreview);}
          setError("待审核内容已经变化，数量已刷新，请再次确认。");
          return;
        }
        throw new Error(failure.message);
      }
      const created = await response.json() as ReviewBulkSummary;
      setSummary(created);
      setDialogOpen(false);
      rememberBulkApproval(created.bulkApprovalId);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "无法开始快速审批");
    } finally {
      setStarting(false);
    }
  }

  async function cancel() {
    if (!summary || terminalStates.has(summary.state)) {return;}
    setCancelling(true);
    setError("");
    try {
      const response = await fetch(`/api/v1/admin/review-bulk-approvals/${summary.bulkApprovalId}/cancel`, {
        method: "POST",
        credentials: "same-origin",
        headers: writeHeaders({
          "Content-Type": "application/json", "If-Match": `"v${summary.version}"`, "Idempotency-Key": newUuid(),
        }),
        body: JSON.stringify({ reason: "管理员停止快速审批" }),
      });
      if (!response.ok) {throw new Error((await responseError(response, "无法停止快速审批")).message);}
      setSummary(await response.json() as ReviewBulkSummary);
      setCancelDialogOpen(false);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "无法停止快速审批");
    } finally {
      setCancelling(false);
    }
  }

  async function retry() {
    if (!summary || summary.state !== "FAILED") {return;}
    setStarting(true);
    setError("");
    try {
      const response = await fetch(`/api/v1/admin/review-bulk-approvals/${summary.bulkApprovalId}/retry`, {
        method: "POST",
        credentials: "same-origin",
        headers: writeHeaders({
          "Content-Type": "application/json", "If-Match": `"v${summary.version}"`, "Idempotency-Key": newUuid(),
        }),
        body: "{}",
      });
      if (!response.ok) {throw new Error((await responseError(response, "无法重试快速审批")).message);}
      refreshedBulk.current = null;
      setResultItems([]);
      setSummary(await response.json() as ReviewBulkSummary);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "无法重试快速审批");
    } finally {
      setStarting(false);
    }
  }

  const status = summary && portalRoot ? createPortal(
    <BulkStatus cancelling={cancelling} error={error} onCancel={() => setCancelDialogOpen(true)} onRetry={() => void retry()} resultItems={resultItems} starting={starting} summary={summary} />,
    portalRoot,
  ) : null;

  const standaloneError = portalRoot && error && !summary && !dialogOpen ? createPortal(
    <p className="review-bulk-error review-bulk-standalone-error" role="alert">{error}</p>,
    portalRoot,
  ) : null;

  return <>
    <ReviewBulkControls cancelDialogOpen={cancelDialogOpen} cancelling={cancelling} dialogOpen={dialogOpen} error={error} loadingPreview={loadingPreview} onCancel={() => void cancel()} onCancelDialogClose={() => { if (!cancelling) {setCancelDialogOpen(false);} }} onOpen={() => void openPreview()} onPreviewClose={() => { if (!starting) {setDialogOpen(false);} }} onStart={() => void start()} preview={preview} starting={starting} summary={summary} />
    {status}
    {standaloneError}
  </>;
}
