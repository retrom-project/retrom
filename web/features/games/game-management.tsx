"use client";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { GameMediaEditor } from "./game-media-editor";
import { GameFileList } from "./game-file-list";
async function roots() {
  return result(await api.GET("/api/v1/admin/source-roots"));
}
export function GameManagement({
  detail,
  onChange,
}: {
  detail: Schema<"GameDetail">;
  onChange: () => void;
}) {
  const router = useRouter();
  const source = useResource(roots);
  const [error, setError] = useState("");
  const [remove, setRemove] = useState(false);
  const [replace, setReplace] = useState(false);
  const [rootId, setRootId] = useState("");
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  async function replaceContent() {
    setBusy(true);
    try {
      result(
        await api.POST("/api/v1/admin/games/{gameId}/content-replacement", {
          params: { path: { gameId: detail.game.id } },
          body: { version: detail.game.version, rootId, relativePath: path },
        }),
      );
      setReplace(false);
      onChange();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "替换失败。");
    } finally {
      setBusy(false);
    }
  }
  async function removeGame() {
    const response = await api.DELETE("/api/v1/admin/games/{gameId}", {
      params: { path: { gameId: detail.game.id } },
      body: { version: detail.game.version },
    });
    if (response.error) {
      setError(response.error.message);
      return;
    }
    router.push("/admin/games");
  }

  return (
    <section className="workspace-card stack">
      <h2>文件与媒体</h2>
      <div className="workspace-actions">
        <button className="button secondary" onClick={() => setReplace(true)}>
          替换游戏内容
        </button>
        <button className="button danger" onClick={() => setRemove(true)}>
          删除游戏
        </button>
      </div>
      {error ? <p role="alert">{error}</p> : null}
      <GameMediaEditor game={detail.game} onChange={onChange} />
      <GameFileList files={detail.files} />

      <ConfirmDialog
        open={replace}
        title="替换游戏内容"
        description="内容变化后，现有存档可能无法恢复。替换直接生效。"
        busy={busy}
        onCancel={() => setReplace(false)}
        onConfirm={() => void replaceContent()}
        confirmDisabled={!rootId || !path}
      >
        <div className="stack">
          <label className="field">
            服务器来源
            <select
              value={rootId}
              onChange={(event) => setRootId(event.target.value)}
            >
              <option value="">选择来源</option>
              {source.data?.items.map((root) => (
                <option value={root.id} key={root.id}>
                  {root.name}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            来源内相对路径
            <input
              value={path}
              onChange={(event) => setPath(event.target.value)}
            />
          </label>
        </div>
      </ConfirmDialog>
      <ConfirmDialog
        open={remove}
        title="删除游戏"
        description="游戏将从游戏库隐藏，关联文件与存档随后清理。"
        tone="danger"
        onCancel={() => setRemove(false)}
        onConfirm={() => void removeGame()}
      />
    </section>
  );
}
