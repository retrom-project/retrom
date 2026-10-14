"use client";
import { useCallback, useEffect, useId, useRef } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";

const noEntries: string[] = [];

export function RPGEntrySelector({ gameId, engine, value, onSelect }: {
  gameId: string; engine: Schema<"RuntimeContent">["engine"]; value: string; onSelect: (path: string) => void;
}) {
  const id = useId();
  const select = useRef<HTMLSelectElement>(null);
  const load = useCallback(async (signal?: AbortSignal) => result(await api.GET("/api/v1/admin/games/{gameId}/runtime-options/rpgmaker", {
    signal, params: { path: { gameId }, query: { engine } },
  })), [gameId, engine]);
  const candidates = useResource(load);
  const entries = candidates.data?.entries ?? noEntries;
  const invalid = !!candidates.data && !!value && !entries.includes(value);
  const unique = entries.length === 1 && entries[0] === value;
  useEffect(() => {
    select.current?.setCustomValidity(invalid ? "请重新选择有效入口文件。" : candidates.data && !entries.length ? "未找到可用入口。" : "");
  }, [invalid, candidates.data, entries.length, unique]);
  useEffect(() => {
    if (!value && entries.length === 1) { onSelect(entries[0]); }
  }, [value, entries, onSelect]);
  return <div>
    <label className="field">入口文件
      {unique ? <input value={value} title={value} readOnly /> : <select ref={select} value={value}
        required disabled={candidates.loading || !!candidates.error} aria-invalid={invalid} aria-describedby={`${id}-help`}
        onChange={event => onSelect(event.target.value)}>
        <option value="">请选择入口</option>
        {value && !entries.includes(value) ? <option value={value} disabled>{value}{invalid ? "（无效入口）" : ""}</option> : null}
        {entries.map(entry => <option key={entry} value={entry}>{entry}</option>)}
      </select>}
    </label>
    <EntryFeedback id={`${id}-help`} loading={candidates.loading} error={candidates.error} onRetry={candidates.reload}
      invalid={invalid} value={value} empty={!!candidates.data && !entries.length} />
  </div>;
}

function EntryFeedback({ id, loading, error, onRetry, invalid, value, empty }: {
  id: string; loading: boolean; error: string; onRetry: () => void; invalid: boolean; value: string; empty: boolean;
}) {
  return <div id={id}>
    {loading ? <p role="status">正在读取入口…</p> : null}
    {error ? <p role="alert">无法读取入口。<button type="button" className="button secondary" onClick={onRetry}>重试读取入口</button></p> : null}
    {invalid ? <p role="alert">原入口“{value}”不适用于当前游戏版本，请重新选择并保存。</p> : null}
    {empty ? <p role="alert">未找到可用入口，请检查游戏文件和 RPG Maker 版本。</p> : null}
  </div>;
}
