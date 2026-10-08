"use client";
import { useCallback, useRef, useState } from "react";
import { useToast } from "@/components/toast-provider";
import type { Schema } from "@/lib/api/types";
import type {
  PlayerRuntimeV1,
  RuntimeFinalSnapshotV1,
  RuntimeCheckpointV1,
} from "./runtime/contract";
import { makeDraft, putDraft, uploadDraft } from "./save-drafts";
import type { SaveDraft } from "./save-drafts";
export function useSaveSession(
  run: Schema<"Run">,
  userId: string,
  native: boolean,
) {
  const { notify } = useToast();
  const selectedSave = useRef(run.save);
  const [currentSave, setCurrentSave] = useState(run.save);
  const saving = useRef(false);
  const inFlight = useRef<Promise<boolean> | null>(null);
  const queued = useRef(false);
  const pending = useRef<SaveDraft | null>(null);
  const [draft, setDraft] = useState<SaveDraft | null>(null);
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<RuntimeFinalSnapshotV1 | null>(null);
  const persist = useCallback(async (record: SaveDraft, checkpoint: RuntimeCheckpointV1, runtime: PlayerRuntimeV1 | null) => {
    let persisted = false;
    try {
      const save = await uploadDraft(record, userId);
      persisted = true;
      selectedSave.current = save;
      setCurrentSave(save);
      if (native) { await runtime?.acknowledgeCheckpoint?.(checkpoint); }
      setDraft(null);
      pending.current = null;
      setStatus("");
      notify({ tone: "good", message: "存档已同步。" });
      return true;
    } catch (failure) {
      // The server commit can succeed before Runtime acknowledgement fails.
      // Keep the same commit identity so retry never creates a second save.
      if (persisted) {
        try { await putDraft(record); }
        catch (storageFailure) { setStatus(storageFailure instanceof Error ? storageFailure.message : "无法保留浏览器草稿。"); return false; }
      }
      setStatus(failure instanceof Error ? failure.message : "同步失败，存档已保留在浏览器。");
      return false;
    }
  }, [userId, native, notify]);
  const commit = useCallback(
    async (
      snapshot: RuntimeFinalSnapshotV1,
      runtime: PlayerRuntimeV1 | null,
      target: Schema<"Save"> | null = null,
    ) => {
      if (run.purpose === "review") {
        setPreview(snapshot);
        setStatus("");
        notify({ tone: "good", message: "预览存档已保留，审核结束后清除。" });
        return true;
      }
      if (snapshot.checkpoint.format !== run.extinfo.checkpointFormat) {
        throw new Error("运行模块生成了不匹配的存档格式。");
      }
      if (native && pending.current && await sameDraftCheckpoint(pending.current, snapshot.checkpoint)) { return false; }
      const record = makeDraft(userId, run, snapshot.checkpoint, snapshot.screenshot, native ? selectedSave.current : target, native);
      setDraft(record);
      pending.current = record;
      await putDraft(record);
      return persist(record, snapshot.checkpoint, runtime);
    },
    [run, userId, native, notify, persist],
  );
  const captureWork = useCallback(
    async (
      runtime: PlayerRuntimeV1,
      intent: "CAPTURE" | "EXPORT" = "CAPTURE",
      target: Schema<"Save"> | null = null,
    ) => {
      if (saving.current) { queued.current = true; return false; }
      if (pending.current) {
        queued.current = native;
        notify({ tone: "warn", message: "请先同步或导出已有草稿，再保存新的进度。" });
        return false;
      }
      saving.current = true;
      setBusy(true);
      try {
        do {
          queued.current = false;
          const checkpoint = await runtime.checkpoint({ intent });
          const screenshot = await runtime.screenshot().catch(() => null);
          const committed = await commit({ checkpoint, screenshot }, runtime, target);
          if (!committed) { return false; }
          intent = "EXPORT";
        } while (
          queued.current &&
          !pending.current &&
          runtime.getCheckpointAvailability().available &&
          runtime.getState() !== "EXITED"
        );
        return true;
      } catch (failure) {
        const message = failure instanceof Error ? failure.message : "无法生成存档。";
        setStatus(message);
        notify({ tone: "bad", message });
        return false;
      } finally {
        saving.current = false;
        setBusy(false);
      }
    },
    [commit, native, notify],
  );
  const capture = useCallback((runtime: PlayerRuntimeV1, intent: "CAPTURE" | "EXPORT" = "CAPTURE", target: Schema<"Save"> | null = null) => {
    if (inFlight.current) { queued.current = true; return inFlight.current; }
    const operation = captureWork(runtime, intent, target);
    inFlight.current = operation;
    void operation.finally(() => { if (inFlight.current === operation) { inFlight.current = null; } });
    return operation;
  }, [captureWork]);
  const settle = useCallback(async () => { await inFlight.current; }, []);
  const nativeChanged = useCallback(
    (runtime: PlayerRuntimeV1) => {
      if (native && run.purpose === "play") {
        void capture(runtime, "EXPORT");
      }
    },
    [native, run.purpose, capture],
  );
  async function retryWork(runtime: PlayerRuntimeV1 | null) {
    const record = pending.current;
    if (!record || saving.current) { return false; }
    saving.current = true;
    setBusy(true);
    let synced = false;
    try {
      synced = await persist(record, {
        format: record.metadata.extinfo.checkpointFormat,
        bytes: new Uint8Array(await record.payload.arrayBuffer()),
        metadata: record.checkpointMetadata,
      }, runtime);
    } catch (failure) {
      setStatus(failure instanceof Error ? failure.message : "同步失败。");
    } finally {
      saving.current = false;
      setBusy(false);
    }
    if (synced && native && runtime?.getCheckpointAvailability().available) { return captureWork(runtime, "EXPORT"); }
    return synced;
  }
  function retry(runtime: PlayerRuntimeV1 | null) {
    if (inFlight.current) { return inFlight.current; }
    const operation = retryWork(runtime);
    inFlight.current = operation;
    void operation.finally(() => { if (inFlight.current === operation) { inFlight.current = null; } });
    return operation;
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
    settle,
  };
}

async function sameDraftCheckpoint(draft: SaveDraft, checkpoint: RuntimeCheckpointV1) {
  if (draft.metadata.extinfo.checkpointFormat !== checkpoint.format || draft.payload.size !== checkpoint.bytes.length) { return false; }
  const bytes = new Uint8Array(await draft.payload.arrayBuffer());
  return bytes.every((byte, index) => byte === checkpoint.bytes[index]);
}
