"use client";

import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { newUuid } from "@/lib/crypto";
import { writeHeaders } from "@/lib/api/client";
import { responseError, uploadFiles, uploadOne, waitForJob, waitForJobEvents } from "@/lib/upload";
import type { ToastMessage } from "@/components/flash-toast";
import type { ArcadeDependencyNode, ArcadeParentAttachment } from "./arcade-dependency-tree";
import type { ReviewMultiDisc, ReviewMultiDiscAttachment, ReviewWorkspace } from "./review-actions-model";

type Runner = (label: string, operation: () => Promise<void>) => Promise<boolean>;

type AttachmentParams = {
  reviewId: string;
  versionRef: { current: number };
  currentValidationId: string | undefined;
  effectiveSourceSnapshotId: string;
  activeParentJobId: string;
  multiDisc: ReviewMultiDisc | null;
  setMultiDisc: Dispatch<SetStateAction<ReviewMultiDisc | null>>;
  refreshReview: () => Promise<ReviewWorkspace>;
  flushDraft: () => Promise<boolean>;
  run: Runner;
  setToast: Dispatch<SetStateAction<ToastMessage | null>>;
};

function errorMessage(caught: unknown, fallback: string) {
  return caught instanceof Error ? caught.message : fallback;
}

export function useReviewAttachments(params: AttachmentParams) {
  const [parentProgress, setParentProgress] = useState("");
  const [multiDiscProgress, setMultiDiscProgress] = useState("");
  const watchedParentJobRef = useRef("");
  const watchedMultiDiscJobRef = useRef("");

  const watchParentJob = useCallback(async (jobId: string) => {
    watchedParentJobRef.current = jobId;
    let terminalError: Error | null = null;
    try {await waitForJobEvents(jobId, setParentProgress);}
    catch (caught) {terminalError = new Error(errorMessage(caught, "Parent ROM 校验失败"));}
    const updated = await params.refreshReview();
    if (terminalError) {params.setToast({ message: terminalError.message, tone: "bad" });}
    else if (updated.validation?.status === "READY") {params.setToast({ message: "Parent ROM 已匹配，运行检查已通过", tone: "good" });}
    else {params.setToast({ message: "Parent ROM 已匹配，仍有依赖需要处理", tone: "warn" });}
  }, [params]);

  useEffect(() => {
    if (!params.activeParentJobId || watchedParentJobRef.current === params.activeParentJobId) {return;}
    setParentProgress("恢复 Parent ROM 校验进度…");
    void watchParentJob(params.activeParentJobId).catch((caught: unknown) => {
      params.setToast({ message: errorMessage(caught, "无法恢复 Parent ROM 校验状态"), tone: "bad" });
    }).finally(() => setParentProgress(""));
  }, [params, watchParentJob]);

  async function attachParent(node: ArcadeDependencyNode, file: File) {
    if (!await params.flushDraft()) {return false;}
    const completed = await params.run("补充 Parent ROM", async () => {
      setParentProgress("正在上传 Parent ZIP…");
      const uploaded = await uploadOne(file, setParentProgress);
      const response = await fetch(`/api/v1/admin/reviews/${params.reviewId}/arcade-parent-attachments`, {
        method: "POST", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${params.versionRef.current}"`, "Idempotency-Key": newUuid() }),
        body: JSON.stringify({ validationId: params.currentValidationId, baseSourceSnapshotId: params.effectiveSourceSnapshotId, dependencyMachine: node.machine, uploadFileId: uploaded.uploadFileId }),
      });
      if (!response.ok) {throw new Error(await responseError(response, "无法创建 Parent ROM 校验任务"));}
      const result = await response.json() as { jobId: string };
      const version = response.headers.get("ETag")?.match(/^"v(\d+)"$/)?.[1];
      if (version) {params.versionRef.current = Number(version);}
      await watchParentJob(result.jobId);
    });
    setParentProgress("");
    return completed;
  }

  async function retryParent(attachment: ArcadeParentAttachment) {
    await params.run("重试 Parent ROM 校验", async () => {
      const snapshot = await fetch(`/api/v1/admin/jobs/${attachment.jobId}`, { cache: "no-store" });
      if (!snapshot.ok) {throw new Error(await responseError(snapshot, "无法读取待重试任务"));}
      const job = await snapshot.json() as { version: number };
      const response = await fetch(`/api/v1/admin/jobs/${attachment.jobId}/retry`, {
        method: "POST", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${job.version}"`, "Idempotency-Key": newUuid() }), body: "{}",
      });
      if (!response.ok) {throw new Error(await responseError(response, "Parent ROM 校验无法重试"));}
      await watchParentJob(attachment.jobId);
    });
    setParentProgress("");
  }

  const watchMultiDiscJob = useCallback(async (jobId: string) => {
    watchedMultiDiscJobRef.current = jobId;
    let terminalError: Error | null = null;
    try {await waitForJob(jobId, setMultiDiscProgress);}
    catch (caught) {terminalError = new Error(errorMessage(caught, "补盘校验失败"));}
    const updated = await params.refreshReview();
    if (terminalError) {throw terminalError;}
    if (updated.multiDisc?.missingDiscCount) {throw new Error("补盘未通过：所选文件与当前缺失盘不一致");}
    params.setToast({ message: "缺失光盘已补齐，正在更新审核结果", tone: "good" });
  }, [params]);

  const activeMultiDiscJobId = params.multiDisc?.activeAttachment?.jobId ?? "";
  useEffect(() => {
    if (!activeMultiDiscJobId || watchedMultiDiscJobRef.current === activeMultiDiscJobId) {return;}
    setMultiDiscProgress("恢复补盘校验进度…");
    void watchMultiDiscJob(activeMultiDiscJobId).catch((caught: unknown) => {
      params.setToast({ message: errorMessage(caught, "无法恢复补盘校验状态"), tone: "bad" });
    }).finally(() => setMultiDiscProgress(""));
  }, [activeMultiDiscJobId, params, watchMultiDiscJob]);

  async function attachMissingDiscs(files: File[], onQueued: () => void) {
    if (!params.multiDisc || !await params.flushDraft()) {return;}
    await params.run("补充缺失光盘", async () => {
      const uploaded = await uploadFiles(files, setMultiDiscProgress);
      const response = await fetch(`/api/v1/admin/reviews/${params.reviewId}/multi-disc-attachments`, {
        method: "POST", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${params.versionRef.current}"`, "Idempotency-Key": newUuid() }),
        body: JSON.stringify({ uploadId: uploaded.uploadId }),
      });
      if (!response.ok) {await rejectMultiDiscResponse(response, params.refreshReview);}
      const result = await response.json() as { attachmentId: string; state: string; jobId: string; reviewVersion?: number };
      updateVersion(response, result.reviewVersion, params.versionRef);
      const queued: ReviewMultiDiscAttachment = { attachmentId: result.attachmentId, state: result.state, errorCode: null, jobId: result.jobId, jobState: "QUEUED", canRetry: false };
      params.setMultiDisc((current) => current ? { ...current, canAttachMissingDiscs: false, latestAttachment: queued, activeAttachment: queued } : current);
      setMultiDiscProgress(`正在校验补充光盘 · Job ${result.jobId.slice(0, 8)}…`);
      onQueued();
      await watchMultiDiscJob(result.jobId);
    });
    setMultiDiscProgress("");
  }

  async function retryMultiDisc(attachment: ReviewMultiDiscAttachment) {
    await params.run("重试补盘校验", async () => {
      const snapshot = await fetch(`/api/v1/admin/jobs/${attachment.jobId}`, { cache: "no-store" });
      if (!snapshot.ok) {throw new Error(await responseError(snapshot, "无法读取待重试补盘任务"));}
      const job = await snapshot.json() as { version: number };
      const response = await fetch(`/api/v1/admin/jobs/${attachment.jobId}/retry`, {
        method: "POST", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${job.version}"`, "Idempotency-Key": newUuid() }), body: "{}",
      });
      if (!response.ok) {throw new Error(await responseError(response, "补盘校验无法重试"));}
      setMultiDiscProgress(`正在重试补盘校验 · Job ${attachment.jobId.slice(0, 8)}…`);
      await watchMultiDiscJob(attachment.jobId);
    });
    setMultiDiscProgress("");
  }

  return { parentProgress, multiDiscProgress, attachParent, retryParent, attachMissingDiscs, retryMultiDisc };
}

async function rejectMultiDiscResponse(response: Response, refreshReview: () => Promise<ReviewWorkspace>): Promise<never> {
  const payload = await response.json().catch(() => null) as { error?: { code?: string; message?: string } } | null;
  const refreshCodes = ["REVIEW_VERSION_CONFLICT", "REVIEW_MULTI_DISC_INPUT_STALE", "REVIEW_MULTI_DISC_ATTACHMENT_SET_MISMATCH", "REVIEW_MULTI_DISC_CONTENT_INVALID", "REVIEW_MULTI_DISC_ATTACHMENT_IN_PROGRESS", "REVIEW_MULTI_DISC_ATTACHMENT_RETRY_REQUIRED"];
  if (refreshCodes.includes(payload?.error?.code ?? "")) {await refreshReview();}
  throw new Error(payload?.error?.message ?? "无法创建补盘校验任务");
}

function updateVersion(response: Response, reviewVersion: number | undefined, versionRef: { current: number }) {
  const responseVersion = response.headers.get("ETag")?.match(/^"v(\d+)"$/)?.[1];
  if (responseVersion) {versionRef.current = Number(responseVersion);}
  else if (reviewVersion) {versionRef.current = reviewVersion;}
}
