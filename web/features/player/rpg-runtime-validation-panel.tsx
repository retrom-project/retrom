"use client";

import { useSyncExternalStore } from "react";
import { rpgValidationGates, type RpgGate, type RpgPosition } from "./rpg-validation-protocol";
import {
  canReturnToReview,
  formatRpgPosition,
  gateEvidenceText,
  validationTerminalSequence,
} from "./rpg-runtime-validation-panel-model";
import type { RpgValidationSnapshot } from "./rpg-runtime-validation";

const gateLabels: Record<RpgGate, string> = {
  RUNTIME_READY: "运行时加载",
  ENGINE_PROFILE: "引擎与版本",
  FRAMES_300: "连续 300 帧",
  INPUT: "输入",
  AUDIO: "音频",
  INITIAL_POSITION_RECORDED: "记录 A",
  SAVE_POINT_RECORDED: "记录 B",
  CHECKPOINT_CREATED: "创建检查点",
  POST_SAVE_STATE_DIVERGED: "继续到 C",
  ORIGINAL_LAUNCH_ENDED: "结束原运行",
  RESTORE_STARTED: "不同 Launch 恢复",
  RESTORE_POSITION_VERIFIED: "精确回到 B",
  RESTORE_SCREENSHOT: "恢复截图",
  RESTORE_INPUT: "恢复后输入",
};

type PanelDriver = {
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => RpgValidationSnapshot;
  runAction: () => Promise<void>;
};

export function RpgRuntimeValidationPanel({ driver }: { driver: PanelDriver }) {
  const snapshot = useSyncExternalStore(driver.subscribe, driver.getSnapshot, driver.getSnapshot);
  const passed = rpgValidationGates.filter((gate) => snapshot.gates[gate] === "PASSED").length;
  return <aside className="rpg-validation-overlay" aria-label="RPG Maker 运行验证">
    <section className="rpg-validation-panel" aria-live="polite">
      <header>
        <div><small>RPG Maker 运行验证</small><h2>{snapshot.title}</h2></div>
        <strong>{passed} / {rpgValidationGates.length}</strong>
      </header>
      <p>{snapshot.message}</p>
      {snapshot.error ? <p className="rpg-validation-error" role="alert">{snapshot.error}</p> : null}
      <dl className="rpg-validation-positions">
        <IdentityRow label="当前身份" value={snapshot.launchRole === "restore" ? "Restore Launch" : "Original Launch"} />
        <IdentityRow label="服务端序号" value={`${snapshot.lastGateSequence} / ${validationTerminalSequence}`} />
        <IdentityRow label="Original Launch" value={snapshot.originalLaunchId} />
        <IdentityRow label="Restore Launch" value={snapshot.restoreLaunchId ?? "尚未创建"} />
      </dl>
      <ol className="rpg-validation-gates">
        {snapshot.machineGates.map((machineGate) => <GateRow key={machineGate.gate} machineGate={machineGate} />)}
      </ol>
      <PositionEvidence
        initial={snapshot.initialPosition}
        saved={snapshot.savedPosition}
        diverged={snapshot.divergedPosition}
        restored={snapshot.restoredPosition}
        restoreInput={snapshot.restoreInputPosition}
      />
      {snapshot.actionLabel ? <button type="button" disabled={snapshot.busy} onClick={() => void driver.runAction()}>
        {snapshot.busy ? "正在提交机器证据…" : snapshot.actionLabel}
      </button> : null}
      {canReturnToReview(snapshot) ? <button type="button" onClick={() => window.close()}>返回审核决定</button> : null}
    </section>
  </aside>;
}

function GateRow({ machineGate }: { machineGate: RpgValidationSnapshot["machineGates"][number] }) {
  const evidence = gateEvidenceText(machineGate);
  return <li data-status={machineGate.status}>
    <i aria-hidden="true" />
    <div style={{ display: "grid", minWidth: 0 }}>
      <span>{gateLabels[machineGate.gate]}</span>
      <small title={evidence} style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        <code>{machineGate.gate}</code> · {evidence}
      </small>
    </div>
    <strong>{statusLabel(machineGate.status)}</strong>
  </li>;
}

type PositionEvidenceProps = {
  initial: RpgPosition | null;
  saved: RpgPosition | null;
  diverged: RpgPosition | null;
  restored: RpgPosition | null;
  restoreInput: RpgPosition | null;
};

function PositionEvidence({ initial, saved, diverged, restored, restoreInput }: PositionEvidenceProps) {
  return <dl className="rpg-validation-positions">
    <PositionRow label="初始 A" position={initial} />
    <PositionRow label="保存 B" position={saved} />
    <PositionRow label="继续 C" position={diverged} />
    <PositionRow label="恢复到 B" position={restored} />
    <PositionRow label="恢复后输入" position={restoreInput} />
  </dl>;
}

function PositionRow({ label, position }: { label: string; position: RpgPosition | null }) {
  return <div><dt>{label}</dt><dd>{formatRpgPosition(position)}</dd></div>;
}

function IdentityRow({ label, value }: { label: string; value: string }) {
  return <div><dt>{label}</dt><dd title={value}>{value}</dd></div>;
}

function statusLabel(status: "NOT_STARTED" | "IN_PROGRESS" | "PASSED" | "FAILED") {
  if (status === "PASSED") {return "通过";}
  if (status === "IN_PROGRESS") {return "进行中";}
  if (status === "FAILED") {return "失败";}
  return "等待";
}
