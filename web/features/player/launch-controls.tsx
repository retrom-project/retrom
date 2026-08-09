"use client";

import Image from "next/image";
import { useState, useSyncExternalStore } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { StatusBadge } from "@/components/ui";
import { formatSaveTime } from "@/features/saves/save-library";
import { readPreferredCore, subscribePreferredCores, writePreferredCore } from "./core-preference";
import { LaunchButton } from "./launch-button";

export type CoreOption = {
  coreId: string;
  name: string;
  isDefault: boolean;
  status: "READY" | "NEEDS_VALIDATION" | "DEPENDENCY_MISSING" | "INCOMPATIBLE";
  reasons: Array<{ code: string; level: string }>;
  requiresThreads?: boolean;
};

export type DOSEntry = {
  path: string;
  originalPath: string;
  kind: "EXE" | "COM" | "BAT";
  rank: number;
  enabled: boolean;
  directLaunchSafe: boolean;
};

const coreStatusLabels: Record<CoreOption["status"], string> = {
  READY: "可运行",
  NEEDS_VALIDATION: "将在启动时检查",
  DEPENDENCY_MISSING: "缺少运行依赖",
  INCOMPATIBLE: "不兼容"
};

export function LaunchControls({ gameId, coreOptions, dosEntries, defaultDosEntry, latestSave, nowMs }: {
  gameId: string;
  coreOptions: CoreOption[];
  dosEntries: DOSEntry[];
  defaultDosEntry: string | null;
  latestSave?: { saveStateId: string; screenshotUrl: string; createdAtMs: number; coreId: string; coreName: string } | null;
  nowMs?: number;
}) {
  const initialCore = coreOptions.find((core) => core.isDefault) ?? coreOptions[0];
  const preferredCoreId = useSyncExternalStore(subscribePreferredCores, () => readPreferredCore(gameId), () => null);
  const storedCoreId = coreOptions.some((core) => core.coreId === preferredCoreId) ? preferredCoreId : null;
  const [coreOverride, setCoreOverride] = useState<{ gameId: string; coreId: string } | null>(null);
  const coreId = coreOverride?.gameId === gameId ? coreOverride.coreId : storedCoreId ?? initialCore?.coreId ?? "";
  const [dosSelection, setDosSelection] = useState<{ gameId: string; value: string | null } | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [stagedCoreId, setStagedCoreId] = useState(coreId);
  const selectedCore = coreOptions.find((core) => core.coreId === coreId);
  const blocked = !selectedCore || selectedCore.status === "DEPENDENCY_MISSING" || selectedCore.status === "INCOMPATIBLE";
  const isDOS = selectedCore?.coreId === "dosbox_pure";
  const dosEntry = dosSelection?.gameId === gameId ? dosSelection.value : isDOS ? defaultDosEntry : null;
  const usesOverride = Boolean(selectedCore && initialCore && selectedCore.coreId !== initialCore.coreId);
  const latestSaveRequiresThreads = coreOptions.find((core) => core.coreId === latestSave?.coreId)?.requiresThreads ?? false;

  function selectCore(value: string) {
    setCoreOverride({ gameId, coreId: value });
    setDosSelection({ gameId, value: value === "dosbox_pure" ? defaultDosEntry : null });
    writePreferredCore(gameId, value, initialCore?.coreId);
  }

  function openCorePicker() {
    setStagedCoreId(coreId);
    setAdvancedOpen(true);
  }

  return <aside className="launch-panel" aria-label="启动游戏">
    <span className="launch-kicker">{latestSave ? "继续游戏" : "开始游戏"}</span>
    <h2>{latestSave ? "接着最近的存档继续" : "从游戏开头开始"}</h2>
    <div className="launch-runtime-status">
      {selectedCore?.status === "READY" ? <StatusBadge tone="good">可以直接开始</StatusBadge> : null}
      {selectedCore?.status === "NEEDS_VALIDATION" ? <StatusBadge tone="info">开始时会自动检查</StatusBadge> : null}
      {blocked ? <StatusBadge tone="bad">当前运行方式需要处理</StatusBadge> : null}
    </div>
    {isDOS ? <div className="field">
      <label htmlFor="dos-entry">启动程序</label>
      <select id="dos-entry" value={dosEntry ?? ""} onChange={(event) => setDosSelection({ gameId, value: event.target.value || null })}>
        <option value="">显示 DOSBox Pure 程序菜单</option>
        {dosEntries.map((entry) => <option key={entry.path} value={entry.path} disabled={!entry.enabled || !entry.directLaunchSafe}>{entry.originalPath}{entry.path === defaultDosEntry ? " · 审核默认" : ""}{entry.directLaunchSafe ? "" : " · 仅程序菜单"}</option>)}
      </select>
    </div> : null}
    {latestSave ? <div className="launch-quick-save">
      <div><Image src={latestSave.screenshotUrl} alt="最近存档截图" fill sizes="126px" unoptimized /></div>
      <div><strong>最近存档</strong><time dateTime={new Date(latestSave.createdAtMs).toISOString()}>{formatSaveTime(latestSave.createdAtMs, nowMs ?? latestSave.createdAtMs)}</time><small>{latestSave.coreName}</small><LaunchButton gameId={gameId} saveStateId={latestSave.saveStateId} requiresThreads={latestSaveRequiresThreads} label="从存档继续" /></div>
    </div> : null}
    {latestSave
      ? <LaunchButton gameId={gameId} coreId={coreId || null} dosEntry={isDOS ? dosEntry : null} requiresThreads={selectedCore?.requiresThreads} disabled={blocked} label="重新开始游戏" />
      : <LaunchButton gameId={gameId} coreId={coreId || null} dosEntry={isDOS ? dosEntry : null} requiresThreads={selectedCore?.requiresThreads} disabled={blocked} />}
    <div className="launch-runtime-row">
      <div><small>运行方式</small><strong>{selectedCore?.name ?? "尚未配置"}</strong>{usesOverride ? <span className="launch-core-override">（未采用默认核心）</span> : null}</div>
      <button type="button" onClick={openCorePicker}>更换 ›</button>
    </div>
    <ConfirmDialog
      open={advancedOpen}
      title="更换运行方式"
      description="重新开始时使用此运行方式；恢复某份存档时，仍采用该存档保存时的 Core。"
      confirmLabel="应用"
      onCancel={() => setAdvancedOpen(false)}
      onConfirm={() => { selectCore(stagedCoreId); setAdvancedOpen(false); }}
    >
      <label className="launch-core-field" htmlFor="core"><span>运行引擎</span><select id="core" name="core" value={stagedCoreId} onChange={(event) => setStagedCoreId(event.target.value)}>
        {coreOptions.map((core) => <option key={core.coreId} value={core.coreId} disabled={core.status === "DEPENDENCY_MISSING" || core.status === "INCOMPATIBLE"}>{core.name}{core.isDefault ? " · 推荐" : ""} · {coreStatusLabels[core.status]}</option>)}
      </select></label>
    </ConfirmDialog>
  </aside>;
}
