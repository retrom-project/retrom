"use client";

import { useState, useSyncExternalStore } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { StatusBadge } from "@/components/ui";
import { SaveScreenshot } from "@/features/saves/save-screenshot";
import { useSaveTimeFormatter } from "@/features/saves/use-save-time";
import { useAuth } from "@/features/auth/auth-provider";
import { readPreferredCore, subscribePreferredCores, writePreferredCore } from "./core-preference";
import { decodePreferredDOSEntry, readPreferredDOSEntry, subscribePreferredDOSEntries, writePreferredDOSEntry } from "./dos-entry-preference";
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

type LatestSave = {
  saveStateId: string;
  screenshotUrl: string | null;
  createdAtMs: number;
  coreId: string;
  coreName: string;
  discIndex?: number | null;
  discLabel?: string | null;
};

function selectedCoreId(coreOptions: CoreOption[], preferredCoreId: string | null, override: { gameId: string; coreId: string } | null, gameId: string) {
  const initial = coreOptions.find((core) => core.isDefault) ?? coreOptions[0];
  const stored = coreOptions.some((core) => core.coreId === preferredCoreId) ? preferredCoreId : null;
  return { id: override?.gameId === gameId ? override.coreId : stored ?? initial?.coreId ?? "", initial };
}

function selectedDOSEntry(dosEntries: DOSEntry[], defaultDosEntry: string | null, preferredRaw: string | null) {
  const preferred = decodePreferredDOSEntry(preferredRaw);
  const preferredValid = preferred.entry === null || dosEntries.some((entry) => entry.path === preferred.entry && entry.enabled && entry.directLaunchSafe);
  const safeDefault = dosEntries.some((entry) => entry.path === defaultDosEntry && entry.enabled && entry.directLaunchSafe) ? defaultDosEntry : null;
  return preferred.present && preferredValid ? preferred.entry : safeDefault;
}

function RuntimeStatus({ blocked, selectedCore }: { blocked: boolean; selectedCore: CoreOption | undefined }) {
  if (blocked) {return <StatusBadge tone="bad">当前运行方式需要处理</StatusBadge>;}
  if (selectedCore?.status === "READY") {return <StatusBadge tone="good">可以直接开始</StatusBadge>;}
  if (selectedCore?.status === "NEEDS_VALIDATION") {return <StatusBadge tone="info">开始时会自动检查</StatusBadge>;}
  return null;
}

function DOSProgramPicker({ defaultDosEntry, dosEntries, onChange, value }: {
  defaultDosEntry: string | null;
  dosEntries: DOSEntry[];
  onChange: (value: string | null) => void;
  value: string | null;
}) {
  return <div className="field">
    <label htmlFor="dos-entry">启动程序</label>
    <select id="dos-entry" value={value ?? ""} onChange={(event) => onChange(event.target.value || null)}>
      <option value="">显示 DOSBox Pure 程序菜单</option>
      {dosEntries.map((entry) => <option key={entry.path} value={entry.path} disabled={!entry.enabled || !entry.directLaunchSafe}>{entry.originalPath}{entry.path === defaultDosEntry ? " · 审核默认" : ""}{entry.directLaunchSafe ? "" : " · 仅程序菜单"}</option>)}
    </select>
  </div>;
}

function LatestSaveCard({ gameId, latestSave, nowMs, requiresThreads }: { gameId: string; latestSave: LatestSave; nowMs: number | undefined; requiresThreads: boolean }) {
  const formatTime = useSaveTimeFormatter();
  return <div className="launch-quick-save">
    <div><SaveScreenshot screenshotUrl={latestSave.screenshotUrl} alt="最近存档" sizes="126px" /></div>
    <div><strong>最近存档</strong><time dateTime={new Date(latestSave.createdAtMs).toISOString()}>{formatTime(latestSave.createdAtMs, nowMs ?? latestSave.createdAtMs)}</time><small>{latestSave.coreName}{latestSave.discLabel ? ` · ${latestSave.discLabel}` : ""}</small><LaunchButton gameId={gameId} saveStateId={latestSave.saveStateId} requiresThreads={requiresThreads} label="从存档继续" /></div>
  </div>;
}

type LaunchViewProps = {
  advancedOpen: boolean;
  blocked: boolean;
  coreId: string;
  coreOptions: CoreOption[];
  defaultDosEntry: string | null;
  dosEntries: DOSEntry[];
  dosEntry: string | null;
  gameId: string;
  isDOS: boolean;
  latestSave: LatestSave | null | undefined;
  latestSaveRequiresThreads: boolean;
  nowMs: number | undefined;
  onAdvancedClose: () => void;
  onAdvancedOpen: () => void;
  onCoreApply: () => void;
  onDOSChange: (value: string | null) => void;
  onLaunchCreated: (() => void) | undefined;
  onStagedCoreChange: (value: string) => void;
  selectedCore: CoreOption | undefined;
  stagedCoreId: string;
  usesOverride: boolean;
};

function DesktopLaunchPanel(props: LaunchViewProps) {
  return <aside className="launch-panel" aria-label="启动游戏">
    <span className="launch-kicker">{props.latestSave ? "继续游戏" : "开始游戏"}</span>
    <h2>{props.latestSave ? "接着最近的存档继续" : "从游戏开头开始"}</h2>
    <div className="launch-runtime-status"><RuntimeStatus blocked={props.blocked} selectedCore={props.selectedCore} /></div>
    {props.isDOS ? <DOSProgramPicker defaultDosEntry={props.defaultDosEntry} dosEntries={props.dosEntries} onChange={props.onDOSChange} value={props.dosEntry} /> : null}
    {props.latestSave ? <LatestSaveCard gameId={props.gameId} latestSave={props.latestSave} nowMs={props.nowMs} requiresThreads={props.latestSaveRequiresThreads} /> : null}
    <LaunchButton gameId={props.gameId} coreId={props.coreId || null} dosEntry={props.isDOS ? props.dosEntry : null} requiresThreads={props.selectedCore?.requiresThreads} disabled={props.blocked} label={props.latestSave ? "重新开始游戏" : undefined} onLaunchCreated={props.onLaunchCreated} />
    <div className="launch-runtime-row"><div><small>运行方式</small><strong>{props.selectedCore?.name ?? "尚未配置"}</strong>{props.usesOverride ? <span className="launch-core-override">（未采用默认核心）</span> : null}</div><button type="button" onClick={props.onAdvancedOpen}>更换 ›</button></div>
    <ConfirmDialog open={props.advancedOpen} title="更换运行方式" description="重新开始时使用此运行方式；恢复某份存档时，仍采用该存档保存时的 Core。" confirmLabel="应用" onCancel={props.onAdvancedClose} onConfirm={props.onCoreApply}>
      <label className="launch-core-field" htmlFor="core"><span>运行引擎</span><select id="core" name="core" value={props.stagedCoreId} onChange={(event) => props.onStagedCoreChange(event.target.value)}>{props.coreOptions.map((core) => <option key={core.coreId} value={core.coreId} disabled={core.status === "DEPENDENCY_MISSING" || core.status === "INCOMPATIBLE"}>{core.name}{core.isDefault ? " · 推荐" : ""} · {coreStatusLabels[core.status]}</option>)}</select></label>
    </ConfirmDialog>
  </aside>;
}

function MobileLaunchDock(props: LaunchViewProps) {
  const formatTime = useSaveTimeFormatter();
  const label = props.latestSave ? "最近存档" : props.blocked ? "当前不可启动" : "推荐运行方式";
  const detail = props.latestSave ? formatTime(props.latestSave.createdAtMs, props.nowMs ?? props.latestSave.createdAtMs) : props.selectedCore?.name ?? "尚未配置";
  return <div className="mobile-launch-dock" aria-label="快速启动">
    <div><small>{label}</small><strong>{detail}</strong></div>
    {props.latestSave
      ? <LaunchButton gameId={props.gameId} saveStateId={props.latestSave.saveStateId} requiresThreads={props.latestSaveRequiresThreads} label="从存档继续" />
      : <LaunchButton gameId={props.gameId} coreId={props.coreId || null} dosEntry={props.isDOS ? props.dosEntry : null} requiresThreads={props.selectedCore?.requiresThreads} disabled={props.blocked} onLaunchCreated={props.onLaunchCreated} />}
  </div>;
}

export function LaunchControls({ gameId, coreOptions, dosEntries, defaultDosEntry, latestSave, nowMs }: {
  gameId: string;
  coreOptions: CoreOption[];
  dosEntries: DOSEntry[];
  defaultDosEntry: string | null;
  latestSave?: { saveStateId: string; screenshotUrl: string | null; createdAtMs: number; coreId: string; coreName: string; discIndex?: number | null; discLabel?: string | null } | null;
  nowMs?: number;
}) {
  const { context } = useAuth();
  const userId = context.user?.userId;
  const preferredCoreId = useSyncExternalStore(subscribePreferredCores, () => readPreferredCore(userId, gameId), () => null);
  const preferredDOSEntryRaw = useSyncExternalStore(subscribePreferredDOSEntries, () => readPreferredDOSEntry(userId, gameId), () => null);
  const [coreOverride, setCoreOverride] = useState<{ gameId: string; coreId: string } | null>(null);
  const selection = selectedCoreId(coreOptions, preferredCoreId, coreOverride, gameId);
  const coreId = selection.id;
  const [dosSelection, setDosSelection] = useState<{ gameId: string; value: string | null } | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [stagedCoreId, setStagedCoreId] = useState(coreId);
  const selectedCore = coreOptions.find((core) => core.coreId === coreId);
  const blocked = !selectedCore || selectedCore.status === "DEPENDENCY_MISSING" || selectedCore.status === "INCOMPATIBLE";
  const isDOS = selectedCore?.coreId === "dosbox_pure";
  const restoredDOSEntry = selectedDOSEntry(dosEntries, defaultDosEntry, preferredDOSEntryRaw);
  const dosEntry = dosSelection?.gameId === gameId ? dosSelection.value : isDOS ? restoredDOSEntry : null;
  const usesOverride = Boolean(selectedCore && selection.initial && selectedCore.coreId !== selection.initial.coreId);
  const latestSaveRequiresThreads = coreOptions.find((core) => core.coreId === latestSave?.coreId)?.requiresThreads ?? false;

  function selectCore(value: string) {
    setCoreOverride({ gameId, coreId: value });
    setDosSelection({ gameId, value: value === "dosbox_pure" ? restoredDOSEntry : null });
    writePreferredCore(userId, gameId, value, selection.initial?.coreId);
  }

  function openCorePicker() {
    setStagedCoreId(coreId);
    setAdvancedOpen(true);
  }

  const view: LaunchViewProps = {
    advancedOpen, blocked, coreId, coreOptions, defaultDosEntry, dosEntries, dosEntry, gameId, isDOS,
    latestSave, latestSaveRequiresThreads, nowMs,
    onAdvancedClose: () => setAdvancedOpen(false), onAdvancedOpen: openCorePicker,
    onCoreApply: () => { selectCore(stagedCoreId); setAdvancedOpen(false); },
    onDOSChange: (value) => setDosSelection({ gameId, value }),
    onLaunchCreated: isDOS ? () => writePreferredDOSEntry(userId, gameId, dosEntry) : undefined,
    onStagedCoreChange: setStagedCoreId, selectedCore, stagedCoreId, usesOverride,
  };
  return <><DesktopLaunchPanel {...view} /><MobileLaunchDock {...view} /></>;
}
