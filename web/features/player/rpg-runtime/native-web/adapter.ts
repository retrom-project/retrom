import type { RpgPlayerInstance } from "../internal-adapter";
import {
  decodeRpgCheckpoint,
  encodeRpgCheckpoint,
  type RpgCheckpointBundle,
  type RpgCheckpointStore,
} from "../checkpoint";
import type { RpgPosition, RpgRuntimeConfig } from "../contract";

type NativeConfig = RpgRuntimeConfig & {
  adapter: Extract<RpgRuntimeConfig["adapter"], { adapterKind: "NATIVE_WEB" }>;
};

type Reply = {
  body: Record<string, unknown>;
  launchId: string;
  nonce: string;
  protocolVersion: number;
  requestId: number;
  type: string;
};

type Pending = {
  reject: (reason: Error) => void;
  resolve: (reply: Reply) => void;
  timer: number;
};

type NativeBootstrapStage = "BOOTSTRAP" | "BRIDGE";
type NativeBootstrapAction = "SEND_TICKET" | "CONNECT" | "IGNORE";

const protocolVersion = 1;
const maximumControlBytes = 64 * 1024;
const maximumCheckpointBytes = 64 * 1024 * 1024;
const maximumScreenshotBytes = 10 * 1024 * 1024;
const bootstrapTimeoutMs = 10_000;
const channelReadyTimeoutMs = 10_000;

export async function mountNativeRpg(
  config: NativeConfig,
  frame: HTMLIFrameElement,
  restorePayload: Uint8Array | null,
) {
  const channel = new NativeChannel(config);
  frame.setAttribute("sandbox", "allow-scripts allow-same-origin allow-pointer-lock");
  frame.referrerPolicy = "no-referrer";
  let expectedEngine: "RPGMV" | "RPGMZ";
  try {
    await bootstrapNativeFrame(config, frame, channel);
    const ready = await channel.ready();
    expectedEngine = config.generation === "RPGMV" ? "RPGMV" : "RPGMZ";
    if (ready.engine !== expectedEngine || ready.engineProfile !== config.adapter.bridgeProfile) {
      throw new Error("RPG_ENGINE_PROFILE_MISMATCH");
    }
    if (restorePayload) {
      const bundle = await decodeRpgCheckpoint(restorePayload, expectedEngine);
      await channel.restore(bundle);
    }
    channel.startProbeLoop();
  } catch (error) {
    channel.close();
    frame.src = "about:blank";
    throw error;
  }

  const listeners = new Map<string, Set<(...args: unknown[]) => void>>();
  const instance: RpgPlayerInstance = {
    paused: false,
    volume: 1,
    muted: false,
    on(event, callback) {
      const current = listeners.get(event) ?? new Set();
      current.add(callback);
      listeners.set(event, current);
    },
    setVolume(value) {
      instance.volume = value;
      instance.muted = value === 0;
      void channel.request("SET_VOLUME", { value });
    },
    takeScreenshot: async () => ({ blob: await channel.screenshot(), format: "png" }),
    gameManager: {
      savePayloadKind: "NATIVE_SAVE_BUNDLE_V1",
      validationPurpose: config.purpose === "RPG_RUNTIME_VALIDATION",
      getRpgPosition: () => channel.position(),
      getCheckpointAvailability: () => ({ available: channel.checkpointAvailable(), reason: channel.checkpointAvailable() ? null : "BUSY" }),
      getStateAsync: () => channel.save(expectedEngine),
      getFrameNum: () => channel.frames(),
      toggleMainLoop: async (running) => {
        instance.paused = !running;
        await channel.request(running ? "RESUME" : "PAUSE", {}, 5_000);
      },
    },
  };
  return {
    instance,
    cleanup: async () => {
      channel.stopProbeLoop();
      await channel.request("CLEANUP", {}, 10_000).catch(() => undefined);
      channel.close();
      frame.src = "about:blank";
      listeners.clear();
    },
  };
}

class NativeChannel {
  private readonly config: NativeConfig;
  private readonly nonce = randomNonce();
  private readonly port = new MessageChannel();
  private connected = false;
  private closed = false;
  private lastRequestId = 0;
  private pending: Pending | null = null;
  private requestTail: Promise<void> = Promise.resolve();
  private readyValue: { engine: string; engineProfile: string; position: RpgPosition } | null = null;
  private readyWaiter: Pending | null = null;
  private lastPosition: RpgPosition | null = null;
  private frameCount = 0;
  private available = false;
  private probeTimer: number | null = null;
  private probeActive = false;

  constructor(config: NativeConfig) {
    this.config = config;
    this.port.port1.onmessage = (event) => this.receive(event.data);
    this.port.port1.start();
  }

  connect(target: Window) {
    if (this.connected) {throw new Error("RPG_NATIVE_PROTOCOL_INVALID");}
    this.connected = true;
    target.postMessage({
      type: "RETROM_RPG_NATIVE_CONNECT", protocolVersion,
      launchId: this.config.launchId, nonce: this.nonce, parentOrigin: window.location.origin,
      profile: this.config.adapter.bridgeProfile,
    }, this.config.adapter.uniqueOrigin, [this.port.port2]);
  }

  ready() {
    if (this.readyValue) {return Promise.resolve(this.readyValue);}
    return new Promise<{ engine: string; engineProfile: string; position: RpgPosition }>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.readyWaiter = null;
        reject(new Error("RPG_RUNTIME_TIMEOUT"));
      }, channelReadyTimeoutMs);
      this.readyWaiter = { resolve: (reply) => resolve(readReady(reply.body)), reject, timer };
    });
  }

  request(type: string, body: Record<string, unknown>, timeout = 30000): Promise<Reply> {
    if (!this.connected || this.closed) {return Promise.reject(new Error("RPG_NATIVE_CHANNEL_CLOSED"));}
    const operation = this.requestTail.then(() => this.sendRequest(type, body, timeout));
    this.requestTail = operation.then(() => undefined, () => undefined);
    return operation;
  }

  private sendRequest(type: string, body: Record<string, unknown>, timeout: number): Promise<Reply> {
    if (this.closed) {return Promise.reject(new Error("RPG_NATIVE_CHANNEL_CLOSED"));}
    if (this.pending) {return Promise.reject(new Error("RPG_NATIVE_PROTOCOL_INVALID"));}
    const requestId = ++this.lastRequestId;
    const message = this.envelope(requestId, type, body);
    if (new TextEncoder().encode(JSON.stringify(message)).byteLength > maximumControlBytes) {
      return Promise.reject(new Error("RPG_NATIVE_CONTROL_TOO_LARGE"));
    }
    return new Promise<Reply>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pending = null;
        reject(new Error("RPG_NATIVE_REQUEST_TIMEOUT"));
      }, timeout);
      this.pending = { resolve, reject, timer };
      this.port.port1.postMessage(message, transferables(body));
    });
  }

  async save(expectedEngine: string) {
    const reply = await this.request("SAVE", {});
    if (reply.type !== "SAVE_RESULT") {throw new Error("RPG_CHECKPOINT_CREATE_FAILED");}
    const bundle = readBundle(reply.body.bundle, expectedEngine);
    this.updatePosition(reply.body.position);
    return encodeRpgCheckpoint(bundle);
  }

  async restore(bundle: Awaited<ReturnType<typeof decodeRpgCheckpoint>>) {
    const body = {
      bundle: {
        engine: bundle.engine, resumeSlot: bundle.resumeSlot,
        entries: bundle.entries.map((entry) => ({
          store: entry.store, key: entry.key, mediaType: entry.mediaType, data: entry.data.slice().buffer,
        })),
      },
    };
    const reply = await this.request("RESTORE", body, 30000);
    if (reply.type !== "RESTORE_RESULT") {throw new Error("RPG_CHECKPOINT_RESTORE_FAILED");}
    this.updatePosition(reply.body.position);
  }

  async screenshot() {
    const reply = await this.request("SCREENSHOT", {});
    const data = reply.body.data;
    const mediaType = reply.body.mediaType;
    if (reply.type !== "SCREENSHOT_RESULT" || !(data instanceof ArrayBuffer) || !data.byteLength ||
      data.byteLength > maximumScreenshotBytes || mediaType !== "image/png") {
      throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");
    }
    return new Blob([data], { type: mediaType });
  }

  position() {
    if (!this.lastPosition) {throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");}
    return { ...this.lastPosition };
  }

  frames() { return this.frameCount; }
  checkpointAvailable() { return this.available; }

  startProbeLoop() {
    this.probeActive = true;
    const probe = async () => {
      try {
        const reply = await this.request("PROBE", {}, 5000);
        if (reply.type === "PROBE_RESULT") {
          this.available = reply.body.ready === true;
          this.updateFrames(reply.body.frameCount);
          this.updatePosition(reply.body.position);
        }
      } catch {
        this.available = false;
      }
      if (this.probeActive) {
        this.probeTimer = window.setTimeout(() => { void probe(); }, 500);
      }
    };
    void probe();
  }

  stopProbeLoop() {
    this.probeActive = false;
    if (this.probeTimer !== null) {window.clearTimeout(this.probeTimer);}
    this.probeTimer = null;
  }

  close() {
    this.closed = true;
    this.stopProbeLoop();
    if (this.pending) {
      window.clearTimeout(this.pending.timer);
      this.pending.reject(new Error("RPG_NATIVE_CHANNEL_CLOSED"));
      this.pending = null;
    }
    if (this.readyWaiter) {
      window.clearTimeout(this.readyWaiter.timer);
      this.readyWaiter.reject(new Error("RPG_NATIVE_CHANNEL_CLOSED"));
      this.readyWaiter = null;
    }
    this.port.port1.close();
  }

  private envelope(requestId: number, type: string, body: Record<string, unknown>) {
    return { protocolVersion, launchId: this.config.launchId, nonce: this.nonce, requestId, type, body };
  }

  private receive(value: unknown) {
    const reply = readReply(value, this.config.launchId, this.nonce);
    if (!reply) {return;}
    if (reply.requestId === 0) { this.receiveEvent(reply); return; }
    if (!this.pending || reply.requestId !== this.lastRequestId) {return;}
    const pending = this.pending;
    this.pending = null;
    window.clearTimeout(pending.timer);
    if (reply.type === "ERROR") {pending.reject(new Error(typeof reply.body.code === "string" ? reply.body.code : "RPG_NATIVE_RUNTIME_FAILED"));}
    else {pending.resolve(reply);}
  }

  private receiveEvent(reply: Reply) {
    if (reply.type === "READY") {
      const ready = readReady(reply.body);
      this.readyValue = ready;
      this.lastPosition = ready.position;
      this.available = true;
      if (this.readyWaiter) {
        const waiter = this.readyWaiter;
        this.readyWaiter = null;
        window.clearTimeout(waiter.timer);
        waiter.resolve(reply);
      }
    } else if (reply.type === "FRAMES") {
      this.updateFrames(reply.body.continuousFrames);
    }
  }

  private updateFrames(value: unknown) {
    if (Number.isSafeInteger(value) && Number(value) >= 0) {this.frameCount = Number(value);}
  }

  private updatePosition(value: unknown) {
    this.lastPosition = readPosition(value);
  }
}

async function bootstrapNativeFrame(config: NativeConfig, frame: HTMLIFrameElement, channel: NativeChannel) {
  const target = frame.contentWindow;
  if (!target) {throw new Error("PLAYER_FRAME_UNAVAILABLE");}
  const runtimeWindow: Window = target;
  let bootstrapTicket = config.adapter.bootstrapTicket;
  config.adapter.bootstrapTicket = "";
  await new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(() => finish(new Error("RPG_NATIVE_BOOTSTRAP_TIMEOUT")), bootstrapTimeoutMs);
    const ticketTimer = window.setTimeout(() => {bootstrapTicket = "";}, 60_000);
    let stage: NativeBootstrapStage = "BOOTSTRAP";
    function finish(error?: Error) {
      window.clearTimeout(timer);
      window.clearTimeout(ticketTimer);
      bootstrapTicket = "";
      window.removeEventListener("message", receive, true);
      if (error) {reject(error);} else {resolve();}
    }
    function receive(event: MessageEvent) {
      if (event.source !== runtimeWindow || event.origin !== config.adapter.uniqueOrigin || !event.data || typeof event.data !== "object") {return;}
      const action = nativeBootstrapAction(stage, event.data);
      if (action === "SEND_TICKET") {
        stage = "BRIDGE";
        if (!bootstrapTicket) {finish(new Error("RPG_NATIVE_BOOTSTRAP_TIMEOUT")); return;}
        runtimeWindow.postMessage({ type: "RETROM_RPG_NATIVE_BOOTSTRAP", protocolVersion, ticket: bootstrapTicket }, config.adapter.uniqueOrigin);
        bootstrapTicket = "";
      } else if (action === "CONNECT") {
        channel.connect(runtimeWindow);
        finish();
      }
    }
    window.addEventListener("message", receive, true);
    frame.src = config.adapter.bootstrapUrl;
  });
}

export function nativeBootstrapAction(stage: NativeBootstrapStage, value: unknown): NativeBootstrapAction {
  if (!value || typeof value !== "object" || Array.isArray(value) ||
    Object.keys(value).sort().join(",") !== "protocolVersion,type") {return "IGNORE";}
  const message = value as { protocolVersion?: unknown; type?: unknown };
  if (message.protocolVersion !== protocolVersion) {return "IGNORE";}
  if (message.type === "RETROM_RPG_NATIVE_BRIDGE_READY") {return "CONNECT";}
  return stage === "BOOTSTRAP" && message.type === "RETROM_RPG_NATIVE_BOOTSTRAP_READY"
    ? "SEND_TICKET" : "IGNORE";
}

function readReply(value: unknown, launchId: string, nonce: string): Reply | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {return null;}
  const reply = value as Partial<Reply>;
  const keys = Object.keys(value).sort().join(",");
  return keys === "body,launchId,nonce,protocolVersion,requestId,type" && reply.protocolVersion === protocolVersion &&
    reply.launchId === launchId && reply.nonce === nonce && Number.isSafeInteger(reply.requestId) && Number(reply.requestId) >= 0 &&
    typeof reply.type === "string" && reply.body && typeof reply.body === "object" && !Array.isArray(reply.body)
    ? reply as Reply : null;
}

function readReady(body: Record<string, unknown>) {
  if (typeof body.engine !== "string" || typeof body.engineProfile !== "string") {throw new Error("RPG_NATIVE_PROTOCOL_INVALID");}
  return { engine: body.engine, engineProfile: body.engineProfile, position: readPosition(body.position) };
}

function readPosition(value: unknown): RpgPosition {
  if (!value || typeof value !== "object" || Array.isArray(value)) {throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");}
  const position = value as Partial<RpgPosition>;
  const valid = [position.mapId, position.playerX, position.playerY, position.fixtureState].every((item) =>
    Number.isSafeInteger(item) && Number(item) >= -2147483648 && Number(item) <= 2147483647);
  if (!valid || Number(position.mapId) < 0) {throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");}
  return position as RpgPosition;
}

function readBundle(value: unknown, engine: string): RpgCheckpointBundle {
  if (!value || typeof value !== "object" || Array.isArray(value)) {throw new Error("RPG_CHECKPOINT_CREATE_FAILED");}
  const bundle = value as { engine?: unknown; resumeSlot?: unknown; entries?: unknown };
  if (bundle.engine !== engine || !Number.isSafeInteger(bundle.resumeSlot) || Number(bundle.resumeSlot) < 1 ||
    !Array.isArray(bundle.entries) || !bundle.entries.length || bundle.entries.length > 2) {throw new Error("RPG_CHECKPOINT_CREATE_FAILED");}
  const entries = bundle.entries.map((raw) => {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) {throw new Error("RPG_CHECKPOINT_CREATE_FAILED");}
    const entry = raw as { store?: unknown; key?: unknown; mediaType?: unknown; data?: unknown };
    if ((entry.store !== "LOCAL_STORAGE" && entry.store !== "LOCALFORAGE") || typeof entry.key !== "string" ||
      typeof entry.mediaType !== "string" || !(entry.data instanceof ArrayBuffer) || !entry.data.byteLength ||
      entry.data.byteLength > maximumCheckpointBytes) {throw new Error("RPG_CHECKPOINT_CREATE_FAILED");}
    const store: RpgCheckpointStore = entry.store;
    if (store !== "LOCAL_STORAGE" && store !== "LOCALFORAGE") {throw new Error("RPG_CHECKPOINT_CREATE_FAILED");}
    return { store, key: entry.key, mediaType: "application/octet-stream" as const, data: new Uint8Array(entry.data) };
  });
  return { engine: engine as "RPGMV" | "RPGMZ", resumeSlot: Number(bundle.resumeSlot), entries };
}

function transferables(value: unknown): Transferable[] {
  const result: Transferable[] = [];
  if (value instanceof ArrayBuffer) {result.push(value);}
  else if (Array.isArray(value)) {value.forEach((item) => result.push(...transferables(item)));}
  else if (value && typeof value === "object") {Object.values(value).forEach((item) => result.push(...transferables(item)));}
  return result;
}

function randomNonce() {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  let binary = "";
  bytes.forEach((value) => { binary += String.fromCharCode(value); });
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/u, "");
}
