import type {
  RuntimeFinalSnapshotV1,
  RuntimeStateV1,
} from "./runtime/contract";
import type { SaveDraft } from "./save-drafts";
import { exportDraft } from "./save-drafts";

export function PlayerFeedback({
  error,
  state,
  step,
  draft,
  busy,
  preview,
  review,
  onRetry,
  onRestore,
  onReturn,
}: {
  error: string;
  state: RuntimeStateV1;
  step: string;
  draft: SaveDraft | null;
  busy: boolean;
  preview: RuntimeFinalSnapshotV1 | null;
  review: boolean;
  onRetry: () => void;
  onRestore: () => void;
  onReturn: () => void;
}) {
  return (
    <>
      {error ? (
        <div className="player-load-overlay">
          <h1>游戏无法运行</h1>
          <p role="alert">{error}</p>
          <button className="button" onClick={onReturn}>
            返回
          </button>
        </div>
      ) : null}
      {["CREATED", "MOUNTING"].includes(state) && !error ? (
        <div className="player-load-overlay" role="status">
          <h1>正在启动游戏</h1>
          <p>{step}</p>
        </div>
      ) : null}
      {draft ? (
        <div className="player-save-upload-progress">
          <p>{draft.recoveryError ?? "同步失败，存档已保留在浏览器。"}</p>
          <button
            className="player-control"
            disabled={busy || !!draft.recoveryError}
            onClick={onRetry}
          >
            重新同步
          </button>
          <button className="player-control" onClick={() => exportDraft(draft)}>
            导出备份
          </button>
        </div>
      ) : null}
      {review && preview ? (
        <div className="player-save-upload-progress">
          <button
            className="player-control"
            disabled={busy || !["RUNNING", "PAUSED"].includes(state)}
            onClick={onRestore}
          >
            恢复预览存档
          </button>
          <span>预览存档仅在本次审核中保留。</span>
        </div>
      ) : null}
    </>
  );
}
