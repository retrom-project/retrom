import {useId} from "react";
import type {DOSEntry} from "./launch-controls";

export function DOSProgramPicker({defaultDosEntry, dosEntries, onChange, value}: {
  defaultDosEntry: string | null;
  dosEntries: DOSEntry[];
  onChange: (value: string | null) => void;
  value: string | null;
}) {
  const id = useId();
  return <div className="field dos-program-field">
    <label htmlFor={id}>启动程序</label>
    <select id={id} value={value ?? ""} onChange={event => onChange(event.target.value || null)}>
      <option value="">显示 DOSBox Pure 程序菜单</option>
      {dosEntries.map(entry => <option key={entry.path} value={entry.path} disabled={!entry.enabled || !entry.directLaunchSafe}>{entry.originalPath}{entry.path === defaultDosEntry ? " · 审核默认" : ""}{entry.directLaunchSafe ? "" : " · 仅程序菜单"}</option>)}
    </select>
  </div>;
}
