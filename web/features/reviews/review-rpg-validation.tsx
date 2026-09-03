"use client";

import { useCallback, useEffect, useRef, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
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
  const popupWatchRef = useRef<number | null>(null);
  const popupRefreshRef = useRef<number | null>(null);
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

  const stopPopupWatch = useCallback(() => {
    if (popupWatchRef.current !== null) {
      window.clearInterval(popupWatchRef.current);
      popupWatchRef.current = null;
    }
    if (popupRefreshRef.current !== null) {
      window.clearTimeout(popupRefreshRef.current);
      popupRefreshRef.current = null;
    }
  }, []);

  const watchPopupClose = useCallback((popup: Window, validationId: string) => {
    stopPopupWatch();
    popupWatchRef.current = window.setInterval(() => {
      if (!popup.closed) {return;}
      if (popupWatchRef.current !== null) {
        window.clearInterval(popupWatchRef.current);
        popupWatchRef.current = null;
      }
      popupRefreshRef.current = window.setTimeout(() => {
        popupRefreshRef.current = null;
        void readValidation(validationId).then((next) => {
          if (["PASSED", "FAILED", "EXPIRED"].includes(next.state)) {
            setNotice("游戏窗口已关闭，可以再次运行游戏。");
          }
        }).catch(() => setNotice("游戏窗口已关闭；刷新页面可获取最新验证状态。"));
      }, 350);
    }, 250);
  }, [readValidation, setNotice, stopPopupWatch]);

  useEffect(() => stopPopupWatch, [stopPopupWatch]);

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
      watchPopupClose(popup, created.validationId);
      setNotice("游戏窗口已打开；确认游戏可运行后即可返回审核页发布，高级恢复验证为可选。");
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
      watchPopupClose(popup, validation.validationId);
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
    canCreate: !validation || !rpgMaker?.runtimeValidationCurrent || ["PASSED", "FAILED", "EXPIRED"].includes(validation.state),
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

function evidenceRecord(evidence: unknown): Record<string, unknown> | null {
  return typeof evidence === "object" && evidence !== null && !Array.isArray(evidence)
    ? evidence as Record<string, unknown>
    : null;
}

function gateEvidenceText(evidence: unknown) {
  const value = evidenceRecord(evidence);
  if (!value || Object.keys(value).length === 0) {return "无额外证据";}
  if ([value.mapId, value.playerX, value.playerY, value.fixtureState].every(Number.isInteger)) {
    return positionText({ mapId: Number(value.mapId), playerX: Number(value.playerX), playerY: Number(value.playerY), fixtureState: Number(value.fixtureState) });
  }
  if (Number.isInteger(value.continuousFrames)) {return `连续帧 ${value.continuousFrames}`;}
  if (typeof value.observed === "boolean") {return `observed=${String(value.observed)}`;}
  if (typeof value.generation === "string" && typeof value.engineProfile === "string") {
    return `${value.generation} · ${value.engineProfile}`;
  }
  if (typeof value.checkpointFormat === "string" && Number.isInteger(value.sizeBytes) && typeof value.sha256 === "string") {
    return `${value.checkpointFormat} · ${formatBytes(Number(value.sizeBytes))} · SHA-256 ${value.sha256.slice(0, 12)}…`;
  }
  return "服务端证据已记录";
}

export function RPGValidationCard({ value, disabled, onChange }: {
  value: RPGMakerReview;
  disabled: boolean;
  onChange: (next: RPGMakerReview) => void;
}) {
  const validation = value.runtimeValidation;
  return <section className="panel review-rpg-validation">
    <div className="panel-head"><div><h2>RPG Maker 运行验证</h2><p>先运行一次游戏即可发布；机器门禁与跨 Launch 恢复是可选高级验证。</p></div></div>
    <div className="panel-body">
      <RPGValidationFacts value={value} />
      <RPGPackControls value={value} disabled={disabled} onChange={onChange} />
      <RPGValidationEvidence validation={validation} />
    </div>
  </section>;
}

function RPGValidationEvidence({ validation }: { validation: RPGRuntimeValidation | null }) {
  if (!validation) {
    return <p className="muted">点击“运行游戏”后即可发布；如需深入验证，可继续按固定顺序采集机器证据。</p>;
  }
  return <details className="review-rpg-evidence">
    <summary>
      <span className="review-rpg-evidence-summary"><strong>高级验证详情</strong><small>{validationSummary(validation)}</small></span>
      <span className="review-rpg-evidence-chevron" aria-hidden="true">⌄</span>
    </summary>
    <div className="review-rpg-evidence-body">
      <p>实时进度与完整证据也会显示在运行游戏窗口中。</p>
      <RPGValidationIdentity validation={validation} />
      <RPGGateList validation={validation} />
      <RPGCheckpointRoundTrip validation={validation} />
    </div>
  </details>;
}

function validationSummary(validation: RPGRuntimeValidation) {
  if (validation.state === "FAILED") {
    const failure = validation.failureCode ?? validation.machineGates.find((gate) => gate.status === "FAILED")?.failureCode;
    return `验证失败${failure ? ` · ${failure}` : ""}`;
  }
  if (validation.state === "EXPIRED") {return "验证已过期";}
  if (["AWAITING_DECISION", "PASSED"].includes(validation.state)) {
    return `服务端进度 ${validation.lastGateSequence} / 28 · 已完成`;
  }
  return `服务端进度 ${validation.lastGateSequence} / 28 · 进行中`;
}

function RPGValidationIdentity({ validation }: { validation: RPGRuntimeValidation | null }) {
  if (!validation) {return null;}
  return <div className="review-rpg-roundtrip"><strong>服务端验证身份</strong><span>Provider Target：<code>{validation.routeEvidence.providerId}/{validation.routeEvidence.targetId}</code></span><span>original Launch：<code>{validation.launchId ?? "尚未创建"}</code></span><span>restore Launch：<code>{validation.restoreLaunchId ?? "尚未创建"}</code></span><span>服务端 gate 序号：{validation.lastGateSequence}</span></div>;
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
  if (!validation) {return null;}
  return <ol className="review-rpg-gates">{validation.machineGates.map((gate) => <li key={gate.gate} data-status={gate.status}><span>{gateLabels[gate.gate] ?? gate.gate}<small>{gateEvidenceText(gate.evidence)}</small></span><strong>{gateStatus(gate)}</strong></li>)}</ol>;
}

function RPGCheckpointRoundTrip({ validation }: { validation: RPGRuntimeValidation | null }) {
  const checkpoint = validation?.checkpointRoundTrip;
  if (!checkpoint?.created) {return null;}
  const payloadSize = checkpoint.sizeBytes === null ? "大小未知" : formatBytes(checkpoint.sizeBytes);
  return <div className="review-rpg-roundtrip"><strong>跨 Launch 恢复证据</strong><span>checkpoint：{checkpoint.checkpointFormat} · {payloadSize} · {checkpoint.sha256?.slice(0, 12) ?? "无摘要"}…</span><span>A：{positionText(checkpoint.initialPosition)}</span><span>B：{positionText(checkpoint.savedPosition)}</span><span>C：{positionText(checkpoint.divergedPosition)}</span><span>恢复：{positionText(checkpoint.restoredPosition)} · {checkpoint.positionVerified ? "逐字段匹配 B，且不同于 A/C" : "尚未完成精确匹配"}</span><span>恢复后输入：{positionText(checkpoint.restoreInputPosition)} · {checkpoint.restoreInputVerified ? "已证明可继续操作" : "尚未验证"}</span></div>;
}

function coreLabel(coreId: string) {
  return ({
    rpgmaker_2000: "RPG Maker 2000", rpgmaker_2003: "RPG Maker 2003", rpgmaker_xp: "RPG Maker XP",
    rpgmaker_vx: "RPG Maker VX", rpgmaker_vx_ace: "RPG Maker VX Ace", rpgmaker_mv: "RPG Maker MV", rpgmaker_mz: "RPG Maker MZ",
  } as Record<string, string>)[coreId] ?? coreId;
}
