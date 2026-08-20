import { EJSNetplayFrameBridge, checkpointCoreStateBytes, coreStateBytes, digestHex } from "./ejs-netplay-4.2.3-v1";
import { decodeServerMessage, decodeStateFrame, encodeStateFrame, type ServerMessage } from "./protocol";
import { RollbackTimeline, predictInputs, type CanonicalInput } from "./rollback";

export type NetplayProfile = {
  schemaVersion: 1;
  protocolVersion: "retrom-netplay-v1";
  profileId: string;
  emulatorjsVersion: "4.2.3";
  playerAdapterId: "ejs-4.2.3-v2";
  netplayAdapterId: "ejs-netplay-4.2.3-v1";
  coreArtifactId: string;
  coreArtifactSha256: string;
  gameVariantRevisionId: string;
  sourceManifestDigest: string;
  dependencySnapshotDigest: string;
  defaultCoreOptions: Record<string, string>;
  controlCount: 24;
  maxPlayers: number;
  maxPredictionFrames: number;
  maxRollbackFrames: 120;
  checkpointEveryFrames: 120;
  canonicalHistoryFrames: 600;
  maxStateBytes: 1048576;
};

export type NetplayLaunchConfig = {
  roomId: string;
  sessionId: string;
  playerNo: number;
  netplayProfile: NetplayProfile;
  runtimeSocketUrl: string;
};

type PendingState = { transferId: string; nextFrame: number; byteLength: number; stateSha256: string; coreSha256: string };

export type NetplayDiagnostics = {
  perturbInitialState?: boolean;
  delayForMessage?: (type: string, fields: Record<string, unknown>) => number;
  onConnect?: (reconnect: boolean) => void;
  onStateCapture?: (evidence: { byteLength: number; stateDigest: string; coreDigest: string }) => void;
  onStateLoad?: (evidence: {
    byteLength: number; stateDigest: string; coreDigest: string;
    changed: boolean; nativeCompletion: boolean; byteExact: boolean; coreExact: boolean;
    expectedCoreBytes: number; recapturedCoreBytes: number; firstCoreMismatch: number;
  }) => void;
  onEpoch?: (evidence: { epoch: number; nextFrame: number; resync: boolean }) => void;
  onCanonical?: (evidence: { frame: number; predictionFrames: number }) => void;
  onRollback?: (evidence: { frame: number; through: number; depth: number }) => void;
  onLockstep?: (evidence: { frame: number; inputBufferFrames: number; roundTripMS: number | null }) => void;
  onFrameStep?: (evidence: { frame: number; phase: "STARTED" | "COMPLETED" }) => void;
  onRetained?: (evidence: { states: number; predicted: number; canonical: number; stateBytes: number }) => void;
  onCheckpoint?: (evidence: { frame: number; coreDigest: string }) => void;
  onEnded?: (reason: string) => void;
};

type NetplayDiagnosticControls = {
  dropConnection: (durationMs: number) => void;
};

declare global {
  interface Window {
    __RETROM_NETPLAY_DIAGNOSTICS_FACTORY__?: (controls: NetplayDiagnosticControls) => NetplayDiagnostics;
  }
}

// Strict lockstep may submit controls ahead to cover network RTT, but never
// executes those frames until the server returns their canonical inputs.
// Keep a low-latency connection at one queued frame and add only the frames
// needed to cover the measured input-to-canonical round trip.
const lockstepFrameDurationMS = 1_000 / 60;
const lockstepInputBufferSafetyMS = 4;
const lockstepMaxInputBufferFrames = 8;
const initialReconnectLeaseMS = 10_000;
const initialSocketOpenTimeoutMS = 5_000;
const reconnectSocketOpenTimeoutMS = 2_000;

const terminalReasons = new Set([
  "ROLLBACK_WINDOW_EXCEEDED", "STATE_RING_CAPACITY_EXCEEDED", "STATE_INVALID",
  "NETPLAY_UNSTABLE", "INTERNAL_ERROR", "PROTOCOL_VIOLATION",
]);

function inputBufferFramesForRoundTrip(roundTripMS: number) {
  return Math.max(1, Math.min(
    lockstepMaxInputBufferFrames,
    Math.ceil((roundTripMS + lockstepInputBufferSafetyMS) / lockstepFrameDurationMS),
  ));
}

export class NetplayController {
  private socket: WebSocket | null = null;
  private clientSeq = 0;
  private serverSeq = 0;
  private epoch = 0;
  private nextFrame = 0;
  private occupiedMask = 0;
  private lastInput: CanonicalInput | null = null;
  private readonly timeline: RollbackTimeline;
  private work = Promise.resolve();
  private pendingState: PendingState | null = null;
  private advancing = false;
  private stopped = false;
  private hasRun = false;
  private connectedOnce = false;
  private epochRunning = false;
  private connecting = false;
  private reconnectDeadline = 0;
  private reconnectAttempt = 0;
  private reconnectTimer: number | null = null;
  private reconnectDeadlineTimer: number | null = null;
  private lastCanonicalFrame = -1;
  private lockstepInputThrough = -1;
  private lockstepInputBufferFrames = 1;
  private lockstepRoundTripMS: number | null = null;
  private readonly lockstepInputSentAtMS = new Map<number, { sentAtMS: number; leadFrames: number }>();
  private lockstepLowerTargetSamples = 0;
  private resumeBlocked = false;
  private sendNotBeforeMS = 0;
  private readonly sendQueue: Array<{ socket: WebSocket; payload: string | Uint8Array; sendAtMS: number }> = [];
  private sendTimer: number | null = null;
  private socketGeneration = 0;
  private leaseMS = initialReconnectLeaseMS;
  private openTimer: number | null = null;
  private endingTimer: number | null = null;
  private terminalRequested = false;
  private terminalReason = "INTERNAL_ERROR";
  private finalized = false;
  private statusKey = "";
  private statusTone: "synced" | "busy" | "warning" | null = null;
  private waitingStatusTimer: number | null = null;
  private readonly pendingCheckpoints = new Set<number>();
  private readonly lockstepCheckpointStates = new Map<number, Uint8Array>();
  private flushingCheckpoints = false;
  private checkpointFlushRequested = false;
  private readonly diagnostics?: NetplayDiagnostics;
  private readonly resumeIfVisible = () => {
    if (document.visibilityState === "hidden") return;
    this.resumeBlocked = false;
    if (this.connectedOnce && this.socket?.readyState !== WebSocket.OPEN) this.scheduleReconnect();
  };

  constructor(
    private readonly config: NetplayLaunchConfig,
    private profileDigest: string,
    private readonly bridge: EJSNetplayFrameBridge,
    private readonly callbacks: {
      onStatus: (text: string, tone: "synced" | "busy" | "warning") => void;
      onRunning: () => void;
      onPaused: () => void;
      onEnded: (reason: string) => void;
    },
    diagnostics?: NetplayDiagnostics,
  ) {
    this.diagnostics = diagnostics;
    if (process.env.NODE_ENV !== "production" && !this.diagnostics) {
      this.diagnostics = window.__RETROM_NETPLAY_DIAGNOSTICS_FACTORY__?.({
        dropConnection: (durationMs) => this.dropConnectionForTest(durationMs),
      });
    }
    this.timeline = new RollbackTimeline(
      this.config.netplayProfile.maxRollbackFrames,
      undefined,
      this.config.netplayProfile.maxPredictionFrames,
    );
  }

  setProfileDigest(value: string) {
    if (this.socket || !/^[0-9a-f]{64}$/.test(value)) throw new Error("PLAYER_NETPLAY_CONFIG_INVALID");
    this.profileDigest = value;
  }

  async start() {
    document.addEventListener("visibilitychange", this.resumeIfVisible);
    await this.bridge.pauseAtBoundary();
    if (process.env.NODE_ENV !== "production" && this.diagnostics?.perturbInitialState) {
      const input = predictInputs(null, this.config.playerNo, this.bridge.sampleLocalControls());
      await this.runFrame(input, true, -1);
      await this.bridge.pauseAtBoundary();
    }
    try {
      await this.connect();
    } catch {
      this.handleTransportLoss();
    }
    this.publishStatus("synchronizing", "正在同步初始状态…", "busy");
  }

  private async connect() {
    if (this.stopped || this.connecting) throw new Error("NETPLAY_SOCKET_UNAVAILABLE");
    this.connecting = true;
    this.resetSendQueue();
    const generation = ++this.socketGeneration;
    const endpoint = new URL(this.config.runtimeSocketUrl, window.location.href);
    endpoint.protocol = endpoint.protocol === "https:" ? "wss:" : "ws:";
    let socket: WebSocket;
    try {
      socket = new WebSocket(endpoint, "retrom.netplay.v1");
    } catch (error) {
      this.connecting = false;
      throw error;
    }
    socket.binaryType = "arraybuffer";
    this.socket = socket;
    try {
      await new Promise<void>((resolve, reject) => {
        const settle = (callback: () => void) => {
          if (this.openTimer !== null) window.clearTimeout(this.openTimer);
          this.openTimer = null;
          callback();
        };
        const remaining = this.reconnectDeadline === 0 ? initialSocketOpenTimeoutMS : this.reconnectDeadline - performance.now();
        const timeoutMS = Math.max(0, Math.min(this.connectedOnce ? reconnectSocketOpenTimeoutMS : initialSocketOpenTimeoutMS, remaining));
        socket.addEventListener("open", () => settle(resolve), { once: true });
        socket.addEventListener("error", () => settle(() => reject(new Error("NETPLAY_SOCKET_UNAVAILABLE"))), { once: true });
        socket.addEventListener("close", () => settle(() => reject(new Error("NETPLAY_SOCKET_UNAVAILABLE"))), { once: true });
        this.openTimer = window.setTimeout(() => {
          this.openTimer = null;
          socket.close(4000, "open timeout");
          reject(new Error("NETPLAY_SOCKET_UNAVAILABLE"));
        }, timeoutMS);
      });
    } finally {
      this.connecting = false;
    }
    if (this.stopped || generation !== this.socketGeneration || socket !== this.socket) {
      socket.close(1000, "stale connection");
      return;
    }
    const reconnect = this.connectedOnce;
    this.connectedOnce = true;
    this.clientSeq = 0;
    socket.addEventListener("message", (event) => {
      const receivedAtMS = performance.now();
      this.work = this.work
        .then(() => this.receive(event.data, receivedAtMS, generation, socket))
        .catch((error: unknown) => this.handleFailure(error));
    });
    socket.addEventListener("close", (event) => {
      if (this.stopped || this.socket !== socket || generation !== this.socketGeneration) return;
      if (event.reason === "connection replaced") {
        this.publishStatus("connection-replaced", "联机已由同一账户的另一页面接管", "warning");
        this.finalizeEnded("CONNECTION_REPLACED", false);
        return;
      }
      this.handleTransportLoss();
    });
    socket.addEventListener("error", () => {
      if (generation === this.socketGeneration && socket === this.socket) this.handleTransportLoss();
    });
    this.diagnostics?.onConnect?.(reconnect);
    socket.send(JSON.stringify({
      v: 1, type: "HELLO", sessionId: this.config.sessionId, epoch: this.epoch, seq: 0,
      protocolVersion: this.config.netplayProfile.protocolVersion, profileDigest: this.profileDigest,
      playerNo: this.config.playerNo, credentialGeneration: 1,
      lastCanonicalFrame: this.lastCanonicalFrame, lastServerSeq: this.serverSeq,
    }));
    if (this.terminalRequested) {
      this.send("END_REQUEST", { reason: this.terminalReason });
    } else if (!this.hasRun) {
      this.send("RUNTIME_READY", {
        adapterId: this.config.netplayProfile.netplayAdapterId,
        coreArtifactId: this.config.netplayProfile.coreArtifactId,
      });
    }
  }

  private scheduleReconnect() {
    if (this.stopped || this.reconnectTimer !== null || this.connecting) return;
    if (this.reconnectDeadline === 0) {
      this.reconnectDeadline = performance.now() + this.leaseMS;
      this.reconnectAttempt = 0;
      this.reconnectDeadlineTimer = window.setTimeout(() => {
        this.reconnectDeadlineTimer = null;
        this.finalizeEnded(this.terminalRequested ? this.terminalReason : "PEER_TIMEOUT");
      }, this.leaseMS);
    }
    const remainingMS = this.reconnectDeadline - performance.now();
    if (remainingMS <= 0) {
      this.finalizeEnded(this.terminalRequested ? this.terminalReason : "PEER_TIMEOUT");
      return;
    }
    this.publishStatus(this.terminalRequested ? "ending" : "reconnecting",
      this.terminalRequested ? "正在结束联机会话…" : "连接中断，正在恢复…", "warning");
    const delayMS = this.resumeBlocked
      ? 250
      : [0, 250, 500, 1_000][Math.min(this.reconnectAttempt, 3)]!;
    this.reconnectAttempt += 1;
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null;
      if (this.resumeBlocked) { this.scheduleReconnect(); return; }
      void this.connect().catch(() => this.scheduleReconnect());
    }, Math.min(delayMS, remainingMS));
  }

  private handleTransportLoss() {
    if (this.stopped) return;
    const socket = this.socket;
    if (socket) {
      // Invalidate the generation immediately. A message already queued by a
      // closed/erroring transport must not mutate the replacement connection.
      this.socketGeneration += 1;
      this.socket = null;
      if (socket.readyState === WebSocket.OPEN) socket.close(4000, "transport lost");
    }
    this.epochRunning = false;
    this.resetSendQueue();
    this.bridge.resetLocalControls();
    if (this.hasRun && !this.terminalRequested) this.callbacks.onPaused();
    this.scheduleReconnect();
  }

  private send(type: string, fields: Record<string, unknown> = {}) {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) throw new Error("NETPLAY_SOCKET_UNAVAILABLE");
    this.clientSeq += 1;
	const encoded = JSON.stringify({
		v: 1, type, sessionId: this.config.sessionId, epoch: this.epoch, seq: this.clientSeq, ...fields,
	});
	const delay = Math.max(0, this.diagnostics?.delayForMessage?.(type, fields) ?? 0);
	this.enqueueSend(this.socket, encoded, delay);
  }

  private enqueueSend(socket: WebSocket, payload: string | Uint8Array, delayMS: number) {
	const now = performance.now();
	const sendAt = Math.max(now + delayMS, this.sendNotBeforeMS);
	this.sendNotBeforeMS = sendAt;
	this.sendQueue.push({ socket, payload, sendAtMS: sendAt });
	this.pumpSendQueue();
  }

  private pumpSendQueue() {
	if (this.sendTimer !== null) return;
	while (this.sendQueue.length > 0) {
		const next = this.sendQueue[0]!;
		const waitMS = next.sendAtMS - performance.now();
		if (waitMS > 0) {
			this.sendTimer = window.setTimeout(() => {
				this.sendTimer = null;
				this.pumpSendQueue();
			}, waitMS);
			return;
		}
		this.sendQueue.shift();
		if (next.socket.readyState === WebSocket.OPEN && !this.stopped && next.socket === this.socket) next.socket.send(next.payload);
	}
  }

  private resetSendQueue() {
	if (this.sendTimer !== null) window.clearTimeout(this.sendTimer);
	this.sendTimer = null;
	this.sendQueue.length = 0;
	this.sendNotBeforeMS = 0;
  }

  private async receive(data: string | ArrayBuffer | Blob, receivedAtMS: number, generation: number, socket: WebSocket) {
    if (this.stopped || generation !== this.socketGeneration || socket !== this.socket) return;
    if (data instanceof ArrayBuffer) { await this.receiveState(new Uint8Array(data)); return; }
    if (data instanceof Blob) {
      const state = new Uint8Array(await data.arrayBuffer());
      if (this.stopped || generation !== this.socketGeneration || socket !== this.socket) return;
      await this.receiveState(state);
      return;
    }
    if (typeof data !== "string") throw new Error("PROTOCOL_VIOLATION");
    const message = decodeServerMessage(data);
    if (message.v !== 1 || message.sessionId !== this.config.sessionId || !Number.isSafeInteger(message.seq) || message.seq <= this.serverSeq) throw new Error("PROTOCOL_VIOLATION");
    this.serverSeq = message.seq;
    if (message.type !== "WELCOME" && message.epoch !== this.epoch && message.type !== "START_EPOCH") throw new Error("PROTOCOL_VIOLATION");
    switch (message.type) {
      case "WELCOME": {
        if (!Number.isSafeInteger(message.leaseMs) || message.leaseMs! < 1_000 || message.leaseMs! > 60_000) {
          throw new Error("PROTOCOL_VIOLATION");
        }
        this.leaseMS = message.leaseMs!;
        this.occupiedMask = message.occupiedSeatMask ?? 0;
        break;
      }
      case "REQUEST_STATE": await this.sendAuthorityState(message); break;
      case "STATE_META": this.acceptStateMeta(message); break;
      case "START_EPOCH": this.startEpoch(message); break;
      case "CANONICAL": await this.acceptCanonical(message, receivedAtMS); break;
      case "HISTORY": await this.acceptHistory(message, receivedAtMS); break;
      case "PAUSE": {
        await this.pauseAtCanonicalBoundary(message.atFrame);
        this.send("PAUSED");
        this.publishStatus("paused", message.affectedPlayerNo ? `等待 P${message.affectedPlayerNo} 重新连接` : "联机已暂停", "warning");
        this.callbacks.onPaused();
        break;
      }
		case "SESSION_ENDED": {
			const reason = message.reason ?? "NORMAL";
			this.finalizeEnded(reason);
			break;
		}
      default: break;
    }
  }

  private async sendAuthorityState(message: ServerMessage) {
    if (this.config.playerNo !== 1 || !message.transferId || !Number.isSafeInteger(message.nextFrame)) throw new Error("PROTOCOL_VIOLATION");
    const captured = this.bridge.captureState();
    const normalized = await this.bridge.loadStateForTransfer(captured);
    let state = normalized.recaptured;
    if (!normalized.coreExact) {
      const fixedPoint = await this.bridge.loadStateForTransfer(state);
      if (!fixedPoint.coreExact) throw new Error("STATE_INVALID");
      state = fixedPoint.recaptured;
    }
    if (state.byteLength > this.config.netplayProfile.maxStateBytes) throw new Error("STATE_RING_CAPACITY_EXCEEDED");
    const [stateSha256, coreSha256] = await Promise.all([digestHex(state), digestHex(coreStateBytes(state))]);
    this.diagnostics?.onStateCapture?.({ byteLength: state.byteLength, stateDigest: stateSha256, coreDigest: coreSha256 });
    this.send("STATE_META", { transferId: message.transferId, nextFrame: message.nextFrame, byteLength: state.byteLength, stateSha256, coreSha256 });
    if (!this.socket) throw new Error("NETPLAY_SOCKET_UNAVAILABLE");
    this.enqueueSend(this.socket, encodeStateFrame(this.config.sessionId, message.transferId, this.epoch, message.nextFrame!, state), 0);
    this.send("STATE_READY", { transferId: message.transferId, stateSha256, coreSha256, recaptureMatched: equalBytes(state, this.bridge.captureState()) });
  }

  private acceptStateMeta(message: ServerMessage) {
    if (!message.transferId || !Number.isSafeInteger(message.nextFrame) || !Number.isSafeInteger(message.byteLength) || !message.stateSha256 || !message.coreSha256 || message.byteLength! < 1 || message.byteLength! > 1048576) throw new Error("PROTOCOL_VIOLATION");
    this.pendingState = { transferId: message.transferId, nextFrame: message.nextFrame!, byteLength: message.byteLength!, stateSha256: message.stateSha256, coreSha256: message.coreSha256 };
  }

  private async receiveState(frame: Uint8Array) {
    const pending = this.pendingState;
    if (!pending) throw new Error("PROTOCOL_VIOLATION");
    const decoded = decodeStateFrame(frame);
    if (decoded.sessionId !== this.config.sessionId || decoded.transferId !== pending.transferId || decoded.epoch !== this.epoch || decoded.nextFrame !== pending.nextFrame || decoded.state.byteLength !== pending.byteLength) throw new Error("PROTOCOL_VIOLATION");
    const [stateSha256, coreSha256] = await Promise.all([digestHex(decoded.state), digestHex(coreStateBytes(decoded.state))]);
    if (stateSha256 !== pending.stateSha256 || coreSha256 !== pending.coreSha256) throw new Error("STATE_INVALID");
	const beforeDigest = await digestHex(coreStateBytes(this.bridge.captureState()));
	const loadResult = await this.bridge.loadStateForTransfer(decoded.state);
	const recaptured = loadResult.recaptured;
	const recapturedDigest = await digestHex(coreStateBytes(recaptured));
	const coreExact = recapturedDigest === coreSha256;
	this.diagnostics?.onStateLoad?.({
		byteLength: decoded.state.byteLength, stateDigest: stateSha256, coreDigest: coreSha256,
		changed: beforeDigest !== recapturedDigest, nativeCompletion: true, byteExact: loadResult.byteExact, coreExact,
		expectedCoreBytes: loadResult.expectedCoreBytes, recapturedCoreBytes: loadResult.recapturedCoreBytes,
		firstCoreMismatch: loadResult.firstCoreMismatch,
	});
	if (!coreExact) throw new Error("STATE_INVALID");
    this.send("STATE_APPLIED", { transferId: pending.transferId, stateSha256, coreSha256, nativeLoadCompleted: true, recaptureMatched: coreExact });
    this.pendingState = null;
  }

  private startEpoch(message: ServerMessage) {
    if (!Number.isSafeInteger(message.epoch) || !Number.isSafeInteger(message.nextFrame) || message.epoch! < this.epoch) throw new Error("PROTOCOL_VIOLATION");
	if (this.terminalRequested) {
		this.epoch = message.epoch!;
		this.occupiedMask = message.occupiedSeatMask ?? this.occupiedMask;
		return;
	}
	const resync = this.hasRun;
	this.epoch = message.epoch!; this.nextFrame = message.nextFrame!; this.occupiedMask = message.occupiedSeatMask ?? this.occupiedMask;
    this.hasRun = true; this.epochRunning = true;
    this.reconnectDeadline = 0; this.reconnectAttempt = 0;
    if (this.reconnectDeadlineTimer !== null) window.clearTimeout(this.reconnectDeadlineTimer);
    this.reconnectDeadlineTimer = null;
    this.timeline.reset(this.nextFrame); this.pendingCheckpoints.clear(); this.lockstepCheckpointStates.clear();
    this.lastInput = null; this.lockstepInputThrough = this.nextFrame - 1;
    this.lockstepInputBufferFrames = 1; this.lockstepRoundTripMS = null; this.lockstepLowerTargetSamples = 0;
    this.lockstepInputSentAtMS.clear();
    this.bridge.resetLocalControls();
    this.publishStatus("stable", "网络稳定", "synced"); this.callbacks.onRunning();
	this.diagnostics?.onEpoch?.({ epoch: this.epoch, nextFrame: this.nextFrame, resync });
    this.requestAdvance();
  }

  private requestAdvance() {
    if (this.terminalRequested || this.stopped) return;
    if (this.config.netplayProfile.maxPredictionFrames === 0) {
      try { this.fillLockstepInputs(); } catch (error) { this.handleFailure(error); }
      return;
    }
    void this.advance().catch((error: unknown) => this.handleFailure(error));
  }

  private fillLockstepInputs() {
    if (this.stopped || this.terminalRequested || !this.epochRunning || this.socket?.readyState !== WebSocket.OPEN) return;
    if (this.lockstepInputThrough < this.nextFrame - 1) throw new Error("NETPLAY_HISTORY_GAP");
    const targetFrame = this.nextFrame + this.lockstepInputBufferFrames - 1;
    while (this.lockstepInputThrough < targetFrame) {
      const frame = this.lockstepInputThrough + 1;
      const local = this.bridge.sampleLocalControls();
      this.send("INPUT", { frame, playerNo: this.config.playerNo, controls: local });
      this.lockstepInputSentAtMS.set(frame, { sentAtMS: performance.now(), leadFrames: frame - this.nextFrame });
      this.lockstepInputThrough = frame;
    }
    this.scheduleWaitingStatus();
  }

  private async advance() {
    if (this.advancing || this.stopped || this.terminalRequested || !this.epochRunning || this.socket?.readyState !== WebSocket.OPEN || !this.timeline.canPredict(this.nextFrame)) return;
    this.advancing = true;
    try {
      while (!this.stopped && this.epochRunning && this.socket?.readyState === WebSocket.OPEN && this.timeline.canPredict(this.nextFrame)) {
        const frame = this.nextFrame;
        const state = this.bridge.captureState();
        this.timeline.recordOwnedStateBefore(frame, state);
        const local = this.bridge.sampleLocalControls();
        const input = predictInputs(this.lastInput, this.config.playerNo, local);
        this.send("INPUT", { frame, playerNo: this.config.playerNo, controls: local });
        this.timeline.recordPrediction(frame, input); this.lastInput = input;
        await this.runFrame(input, false, frame);
        this.nextFrame += 1;
        this.requestCheckpointFlush();
      }
      if (!this.timeline.canPredict(this.nextFrame)) this.scheduleWaitingStatus();
    } finally { this.advancing = false; }
  }

  private async acceptCanonical(message: ServerMessage, receivedAtMS: number) {
    if (!Number.isSafeInteger(message.frame) || !message.players || message.players.length !== 4) throw new Error("PROTOCOL_VIOLATION");
    const rollback = this.timeline.receiveCanonical(message.frame!, message.players);
    this.lastCanonicalFrame = Math.max(this.lastCanonicalFrame, message.frame!);
	this.diagnostics?.onCanonical?.({
		frame: message.frame!, predictionFrames: Math.max(0, this.nextFrame - message.frame! - 1),
    });
    if (this.config.netplayProfile.maxPredictionFrames === 0) {
      const advancesFrame = message.frame === this.nextFrame;
      if (message.frame! > this.nextFrame) throw new Error("NETPLAY_HISTORY_GAP");
      if (advancesFrame) {
        if (this.lockstepInputThrough < message.frame!) throw new Error("NETPLAY_HISTORY_GAP");
        this.updateLockstepInputBuffer(message.frame!, receivedAtMS);
        this.diagnostics?.onLockstep?.({
          frame: message.frame!, inputBufferFrames: this.lockstepInputBufferFrames,
          roundTripMS: this.lockstepRoundTripMS,
        });
        this.lastInput = message.players;
        this.nextFrame = message.frame! + 1;
        this.fillLockstepInputs();
        await this.runFrame(message.players, false, message.frame!);
        this.publishStatus("stable", "网络稳定", "synced");
      }
      if (advancesFrame && (message.frame! + 1) % this.config.netplayProfile.checkpointEveryFrames === 0) {
        this.lockstepCheckpointStates.set(message.frame!, this.bridge.captureState());
        this.pendingCheckpoints.add(message.frame!);
        this.requestCheckpointFlush();
      }
      this.requestAdvance();
      return;
    }
    if (rollback !== null) {
		await this.pauseAdvancement();
		if (this.stopped) return;
		if (rollback >= this.nextFrame) throw new Error("NETPLAY_FRAME_STEP_TIMEOUT");
      const through = this.nextFrame - 1; const plan = this.timeline.rollbackPlan(rollback, through);
		this.diagnostics?.onRollback?.({ frame: rollback, through, depth: through - rollback + 1 });
      await this.bridge.loadStateAndWait(plan.state);
      for (const item of plan.frames) {
        this.timeline.recordOwnedStateBefore(item.frame, this.bridge.captureState());
        await this.runFrame(item.input, true, item.frame);
      }
      this.lastInput = plan.frames.at(-1)?.input ?? this.lastInput;
		this.epochRunning = true;
		this.requestCheckpointFlush();
      this.publishStatus("rollback", `已同步 · 回滚 ${through - rollback + 1} 帧`, "synced");
    } else {
      this.publishStatus("stable", "网络稳定", "synced");
    }
    if ((message.frame! + 1) % this.config.netplayProfile.checkpointEveryFrames === 0) {
      this.pendingCheckpoints.add(message.frame!);
      this.requestCheckpointFlush();
    }
    this.requestAdvance();
    this.diagnostics?.onRetained?.(this.timeline.retained());
  }

  private async runFrame(input: CanonicalInput, suppressOutput: boolean, frame: number) {
    this.diagnostics?.onFrameStep?.({ frame, phase: "STARTED" });
    await this.bridge.runNetplayFrame(input, suppressOutput);
    this.diagnostics?.onFrameStep?.({ frame, phase: "COMPLETED" });
  }

  private updateLockstepInputBuffer(frame: number, receivedAtMS: number) {
    const sent = this.lockstepInputSentAtMS.get(frame);
    this.lockstepInputSentAtMS.delete(frame);
    if (sent === undefined) return;
    // Inputs intentionally submitted ahead spend leadFrames in the local
    // lockstep pipeline. Remove that residence time so a larger buffer can
    // shrink again after the transport RTT recovers.
    const sampleMS = Math.max(0, receivedAtMS - sent.sentAtMS - sent.leadFrames * lockstepFrameDurationMS);
    this.lockstepRoundTripMS = this.lockstepRoundTripMS === null
      ? sampleMS
      : this.lockstepRoundTripMS * 0.75 + sampleMS * 0.25;
    const target = inputBufferFramesForRoundTrip(this.lockstepRoundTripMS);
    if (target > this.lockstepInputBufferFrames) {
      this.lockstepInputBufferFrames = target;
      this.lockstepLowerTargetSamples = 0;
    } else if (target === this.lockstepInputBufferFrames) {
      this.lockstepLowerTargetSamples = 0;
    } else {
      this.lockstepLowerTargetSamples += 1;
      if (this.lockstepLowerTargetSamples >= 120) {
        this.lockstepInputBufferFrames -= 1;
        this.lockstepLowerTargetSamples = 0;
      }
    }
  }

  private async pauseAdvancement() {
    this.epochRunning = false;
    while (this.advancing) await new Promise((resolve) => window.setTimeout(resolve, 0));
  }

  private async pauseAtCanonicalBoundary(atFrame: number | undefined) {
    if (!Number.isSafeInteger(atFrame) || atFrame! < -1) throw new Error("PROTOCOL_VIOLATION");
    await this.pauseAdvancement();
    if (this.stopped) return;
    const targetNextFrame = atFrame! + 1;
    if (this.nextFrame < targetNextFrame) throw new Error("NETPLAY_HISTORY_GAP");
    if (this.config.netplayProfile.maxPredictionFrames === 0) {
      if (this.nextFrame !== targetNextFrame) throw new Error("NETPLAY_HISTORY_GAP");
      return;
    }
    if (atFrame === -1) {
      const initialState = this.timeline.stateAt(0);
      if (!initialState) throw new Error("ROLLBACK_WINDOW_EXCEEDED");
      await this.bridge.loadStateAndWait(initialState);
    } else {
      const stateBefore = this.timeline.stateAt(atFrame!);
      const canonical = this.timeline.canonicalAt(atFrame!);
      if (!stateBefore || !canonical) throw new Error("ROLLBACK_WINDOW_EXCEEDED");
      // Native state load restores the core framebuffer but EmulatorJS does not
      // repaint its HTML canvas. Replaying this exact canonical frame restores
      // both the logical boundary and the visible paused frame on every peer.
      await this.bridge.loadStateAndWait(stateBefore);
      await this.bridge.runNetplayFrame(canonical);
    }
    this.nextFrame = targetNextFrame;
  }

  private requestCheckpointFlush() {
    this.checkpointFlushRequested = true;
    if (!this.flushingCheckpoints) void this.flushCheckpoints();
  }

  private async flushCheckpoints() {
    if (this.flushingCheckpoints) return;
    this.flushingCheckpoints = true;
    try {
      do {
        this.checkpointFlushRequested = false;
        for (const frame of [...this.pendingCheckpoints].sort((left, right) => left - right)) {
          const lockstepState = this.lockstepCheckpointStates.get(frame);
          const after = lockstepState ?? this.timeline.stateAt(frame + 1);
          if (!after) continue;
          this.pendingCheckpoints.delete(frame);
          this.lockstepCheckpointStates.delete(frame);
          const core = coreStateBytes(after);
          const checkpointCore = checkpointCoreStateBytes(core, this.config.netplayProfile.profileId);
          const coreDigest = await digestHex(checkpointCore);
          this.diagnostics?.onCheckpoint?.({ frame, coreDigest });
          this.send("HASH", { frame, coreDigest });
        }
      } while (this.checkpointFlushRequested);
    } catch (error) {
      this.handleFailure(error);
    } finally {
      this.flushingCheckpoints = false;
    }
  }

  private async acceptHistory(message: ServerMessage, receivedAtMS: number) {
    for (const frame of message.canonical ?? []) await this.acceptCanonical({ ...message, type: "CANONICAL", frame: frame.frame, occupiedSeatMask: frame.occupiedSeatMask, players: frame.players }, receivedAtMS);
    this.send("HISTORY_APPLIED", { historyAppliedThrough: message.toFrame ?? -1 });
  }

  handleFocusLoss() {
    if (this.stopped) return;
    this.bridge.resetLocalControls();
  }

  end() {
    this.requestTerminalEnd(new Error("USER_EXIT"));
  }

  dispose() {
    this.finalizeEnded("CLIENT_DISPOSED", false);
  }

  private requestTerminalEnd(error: unknown) {
    if (this.stopped || this.terminalRequested) return;
    this.terminalRequested = true;
    this.terminalReason = terminalReason(error);
    this.epochRunning = false;
    this.bridge.resetLocalControls();
    this.resetSendQueue();
    this.publishStatus("ending", this.terminalReason === "USER_EXIT" ? "正在结束联机会话…" : "联机运行异常，正在安全结束…", "warning");
    if (this.reconnectDeadline === 0) this.reconnectDeadline = performance.now() + this.leaseMS;
    const remainingMS = Math.max(0, this.reconnectDeadline - performance.now());
    this.endingTimer = window.setTimeout(() => this.finalizeEnded(this.terminalReason), remainingMS);
    try {
      this.send("END_REQUEST", { reason: this.terminalReason });
    } catch {
      this.handleTransportLoss();
    }
  }

  private finalizeEnded(reason: string, notify = true) {
    if (this.finalized) return;
    this.finalized = true;
    this.stopped = true;
    this.epochRunning = false;
    this.resetSendQueue();
    for (const timer of [this.reconnectTimer, this.reconnectDeadlineTimer, this.openTimer, this.endingTimer, this.waitingStatusTimer]) {
      if (timer !== null) window.clearTimeout(timer);
    }
    this.reconnectTimer = null;
    this.reconnectDeadlineTimer = null;
    this.openTimer = null;
    this.endingTimer = null;
    this.waitingStatusTimer = null;
    document.removeEventListener("visibilitychange", this.resumeIfVisible);
    this.socketGeneration += 1;
    this.socket?.close(1000, reason);
    this.socket = null;
    this.bridge.close();
    if (notify) {
      this.diagnostics?.onEnded?.(reason);
      this.callbacks.onEnded(reason);
    }
  }

  private handleFailure(error: unknown) {
    if (this.stopped) return;
    const message = error instanceof Error ? error.message : "INTERNAL_ERROR";
    if (message === "NETPLAY_SOCKET_UNAVAILABLE") {
      this.handleTransportLoss();
      return;
    }
    this.requestTerminalEnd(error);
  }

  private publishStatus(key: string, text: string, tone: "synced" | "busy" | "warning") {
    if (key !== "waiting-peer-input" && this.waitingStatusTimer !== null) {
      window.clearTimeout(this.waitingStatusTimer);
      this.waitingStatusTimer = null;
    }
    if (this.statusKey === key && this.statusTone === tone) return;
    this.statusKey = key;
    this.statusTone = tone;
    this.callbacks.onStatus(text, tone);
  }

  private scheduleWaitingStatus() {
    if (this.waitingStatusTimer !== null || this.statusKey === "waiting-peer-input" || this.stopped || this.terminalRequested) return;
    this.waitingStatusTimer = window.setTimeout(() => {
      this.waitingStatusTimer = null;
      if (this.epochRunning && !this.stopped && !this.terminalRequested) {
        this.publishStatus("waiting-peer-input", "等待其他玩家输入…", "busy");
      }
    }, 100);
  }

	private dropConnectionForTest(durationMs: number) {
		if (process.env.NODE_ENV === "production") return;
		this.resumeBlocked = true;
		this.socket?.close(1000, "TEST_DISCONNECT");
		window.setTimeout(() => {
			this.resumeBlocked = false;
			this.scheduleReconnect();
		}, Math.max(0, Math.min(durationMs, 15_000)));
	}

}

function equalBytes(left: Uint8Array, right: Uint8Array) { return left.byteLength === right.byteLength && left.every((value, index) => value === right[index]); }

function terminalReason(error: unknown) {
  const message = error instanceof Error ? error.message : "INTERNAL_ERROR";
  if (message === "USER_EXIT") return message;
  if (message === "STATE_LOAD_TIMEOUT" || message === "STATE_INVALID" || message.includes("RASTATE")) return "STATE_INVALID";
  if (terminalReasons.has(message)) return message;
  if (message === "NETPLAY_FRAME_STEP_TIMEOUT") return "INTERNAL_ERROR";
  if (message.startsWith("NETPLAY_") && ["NETPLAY_HISTORY_GAP", "NETPLAY_CANONICAL_INVALID", "NETPLAY_CANONICAL_MUTATED", "NETPLAY_INPUT_INVALID"].includes(message)) {
    return "PROTOCOL_VIOLATION";
  }
  return "INTERNAL_ERROR";
}
