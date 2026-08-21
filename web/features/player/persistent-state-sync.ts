import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

type PersistentStateManager = NonNullable<EmulatorInstance["gameManager"]>;
type SyncEvent = "AUTO_INTERVAL" | "EXIT";

type SyncOptions = {
  intervalMs?: number;
  isPaused?: () => boolean;
  onError?: (error: Error) => void;
};

function stateError() {
  return new Error("PLAYER_PERSISTENT_STATE_UNAVAILABLE");
}

function fingerprint(bytes: Uint8Array) {
  let hash = 0x811c9dc5;
  for (const byte of bytes) {
    hash ^= byte;
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return `${bytes.byteLength}:${hash.toString(16).padStart(8, "0")}`;
}

export class PersistentStateSync {
  private readonly intervalMs: number;
  private readonly isPaused: () => boolean;
  private readonly onError: (error: Error) => void;
  private timer: ReturnType<typeof setInterval> | null = null;
  private active: Promise<boolean> | null = null;
  private savedFingerprint: string | null;
  private failedFingerprint: string | null = null;

  constructor(
    private readonly manager: PersistentStateManager,
    restoredState: Uint8Array | null,
    private readonly upload: (bytes: Uint8Array, event: SyncEvent) => Promise<boolean>,
    options: SyncOptions = {},
  ) {
    this.intervalMs = options.intervalMs ?? 30_000;
    this.isPaused = options.isPaused ?? (() => false);
    this.onError = options.onError ?? (() => undefined);
    this.savedFingerprint = restoredState ? fingerprint(restoredState) : null;
  }

  start() {
    if (this.timer !== null) return;
    this.timer = setInterval(() => { void this.poll(); }, this.intervalMs);
  }

  stop() {
    if (this.timer !== null) clearInterval(this.timer);
    this.timer = null;
  }

  poll() {
    return this.captureAndUpload("AUTO_INTERVAL");
  }

  async flush() {
    this.stop();
    if (this.active) await this.active;
    return this.captureAndUpload("EXIT");
  }

  private captureAndUpload(event: SyncEvent) {
    if (this.active) return this.active;
    this.active = (async () => {
      let currentFingerprint: string | undefined;
      try {
        if (!this.manager.getState || !this.manager.toggleMainLoop) throw stateError();
        const shouldResume = !this.isPaused();
        this.manager.toggleMainLoop(false);
        let state: Uint8Array;
        try {
          const view = this.manager.getState();
          if (!view || !ArrayBuffer.isView(view) || view.byteLength === 0) throw stateError();
          state = new Uint8Array(view).slice();
        } finally {
          this.manager.toggleMainLoop(shouldResume);
        }
        currentFingerprint = fingerprint(state);
        if (currentFingerprint === this.savedFingerprint) return false;
        const uploaded = await this.upload(state, event);
        if (uploaded) {
          this.savedFingerprint = currentFingerprint;
          this.failedFingerprint = null;
        }
        return uploaded;
      } catch (error) {
        if (!currentFingerprint || currentFingerprint !== this.failedFingerprint) {
          this.failedFingerprint = currentFingerprint ?? "unavailable";
          this.onError(error instanceof Error ? error : stateError());
        }
        return false;
      } finally {
        this.active = null;
      }
    })();
    return this.active;
  }
}
