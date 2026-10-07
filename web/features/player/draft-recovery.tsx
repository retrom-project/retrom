"use client";
import { useCallback, useState } from "react";
import { useAuth } from "@/features/auth/auth-provider";
import { useResource } from "@/lib/use-resource";
import {
  exportDraft,
  listDrafts,
  removeDraft,
  uploadDraft,
} from "./save-drafts";
import type { SaveDraft } from "./save-drafts";
import { BrowserTime } from "@/components/browser-time";
import { ConfirmDialog } from "@/components/confirm-dialog";
export function DraftRecovery({ gameId }: { gameId?: string }) {
  const { context } = useAuth();
  const userId = context?.user?.id ?? "";
  const loader = useCallback(
    async () =>
      userId
        ? (await listDrafts(userId)).filter(
            (d) => !gameId || d.gameId === gameId,
          )
        : [],
    [userId, gameId],
  );
  const drafts = useResource(loader);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [deleting, setDeleting] = useState<SaveDraft | null>(null);
  async function retry(draft: SaveDraft) {
    setBusy(true);
    try {
      await uploadDraft(draft, userId);
      drafts.reload();
      setError("");
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : "同步失败。请保留草稿或导出文件。",
      );
      drafts.reload();
    } finally {
      setBusy(false);
    }
  }
  async function discard() {
    if (!deleting) {
      return;
    }
    await removeDraft(userId, deleting.id);
    setDeleting(null);
    drafts.reload();
  }
  if (drafts.error) {
    return <p role="alert">{drafts.error}</p>;
  }
  if (!drafts.data?.length) {
    return null;
  }
  return (
    <section className="workspace-card stack">
      <h2>未同步的存档</h2>
      <p>这些存档保留在当前浏览器。同步完成前，请保留草稿或导出备份。</p>
      {error ? <p role="alert">{error}</p> : null}
      {drafts.data.map((draft) => (
        <div className="workspace-row" key={draft.id}>
          <div>
            <strong>{draft.gameTitle}</strong>
            <p>
              <BrowserTime value={draft.createdAtMs} />
            </p>
            {draft.recoveryError ? (
              <p role="status">{draft.recoveryError}</p>
            ) : null}
          </div>
          <div className="workspace-actions">
            <button
              className="button secondary"
              disabled={busy || !!draft.recoveryError}
              onClick={() => void retry(draft)}
            >
              重新同步
            </button>
            <button
              className="button secondary"
              onClick={() => exportDraft(draft)}
            >
              导出
            </button>
            <button
              className="button secondary"
              onClick={() => setDeleting(draft)}
            >
              删除草稿
            </button>
          </div>
        </div>
      ))}
      <ConfirmDialog
        open={!!deleting}
        title="删除本地草稿"
        description="服务器尚未保存这份存档。删除后将无法恢复。"
        confirmLabel="删除草稿"
        tone="danger"
        onCancel={() => setDeleting(null)}
        onConfirm={() => void discard()}
      />
    </section>
  );
}
