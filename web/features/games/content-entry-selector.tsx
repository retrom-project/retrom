"use client";
import {useCallback, useId, useState} from "react";
import {api, result} from "@/lib/api/client";
import {useResource, type Resource} from "@/lib/use-resource";
import {formatBytes} from "@/lib/format-bytes";
import type {Schema} from "@/lib/api/types";

export function ContentEntrySelector({gameId, value, onSelect}: {
  gameId: string; value: string; onSelect: (path: string) => void;
}) {
  const [prefix, setPrefix] = useState("");
  const [after, setAfter] = useState("");
  const load = useCallback(async (signal?: AbortSignal) => result(await api.GET("/api/v1/admin/games/{gameId}/content", {
    signal, params: {path: {gameId}, query: {after, prefix, limit: 100}},
  })), [gameId, after, prefix]);
  const page = useResource(load);
  const files = page.data?.items ?? [];
  return <div className="content-entry-selector stack">
    <label className="field">入口文件
      <select value={value} required disabled={page.loading} onChange={event => onSelect(event.target.value)}>
        <option value="">请选择入口</option>
        {value && !files.some(file => file.logicalKey === value) ? <option value={value}>{value}</option> : null}
        {files.map(file => <option key={file.logicalKey} value={file.logicalKey}>{file.logicalKey}</option>)}
      </select>
    </label>
    <ContentFileBrowser page={page} value={value} onSelect={onSelect} prefix={prefix} after={after}
      onPrefix={next => {setPrefix(next); setAfter("");}} onAfter={setAfter} />
    {page.loading ? <p role="status">正在读取文件…</p> : null}
    {page.error ? <p role="alert">{page.error}<button type="button" className="button secondary" onClick={page.reload}>重试</button></p> : null}
  </div>;
}

function ContentFileBrowser({page, value, onSelect, prefix, after, onPrefix, onAfter}: {
  page: Resource<Schema<"ContentPage">>; value: string; onSelect: (path: string) => void;
  prefix: string; after: string; onPrefix: (prefix: string) => void; onAfter: (cursor: string) => void;
}) {
  const prefixId = useId();
  const files = page.data?.items ?? [];
  return <details><summary>浏览项目文件</summary>
      <div className="content-file-browser stack">
        <div className="field">
          <label htmlFor={prefixId}>路径前缀</label>
          <input id={prefixId} aria-describedby={`${prefixId}-hint`} value={prefix} placeholder="例如 scripts/"
            onChange={event => onPrefix(event.target.value)} />
          <small id={`${prefixId}-hint`}>留空显示全部文件，按路径开头筛选。</small>
        </div>
        {files.length ? <ul className="content-file-list" aria-label="项目文件">
          {files.map(file => <li key={file.logicalKey}>
            <button type="button" className="button secondary multiline" aria-pressed={file.logicalKey === value}
              onClick={() => onSelect(file.logicalKey)}>
              <span>{file.logicalKey}</span>
              <span className="workspace-note">{file.logicalKey === value ? "当前入口 · " : ""}{formatBytes(file.sizeBytes)}</span>
            </button>
          </li>)}
        </ul> : !page.loading && !page.error ? <p className="workspace-note">没有匹配的项目文件。</p> : null}
        {!page.loading && !page.error ? <p className="workspace-note" aria-live="polite">本页 {files.length} 个文件{page.data?.nextCursor ? "" : "，已到最后一页"}。点击文件可设为入口，保存后生效。</p> : null}
        <div className="content-file-actions">
          <button type="button" className="button secondary" disabled={page.loading || !after} onClick={() => onAfter("")}>返回第一页</button>
          <button type="button" className="button secondary" disabled={page.loading || !page.data?.nextCursor}
            onClick={() => onAfter(page.data?.nextCursor ?? "")}>下一页文件</button>
        </div>
      </div>
    </details>;
}
