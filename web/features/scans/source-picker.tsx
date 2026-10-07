"use client";
import { useCallback, useState } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { AppIcon } from "@/components/app-icon";
import styles from "./scan.module.css";
export function SourcePicker({ path, onChange, onPendingChange }: {
  path: string;
  onChange: (path: string) => void;
  onPendingChange: (pending: boolean) => void;
}) {
  const [draft, setDraft] = useState(path);
  const absolute = path.startsWith("/");
  function navigate(value: string) {
    setDraft(value);
    onPendingChange(false);
    onChange(value);
  }
  const loader = useCallback(async () => absolute
    ? result(await api.GET("/api/v1/admin/source-directories", { params: { query: { path } } }))
    : { items: [] }, [absolute, path]);
  const directories = useResource(loader);
  const parent = path.replace(/\/+$/, "").split("/").slice(0, -1).join("/") || "/";
  return <div className={styles.picker}>
    <form className={styles.pathEntry} onSubmit={(event) => { event.preventDefault(); if (draft.startsWith("/")) { navigate(draft); } }}>
      <label className="field">
        <span className="field-label">服务器目录</span>
        <input aria-label="服务器目录" value={draft} placeholder="/" spellCheck={false}
          onChange={(event) => { setDraft(event.target.value); onPendingChange(event.target.value !== path); }} />
      </label>
      <button className="button secondary" disabled={!draft.startsWith("/")} type="submit">进入目录</button>
    </form>
    <small>从 / 浏览服务进程可见的目录；输入绝对路径后按回车或“进入目录”。</small>
    {absolute ? <section className={styles.directoryBrowser} aria-label="服务器目录浏览器">
      <header>{path}</header>
      <div className={styles.directoryRows}>
        <button type="button" className="button ghost" disabled={path === "/"} onClick={() => navigate(parent)}>
          <AppIcon name="arrow-left" /><span>上级目录</span>
        </button>
        {directories.loading ? <p role="status">正在读取目录…</p>
          : directories.data?.items.map((directory) => <button type="button" className="button ghost"
            key={directory.path} onClick={() => navigate(directory.path)} title={directory.path}>
            <AppIcon name="folder" /><span>{directory.name}</span>
          </button>)}
        {!directories.loading && !directories.error && !directories.data?.items.length
          ? <p>这个目录中没有子目录。</p> : null}
      </div>
    </section> : <p role="alert">请输入以 / 开头的绝对路径。</p>}
    {directories.error ? <div><p role="alert">{directories.error}</p>
      <button type="button" className="button secondary" onClick={directories.reload}>重新读取目录</button></div> : null}
  </div>;
}
