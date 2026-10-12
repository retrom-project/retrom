"use client";
import { useCallback, useState } from "react";
import { api, result, upload } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { useToast } from "@/components/toast-provider";

export function ArcadeParentFiles({ gameId, version, coreId, entryFile, files, selected, extensions = ["zip"], configurationChanged = false, onChange, onUploaded }: {
  gameId: string; version: number; coreId: string; entryFile: string | undefined;
  files: Schema<"GameFile">[]; selected: string[];
  configurationChanged?: boolean;
  extensions?: string[];
  onChange: (files: string[]) => void; onUploaded: () => void;
}) {
  const load = useCallback(async () => result(await api.GET("/api/v1/admin/games/{gameId}/runtime-options/arcade", {
    params: { path: { gameId }, query: { coreId } },
  })), [gameId, coreId]);
  const parents = useResource(load);
  const [busy, setBusy] = useState(false);
  const { notify } = useToast();
  const includesMedia = extensions.includes("chd");
  const resourceName = includesMedia ? "配套文件" : "父包";
  async function send(file: File) {
    setBusy(true);
    const body = new FormData();
    body.set("version", String(version)); body.set("coreId", coreId); body.set("file", file);
    try {
      await upload<Schema<"GameDetail">>(`/api/v1/admin/games/${gameId}/parents`, body);
      notify({ tone: "good", message: `${resourceName}已上传并更新运行配置。` });
      onUploaded();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : `${resourceName}上传失败，请重试。` });
    } finally { setBusy(false); }
  }
  return <section className="arcade-parent-files" aria-label={includesMedia ? "运行配套文件" : "街机父包"}>
    <div className="arcade-parent-header"><h3>{includesMedia ? "运行配套文件" : "Parent 父包"}</h3>
      <label className="button secondary">
        {busy ? `正在上传${resourceName}…` : `上传${resourceName}`}
        <input aria-label={`上传${resourceName}`} type="file" accept={extensions.map(extension => `.${extension}`).join(",")} disabled={busy || configurationChanged} hidden onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (file) { void send(file); }
        }} />
      </label>
    </div>
    {configurationChanged ? <p role="status">ROM 集或入口文件已更改，请先保存运行配置，再检查或上传 Parent。</p> : parents.loading ? <p role="status">正在检查已保存配置的父包…</p> : parents.error ? <p role="alert">无法检查父包：{parents.error} <button type="button" className="button secondary" onClick={parents.reload}>重新检查父包</button></p>
      : parents.data?.missingParents.length ? <div role="status"><p>缺少以下父包，请上传对应 ZIP：</p><ul>{parents.data.missingParents.map((name) => <li key={name}>{name}</li>)}</ul></div>
      : <p role="status">已保存配置未发现缺失父包。</p>}
    {includesMedia ? <DiscFiles options={parents.data} changed={configurationChanged} /> : null}
    <label className="field">Parent 文件
      <select multiple value={selected} disabled={busy} onChange={(event) => onChange(Array.from(event.target.selectedOptions, (item) => item.value))}>
        {files.filter((file) => file.logicalKey !== entryFile && /\.zip$/i.test(file.logicalKey)).map((file) => <option key={file.logicalKey} value={file.logicalKey}>{file.logicalKey}</option>)}
      </select>
    </label>
    <p className="workspace-note">缺失名称来自运行核心对已保存配置的检查。上传会补齐当前核心的{resourceName}并刷新资料；请先保存其他修改。文件是否可运行仍以实际启动结果为准。</p>
  </section>;
}

function DiscFiles({ options, changed }: { options: Schema<"ArcadeParentOptions"> | null; changed: boolean }) {
  if (!options || changed) { return null; }
  if (options.missingMedia.length) { return <div role="status"><p>缺少以下光盘文件，请上传对应 CHD：</p>
    <ul>{options.missingMedia.map(name => <li key={name}>{name}</li>)}</ul></div>; }
  if (options.mediaFiles.length) { return <div role="status"><p>已关联光盘文件：</p>
    <ul>{options.mediaFiles.map(name => <li key={name}>{name}</li>)}</ul></div>; }
  return null;
}
