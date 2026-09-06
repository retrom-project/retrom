import {GameSaveConflict} from "./game-save-upload-error";
import {newUuid} from "@/lib/crypto";
import type {PlayerRuntimeV1, RuntimeCheckpointV1} from "./runtime/contract";
import {captureRuntimeSave, type RuntimeSavePayload} from "./runtime/runtime-actions";

type GameSaveRuntime = Pick<PlayerRuntimeV1,
  "checkpoint" | "screenshot" | "acknowledgeCheckpoint" | "subscribe" | "getCheckpointAvailability">;
export type GameSavePresentation = {available: false; retryAvailable: boolean; text: string; tone: "synced" | "busy" | "warning"};

/** One serialized, idempotent stream of complete native data snapshots per launch. */
export class GameSaveSync {
  private unsubscribe: (() => void) | null = null;
  private pending: Promise<boolean> | null = null;
  private stopped = false;
  private attemptedRevision: string | undefined;
  private failed = false;
  private conflict: GameSaveConflict | null = null;
  private captured: RuntimeSavePayload | null = null;
  private persisted: RuntimeCheckpointV1 | null = null;

  constructor(
    private readonly runtime: GameSaveRuntime,
    private readonly upload: (payload: RuntimeSavePayload) => Promise<boolean>,
    private readonly present: (state: GameSavePresentation) => void,
  ) {}

  start() {
    this.unsubscribe = this.runtime.subscribe((event) => {
      if (event.type === "CHECKPOINT_AVAILABILITY_CHANGED") {this.refresh();}
    });
    this.refresh();
  }

  save(): Promise<boolean> {
    const availability = this.runtime.getCheckpointAvailability();
    if (this.stopped || this.pending || this.conflict || !this.runtime.acknowledgeCheckpoint ||
      !this.captured && !availability.available) {return Promise.resolve(false);}
    if (!this.captured) {this.attemptedRevision = availability.revision;}
    const pending = Promise.resolve().then(() => this.performSave());
    this.pending = pending;
    this.present({available: false, retryAvailable: false, text: "正在同步游戏数据…", tone: "busy"});
    void pending.then((ok) => {
      this.failed = !ok;
      this.pending = null;
      this.refresh();
    });
    return pending;
  }

  /** Drain stable changes before a normal exit. Keep the session alive on failure. */
  async flush() {
    const deadline = Date.now() + 10_000;
    while (!this.stopped) {
      if (this.conflict) {return;}
      if (this.pending) {if (!await this.pending) {throw new Error("GAME_DATA_SYNC_FAILED");} continue;}
      if (this.failed) {throw new Error("GAME_DATA_SYNC_FAILED");}
      const availability = this.runtime.getCheckpointAvailability();
      if (availability.available && availability.revision !== this.attemptedRevision) {
        if (!await this.save()) {throw new Error("GAME_DATA_SYNC_FAILED");}
        continue;
      }
      if (availability.reason === "NO_SAVE" || availability.reason === "UNCHANGED") {return;}
      if (availability.reason !== "BUSY") {throw new Error("GAME_DATA_SYNC_UNAVAILABLE");}
      if (Date.now() >= deadline) {throw new Error("GAME_DATA_SYNC_BUSY");}
      await new Promise<void>((resolve) => setTimeout(resolve, 100));
    }
  }

  async stop() {
    this.stopped = true;
    this.unsubscribe?.();
    this.unsubscribe = null;
    await this.pending;
  }

  private async performSave() {
    try {
      if (!this.captured) {
        this.captured = {...await captureRuntimeSave(this.runtime), source: "GAME_SAVE", requestId: newUuid()};
      }
      if (!this.persisted) {
        if (!await this.upload(this.captured)) {return false;}
        this.persisted = this.captured.checkpoint;
      }
      await this.runtime.acknowledgeCheckpoint?.(this.persisted);
      this.persisted = null;
      this.captured = null;
      return true;
    } catch (error) {
      if (error instanceof GameSaveConflict) {this.conflict = error;}
      return false;
    }
  }

  private presentationText(availability: ReturnType<GameSaveRuntime["getCheckpointAvailability"]>) {
    if (this.conflict) {return this.conflict.message;}
    if (this.failed) {return "游戏数据同步失败，请重试同步";}
    if (availability.available) {return "游戏数据待同步";}
    switch (availability.reason) {
      case "UNCHANGED": return "游戏数据已同步";
      case "NO_SAVE": return "尚无游戏数据变化";
      case "BUSY": return "等待游戏完成数据写入…";
      default: return "游戏数据暂不可同步";
    }
  }

  private refresh() {
    if (this.stopped || this.pending) {return;}
    const availability = this.runtime.getCheckpointAvailability();
    if (!this.failed && availability.available && availability.revision && this.runtime.acknowledgeCheckpoint &&
      availability.revision !== this.attemptedRevision) {void this.save(); return;}
    const text = this.presentationText(availability);
    this.present({available: false, retryAvailable: this.failed && !this.conflict, text, tone: this.failed ? "warning" : "synced"});
  }
}
