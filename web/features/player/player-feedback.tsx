import type {
  RuntimeFinalSnapshotV1,
  RuntimeStateV1,
  RuntimeStartupTaskV1,
} from "./runtime/contract";
import type { SaveDraft } from "./save-drafts";
import { exportDraft } from "./save-drafts";
import { PlayerStartupTasks } from "./player-startup-tasks";

export function PlayerFeedback({
  error,
  state,
  step,
  tasks,
  draft,
  status,
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
  tasks: RuntimeStartupTaskV1[];
  draft: SaveDraft | null;
  status: string;
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
        <div className="player-loading-layer"><section className="player-loading player-failure" role="alert">
          <strong>游戏无法运行</strong>
          <p className="player-loading-error">{error}</p>
          <button className="button" onClick={onReturn}>
            返回
          </button>
        </section></div>
      ) : null}
      {["CREATED", "MOUNTING"].includes(state) && !error ? (
        <div className="player-loading-layer"><section className="player-loading" role="status" aria-live="polite">
          <strong>游戏启动中</strong>
          {tasks.length ? <PlayerStartupTasks tasks={tasks} /> : <i aria-hidden="true" />}
          <p>{step}</p>
        </section></div>
      ) : null}
      {draft ? (
        <div className="player-save-upload-progress">
          <p>{busy ? "正在同步存档…" : draft.recoveryError || status || "同步失败，存档已保留在浏览器。"}</p>
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
