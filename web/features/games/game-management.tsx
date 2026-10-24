"use client";
import { useToast } from "@/components/toast-provider";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { api, ApiError } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { GameContentReplacement } from "./content-replacement";
export function GameManagement({
  detail,
  onChange,
}: {
  detail: Schema<"GameDetail">;
  onChange: () => void;
}) {
  const router = useRouter();
  const { notify } = useToast();
  const [remove, setRemove] = useState(false);
  const [busy, setBusy] = useState(false);
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
        <GameContentReplacement detail={detail} onChange={onChange} />
        <article><div><h3>删除游戏</h3><p>从游戏库移除当前游戏，关联内容按清理规则处理。</p></div>
        <button className="button danger" onClick={() => setRemove(true)}>
          删除游戏
        </button>
        </article>
      </div>
      </div>

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
