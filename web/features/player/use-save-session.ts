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
import { prepareSaveScreenshot } from "./save-screenshot";
import { checkpointScreenshot } from "./runtime/dispose-runtime";
import { reclaimProviderContentCache } from "./runtime/provider-dispatcher";
type SaveFeedback = "FOREGROUND" | "BACKGROUND";
export function useSaveSession(
  run: Schema<"Run">,
  userId: string,
  native: boolean,
) {
  const { notify } = useToast();
  const protectedSourceId = run.save?.restoreWarning === "core_changed" ? run.save.id : null;
  const writableSave = protectedSourceId ? null : run.save;
  const selectedSave = useRef(writableSave);
  const [currentSave, setCurrentSave] = useState(writableSave);
  const saving = useRef(false);
  const inFlight = useRef<Promise<boolean> | null>(null);
  const queued = useRef(false);
  const pending = useRef<SaveDraft | null>(null);
  const [draft, setDraft] = useState<SaveDraft | null>(null);
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [background, setBackground] = useState(false);
  const [preview, setPreview] = useState<RuntimeFinalSnapshotV1 | null>(null);
  const reclaimCache = useCallback<NonNullable<Parameters<typeof putDraft>[1]>>(
    (request) => reclaimProviderContentCache(run.envelope, request),
    [run.envelope],
  );
  const persist = useCallback(async (record: SaveDraft, checkpoint: RuntimeCheckpointV1, runtime: PlayerRuntimeV1 | null, feedback: SaveFeedback = "FOREGROUND") => {
    let persisted = false;
    try {
      const save = await uploadDraft(record, userId, reclaimCache);
      persisted = true;
      selectedSave.current = save;
      setCurrentSave(save);
      if (native) { await acknowledgeNativeSave(runtime, checkpoint); }
      setDraft(null);
      pending.current = null;
      setStatus("");
      if (feedback === "FOREGROUND") { notify({ tone: "good", message: "存档已同步。" }); }
      return true;
    } catch (failure) {
      // The server commit can succeed before Runtime acknowledgement fails.
      // Keep the same commit identity so retry never creates a second save.
      if (persisted) {
        try { await putDraft(record, reclaimCache); }
        catch (storageFailure) { setStatus(storageFailure instanceof Error ? storageFailure.message : "无法保留浏览器草稿。"); return false; }
      }
      setStatus(failure instanceof Error ? failure.message : "同步失败，存档已保留在浏览器。");
      return false;
    }
  }, [userId, native, notify, reclaimCache]);
  const writableTarget = useCallback((target: Schema<"Save"> | null) => {
    const chosen = native ? selectedSave.current : target;
    return chosen?.id === protectedSourceId ? null : chosen;
  }, [native, protectedSourceId]);
  const commit = useCallback(
    async (
      snapshot: RuntimeFinalSnapshotV1,
      runtime: PlayerRuntimeV1 | null,
      target: Schema<"Save"> | null = null,
      feedback: SaveFeedback = "FOREGROUND",
    ) => {
      // A final snapshot can arrive while an ordinary native save is still
      // uploading. Finish it before choosing the save ID and expected version.
      if (runtime === null) { await inFlight.current; }
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
      const writable = writableTarget(target);
      const screenshot = await prepareSaveScreenshot(snapshot.screenshot);
      const record = makeDraft(userId, run, snapshot.checkpoint, screenshot, writable, native);
      setDraft(record);
      pending.current = record;
      await putDraft(record, reclaimCache);
      return persist(record, snapshot.checkpoint, runtime, feedback);
    },
    [run, userId, native, notify, persist, writableTarget, reclaimCache],
  );
  const captureWork = useCallback(
    async (
      runtime: PlayerRuntimeV1,
      intent: "CAPTURE" | "EXPORT" = "CAPTURE",
      target: Schema<"Save"> | null = null,
      feedback: SaveFeedback = "FOREGROUND",
    ) => {
      if (saving.current) { queued.current = true; return false; }
      if (pending.current) {
        queued.current = native;
        if (feedback === "FOREGROUND") { notify({ tone: "warn", message: "请先同步或导出已有草稿，再保存新的进度。" }); }
        return false;
      }
      saving.current = true;
      setBusy(true);
      setBackground(feedback === "BACKGROUND");
      try {
        do {
          queued.current = false;
          const checkpoint = await runtime.checkpoint({ intent });
          // A record without an image still takes the current frame.
          const screenshot = await checkpointScreenshot(runtime, checkpoint, Boolean(writableTarget(target)?.screenshotUrl));
          const committed = await commit({ checkpoint, screenshot }, runtime, target, feedback);
          if (!committed) { return false; }
          intent = "EXPORT";
          // Revisions queued during a requested save are subsequent background work.
          feedback = "BACKGROUND";
          setBackground(true);
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
        setBackground(false);
      }
    },
    [commit, native, notify, writableTarget],
  );
  const capture = useCallback((runtime: PlayerRuntimeV1, intent: "CAPTURE" | "EXPORT" = "CAPTURE", target: Schema<"Save"> | null = null, feedback: SaveFeedback = "FOREGROUND") => {
    if (inFlight.current) { queued.current = true; return inFlight.current; }
    const operation = captureWork(runtime, intent, target, feedback);
    inFlight.current = operation;
    void operation.finally(() => { if (inFlight.current === operation) { inFlight.current = null; } });
    return operation;
  }, [captureWork]);
  const settle = useCallback(async () => { await inFlight.current; }, []);
  const nativeChanged = useCallback(
    (runtime: PlayerRuntimeV1) => {
      if (native && run.purpose === "play") {
        void capture(runtime, "EXPORT", null, "BACKGROUND");
      }
    },
    [native, run.purpose, capture],
  );
  async function retryWork(runtime: PlayerRuntimeV1 | null) {
    const record = pending.current;
    if (!record || saving.current) { return false; }
    saving.current = true;
    setBusy(true);
    setBackground(false);
    let synced = false;
    try {
      await putDraft(record, reclaimCache);
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
    if (synced && native && runtime?.getCheckpointAvailability().available) { return captureWork(runtime, "EXPORT", null, "BACKGROUND"); }
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
    background,
    preview,
    currentSave,
    commit,
    capture,
    nativeChanged,
    retry,
    settle,
  };
}

async function acknowledgeNativeSave(runtime: PlayerRuntimeV1 | null, checkpoint: RuntimeCheckpointV1) {
  if (!runtime || normallyExiting(runtime)) { return; }
  try { await runtime.acknowledgeCheckpoint?.(checkpoint); }
  catch (failure) {
    // Confirmation only advances the running core's revision. After normal
    // termination there is no live revision to advance; the server commit won.
    if (!normallyExiting(runtime)) { throw failure; }
  }
}

function normallyExiting(runtime: PlayerRuntimeV1) {
  const state = runtime.getState();
  return state === "EXITING" || state === "EXITED";
}

async function sameDraftCheckpoint(draft: SaveDraft, checkpoint: RuntimeCheckpointV1) {
  if (draft.metadata.extinfo.checkpointFormat !== checkpoint.format || draft.payload.size !== checkpoint.bytes.length) { return false; }
  const bytes = new Uint8Array(await draft.payload.arrayBuffer());
  return bytes.every((byte, index) => byte === checkpoint.bytes[index]);
}
