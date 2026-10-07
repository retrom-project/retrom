"use client";
import { useState } from "react";
import { api, upload } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { ConfirmDialog } from "@/components/confirm-dialog";
export function GameMediaEditor({
  game,
  onChange,
}: {
  game: Schema<"Game">;
  onChange: () => void;
}) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [removing, setRemoving] = useState<Schema<"GameMedia"> | null>(null);
  async function send(file: File, kind: "cover" | "video") {
    setBusy(true);
    setError("");
    const body = new FormData();
    body.set("file", file);
    body.set("kind", kind);
    body.set("version", String(game.version));
    try {
      await upload<Schema<"GameDetail">>(
        `/api/v1/admin/games/${game.id}/media`,
        body,
      );
      onChange();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "上传失败。");
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    if (!removing) {
      return;
    }
    setBusy(true);
    try {
      const response = await api.DELETE(
        "/api/v1/admin/games/{gameId}/media/{mediaId}",
        {
          params: { path: { gameId: game.id, mediaId: removing.id } },
          body: { version: game.version },
        },
      );
      if (response.error) {
        throw new Error(response.error.message);
      }
      setRemoving(null);
      onChange();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "删除失败。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="stack">
      <div className="workspace-actions">
        {(["cover", "video"] as const).map((kind) => (
          <label className="button secondary" key={kind}>
            {kind === "cover" ? "上传封面" : "上传视频"}
            <input
              type="file"
              accept={kind === "cover" ? "image/*" : "video/*"}
              disabled={busy}
              hidden
              onChange={(event) => {
                const file = event.target.files?.[0];
                if (file) {
                  void send(file, kind);
                }
              }}
            />
          </label>
        ))}
      </div>
      <div className="workspace-actions">
        {game.media.map((media) => (
          <div className="workspace-actions" key={media.id}>
            <a href={media.url} target="_blank" rel="noreferrer">
              {media.kind === "cover"
                ? "当前封面"
                : media.kind === "video"
                  ? "当前视频"
                  : "截图"}
            </a>
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => {
                setError("");
                setRemoving(media);
              }}
            >
              删除
              {media.kind === "cover"
                ? "封面"
                : media.kind === "video"
                  ? "视频"
                  : "截图"}
            </button>
          </div>
        ))}
      </div>
      {error ? <p role="alert">{error}</p> : null}
      <ConfirmDialog
        open={!!removing}
        title="删除游戏媒体"
        description="游戏内容与存档不受影响。"
        tone="danger"
        busy={busy}
        onCancel={() => setRemoving(null)}
        onConfirm={() => void remove()}
      />
    </div>
  );
}
