import {GameSaveConflict} from "./game-save-upload-error";
import {newUuid} from "@/lib/crypto";
import type {PlayerRuntimeV1, RuntimeCheckpointRequestV1, RuntimeFinalSnapshotV1} from "./runtime/contract";
import type {NativeSaveCapabilities} from "./checkpoint-semantics";
import {captureRuntimeSave, type RuntimeSavePayload} from "./runtime/runtime-actions";
import type {GameSaveDraftStore} from "./game-save-draft-store";
import {prepareManualSaveScreenshot} from "./manual-save-screenshot";

type GameSaveRuntime = Pick<PlayerRuntimeV1,
  "checkpoint" | "screenshot" | "acknowledgeCheckpoint" | "subscribe" | "getCheckpointAvailability">;
export type GameSavePresentation = {
  available: boolean; dirty: boolean; retryAvailable: boolean; save?: NativeSaveCapabilities;
  text: string; tone: "synced" | "busy" | "warning";
};

/** Export locally while playing; only explicit save/capture actions submit to the server. */
export class GameSaveSync {
  private unsubscribe: (() => void) | null = null;
  private pending: Promise<boolean> | null = null;
  private stopped = false;
  private ended = false;
  private attemptedRevision: string | undefined;
  private failed = false;
  private conflict: GameSaveConflict | null = null;
  private captured: RuntimeSavePayload | null = null;
  private committing = false;
  private uploaded = false;

  constructor(
    private readonly runtime: GameSaveRuntime,
    private readonly upload: (payload: RuntimeSavePayload) => Promise<boolean>,
    private readonly present: (state: GameSavePresentation) => void,
    private readonly store: GameSaveDraftStore,
  ) {}

  start() {
    this.unsubscribe = this.runtime.subscribe((event) => {
      if (event.type === "CHECKPOINT_AVAILABILITY_CHANGED") {this.refresh();}
    });
    this.refresh();
  }

  /** Wait for a complete local snapshot, never upload during exit preparation. */
  async flush() {
    const deadline = Date.now() + 10_000;
    while (!this.stopped) {
      if (this.pending) {if (!await this.pending) {throw Error("LOCAL_DRAFT_STORAGE_FAILED");} continue;}
      if (this.failed) {throw Error("LOCAL_DRAFT_STORAGE_FAILED");}
      if (this.ended) {return;}
      const availability = this.runtime.getCheckpointAvailability();
      if (availability.available && availability.revision !== this.attemptedRevision) {
        await this.stage(); continue;
      }
      if ((availability.reason === "UNCHANGED" || availability.reason === "NO_SAVE") && this.captured && !this.uploaded) {
        await this.store.remove(); this.captured = null; this.attemptedRevision = undefined;
      }
      if (availability.reason !== "BUSY") {return;}
      if (Date.now() >= deadline) {throw Error("GAME_DATA_BUSY");}
      await new Promise<void>((resolve) => setTimeout(resolve, 100));
    }
  }

  async retry() {
    if (this.stopped || this.committing) {return false;}
    if (this.ended) {
      if (!this.captured) {return !this.failed;}
      try {await this.store.put(this.captured); this.failed = false; return true;}
      catch {return false;}
      finally {this.refresh();}
    }
    this.failed = false; this.attemptedRevision = undefined;
    this.refresh();
    try {await this.flush(); return true;} catch {return false;}
  }

  async save(): Promise<boolean> {
    if (this.stopped || this.committing || this.conflict) {return false;}
    this.committing = true;
    try {
      if (this.failed && !this.ended) {this.failed = false; this.attemptedRevision = undefined;}
      if (this.ended) {await this.pending; if (!this.captured && this.failed) {return false;}}
      else {await this.flush();}
      return await this.commitCaptured();
    } catch (error) {
      if (error instanceof GameSaveConflict) {this.conflict = error;}
      this.show(this.conflict?.message ?? "保存失败，本地草稿已保留，请重试", "warning");
      return false;
    } finally {this.committing = false;}
  }

  canCapture(): boolean {
    return !this.stopped && !this.ended && !this.pending && !this.committing && !this.conflict &&
      Boolean(this.runtime.getCheckpointAvailability().save?.captureAvailable);
  }

  isStorageContainer(): boolean {
    return this.runtime.getCheckpointAvailability().save?.dataKind === "STORAGE";
  }

  async capture(): Promise<boolean> {
    if (this.stopped || this.ended || this.pending || this.committing || this.conflict ||
      !this.runtime.getCheckpointAvailability().save?.captureAvailable) {return false;}
    this.committing = true;
    try {
      if (!await this.stage({intent: "CAPTURE"})) {return false;}
      this.attemptedRevision = this.runtime.getCheckpointAvailability().revision;
      return await this.commitCaptured();
    } catch (error) {
      if (error instanceof GameSaveConflict) {this.conflict = error;}
      this.show(this.conflict?.message ?? "保存失败，本地草稿已保留，请重试", "warning");
      return false;
    } finally {this.committing = false; this.refresh();}
  }

  private async commitCaptured(): Promise<boolean> {
  if (!this.captured) {return true;}
  this.show("正在保存游戏数据…", "busy");
  if (!this.uploaded) {
    if (!await this.upload(this.captured)) {this.show("保存失败，本地草稿已保留，可重试或继续游戏", "warning"); return false;}
    this.uploaded = true;
  }
  if (!this.ended) {await this.runtime.acknowledgeCheckpoint?.(this.captured.checkpoint);}
  await this.store.remove();
  this.captured = null; this.uploaded = false;
  this.show("游戏数据已保存", "synced");
  return true;
  }

  /** Freeze final bytes before awaiting an older draft; the live core can be removed immediately. */
  finish(snapshot?: RuntimeFinalSnapshotV1): Promise<boolean> {
    if (this.ended) {return this.pending !== null ? this.pending : Promise.resolve(!this.failed);}
    this.ended = true; this.unsubscribe?.(); this.unsubscribe = null;
    if (!snapshot) {return this.pending !== null ? this.pending : Promise.resolve(!this.failed);}
    const payload: RuntimeSavePayload = {checkpoint: {...snapshot.checkpoint, bytes: snapshot.checkpoint.bytes.slice()},
      screenshot: snapshot.screenshot ?? new Blob(), source: "GAME_SAVE", requestId: newUuid(), name: "退出时的游戏存档"};
    const previous = this.pending !== null ? this.pending : Promise.resolve(true);
    const pending = previous.then(async () => {
      this.captured = payload; this.uploaded = false;
      try {
        if (payload.screenshot.size) {
          const image = await prepareManualSaveScreenshot({screenshot: payload.screenshot, format: payload.screenshot.type});
          payload.screenshot = image?.screenshot ?? new Blob();
        }
        await this.store.put(payload); this.failed = false; return true;
      } catch {this.failed = true; return false;}
    });
    this.pending = pending;
    void pending.then(() => {if (this.pending === pending) {this.pending = null;} this.refresh();});
    return pending;
  }

  async discard() {
    if (this.committing) {throw Error("GAME_SAVE_BUSY");}
    this.committing = true;
    try {
      await this.pending;
      await this.store.remove();
      this.captured = null;
      await this.stop();
    } finally {this.committing = false; this.refresh();}
  }

  hasChanges() {
    if (this.ended) {return Boolean(this.captured) || this.failed || Boolean(this.pending);}
    const state = this.runtime.getCheckpointAvailability();
    return Boolean(this.captured) || this.failed || state.available || state.reason === "BUSY";
  }

  async stop() {
    this.stopped = true;
    this.unsubscribe?.(); this.unsubscribe = null;
    await this.pending;
  }

  private stage(request: RuntimeCheckpointRequestV1 = {intent: "EXPORT"}): Promise<boolean> {
    if (this.pending) {return this.pending;}
    this.attemptedRevision = this.runtime.getCheckpointAvailability().revision;
    const pending = Promise.resolve().then(async () => {
      try {
        const payload = {...await captureRuntimeSave(this.runtime, request), source: "GAME_SAVE" as const, requestId: newUuid(), name: `游戏存档 ${new Date().toLocaleString("zh-CN")}`};
        const image = await prepareManualSaveScreenshot({screenshot: payload.screenshot, format: payload.screenshot.type});
        if (!image) {throw Error("LOCAL_DRAFT_SCREENSHOT_FAILED");}
        payload.screenshot = image.screenshot;
        await this.store.put(payload);
        this.captured = payload; this.uploaded = false; this.failed = false;
        return true;
      } catch {this.failed = true; return false;}
    });
    this.pending = pending;
    this.show("正在暂存本次游戏数据…", "busy");
    void pending.then(() => {if (this.pending === pending) {this.pending = null;} this.refresh();});
    return pending;
  }

  private clearUnchanged() {
    const pending = this.store.remove().then(() => {this.captured = null; this.attemptedRevision = undefined; return true;}, () => {this.failed = true; return false;});
    this.pending = pending;
    void pending.then(() => {if (this.pending === pending) {this.pending = null;} this.refresh();});
  }

  private show(text: string, tone: GameSavePresentation["tone"]) {
    const availability = this.runtime.getCheckpointAvailability();
    this.present({available: !this.ended && !this.pending && !this.committing && Boolean(availability.save?.captureAvailable),
      dirty: Boolean(this.captured) || !this.ended && (availability.available || availability.reason === "BUSY"),
      retryAvailable: this.failed, save: availability.save, text, tone});
  }

  private showFinal() {
    this.show(this.failed ? "最终存档暂存失败，可重试保存或保留数据" : "游戏已结束，最终存档可保存", this.failed ? "warning" : "synced");
  }

  private refresh() {
    if (this.stopped || this.pending || this.committing) {return;}
    if (this.ended) {
      this.showFinal();
      return;
    }
    const availability = this.runtime.getCheckpointAvailability();
    if (this.failed) {this.show("本地暂存失败，请重试或在退出时保存", "warning"); return;}
    if (availability.available && availability.revision && availability.revision !== this.attemptedRevision) {void this.stage(); return;}
    if ((availability.reason === "UNCHANGED" || availability.reason === "NO_SAVE") && this.captured) {
      this.clearUnchanged(); return;
    }
    this.show(this.captured ? "数据已暂存在此浏览器，退出时可保存" : availability.reason === "BUSY"
      ? "等待游戏完成数据写入…" : "本次游戏数据未变化", "synced");
  }
}
