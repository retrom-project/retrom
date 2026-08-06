"use client";

import { useState } from "react";
import { StatusBadge } from "@/components/ui";
import { LaunchButton } from "./launch-button";

export type CoreOption = {
  coreId: string;
  name: string;
  isDefault: boolean;
  status: "READY" | "NEEDS_VALIDATION" | "DEPENDENCY_MISSING" | "INCOMPATIBLE";
  reasons: Array<{ code: string; level: string }>;
};

export type DOSEntry = {
  path: string;
  originalPath: string;
  kind: "EXE" | "COM" | "BAT";
  rank: number;
  enabled: boolean;
  directLaunchSafe: boolean;
};

export function LaunchControls({ gameId, coreOptions, dosEntries, defaultDosEntry }: {
  gameId: string;
  coreOptions: CoreOption[];
  dosEntries: DOSEntry[];
  defaultDosEntry: string | null;
}) {
  const initialCore = coreOptions.find((core) => core.isDefault) ?? coreOptions[0];
  const [coreId, setCoreId] = useState(initialCore?.coreId ?? "");
  const [dosEntry, setDosEntry] = useState<string | null>(initialCore?.coreId === "dosbox_pure" ? defaultDosEntry : null);
  const selectedCore = coreOptions.find((core) => core.coreId === coreId);
  const blocked = !selectedCore || selectedCore.status === "DEPENDENCY_MISSING" || selectedCore.status === "INCOMPATIBLE";
  const isDOS = selectedCore?.coreId === "dosbox_pure";

  function selectCore(value: string) {
    setCoreId(value);
    setDosEntry(value === "dosbox_pure" ? defaultDosEntry : null);
  }

  return <aside className="launch-panel">
    <h2>启动配置</h2>
    <div className="field">
      <label htmlFor="core">本次运行核心</label>
      <select id="core" name="core" value={coreId} onChange={(event) => selectCore(event.target.value)}>
        {coreOptions.map((core) => <option key={core.coreId} value={core.coreId} disabled={core.status === "DEPENDENCY_MISSING" || core.status === "INCOMPATIBLE"}>{core.name}{core.isDefault ? " · 目录默认" : ""} · {core.status}</option>)}
      </select>
    </div>
    {isDOS ? <div className="field">
      <label htmlFor="dos-entry">启动程序</label>
      <select id="dos-entry" value={dosEntry ?? ""} onChange={(event) => setDosEntry(event.target.value || null)}>
        <option value="">显示 DOSBox Pure 程序菜单</option>
        {dosEntries.map((entry) => <option key={entry.path} value={entry.path} disabled={!entry.enabled || !entry.directLaunchSafe}>{entry.originalPath}{entry.path === defaultDosEntry ? " · 审核默认" : ""}{entry.directLaunchSafe ? "" : " · 仅程序菜单"}</option>)}
      </select>
    </div> : null}
    {selectedCore?.status === "READY" ? <p><StatusBadge tone="good">运行依赖已就绪</StatusBadge></p> : null}
    {selectedCore?.status === "NEEDS_VALIDATION" ? <p><StatusBadge tone="neutral">启动时验证此核心</StatusBadge></p> : null}
    {blocked ? <p><StatusBadge tone="bad">当前核心需要修复依赖</StatusBadge></p> : null}
    <p className="game-meta">启动时会再次验证 Core、DAT、BIOS 与内容 revision。</p>
    <LaunchButton gameId={gameId} coreId={coreId || null} dosEntry={isDOS ? dosEntry : null} disabled={blocked} />
  </aside>;
}
