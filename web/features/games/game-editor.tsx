"use client";
import { useToast } from "@/components/toast-provider";
import { GameTagPicker } from "./game-tag-picker";
import { GameMediaEditor } from "./game-media-editor";
import { BrowserTime } from "@/components/browser-time";
import { RuntimeConfigEditor } from "./runtime-config-editor";
import { useState } from "react";
import type { FormEvent } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { loadDirectories, loadTags } from "@/features/library/api";
import { useResource } from "@/lib/use-resource";
export function GameEditor({
  detail,
  coreId,
  onCoreChange,
  mode,
  onSaved,
}: {
  detail: Schema<"GameDetail">;
  coreId: string;
  onCoreChange: (coreId: string) => void;
  mode: "admin" | "review";
  onSaved: () => void;
}) {
  const directories = useResource(loadDirectories);
  const tags = useResource(loadTags);
  const [selectedTags, setSelectedTags] = useState(
    detail.game.tags.map((tag) => tag.id),
  );
  const [runtimeConfig, setRuntimeConfig] = useState(detail.runtimeConfig);
  const [directoryId, setDirectoryId] = useState(
    detail.game.platformInstanceId,
  );
  const { notify } = useToast();
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    const form = new FormData(event.currentTarget);
    const body: Schema<"GameWriteRequest"> = {
      version: detail.game.version,
      platformInstanceId: String(form.get("directory")),
      title: String(form.get("title")),
      description: String(form.get("description")),
      developer: String(form.get("developer")),
      publisher: String(form.get("publisher")),
      genre: String(form.get("genre")),
      players: String(form.get("players") ?? "").trim() || null,
      releaseYear: optionalInteger(form.get("releaseYear")),
      tagIds: selectedTags,
      runtimeConfig,
    };
    try {
      const params = { path: { gameId: detail.game.id } };
      if (mode === "review") {
        result(
          await api.PATCH("/api/v1/admin/reviews/{gameId}", { params, body }),
        );
      } else {
        result(
          await api.PATCH("/api/v1/admin/games/{gameId}", { params, body }),
        );
      }
      notify({ tone: "good", message: "游戏资料与运行配置已保存" });
      onSaved();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "保存失败，请重试。" });
      tags.reload();
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="admin-game-editor">
      <form className="admin-game-editor-form" onSubmit={(event) => void submit(event)}>
        <section className="panel admin-game-tags admin-game-wide-panel">
          <div className="panel-head"><div><h2>游戏标签</h2><p>为游戏添加分类标签，与发布资料一并保存。</p></div></div>
          <div className="panel-body">
            {tags.error ? <p role="alert">{tags.error} <button type="button" className="button secondary" onClick={tags.reload}>重试读取标签</button></p> : null}
            <GameTagPicker tags={tags.data?.items ?? []} selected={selectedTags} onChange={setSelectedTags} />
          </div>
        </section>
        <section className="panel admin-game-form-panel">
          <div className="panel-head"><h2>发布信息</h2></div>
          <div className="panel-body admin-game-publish-form">
            <Field label="标题" name="title" value={detail.game.title} full />
            <label className="field full">简介<textarea name="description" defaultValue={detail.game.description} /></label>
            <Field label="开发商" name="developer" value={detail.game.developer} />
            <Field label="发行商" name="publisher" value={detail.game.publisher} />
            <Field label="类型" name="genre" value={detail.game.genre} />
            <Field label="玩家人数" name="players" value={detail.game.players ?? ""} />
            <Field label="发行年份" name="releaseYear" value={detail.game.releaseYear?.toString() ?? ""} type="number" />
            <label className="field">游戏目录
              <select aria-label="游戏目录" name="directory" value={directoryId} onChange={(event) => setDirectoryId(event.target.value)}>
                {!directories.data?.items.some((directory) => directory.id === directoryId) ? <option value={directoryId}>{detail.game.directoryName}</option> : null}
                {directories.data?.items.map((directory) => <option key={directory.id} value={directory.id}>{directory.name}</option>)}
              </select>
            </label>
            {directories.error ? <p className="full" role="alert">{directories.error} <button type="button" className="button secondary" onClick={directories.reload}>重试读取目录</button></p> : null}
            <div className="admin-game-savebar full"><span>上次保存：<BrowserTime value={detail.game.updatedAtMs} /></span><button className="button" disabled={busy}>{busy ? "正在保存…" : "保存发布信息"}</button></div>
          </div>
        </section>
        <section className="panel admin-game-wide-panel">
          <div className="panel-head"><div><h2>运行配置</h2><p>配置当前游戏的内容与核心选项，保存后生效。</p></div></div>
          <div className="panel-body"><RuntimeConfigEditor coreId={coreId} onCoreChange={onCoreChange} gameId={detail.game.id} version={detail.game.version} onUploaded={onSaved} value={runtimeConfig} coreIds={detail.coreIds} files={detail.files} onChange={setRuntimeConfig} />
            <div className="admin-game-savebar"><span>运行配置与发布资料一起保存。</span><button className="button" disabled={busy}>{busy ? "正在保存…" : "保存更改"}</button></div>
          </div>
        </section>
      </form>
      <GameMediaEditor game={detail.game} onChange={onSaved} />
    </div>
  );
}
function Field({
  label,
  name,
  value,
  type = "text",
  full = false,
}: {
  label: string;
  name: string;
  value: string;
  type?: string;
  full?: boolean;
}) {
  return (
    <label className={`field${full ? " full" : ""}`}>
      {label}
      <input
        name={name}
        defaultValue={value}
        type={type}
        min={type === "number" ? 0 : undefined}
      />
    </label>
  );
}

function optionalInteger(value: FormDataEntryValue | null) {
  if (!value || typeof value !== "string" || !value.trim()) {
    return null;
  }
  return Number(value);
}
