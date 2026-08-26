"use client";

import { useCallback, useEffect, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import { newUuid } from "@/lib/crypto";
import { writeHeaders } from "@/lib/api/client";
import { responseError } from "@/lib/upload";
import { formatBytes } from "@/lib/backend";
import type { ToastMessage } from "@/components/flash-toast";
import type { RPGMakerReview, RPGMachineGate, RPGRuntimeValidation } from "./review-actions-model";
import { RPGPackControls } from "./review-rpg-packs";

type Runner = (label: string, operation: () => Promise<void>) => Promise<boolean>;

type RPGValidationParams = {
  reviewId: string;
  versionRef: MutableRefObject<number>;
  rpgMaker: RPGMakerReview | null;
  setRPGMaker: Dispatch<SetStateAction<RPGMakerReview | null>>;
  flushDraft: () => Promise<boolean>;
  run: Runner;
  setNotice: Dispatch<SetStateAction<string>>;
  setToast: Dispatch<SetStateAction<ToastMessage | null>>;
};

export function useRPGReviewValidation(params: RPGValidationParams) {
  const { reviewId, versionRef, rpgMaker, setRPGMaker, flushDraft, run, setNotice, setToast } = params;
  const validation = rpgMaker?.runtimeValidation ?? null;
  const replaceValidation = useCallback((next: RPGRuntimeValidation) => {
    setRPGMaker((current) => current ? {
      ...current,
      runtimeBindingRevision: next.runtimeBindingRevision,
      runtimeValidation: next,
      runtimeValidationCurrent: true,
    } : current);
  }, [setRPGMaker]);

  const readValidation = useCallback(async (validationId: string) => {
    const response = await fetch(`/api/v1/admin/reviews/${reviewId}/runtime-validations/${validationId}`, {
      credentials: "same-origin", cache: "no-store",
    });
    if (!response.ok) {throw new Error(await responseError(response, "无法读取 RPG Maker 运行验证状态"));}
    const next = await response.json() as RPGRuntimeValidation;
    replaceValidation(next);
    return next;
  }, [reviewId, replaceValidation]);

  useEffect(() => {
    if (!validation || ["PASSED", "FAILED", "EXPIRED"].includes(validation.state)) {return;}
    let cancelled = false;
    const poll = async () => {
      try {
        await readValidation(validation.validationId);
      } catch (error) {
        if (!cancelled) {setToast({ message: error instanceof Error ? error.message : "无法刷新运行验证", tone: "warn" });}
      }
    };
    const timer = window.setInterval(() => { void poll(); }, 1_000);
    return () => { cancelled = true; window.clearInterval(timer); };
  }, [readValidation, setToast, validation]);

  async function create() {
    const popup = openRPGPlayerWindow(setToast, "正在准备 RPG Maker 运行验证…");
    if (!popup) {return;}
    const succeeded = await run("运行游戏", async () => {
      if (!await flushDraft()) {throw new Error("无法保存当前审核内容");}
      const response = await fetch(`/api/v1/admin/reviews/${reviewId}/runtime-validations`, {
        method: "POST", credentials: "same-origin",
        headers: await rpgWriteHeaders(versionRef.current),
        body: JSON.stringify({ clientCapabilities: browserCapabilities() }),
      });
      if (!response.ok) {throw new Error(await responseError(response, "无法创建 RPG Maker 运行验证"));}
      const created = await response.json() as { validationId: string; playerUrl: string };
      await readValidation(created.validationId);
      navigatePopup(popup, created.playerUrl);
      setNotice("运行验证已创建；请在游戏窗口按顺序完成保存点与继续游玩验证。");
    });
    if (!succeeded && !popup.closed) {popup.close();}
  }

  async function restore() {
    if (!validation) {return;}
    const popup = openRPGPlayerWindow(setToast, "正在准备不同 Launch 的恢复验证…");
    if (!popup) {return;}
    const succeeded = await run("验证恢复", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${reviewId}/runtime-validations/${validation.validationId}/restore-launch`, {
        method: "POST", credentials: "same-origin",
        headers: await rpgWriteHeaders(versionRef.current),
        body: JSON.stringify({ clientCapabilities: browserCapabilities() }),
      });
      if (!response.ok) {throw new Error(await responseError(response, "无法创建恢复验证 Launch"));}
      const created = await response.json() as { playerUrl: string };
      await readValidation(validation.validationId);
      navigatePopup(popup, created.playerUrl);
      setNotice("已创建不同的恢复 Launch；系统将逐字段核对地图、坐标和验证状态。");
    });
    if (!succeeded && !popup.closed) {popup.close();}
  }

  async function decide(decision: "PASS" | "FAIL") {
    if (!validation) {return;}
    await run(decision === "PASS" ? "确认运行验证" : "拒绝运行验证", async () => {
      const response = await fetch(`/api/v1/admin/reviews/${reviewId}/runtime-validations/${validation.validationId}/decision`, {
        method: "POST", credentials: "same-origin",
        headers: await rpgWriteHeaders(versionRef.current),
        body: JSON.stringify({ decision, note: decision === "PASS" ? "机器门禁与跨 Launch 精确恢复证据已核对" : "运行验证未通过人工审核" }),
      });
      if (!response.ok) {throw new Error(await responseError(response, "无法提交 RPG Maker 运行验证决定"));}
      replaceValidation(await response.json() as RPGRuntimeValidation);
      setNotice(decision === "PASS" ? "运行验证已通过，可以发布。" : "运行验证已标记失败，可重新创建验证。");
    });
  }

  return {
    validation,
    create,
    restore,
    decide,
    canCreate: !validation || !rpgMaker?.runtimeValidationCurrent || ["FAILED", "EXPIRED"].includes(validation.state),
    canRestore: rpgMaker?.runtimeValidationCurrent === true && validation?.state === "CHECKPOINTED" && validation.checkpointRoundTrip.originalLaunchEnded,
    canDecide: rpgMaker?.runtimeValidationCurrent === true && validation?.state === "AWAITING_DECISION",
  };
}

async function rpgWriteHeaders(version: number) {
  return writeHeaders({
    "Content-Type": "application/json",
    "If-Match": `"v${version}"`,
    "Idempotency-Key": newUuid(),
  });
}

function browserCapabilities() {
  return {
    secureContext: window.isSecureContext,
    crossOriginIsolated: window.crossOriginIsolated,
    sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined",
  };
}

function openRPGPlayerWindow(setToast: Dispatch<SetStateAction<ToastMessage | null>>, message: string) {
  const popup = window.open("about:blank", "_blank", "popup=yes,width=1280,height=820,resizable=yes,scrollbars=no");
  if (!popup) {
    setToast({ message: "浏览器阻止了游戏子窗体，请允许本站弹出窗口后重试", tone: "warn" });
    return null;
  }
  popup.document.title = "RPG Maker 运行验证";
  popup.document.body.style.cssText = "margin:0;display:grid;place-items:center;min-height:100vh;background:#0b0d12;color:#d9dce5;font:14px system-ui";
  popup.document.body.textContent = message;
  return popup;
}

function navigatePopup(popup: Window, playerUrl: string) {
  if (popup.closed) {throw new Error("游戏子窗体已关闭");}
  popup.location.replace(playerUrl);
}

const gateLabels: Record<string, string> = {
  RUNTIME_READY: "运行时已加载", ENGINE_PROFILE: "引擎与版本匹配", FRAMES_300: "连续运行 300 帧",
  INPUT: "输入可用", AUDIO: "音频可用", INITIAL_POSITION_RECORDED: "记录初始位置 A",
  SAVE_POINT_RECORDED: "记录保存位置 B",
  CHECKPOINT_CREATED: "创建 checkpoint", POST_SAVE_STATE_DIVERGED: "继续到不同位置 C",
  ORIGINAL_LAUNCH_ENDED: "结束原 Launch", RESTORE_STARTED: "启动不同恢复 Launch",
  RESTORE_POSITION_VERIFIED: "精确恢复到 B", RESTORE_SCREENSHOT: "保存恢复证据截图",
  RESTORE_INPUT: "验证恢复后输入",
};

function gateStatus(gate: RPGMachineGate) {
  if (gate.status === "PASSED") {return "已通过";}
  if (gate.status === "FAILED") {return `失败${gate.failureCode ? ` · ${gate.failureCode}` : ""}`;}
  if (gate.status === "IN_PROGRESS") {return "验证中";}
  return "等待";
}

function positionText(position: { mapId: number; playerX: number; playerY: number; fixtureState: number } | null) {
  return position ? `地图 ${position.mapId} · (${position.playerX}, ${position.playerY}) · 状态 ${position.fixtureState}` : "尚未记录";
}

export function RPGValidationCard({ value, disabled, onChange }: {
  value: RPGMakerReview;
  disabled: boolean;
  onChange: (next: RPGMakerReview) => void;
}) {
  const validation = value.runtimeValidation;
  return <section className="panel review-rpg-validation">
    <div className="panel-head"><div><h2>RPG Maker 运行验证</h2><p>发布前必须完成不同 Launch 的精确存档恢复。</p></div><strong>{validation && !value.runtimeValidationCurrent ? "历史验证" : validation?.state ?? "尚未开始"}</strong></div>
    <div className="panel-body">
      <RPGValidationFacts value={value} />
      <RPGPackControls value={value} disabled={disabled} onChange={onChange} />
      <RPGGateList validation={validation} />
      <RPGCheckpointRoundTrip validation={validation} />
    </div>
  </section>;
}

function RPGValidationFacts({ value }: { value: RPGMakerReview }) {
  const packLabel = value.selfContained || value.selfContainedOverride ? "项目自包含" : `${value.runtimePackSelections.length} 个已锁定运行包`;
  return <div className="review-rpg-facts">
    <div><span>所选版本</span><strong>{coreLabel(value.selectedCoreId)}</strong></div>
    <div><span>内容校验</span><strong>{value.evidenceConfidence === "MATCHED" ? "版本精确匹配" : "2000/2003 家族匹配"}</strong></div>
    <div><span>运行包</span><strong>{packLabel}</strong></div>
    <div><span>绑定版本</span><strong>Revision {value.runtimeBindingRevision}</strong></div>
  </div>;
}

function RPGGateList({ validation }: { validation: RPGRuntimeValidation | null }) {
  if (!validation) {return <p className="muted">点击“运行游戏”后，系统会按固定顺序采集机器证据。</p>;}
  return <ol className="review-rpg-gates">{validation.machineGates.map((gate) => <li key={gate.gate} data-status={gate.status}><span>{gateLabels[gate.gate] ?? gate.gate}</span><strong>{gateStatus(gate)}</strong></li>)}</ol>;
}

function RPGCheckpointRoundTrip({ validation }: { validation: RPGRuntimeValidation | null }) {
  const checkpoint = validation?.checkpointRoundTrip;
  if (!checkpoint?.created) {return null;}
  const payloadSize = checkpoint.sizeBytes === null ? "大小未知" : formatBytes(checkpoint.sizeBytes);
  return <div className="review-rpg-roundtrip"><strong>跨 Launch 恢复证据</strong><span>checkpoint：{payloadSize} · {checkpoint.sha256?.slice(0, 12) ?? "无摘要"}…</span><span>A：{positionText(checkpoint.initialPosition)}</span><span>B：{positionText(checkpoint.savedPosition)}</span><span>C：{positionText(checkpoint.divergedPosition)}</span><span>恢复：{positionText(checkpoint.restoredPosition)} · {checkpoint.positionVerified ? "逐字段匹配 B，且不同于 A/C" : "尚未完成精确匹配"}</span><span>恢复后输入：{positionText(checkpoint.restoreInputPosition)} · {checkpoint.restoreInputVerified ? "已证明可继续操作" : "尚未验证"}</span></div>;
}

function coreLabel(coreId: string) {
  return ({
    rpgmaker_2000: "RPG Maker 2000", rpgmaker_2003: "RPG Maker 2003", rpgmaker_xp: "RPG Maker XP",
    rpgmaker_vx: "RPG Maker VX", rpgmaker_vx_ace: "RPG Maker VX Ace", rpgmaker_mv: "RPG Maker MV", rpgmaker_mz: "RPG Maker MZ",
  } as Record<string, string>)[coreId] ?? coreId;
}
