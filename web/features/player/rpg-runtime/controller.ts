import type {
  CheckpointAvailability,
  CheckpointPayload,
  RetromRpgRuntime,
  RuntimeEvent,
  RuntimeState,
} from "./contract";
import type { RpgPlayerInstance, RpgRuntimeAdapter } from "./internal-adapter";

type AdapterMount = (target: HTMLElement) => Promise<RpgRuntimeAdapter>;

const unavailable: CheckpointAvailability = { available: false, reason: "RUNTIME_NOT_READY" };

export class RetromRpgRuntimeController implements RetromRpgRuntime {
  private readonly mountAdapter: AdapterMount;
  private readonly listeners = new Set<(event: RuntimeEvent) => void>();
  private readonly abortSignal: AbortSignal | null;
  private adapter: RpgRuntimeAdapter | null = null;
  private state: RuntimeState = "CREATED";
  private mountCalled = false;
  private exitPromise: Promise<void> | null = null;
  private playerInstance: RpgPlayerInstance | null = null;
  private lastAvailability: CheckpointAvailability = unavailable;
  private availabilityTimer: number | null = null;

  private readonly validationPurpose: boolean;

  constructor(mountAdapter: AdapterMount, abortSignal: AbortSignal | null = null, validationPurpose = false) {
    this.mountAdapter = mountAdapter;
    this.abortSignal = abortSignal;
    this.validationPurpose = validationPurpose;
    abortSignal?.addEventListener("abort", this.abort, { once: true });
  }

  async mount(target: HTMLElement) {
    if (this.mountCalled || this.state !== "CREATED") {throw new Error("RPG_RUNTIME_INVALID_STATE");}
    this.mountCalled = true;
    if (this.abortSignal?.aborted) {
      await this.exit();
      throw new DOMException("Aborted", "AbortError");
    }
    this.transition("LOADING");
    try {
      const adapter = await this.mountAdapter(target);
      if (this.abortSignal?.aborted || exitHasStarted(this.state)) {
        await adapter.exit();
        throw new DOMException("Aborted", "AbortError");
      }
      this.adapter = adapter;
      this.playerInstance = this.createPlayerInstance(adapter);
      this.transition("RUNNING");
      this.refreshAvailability();
      this.startAvailabilityPolling();
      this.emit({ type: "READY" });
    } catch (error) {
      await this.fail(error);
      throw stableError(error);
    }
  }

  async pause() {
    this.requireState("RUNNING");
    try {
      await this.requireAdapter().pause();
      this.assertOperationActive("RUNNING");
      this.transition("PAUSED");
    } catch (error) {
      if (!exitHasStarted(this.state)) {await this.fail(error);}
      throw stableError(error);
    }
  }

  async resume() {
    this.requireState("PAUSED");
    try {
      await this.requireAdapter().resume();
      this.assertOperationActive("PAUSED");
      this.transition("RUNNING");
    } catch (error) {
      if (!exitHasStarted(this.state)) {await this.fail(error);}
      throw stableError(error);
    }
  }

  async checkpoint(): Promise<CheckpointPayload> {
    if (this.state !== "RUNNING" && this.state !== "PAUSED") {throw new Error("RPG_RUNTIME_INVALID_STATE");}
    if (!this.getCheckpointAvailability().available) {throw new Error("RPG_CHECKPOINT_UNAVAILABLE");}
    const previous = this.state;
    this.transition("CHECKPOINTING");
    try {
      const payload = await this.requireAdapter().checkpoint();
      if (!payload.bytes.byteLength) {throw new Error("RPG_CHECKPOINT_CREATE_FAILED");}
      this.assertOperationActive("CHECKPOINTING");
      this.transition(previous);
      this.refreshAvailability();
      return { bytes: payload.bytes.slice(), payloadKind: payload.payloadKind };
    } catch (error) {
      if (isCheckpointing(this.state)) {
        this.transition(previous);
        this.refreshAvailability();
      }
      throw stableError(error);
    }
  }

  async screenshot() {
    if (this.state !== "RUNNING" && this.state !== "PAUSED") {throw new Error("RPG_RUNTIME_INVALID_STATE");}
    const screenshot = await this.requireAdapter().screenshot();
    if (!screenshot.size) {throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");}
    return screenshot;
  }

  async exit() {
    if (this.exitPromise) {return this.exitPromise;}
    this.exitPromise = this.performExit();
    return this.exitPromise;
  }

  getState() {return this.state;}

  getCheckpointAvailability() {
    if (this.state === "FAILED") {return { available: false, reason: "RUNTIME_FAILED" } as const;}
    if ((this.state !== "RUNNING" && this.state !== "PAUSED") || !this.adapter) {return unavailable;}
    return this.refreshAvailability();
  }

  subscribe(listener: (event: RuntimeEvent) => void) {
    this.listeners.add(listener);
    return () => {this.listeners.delete(listener);};
  }

  getPlayerInstance() {
    if (!this.playerInstance) {throw new Error("RPG_RUNTIME_INVALID_STATE");}
    return this.playerInstance;
  }

  private readonly abort = () => {void this.exit();};

  private async performExit() {
    if (this.state === "EXITED") {return;}
    const failed = this.state === "FAILED";
    if (!failed) {this.transition("EXITING");}
    let exitError: unknown;
    this.stopAvailabilityPolling();
    try {await this.adapter?.exit();}
    catch (error) {exitError = error;}
    this.adapter = null;
    this.playerInstance = null;
    this.abortSignal?.removeEventListener("abort", this.abort);
    if (!failed) {this.transition("EXITED");}
    this.listeners.clear();
    if (exitError) {throw stableError(exitError);}
  }

  private async fail(error: unknown) {
    this.stopAvailabilityPolling();
    const adapter = this.adapter;
    this.adapter = null;
    this.playerInstance = null;
    if (this.state !== "FAILED" && this.state !== "EXITED") {this.transition("FAILED");}
    try {await adapter?.exit();}
    catch { /* The original runtime failure remains authoritative. */ }
    this.emit({ type: "FATAL_ERROR", code: stableError(error).message });
  }

  private createPlayerInstance(adapter: RpgRuntimeAdapter): RpgPlayerInstance {
    let mainLoopTransition: Promise<void> = Promise.resolve();
    const instance: RpgPlayerInstance = {
      canvas: adapter.getCanvas(), paused: false, volume: 1, muted: false,
      on: () => undefined,
      setVolume: (value) => adapter.setVolume(value),
      takeScreenshot: async () => ({ blob: await this.screenshot(), format: "png" }),
      gameManager: {
        savePayloadKind: adapter.getPayloadKind(),
        validationPurpose: this.validationPurpose,
        getRpgPosition: () => adapter.getPosition(),
        getCheckpointAvailability: () => this.getCheckpointAvailability(),
        getStateAsync: async () => {
          await mainLoopTransition;
          const payload = await this.checkpoint();
          return payload.bytes;
        },
        getFrameNum: () => adapter.getFrameCount(),
        getVideoDimensions: (dimension) => videoDimension(adapter.getCanvas(), dimension),
        toggleMainLoop: (running) => {
          instance.paused = !running;
          mainLoopTransition = mainLoopTransition.then(() => running ? this.resume() : this.pause());
          void mainLoopTransition.catch(() => undefined);
        },
      },
    };
    return instance;
  }

  private refreshAvailability() {
    const next = normalizedAvailability(this.requireAdapter().getCheckpointAvailability());
    if (next.available !== this.lastAvailability.available || next.reason !== this.lastAvailability.reason) {
      this.lastAvailability = next;
      this.emit({ type: "CHECKPOINT_AVAILABILITY_CHANGED", availability: next });
    }
    return next;
  }

  private startAvailabilityPolling() {
    this.availabilityTimer = window.setInterval(() => {
      if (this.state !== "RUNNING" && this.state !== "PAUSED") {return;}
      try {this.refreshAvailability();}
      catch (error) {
        if (!exitHasStarted(this.state)) {void this.fail(error);}
      }
    }, 250);
  }

  private stopAvailabilityPolling() {
    if (this.availabilityTimer !== null) {window.clearInterval(this.availabilityTimer);}
    this.availabilityTimer = null;
  }

  private requireAdapter() {
    if (!this.adapter) {throw new Error("RPG_RUNTIME_INVALID_STATE");}
    return this.adapter;
  }

  private requireState(expected: RuntimeState) {
    if (this.state !== expected) {throw new Error("RPG_RUNTIME_INVALID_STATE");}
  }

  private assertOperationActive(expected: RuntimeState) {
    if (this.state !== expected) {throw new DOMException("Aborted", "AbortError");}
  }

  private transition(next: RuntimeState) {
    if (!validTransition(this.state, next)) {throw new Error("RPG_RUNTIME_INVALID_STATE");}
    const previous = this.state;
    this.state = next;
    this.emit({ type: "STATE_CHANGED", previous, state: next });
  }

  private emit(event: RuntimeEvent) {
    for (const listener of this.listeners) {listener(event);}
  }
}

function validTransition(previous: RuntimeState, next: RuntimeState) {
  if (next === "FAILED") {return previous !== "FAILED" && previous !== "EXITED";}
  return transitions[previous].includes(next);
}

const transitions: Record<RuntimeState, readonly RuntimeState[]> = {
  CREATED: ["LOADING", "EXITING"],
  LOADING: ["RUNNING", "EXITING"],
  RUNNING: ["PAUSED", "CHECKPOINTING", "EXITING"],
  PAUSED: ["RUNNING", "CHECKPOINTING", "EXITING"],
  CHECKPOINTING: ["RUNNING", "PAUSED", "EXITING"],
  EXITING: ["EXITED"],
  EXITED: [],
  FAILED: [],
};

function exitHasStarted(state: RuntimeState) {
  return state === "EXITING" || state === "EXITED";
}

function isCheckpointing(state: RuntimeState) {
  return state === "CHECKPOINTING";
}

function normalizedAvailability(value: CheckpointAvailability): CheckpointAvailability {
  if (value.available === true && value.reason === null) {return { available: true, reason: null };}
  if (value.available === false && value.reason !== null) {return { available: false, reason: value.reason };}
  return { available: false, reason: "RUNTIME_FAILED" };
}

function videoDimension(canvas: HTMLCanvasElement | undefined, dimension: "aspect" | "width" | "height") {
  if (!canvas) {return undefined;}
  if (dimension === "width") {return canvas.width;}
  if (dimension === "height") {return canvas.height;}
  return canvas.height > 0 ? canvas.width / canvas.height : undefined;
}

function stableError(error: unknown) {
  if (error instanceof DOMException && error.name === "AbortError") {return error;}
  if (error instanceof Error && /^(?:PLAYER|RPG)_[A-Z0-9_]+$/u.test(error.message)) {return error;}
  return new Error("RPG_RUNTIME_FAILED");
}
