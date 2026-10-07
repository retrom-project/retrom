"use client";
import { useToast } from "@/components/toast-provider";
import Image from "next/image";
import { useState } from "react";
import { api, upload, ApiError } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { ConfirmDialog } from "@/components/confirm-dialog";
export function GameMediaEditor({
  game,
  onChange,
}: {
  game: Schema<"Game">;
  onChange: () => void;
}) {
  const [kind, setKind] = useState<Schema<"GameMedia">["kind"]>("cover");
  const selected = game.media.find((media) => media.kind === kind);
  const { notify } = useToast();
  const [busy, setBusy] = useState(false);
  const [removing, setRemoving] = useState<Schema<"GameMedia"> | null>(null);
  async function send(file: File, kind: "cover" | "video") {
    setBusy(true);
    const body = new FormData();
    body.set("file", file);
    body.set("kind", kind);
    body.set("version", String(game.version));
    try {
      await upload<Schema<"GameDetail">>(
        `/api/v1/admin/games/${game.id}/media`,
        body,
      );
      notify({ tone: "good", message: kind === "cover" ? "游戏封面已更新" : "游戏视频已更新" });
      onChange();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "上传失败，请重试。" });
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
        throw new ApiError(response.error.code, response.error.message, response.response.status);
      }
      notify({ tone: "good", message: "游戏媒体已移除" });
      setRemoving(null);
      onChange();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "删除失败，请重试。" });
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel admin-game-media">
      <div className="panel-head"><h2>媒体</h2><div className="admin-game-media-tabs" role="tablist" aria-label="游戏媒体">
        <button type="button" className="button secondary option-tab" role="tab" aria-selected={kind === "cover"} onClick={() => setKind("cover")}>封面</button>
        <button type="button" className="button secondary option-tab" role="tab" aria-selected={kind === "video"} onClick={() => setKind("video")}>视频</button>
        {game.media.some((media) => media.kind === "screenshot") ? <button type="button" className="button secondary option-tab" role="tab" aria-selected={kind === "screenshot"} onClick={() => setKind("screenshot")}>截图</button> : null}
      </div></div>
      <div className="panel-body">
        <div className="admin-game-media-stage">
          {selected ? selected.kind === "video"
            ? <video src={selected.url} controls preload="metadata" playsInline />
            : <Image src={selected.url} alt={`${game.title} ${kind === "cover" ? "封面" : "截图"}`} fill sizes="(max-width: 767px) 90vw, 520px" unoptimized />
            : <div className="admin-game-media-empty">暂无{kind === "video" ? "视频" : "封面"}</div>}
        </div>
        <div className="admin-game-media-footer"><span>{kind === "video" ? "游戏预览视频" : "游戏封面与画面"}</span><div>
          {kind !== "screenshot" ? <label className="button secondary">
            {selected ? "替换" : "上传"}{kind === "video" ? "视频" : "封面"}
            <input type="file" accept={kind === "cover" ? "image/*" : "video/*"} disabled={busy} hidden onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) { void send(file, kind); }
            }} />
          </label> : null}
          {selected ? <button type="button" className="button secondary" disabled={busy} onClick={() => { setRemoving(selected); }}>移除</button> : null}
        </div></div>
      </div>
      <ConfirmDialog
        open={!!removing}
        title="删除游戏媒体"
        description="游戏内容与存档不受影响。"
        tone="danger"
        busy={busy}
        onCancel={() => setRemoving(null)}
        onConfirm={() => void remove()}
      />
    </section>
  );
}
