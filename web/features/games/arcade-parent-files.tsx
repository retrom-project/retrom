"use client";
import { useCallback, useState } from "react";
import { api, result, upload } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { useToast } from "@/components/toast-provider";

export function ArcadeParentFiles({ gameId, version, coreId, entryFile, files, selected, onChange, onUploaded }: {
  gameId: string; version: number; coreId: string; entryFile: string | undefined;
  files: Schema<"GameFile">[]; selected: string[];
  onChange: (files: string[]) => void; onUploaded: () => void;
}) {
  const load = useCallback(async () => result(await api.GET("/api/v1/admin/games/{gameId}/runtime-options/arcade", {
    params: { path: { gameId }, query: { coreId } },
  })), [gameId, coreId]);
  const parents = useResource(load);
  const [busy, setBusy] = useState(false);
  const { notify } = useToast();
  async function send(file: File) {
    setBusy(true);
    const body = new FormData();
    body.set("version", String(version)); body.set("coreId", coreId); body.set("file", file);
    try {
      await upload<Schema<"GameDetail">>(`/api/v1/admin/games/${gameId}/parents`, body);
      notify({ tone: "good", message: "父包已上传并更新运行配置。" });
      onUploaded();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "父包上传失败，请重试。" });
    } finally { setBusy(false); }
  }
  return <section className="arcade-parent-files" aria-label="街机父包">
    <div className="arcade-parent-header"><h3>Parent 父包</h3>
      <label className="button secondary">
        {busy ? "正在上传父包…" : "上传父包"}
        <input aria-label="上传父包" type="file" accept=".zip,application/zip" disabled={busy} hidden onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (file) { void send(file); }
        }} />
      </label>
    </div>
    {parents.loading ? <p role="status">正在检查已保存配置的父包…</p> : parents.error ? <p role="alert">无法检查父包：{parents.error} <button type="button" className="button secondary" onClick={parents.reload}>重新检查父包</button></p>
      : parents.data?.missingParents.length ? <div role="status"><p>缺少以下父包，请上传对应 ZIP：</p><ul>{parents.data.missingParents.map((name) => <li key={name}>{name}</li>)}</ul></div>
      : <p role="status">已保存配置未发现缺失父包。</p>}
    <label className="field">Parent 文件
      <select multiple value={selected} disabled={busy} onChange={(event) => onChange(Array.from(event.target.selectedOptions, (item) => item.value))}>
        {files.filter((file) => file.logicalKey !== entryFile).map((file) => <option key={file.logicalKey} value={file.logicalKey}>{file.logicalKey}</option>)}
      </select>
    </label>
    <p className="workspace-note">缺失名称来自运行核心对已保存配置的检查。上传会直接补齐当前核心的父包并刷新资料；请先保存其他修改。文件是否可运行仍以实际启动结果为准。</p>
  </section>;
}
