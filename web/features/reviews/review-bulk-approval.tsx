"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useAuth } from "@/features/auth/auth-provider";
import { userStoragePrefix } from "@/features/auth/storage";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";

type ReviewBulkSummary = {
  bulkApprovalId: string;
  state: "QUEUED" | "RUNNING" | "COMPLETED" | "FAILED";
  initialPendingCount: number;
  scannedCount: number;
  publishedCount: number;
  skippedChangedCount: number;
  skippedDuplicateCount: number;
  skippedNotReadyCount: number;
  lastErrorCode: string | null;
};

type ActiveResponse = { activeBulkApproval: ReviewBulkSummary | null };
type APIError = { error?: { message?: string } };

function clearReviewQueueSnapshots(userId: string | null | undefined) {
  if (!userId) {return;}
  const prefix = `${userStoragePrefix(userId)}reviews:`;
  for (let index = sessionStorage.length - 1; index >= 0; index--) {
    const key = sessionStorage.key(index);
    if (key?.startsWith(prefix)) {sessionStorage.removeItem(key);}
  }
}

function active(summary: ReviewBulkSummary) {
  return summary.state === "QUEUED" || summary.state === "RUNNING";
}

function ReviewBulkStatus({ summary, error }: { summary: ReviewBulkSummary; error: string }) {
  const running = active(summary);
  return <section className={`review-bulk-status ${running ? "" : "is-terminal"}`} aria-live="polite">
    <div className="review-bulk-status-head">
      <div>
        <h2>{running ? "正在快速审批" : summary.state === "COMPLETED" ? "快速审批已完成" : "快速审批失败"}</h2>
        <p>创建时待审约 {summary.initialPendingCount} 项 · 已扫描 {summary.scannedCount} 项</p>
      </div>
    </div>
    <div className="review-bulk-stats">
      <span><small>已发布</small><strong>{summary.publishedCount}</strong></span>
      <span><small>继续待审</small><strong>{summary.skippedChangedCount + summary.skippedDuplicateCount + summary.skippedNotReadyCount}</strong></span>
      <span><small>已扫描</small><strong>{summary.scannedCount}</strong></span>
    </div>
    {summary.state === "FAILED" ? <p className="review-bulk-error" role="alert">任务中断，尚未扫描的项目仍在待审队列。</p> : null}
    {error ? <p className="review-bulk-error" role="alert">{error}</p> : null}
  </section>;
}

export function ReviewBulkApproval({ restoreBulkApprovalId }: { restoreBulkApprovalId?: string }) {
  const router = useRouter();
  const { context } = useAuth();
  const [summary, setSummary] = useState<ReviewBulkSummary | null>(null);
  const [discovering, setDiscovering] = useState(true);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState("");
  const refreshed = useRef<string | null>(null);
  const portalRoot = typeof document === "undefined" ? null
    : document.querySelector<HTMLElement>("#review-bulk-status-root");

  const loadSummary = useCallback(async (id: string) => {
    const response = await fetch(`/api/v1/admin/review-bulk-approvals/${id}`, { cache: "no-store" });
    if (!response.ok) {throw new Error("无法读取快速审批进度");}
    const next = await response.json() as ReviewBulkSummary;
    setError("");
    setSummary(next);
    return next;
  }, []);

  const discoverActive = useCallback(async () => {
    const response = await fetch("/api/v1/admin/review-bulk-approvals/active", { cache: "no-store" });
    if (!response.ok) {throw new Error("无法查询快速审批任务");}
    const found = await response.json() as ActiveResponse;
    if (found.activeBulkApproval) {
      setSummary(found.activeBulkApproval);
      return found.activeBulkApproval;
    }
    if (restoreBulkApprovalId) {return loadSummary(restoreBulkApprovalId);}
    return null;
  }, [loadSummary, restoreBulkApprovalId]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void discoverActive().catch((caught: unknown) => {
        setError(caught instanceof Error ? caught.message : "无法查询快速审批任务");
      }).finally(() => setDiscovering(false));
    }, 0);
    return () => window.clearTimeout(timer);
  }, [discoverActive]);

  useEffect(() => {
    if (!summary || !active(summary)) {return;}
    const timer = window.setInterval(() => {
      void loadSummary(summary.bulkApprovalId).catch(() => setError("连接中断，正在重试快速审批进度。"));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [loadSummary, summary]);

  useEffect(() => {
    if (!summary || active(summary) || refreshed.current === summary.bulkApprovalId) {return;}
    refreshed.current = summary.bulkApprovalId;
    clearReviewQueueSnapshots(context.user?.userId);
    router.refresh();
  }, [context.user?.userId, router, summary]);

  async function start() {
    if (discovering || starting || (summary && active(summary))) {return;}
    setStarting(true);
    setError("");
    try {
      const response = await fetch("/api/v1/admin/review-bulk-approvals", {
        method: "POST",
        credentials: "same-origin",
        headers: writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
        body: "{}",
      });
      if (response.status === 409) {
        const found = await discoverActive();
        if (found && active(found)) {return;}
        const failure = await response.json() as APIError;
        throw new Error(failure.error?.message ?? "快速审批无法开始");
      }
      if (!response.ok) {
        const failure = await response.json() as APIError;
        throw new Error(failure.error?.message ?? "无法开始快速审批");
      }
      const created = await response.json() as ReviewBulkSummary;
      setSummary(created);
      const url = new URL(window.location.href);
      url.searchParams.set("bulkApprovalId", created.bulkApprovalId);
      window.history.replaceState(null, "", `${url.pathname}${url.search}${url.hash}`);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "无法开始快速审批");
    } finally {
      setStarting(false);
    }
  }

  const running = summary !== null && active(summary);
  const status = summary && portalRoot ? createPortal(<ReviewBulkStatus summary={summary} error={error} />, portalRoot) : null;

  return <>
    <button className="button" type="button" disabled={discovering || starting || running} onClick={() => void start()}>
      {discovering ? "正在查询快速审批…" : starting ? "正在启动…" : running ? "正在快速审批" : "快速审批全部待审"}
    </button>
    {status}
    {portalRoot && error && !summary ? createPortal(
      <p className="review-bulk-error review-bulk-standalone-error" role="alert">{error}</p>,
      portalRoot,
    ) : null}
  </>;
}
