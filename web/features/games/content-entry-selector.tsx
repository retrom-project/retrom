"use client";
import {useCallback, useId, useState} from "react";
import {api, result} from "@/lib/api/client";
import {useResource, type Resource} from "@/lib/use-resource";
import type {Schema} from "@/lib/api/types";

export function ContentEntrySelector({gameId, kind, value, onSelect}: {
  gameId: string; kind: Schema<"RuntimeContent">["kind"]; value: string; onSelect: (path: string) => void;
}) {
  const prefixId = useId();
  const [prefix, setPrefix] = useState("");
  const [after, setAfter] = useState("");
  const load = useCallback((signal?: AbortSignal) => loadEntries(gameId, kind, after, prefix, signal), [gameId, kind, after, prefix]);
  const page = useResource(load);
  const entries = page.data?.entries ?? [];
  return <div className="content-entry-selector stack">
    <label className="field">入口文件
      <select value={value} required disabled={page.loading || !!page.error} onChange={event => onSelect(event.target.value)}>
        <option value="">请选择入口</option>
        {value && !entries.includes(value) ? <option value={value}>{value}</option> : null}
        {entries.map(path => <option key={path} value={path}>{path}</option>)}
      </select>
    </label>
    <div className="field">
      <label htmlFor={prefixId}>按路径筛选</label>
      <input id={prefixId} value={prefix} placeholder="例如 scripts/" aria-describedby={`${prefixId}-hint`}
        onChange={event => {setPrefix(event.target.value); setAfter("");}} />
      <small id={`${prefixId}-hint`}>按路径开头筛选；留空显示全部候选。筛选和翻页保留当前选择。</small>
    </div>
    <EntryFeedback page={page} />
    <div className="content-entry-actions">
      <button type="button" className="button secondary" disabled={page.loading || !after} onClick={() => setAfter("")}>返回第一页</button>
      <button type="button" className="button secondary" disabled={page.loading || !page.data?.nextCursor}
        onClick={() => setAfter(page.data?.nextCursor ?? "")}>下一页入口</button>
    </div>
  </div>;
}

function EntryFeedback({page}: {page: Resource<Schema<"ProjectEntryPage">>}) {
  if (page.loading) {return <p role="status">正在读取入口…</p>;}
  if (page.error) {return <p role="alert">{page.error}<button type="button" className="button secondary" onClick={page.reload}>重试</button></p>;}
  const entries = page.data?.entries ?? [];
  return <p className="workspace-note" aria-live="polite">{entries.length
    ? `本页 ${entries.length} 个候选${page.data?.nextCursor ? "" : "，已到最后一页"}。选择入口后保存生效。`
    : "没有匹配的入口候选。"}</p>;
}

async function loadEntries(gameId: string, kind: Schema<"RuntimeContent">["kind"], after: string, prefix: string, signal?: AbortSignal) {
  const params = {path: {gameId}, query: {after, prefix, limit: 100}};
  if (kind === "KIRIKIRI_PROJECT") {
    return result(await api.GET("/api/v1/admin/games/{gameId}/runtime-options/kirikiri", {signal, params}));
  }
  const page = result(await api.GET("/api/v1/admin/games/{gameId}/content", {signal, params}));
  return {entries: page.items.map(file => file.logicalKey), nextCursor: page.nextCursor};
}
