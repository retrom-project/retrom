"use client";
import {useCallback, useState} from "react";
import {api, result} from "@/lib/api/client";
import {useResource} from "@/lib/use-resource";

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
  return <div>
    <label className="field">入口文件
      <select value={value} required disabled={page.loading} onChange={event => onSelect(event.target.value)}>
        <option value="">请选择入口</option>
        {value && !files.some(file => file.logicalKey === value) ? <option value={value}>{value}</option> : null}
        {files.map(file => <option key={file.logicalKey} value={file.logicalKey}>{file.logicalKey}</option>)}
      </select>
    </label>
    <details><summary>浏览项目文件</summary>
      <label className="field">路径前缀
        <input value={prefix} onChange={event => {setPrefix(event.target.value); setAfter("");}} />
      </label>
      {after ? <button type="button" className="button secondary" disabled={page.loading} onClick={() => setAfter("")}>返回第一页</button> : null}
      {page.data?.nextCursor ? <button type="button" className="button secondary" disabled={page.loading}
        onClick={() => setAfter(page.data?.nextCursor ?? "")}>下一页文件</button> : null}
    </details>
    {page.loading ? <p role="status">正在读取文件…</p> : null}
    {page.error ? <p role="alert">{page.error}<button type="button" className="button secondary" onClick={page.reload}>重试</button></p> : null}
  </div>;
}
