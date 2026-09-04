"use client";

import { useState, type Dispatch, type SetStateAction } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/features/auth/auth-provider";
import { userStorageKey } from "@/features/auth/storage";
import { queueFlashToast, type ToastMessage } from "@/components/flash-toast";
import { newUuid } from "@/lib/crypto";
import { writeHeaders } from "@/lib/api/client";
import { responseError, uploadOne, waitForJob } from "@/lib/upload";
import {
  candidateForm, readyCover, scrapeResult,
  type Comparison, type CoverSelection, type DraftPayload, type DuplicateGame, type MetadataForm,
  type ReviewCandidate, type ReviewWorkspace, type UploadedReviewAsset,
} from "./review-actions-model";

type Runner = (label: string, operation: () => Promise<void>) => Promise<boolean>;
type SaveDraft = (key: string, payload: DraftPayload, force?: boolean) => Promise<boolean>;

type CommandParams = {
  review: ReviewWorkspace; returnTo: string; nextItemId: string | null;
  versionRef: { current: number };
  draftKey: string; draftPayload: DraftPayload; form: MetadataForm; cover: CoverSelection;
  uploadedAssets: UploadedReviewAsset[]; comparison: Comparison | null;
  refreshReview: () => Promise<ReviewWorkspace>; flushDraft: () => Promise<boolean>; enqueueSave: SaveDraft; run: Runner;
  setJobProgress: Dispatch<SetStateAction<string>>; setNotice: Dispatch<SetStateAction<string>>; setToast: Dispatch<SetStateAction<ToastMessage | null>>;
  setCandidates: Dispatch<SetStateAction<ReviewCandidate[]>>; setUploadedAssets: Dispatch<SetStateAction<UploadedReviewAsset[]>>;
  setComparison: Dispatch<SetStateAction<Comparison | null>>; setForm: Dispatch<SetStateAction<MetadataForm>>;
  setCandidateId: Dispatch<SetStateAction<string | null>>; setCover: Dispatch<SetStateAction<CoverSelection>>;
  setBackgroundId: Dispatch<SetStateAction<string | null>>; setScreenshotIds: Dispatch<SetStateAction<string[]>>;
};

export function useReviewCommands(params: CommandParams) {
  const router = useRouter();
  const { context } = useAuth();
  const [duplicateConfirmation, setDuplicateConfirmation] = useState<DuplicateGame[] | null>(null);

  async function rescrape(metadataProvider: "HASHEOUS" | "NONE") {
    const label = metadataProvider === "HASHEOUS" ? "重新查询 Hasheous" : "停用元信息源";
    if (!await params.flushDraft()) {return;}
    await params.run(label, async () => {
      const response = await fetch(`/api/v1/admin/reviews/${params.review.itemId}/scrape-candidates`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${params.versionRef.current}"`, "Idempotency-Key": newUuid() }), body: JSON.stringify({ metadataProvider }) });
      if (!response.ok) {throw new Error(await responseError(response, "重新查询失败：条目或版本已经变化"));}
      const result = await response.json() as { version: number; state: string; scrapeRunId: string; jobId: string };
      params.versionRef.current = result.version;
      params.setJobProgress(`${result.state} · Job ${result.jobId.slice(0, 8)}…`);
      await waitForJob(result.jobId, params.setJobProgress);
      const updatedResponse = await fetch(`/api/v1/admin/reviews/${params.review.itemId}`, { cache: "no-store" });
      if (!updatedResponse.ok) {throw new Error(await responseError(updatedResponse, "查询完成，但无法读取新游戏信息"));}
      const updated = await updatedResponse.json() as ReviewWorkspace;
      params.setCandidates(updated.candidates);
      params.setUploadedAssets(updated.uploadedAssets ?? params.uploadedAssets);
      handleScrapeResult(metadataProvider, result.scrapeRunId, updated, params);
    });
  }

  async function uploadCover(file: File, target: "current" | "comparison") {
    await params.run("上传封面", async () => {
      const uploaded = await uploadOne(file, params.setNotice);
      const response = await fetch(`/api/v1/admin/reviews/${params.review.itemId}/assets`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${params.versionRef.current}"`, "Idempotency-Key": newUuid() }), body: JSON.stringify({ uploadFileId: uploaded.uploadFileId, kind: "COVER" }) });
      if (!response.ok) {throw new Error(await responseError(response, "封面上传失败"));}
      const asset = await response.json() as UploadedReviewAsset;
      params.setUploadedAssets((current) => current.some((entry) => entry.assetId === asset.assetId) ? current : [...current, asset]);
      if (target === "current") {params.setCover({ candidateId: null, uploadedId: asset.assetId });}
      else {params.setComparison((current) => current ? { ...current, nextCover: { candidateId: null, uploadedId: asset.assetId } } : null);}
      params.setNotice(target === "current" ? "新封面已上传，正在实时保存。" : "新封面已放入右侧对比结果，点击应用后生效。");
    });
  }

  function applyComparison() {
    if (!params.comparison) {return;}
    params.setForm(params.comparison.next);
    params.setCandidateId(params.comparison.candidate.candidateId);
    params.setCover(params.comparison.nextCover);
    params.setBackgroundId(null);
    params.setScreenshotIds([]);
    params.setComparison(null);
    params.setNotice("新查询结果已应用，系统会实时保存；旧候选素材选择已清除。");
  }

  function clearQueueCache() {
    const query = new URL(params.returnTo, window.location.origin).searchParams.toString();
    const key = userStorageKey(context.user?.userId, "reviews", `queue:${query}`);
    if (key) {sessionStorage.removeItem(key);}
  }

  const destination = () => params.nextItemId ? `/admin/reviews/${params.nextItemId}?returnTo=${encodeURIComponent(params.returnTo)}` : params.returnTo;

  async function publish(duplicateGames: DuplicateGame[] = []) {
    await params.run("发布", async () => {
      const body = duplicateGames.length ? { duplicatePolicy: "ALLOW_NEW", acknowledgedGameIds: duplicateGames.map((game) => game.gameId) } : {};
      const published = await publishUntilTerminal(body, params, setDuplicateConfirmation);
      if (!published) {return;}
      clearQueueCache();
      queueFlashToast({ message: "游戏已成功发布，待审核队列已更新。", tone: "good" });
      router.replace(destination());
    });
  }

  async function approve() {
    if (!await params.flushDraft()) {return;}
    const duplicates = params.review.duplicateGames ?? [];
    if (duplicates.length) {setDuplicateConfirmation(duplicates); return;}
    await publish();
  }

  async function launchPreview() {
    const popup = openPreviewWindow(params.setToast);
    if (!popup) {return;}
    const succeeded = await params.run("运行游戏", async () => {
      if (!await params.flushDraft()) {throw new Error("无法保存当前审核内容");}
      await createPreview(params.review.itemId, popup);
    });
    if (!succeeded && !popup.closed) {popup.close();}
  }

  async function confirmDuplicatePublish() {
    if (!duplicateConfirmation || !await params.flushDraft()) {return;}
    await publish(duplicateConfirmation);
  }

  async function discard() {
    if (!await params.flushDraft()) {return;}
    await params.run("丢弃", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${params.review.itemId}/discard`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${params.versionRef.current}"`, "Idempotency-Key": newUuid() }), body: "{}" });
      if (!response.ok) {throw new Error(await responseError(response, "丢弃失败：审核状态或版本已经变化"));}
      clearQueueCache();
      queueFlashToast({ message: "条目已丢弃，待审核队列已更新。", tone: "good" });
      router.replace(destination());
    });
  }

  return { duplicateConfirmation, setDuplicateConfirmation, rescrape, uploadCover, applyComparison, approve, launchPreview, confirmDuplicatePublish, discard };
}

function handleScrapeResult(metadataProvider: "HASHEOUS" | "NONE", scrapeRunId: string, updated: ReviewWorkspace, params: CommandParams) {
  if (metadataProvider === "NONE") {params.setNotice("已记录不使用在线游戏信息"); return;}
  const completed = (updated.scrapeRuns ?? []).find((entry) => entry.scrapeRunId === scrapeRunId);
  const latest = updated.candidates.find((entry) => entry.scrapeRunId === scrapeRunId);
  if (!completed) {throw new Error("查询完成，但服务器没有返回对应结果");}
  if (!latest) {
    const message = `查询完成，但${scrapeResult(completed)}`;
    params.setNotice(message);
    params.setToast({ message, tone: "warn" });
    return;
  }
  params.setComparison({ candidate: latest, current: { ...params.form }, next: candidateForm(latest, params.form), currentCover: params.cover, nextCover: { candidateId: readyCover(latest)?.candidateAssetId ?? null, uploadedId: null } });
}

async function publishUntilTerminal(body: object, params: CommandParams, setDuplicateConfirmation: Dispatch<SetStateAction<DuplicateGame[] | null>>) {
  const response = await fetch(`/api/v1/admin/reviews/${params.review.itemId}/approve`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${params.versionRef.current}"`, "Idempotency-Key": newUuid() }), body: JSON.stringify(body) });
  if (response.ok) {setDuplicateConfirmation(null); return true;}
  const payload = await response.json().catch(() => null) as ApprovalErrorPayload;
  const duplicates = duplicateGamesFrom(payload);
  if (duplicates) {setDuplicateConfirmation(duplicates); return false;}
  throw new Error(payload?.error?.message ?? "发布失败：请确认实时保存和运行检查均已完成");
}

type ApprovalErrorPayload = { error?: { code?: string; message?: string; details?: { games?: DuplicateGame[] } } } | null;

function duplicateGamesFrom(payload: ApprovalErrorPayload) {
  if (payload?.error?.code !== "DUPLICATE_GAME_CONFIRMATION_REQUIRED") {return null;}
  return payload.error.details?.games?.length ? payload.error.details.games : null;
}

function openPreviewWindow(setToast: Dispatch<SetStateAction<ToastMessage | null>>) {
  const popup = window.open("about:blank", "_blank", "popup=yes,width=1280,height=820,resizable=yes,scrollbars=no");
  if (!popup) {setToast({ message: "浏览器阻止了游戏子窗体，请允许本站弹出窗口后重试", tone: "warn" }); return null;}
  popup.document.title = "正在准备审核游戏预览";
  popup.document.body.style.cssText = "margin:0;display:grid;place-items:center;min-height:100vh;background:#0b0d12;color:#d9dce5;font:14px system-ui";
  popup.document.body.textContent = "正在准备审核游戏预览…";
  return popup;
}

async function createPreview(reviewId: string, popup: Window) {
  const response = await fetch(`/api/v1/admin/reviews/${reviewId}/previews`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }), body: JSON.stringify({ clientCapabilities: { secureContext: window.isSecureContext, crossOriginIsolated: window.crossOriginIsolated, sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined" } }) });
  if (!response.ok) {throw new Error(await responseError(response, "当前审核来源无法组成游戏预览"));}
  const result = await response.json() as { playUrl: string };
  if (popup.closed) {throw new Error("游戏子窗体已关闭");}
  popup.location.replace(result.playUrl);
}
