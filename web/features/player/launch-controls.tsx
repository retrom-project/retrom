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

const coreStatusLabels: Record<CoreOption["status"], string> = {
  READY: "可运行",
  NEEDS_VALIDATION: "将在启动时检查",
  DEPENDENCY_MISSING: "缺少运行依赖",
  INCOMPATIBLE: "不兼容"
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
    <span className="launch-kicker">推荐配置</span>
    <h2>准备开始游戏</h2>
    {selectedCore?.status === "READY" ? <p><StatusBadge tone="good">可以直接开始</StatusBadge></p> : null}
    {selectedCore?.status === "NEEDS_VALIDATION" ? <p><StatusBadge tone="info">开始时会自动检查</StatusBadge></p> : null}
    {blocked ? <p><StatusBadge tone="bad">当前运行方式需要处理</StatusBadge></p> : null}
    {isDOS ? <div className="field">
      <label htmlFor="dos-entry">启动程序</label>
      <select id="dos-entry" value={dosEntry ?? ""} onChange={(event) => setDosEntry(event.target.value || null)}>
        <option value="">显示 DOSBox Pure 程序菜单</option>
        {dosEntries.map((entry) => <option key={entry.path} value={entry.path} disabled={!entry.enabled || !entry.directLaunchSafe}>{entry.originalPath}{entry.path === defaultDosEntry ? " · 审核默认" : ""}{entry.directLaunchSafe ? "" : " · 仅程序菜单"}</option>)}
      </select>
    </div> : null}
    <p className="launch-help">系统已选择适合这个目录的运行方式，开始前会自动检查所需文件。</p>
    <LaunchButton gameId={gameId} coreId={coreId || null} dosEntry={isDOS ? dosEntry : null} disabled={blocked} />
    <details className="launch-advanced">
      <summary>更换运行方式</summary>
      <div className="launch-advanced-popover">
        <div className="field">
          <label htmlFor="core">运行引擎</label>
          <select id="core" name="core" value={coreId} onChange={(event) => selectCore(event.target.value)}>
            {coreOptions.map((core) => <option key={core.coreId} value={core.coreId} disabled={core.status === "DEPENDENCY_MISSING" || core.status === "INCOMPATIBLE"}>{core.name}{core.isDefault ? " · 推荐" : ""} · {coreStatusLabels[core.status]}</option>)}
          </select>
        </div>
        <p>只有遇到兼容问题或需要特定存档时才建议更改。</p>
      </div>
    </details>
  </aside>;
}
