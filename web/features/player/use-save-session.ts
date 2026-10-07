"use client";
import { useCallback, useRef, useState } from "react";
import type { Schema } from "@/lib/api/types";
import type {
  PlayerRuntimeV1,
  RuntimeFinalSnapshotV1,
} from "./runtime/contract";
import { makeDraft, putDraft, uploadDraft } from "./save-drafts";
import type { SaveDraft } from "./save-drafts";
export function useSaveSession(
  run: Schema<"Run">,
  userId: string,
  native: boolean,
) {
  const selectedSave = useRef(run.save);
  const [currentSave, setCurrentSave] = useState(run.save);
  const saving = useRef(false);
  const queued = useRef(false);
  const pending = useRef<SaveDraft | null>(null);
  const [draft, setDraft] = useState<SaveDraft | null>(null);
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<RuntimeFinalSnapshotV1 | null>(null);
  const commit = useCallback(
    async (
      snapshot: RuntimeFinalSnapshotV1,
      runtime: PlayerRuntimeV1 | null,
      target: Schema<"Save"> | null = null,
    ) => {
      if (run.purpose === "review") {
        setPreview(snapshot);
        setStatus("预览存档已保留，审核结束后清除。");
        return;
      }
      if (snapshot.checkpoint.format !== run.extinfo.checkpointFormat) {
        throw new Error("运行模块生成了不匹配的存档格式。");
      }
      const record = makeDraft(
        userId,
        run,
        snapshot.checkpoint,
        snapshot.screenshot,
        native ? selectedSave.current : target,
        native,
      );
      setDraft(record);
      pending.current = record;
      await putDraft(record);
      try {
        const save = await uploadDraft(record, userId);
        selectedSave.current = save;
        setCurrentSave(save);
        setDraft(null);
        pending.current = null;
        setStatus("存档已同步。");
        if (native) {
          await runtime?.acknowledgeCheckpoint?.(snapshot.checkpoint);
        }
      } catch (failure) {
        setStatus(
          failure instanceof Error
            ? failure.message
            : "同步失败，存档已保留在浏览器。",
        );
      }
    },
    [run, userId, native],
  );
  const capture = useCallback(
    async (
      runtime: PlayerRuntimeV1,
      intent: "CAPTURE" | "EXPORT" = "CAPTURE",
      target: Schema<"Save"> | null = null,
    ) => {
      if (pending.current) {
        queued.current = native;
        setStatus("请先同步或导出已有草稿，再保存新的进度。");
        return;
      }
      if (saving.current) {
        queued.current = true;
        return;
      }
      saving.current = true;
      setBusy(true);
      try {
        do {
          queued.current = false;
          const checkpoint = await runtime.checkpoint({ intent });
          const screenshot = await runtime.screenshot().catch(() => null);
          await commit({ checkpoint, screenshot }, runtime, target);
          intent = "EXPORT";
        } while (
          queued.current &&
          !pending.current &&
          runtime.getCheckpointAvailability().available &&
          runtime.getState() !== "EXITED"
        );
      } catch (failure) {
        setStatus(
          failure instanceof Error ? failure.message : "无法生成存档。",
        );
      } finally {
        saving.current = false;
        setBusy(false);
      }
    },
    [commit, native],
  );
  const nativeChanged = useCallback(
    (runtime: PlayerRuntimeV1) => {
      if (native && run.purpose === "play") {
        void capture(runtime, "EXPORT");
      }
    },
    [native, run.purpose, capture],
  );
  async function retry(runtime: PlayerRuntimeV1 | null) {
    if (!draft) {
      return;
    }
    setBusy(true);
    try {
      const save = await uploadDraft(draft, userId);
      selectedSave.current = save;
      setCurrentSave(save);
      setDraft(null);
      pending.current = null;
      setStatus("存档已同步。");
      if (native && runtime) {
        await runtime.acknowledgeCheckpoint?.({
          format: draft.metadata.extinfo.checkpointFormat,
          bytes: new Uint8Array(await draft.payload.arrayBuffer()),
          metadata: draft.checkpointMetadata,
        });
        if (runtime.getCheckpointAvailability().available) {
          await capture(runtime, "EXPORT");
        }
      }
    } catch (failure) {
      setStatus(failure instanceof Error ? failure.message : "同步失败。");
    } finally {
      setBusy(false);
    }
  }
  return {
    draft,
    status,
    busy,
    preview,
    currentSave,
    commit,
    capture,
    nativeChanged,
    retry,
  };
}
