"use client";
import { useToast } from "@/components/toast-provider";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { api, result, ApiError } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { ConfirmDialog } from "@/components/confirm-dialog";
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
  const { notify } = useToast();
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
      notify({ tone: "good", message: "游戏内容已替换" });
      setReplace(false);
      onChange();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "替换失败，请重试。" });
    } finally {
      setBusy(false);
    }
  }
  async function removeGame() {
    setBusy(true);
    try {
      const response = await api.DELETE("/api/v1/admin/games/{gameId}", {
        params: { path: { gameId: detail.game.id } },
        body: { version: detail.game.version },
      });
      if (response.error) { throw new ApiError(response.error.code, response.error.message, response.response.status); }
      notify({ tone: "good", message: "游戏已删除" });
      router.push("/admin/games");
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "删除游戏失败，请重试。" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="panel">
      <div className="panel-head"><h2>文件与管理操作</h2></div>
      <div className="panel-body stack">
      <div className="admin-game-action-grid">
        <article><h3>替换游戏内容</h3><p>从服务器来源选择新的内容文件，现有存档按实际内容身份判断能否恢复。</p>
        <button className="button secondary" onClick={() => setReplace(true)}>
          替换游戏内容
        </button>
        </article>
        <article><h3>删除游戏</h3><p>从游戏库移除当前游戏，关联内容按清理规则处理。</p>
        <button className="button danger" onClick={() => setRemove(true)}>
          删除游戏
        </button>
        </article>
      </div>
      <GameFileList files={detail.files} />
      </div>

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
          {source.error ? <p role="alert">{source.error} <button className="button secondary" onClick={source.reload}>重试读取来源</button></p> : null}
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
        busy={busy}
        onCancel={() => setRemove(false)}
        onConfirm={() => void removeGame()}
      />
    </section>
  );
}
