"use client";
import { RuntimeConfigEditor } from "./runtime-config-editor";
import { useState } from "react";
import type { FormEvent } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { loadDirectories, loadTags } from "@/features/library/api";
import { useResource } from "@/lib/use-resource";
import { FeedbackBanner } from "@/components/ui";
export function GameEditor({
  detail,
  mode,
  onSaved,
}: {
  detail: Schema<"GameDetail">;
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
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
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
      onSaved();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "保存失败。");
      tags.reload();
    } finally {
      setBusy(false);
    }
  }
  return (
    <form
      className="workspace-card stack"
      onSubmit={(event) => void submit(event)}
    >
      <h2>游戏资料</h2>
      {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
      <div className="form-grid">
        <Field label="标题" name="title" value={detail.game.title} />
        <label className="field">
          游戏目录
          <select
            aria-label="游戏目录"
            name="directory"
            value={directoryId}
            onChange={(event) => setDirectoryId(event.target.value)}
          >
            {!directories.data?.items.some(
              (directory) => directory.id === directoryId,
            ) ? (
              <option value={directoryId}>{detail.game.directoryName}</option>
            ) : null}
            {directories.data?.items.map((directory) => (
              <option key={directory.id} value={directory.id}>
                {directory.name}
              </option>
            ))}
          </select>
        </label>
        <Field label="开发商" name="developer" value={detail.game.developer} />
        <Field label="发行商" name="publisher" value={detail.game.publisher} />
        <Field label="类型" name="genre" value={detail.game.genre} />
        <Field
          label="发行年份"
          name="releaseYear"
          value={detail.game.releaseYear?.toString() ?? ""}
          type="number"
        />
        <Field
          label="玩家人数"
          name="players"
          value={detail.game.players ?? ""}
        />
        <label className="field full">
          简介
          <textarea name="description" defaultValue={detail.game.description} />
        </label>
      </div>
      <fieldset>
        <legend>标签</legend>
        <div className="workspace-tags">
          {tags.data?.items.map((tag) => (
            <label key={tag.id}>
              <input
                type="checkbox"
                checked={selectedTags.includes(tag.id)}
                onChange={(event) =>
                  setSelectedTags((value) =>
                    event.target.checked
                      ? [...value, tag.id]
                      : value.filter((id) => id !== tag.id),
                  )
                }
              />
              {tag.name}
            </label>
          ))}
        </div>
      </fieldset>
      <RuntimeConfigEditor
        gameId={detail.game.id}
        value={runtimeConfig}
        coreIds={detail.coreIds}
        files={detail.files}
        onChange={setRuntimeConfig}
      />
      <button className="button" disabled={busy}>
        {busy ? "正在保存…" : "保存资料"}
      </button>
    </form>
  );
}
function Field({
  label,
  name,
  value,
  type = "text",
}: {
  label: string;
  name: string;
  value: string;
  type?: string;
}) {
  return (
    <label className="field">
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
