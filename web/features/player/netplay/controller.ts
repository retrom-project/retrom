import type {NetplayRuntimePort} from "../runtime/netplay-port-adapter";
import { prepareAuthorityTransfer } from "./authority-state";
import { decodeServerMessage, encodeStateFrame, type ServerMessage } from "./protocol";
import { RollbackTimeline, predictInputs, type CanonicalInput } from "./rollback";
import { lockstepFrameDurationMS, terminalReason, updateLockstepBuffer, type NetplayDiagnostics, type NetplayLaunchConfig, type NetplayProfile } from "./controller-model";
import { NetplayCheckpointQueue } from "./checkpoint-queue";
import { OrderedSocketSender } from "./ordered-socket-sender";
import { applyTransferredState, pendingStateFromMessage, type PendingState } from "./state-transfer";
import { NetplayStatusPublisher } from "./status-publisher";
export type { NetplayDiagnostics, NetplayLaunchConfig, NetplayProfile };

// Strict lockstep may submit controls ahead to cover network RTT, but never
// executes those frames until the server returns their canonical inputs.
// Keep a low-latency connection at one queued frame and add only the frames
// needed to cover the measured input-to-canonical round trip.
const initialReconnectLeaseMS = 10_000;
const initialSocketOpenTimeoutMS = 5_000;
const reconnectSocketOpenTimeoutMS = 2_000;
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
  private readonly sender: OrderedSocketSender;
  private socketGeneration = 0;
  private leaseMS = initialReconnectLeaseMS;
  private openTimer: number | null = null;
  private endingTimer: number | null = null;
  private terminalRequested = false;
  private terminalReason = "INTERNAL_ERROR";
  private finalized = false;
  private readonly status: NetplayStatusPublisher;
  private readonly checkpoints: NetplayCheckpointQueue;
  private readonly diagnostics?: NetplayDiagnostics;
  private readonly resumeIfVisible = () => {
    if (document.visibilityState === "hidden") {return;}
    this.resumeBlocked = false;
    if (this.connectedOnce && this.socket?.readyState !== WebSocket.OPEN) {this.scheduleReconnect();}
  };

  constructor(
    private readonly config: NetplayLaunchConfig,
    private profileDigest: string,
    private readonly bridge: NetplayRuntimePort,
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
    this.checkpoints = new NetplayCheckpointQueue(
      this.config.netplayProfile, this.bridge, this.timeline, this.diagnostics,
      (frame, coreDigest) => this.send("HASH", { frame, coreDigest }),
      (error) => this.handleFailure(error),
    );
    this.sender = new OrderedSocketSender((socket) => socket.readyState === WebSocket.OPEN && !this.stopped && socket === this.socket);
    this.status = new NetplayStatusPublisher(this.callbacks.onStatus, () => this.epochRunning && !this.stopped && !this.terminalRequested);
  }

  setProfileDigest(value: string) {
    if (this.socket || !/^[0-9a-f]{64}$/.test(value)) {throw new Error("PLAYER_NETPLAY_CONFIG_INVALID");}
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
    this.status.publish("synchronizing", "正在同步初始状态…", "busy");
  }

  private async connect() {
    if (this.stopped || this.connecting) {throw new Error("NETPLAY_SOCKET_UNAVAILABLE");}
    this.connecting = true;
    this.sender.reset();
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
          if (this.openTimer !== null) {window.clearTimeout(this.openTimer);}
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
      if (this.stopped || this.socket !== socket || generation !== this.socketGeneration) {return;}
      if (event.reason === "connection replaced") {
        this.status.publish("connection-replaced", "联机已由同一账户的另一页面接管", "warning");
        this.finalizeEnded("CONNECTION_REPLACED", false);
        return;
      }
      this.handleTransportLoss();
    });
    socket.addEventListener("error", () => {
      if (generation === this.socketGeneration && socket === this.socket) {this.handleTransportLoss();}
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
        providerId: this.config.netplayProfile.providerId,
        targetId: this.config.netplayProfile.targetId,
        targetContractSha256: this.config.netplayProfile.targetContractSha256,
      });
    }
  }

  private scheduleReconnect() {
    if (this.stopped || this.reconnectTimer !== null || this.connecting) {return;}
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
    this.status.publish(this.terminalRequested ? "ending" : "reconnecting",
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
    if (this.stopped) {return;}
    const socket = this.socket;
    if (socket) {
      // Invalidate the generation immediately. A message already queued by a
      // closed/erroring transport must not mutate the replacement connection.
      this.socketGeneration += 1;
      this.socket = null;
      if (socket.readyState === WebSocket.OPEN) {socket.close(4000, "transport lost");}
    }
    this.epochRunning = false;
    this.sender.reset();
    this.bridge.resetLocalControls();
    if (this.hasRun && !this.terminalRequested) {this.callbacks.onPaused();}
    this.scheduleReconnect();
  }

  private send(type: string, fields: Record<string, unknown> = {}) {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {throw new Error("NETPLAY_SOCKET_UNAVAILABLE");}
    this.clientSeq += 1;
	const encoded = JSON.stringify({
		v: 1, type, sessionId: this.config.sessionId, epoch: this.epoch, seq: this.clientSeq, ...fields,
	});
	const delay = Math.max(0, this.diagnostics?.delayForMessage?.(type, fields) ?? 0);
	this.sender.enqueue(this.socket, encoded, delay);
  }

  private async receive(data: string | ArrayBuffer | Blob, receivedAtMS: number, generation: number, socket: WebSocket) {
    if (this.stopped || generation !== this.socketGeneration || socket !== this.socket) {return;}
    if (await this.receiveBinary(data, generation, socket)) {return;}
    if (typeof data !== "string") {throw new Error("PROTOCOL_VIOLATION");}
    const message = decodeServerMessage(data);
    this.validateServerMessage(message);
    this.serverSeq = message.seq;
    await this.acceptServerMessage(message, receivedAtMS);
  }

  private async receiveBinary(data: string | ArrayBuffer | Blob, generation: number, socket: WebSocket) {
    if (data instanceof ArrayBuffer) {await this.receiveState(new Uint8Array(data)); return true;}
    if (!(data instanceof Blob)) {return false;}
    const state = new Uint8Array(await data.arrayBuffer());
    if (this.stopped || generation !== this.socketGeneration || socket !== this.socket) {return true;}
    await this.receiveState(state);
    return true;
  }

  private validateServerMessage(message: ServerMessage) {
    if (message.v !== 1 || message.sessionId !== this.config.sessionId || !Number.isSafeInteger(message.seq) || message.seq <= this.serverSeq) {throw new Error("PROTOCOL_VIOLATION");}
    if (message.type !== "WELCOME" && message.epoch !== this.epoch && message.type !== "START_EPOCH") {throw new Error("PROTOCOL_VIOLATION");}
  }

  private async acceptServerMessage(message: ServerMessage, receivedAtMS: number) {
    switch (message.type) {
      case "WELCOME": this.acceptWelcome(message); break;
      case "REQUEST_STATE": await this.sendAuthorityState(message); break;
      case "STATE_META": this.acceptStateMeta(message); break;
      case "START_EPOCH": this.startEpoch(message); break;
      case "CANONICAL": await this.acceptCanonical(message, receivedAtMS); break;
      case "HISTORY": await this.acceptHistory(message, receivedAtMS); break;
      case "PAUSE": await this.acceptPause(message); break;
      case "SESSION_ENDED": this.finalizeEnded(message.reason ?? "NORMAL"); break;
      default: break;
    }
  }

  private acceptWelcome(message: ServerMessage) {
    if (!Number.isSafeInteger(message.leaseMs) || message.leaseMs! < 1_000 || message.leaseMs! > 60_000) {throw new Error("PROTOCOL_VIOLATION");}
    this.leaseMS = message.leaseMs!;
    this.occupiedMask = message.occupiedSeatMask ?? 0;
  }

  private async acceptPause(message: ServerMessage) {
    await this.pauseAtCanonicalBoundary(message.atFrame);
    this.diagnostics?.onPause?.({ epoch: this.epoch, reason: message.reason ?? "UNKNOWN", atFrame: message.atFrame! });
    this.send("PAUSED");
    this.status.publish("paused", message.affectedPlayerNo ? `等待 P${message.affectedPlayerNo} 重新连接` : "联机已暂停", "warning");
    this.callbacks.onPaused();
  }

  private async sendAuthorityState(message: ServerMessage) {
    if (this.config.playerNo !== 1 || !message.transferId || !Number.isSafeInteger(message.nextFrame)) {throw new Error("PROTOCOL_VIOLATION");}
    const profileID = this.config.netplayProfile.profileId;
    const transfer = await prepareAuthorityTransfer(
      profileID, this.config.netplayProfile.maxStateBytes, this.bridge, this.diagnostics,
      { epoch: this.epoch, nextFrame: message.nextFrame! },
    );
    const { state, stateSha256, coreSha256 } = transfer;
    this.send("STATE_META", { transferId: message.transferId, nextFrame: message.nextFrame, byteLength: state.byteLength, stateSha256, coreSha256 });
    if (!this.socket) {throw new Error("NETPLAY_SOCKET_UNAVAILABLE");}
    this.sender.enqueue(this.socket, encodeStateFrame(this.config.sessionId, message.transferId, this.epoch, message.nextFrame!, state), 0);
    this.send("STATE_READY", {
      transferId: message.transferId,
      stateSha256,
      coreSha256,
      recaptureMatched: transfer.recaptureMatched,
    });
  }

  private acceptStateMeta(message: ServerMessage) {
    this.pendingState = pendingStateFromMessage(message);
  }

  private async receiveState(frame: Uint8Array) {
    const pending = this.pendingState;
    if (!pending) {throw new Error("PROTOCOL_VIOLATION");}
    const digests = await applyTransferredState(
      frame,
      pending,
      this.config.sessionId,
      this.epoch,
      this.config.netplayProfile.profileId,
      this.bridge,
      this.diagnostics,
    );
    this.send("STATE_APPLIED", { transferId: pending.transferId, ...digests, nativeLoadCompleted: true, recaptureMatched: true });
    this.pendingState = null;
  }

  private startEpoch(message: ServerMessage) {
    if (!Number.isSafeInteger(message.epoch) || !Number.isSafeInteger(message.nextFrame) || message.epoch! < this.epoch) {throw new Error("PROTOCOL_VIOLATION");}
	if (this.terminalRequested) {
		this.epoch = message.epoch!;
		this.occupiedMask = message.occupiedSeatMask ?? this.occupiedMask;
		return;
	}
	const resync = this.hasRun;
	this.epoch = message.epoch!; this.nextFrame = message.nextFrame!; this.occupiedMask = message.occupiedSeatMask ?? this.occupiedMask;
    this.hasRun = true; this.epochRunning = true;
    this.reconnectDeadline = 0; this.reconnectAttempt = 0;
    if (this.reconnectDeadlineTimer !== null) {window.clearTimeout(this.reconnectDeadlineTimer);}
    this.reconnectDeadlineTimer = null;
    this.timeline.reset(this.nextFrame); this.checkpoints.reset();
    this.lastInput = null; this.lockstepInputThrough = this.nextFrame - 1;
    this.lockstepInputBufferFrames = 1; this.lockstepRoundTripMS = null; this.lockstepLowerTargetSamples = 0;
    this.lockstepInputSentAtMS.clear();
    this.bridge.resetLocalControls();
    this.status.publish("stable", "网络稳定", "synced"); this.callbacks.onRunning();
	this.diagnostics?.onEpoch?.({ epoch: this.epoch, nextFrame: this.nextFrame, resync });
    this.requestAdvance();
  }

  private requestAdvance() {
    if (this.terminalRequested || this.stopped) {return;}
    if (this.config.netplayProfile.maxPredictionFrames === 0) {
      try { this.fillLockstepInputs(); } catch (error) { this.handleFailure(error); }
      return;
    }
    void this.advance().catch((error: unknown) => this.handleFailure(error));
  }

  private fillLockstepInputs() {
    if (this.stopped || this.terminalRequested || !this.epochRunning || this.socket?.readyState !== WebSocket.OPEN) {return;}
    if (this.lockstepInputThrough < this.nextFrame - 1) {throw new Error("NETPLAY_HISTORY_GAP");}
    const targetFrame = this.nextFrame + this.lockstepInputBufferFrames - 1;
    while (this.lockstepInputThrough < targetFrame) {
      const frame = this.lockstepInputThrough + 1;
      const local = this.bridge.sampleLocalControls();
      this.send("INPUT", { frame, playerNo: this.config.playerNo, controls: local });
      this.lockstepInputSentAtMS.set(frame, { sentAtMS: performance.now(), leadFrames: frame - this.nextFrame });
      this.lockstepInputThrough = frame;
    }
    this.status.scheduleWaiting();
  }

  private async advance() {
    if (this.advancing || this.stopped || this.terminalRequested || !this.epochRunning || this.socket?.readyState !== WebSocket.OPEN || !this.timeline.canPredict(this.nextFrame)) {return;}
    this.advancing = true;
    try {
      while (!this.stopped && this.epochRunning && this.socket?.readyState === WebSocket.OPEN && this.timeline.canPredict(this.nextFrame)) {
        const frame = this.nextFrame;
        const state = await this.bridge.captureState(frame);
        this.timeline.recordOwnedStateBefore(frame, state);
        const local = this.bridge.sampleLocalControls();
        const input = predictInputs(this.lastInput, this.config.playerNo, local);
        this.send("INPUT", { frame, playerNo: this.config.playerNo, controls: local });
        this.timeline.recordPrediction(frame, input); this.lastInput = input;
        await this.runFrame(input, false, frame);
        this.nextFrame += 1;
        this.checkpoints.requestFlush();
      }
      if (!this.timeline.canPredict(this.nextFrame)) {this.status.scheduleWaiting();}
    } finally { this.advancing = false; }
  }

  private async acceptCanonical(message: ServerMessage, receivedAtMS: number) {
    if (!Number.isSafeInteger(message.frame) || !message.players || message.players.length !== 4) {throw new Error("PROTOCOL_VIOLATION");}
    const rollback = this.timeline.receiveCanonical(message.frame!, message.players);
    this.lastCanonicalFrame = Math.max(this.lastCanonicalFrame, message.frame!);
	this.diagnostics?.onCanonical?.({
		frame: message.frame!, predictionFrames: Math.max(0, this.nextFrame - message.frame! - 1),
    });
    if (this.config.netplayProfile.maxPredictionFrames === 0) {
      await this.acceptLockstepCanonical(message, receivedAtMS);
      return;
    }
    await this.acceptPredictedCanonical(message, rollback);
  }

  private async acceptLockstepCanonical(message: ServerMessage, receivedAtMS: number) {
    const frame = message.frame!;
    const advancesFrame = frame === this.nextFrame;
    if (frame > this.nextFrame) {throw new Error("NETPLAY_HISTORY_GAP");}
    if (advancesFrame) {
      if (this.lockstepInputThrough < frame) {throw new Error("NETPLAY_HISTORY_GAP");}
      this.updateLockstepInputBuffer(frame, receivedAtMS);
      this.diagnostics?.onLockstep?.({ frame, inputBufferFrames: this.lockstepInputBufferFrames, roundTripMS: this.lockstepRoundTripMS });
      this.lastInput = message.players!;
      this.nextFrame = frame + 1;
      this.fillLockstepInputs();
      await this.runFrame(message.players!, false, frame);
      this.status.publish("stable", "网络稳定", "synced");
    }
    if (advancesFrame && (frame + 1) % this.config.netplayProfile.checkpointEveryFrames === 0) {
      await this.checkpoints.queueLockstep(frame, this.epoch);
    }
    this.requestAdvance();
  }

  private async acceptPredictedCanonical(message: ServerMessage, rollback: number | null) {
    if (rollback !== null) {
		await this.pauseAdvancement();
		if (this.stopped) {return;}
		if (rollback >= this.nextFrame) {throw new Error("NETPLAY_FRAME_STEP_TIMEOUT");}
      const through = this.nextFrame - 1; const plan = this.timeline.rollbackPlan(rollback, through);
		this.diagnostics?.onRollback?.({ frame: rollback, through, depth: through - rollback + 1 });
      await this.bridge.loadStateAndWait(plan.state, rollback);
      for (const item of plan.frames) {
        this.timeline.recordOwnedStateBefore(item.frame, await this.bridge.captureState(item.frame));
        await this.runFrame(item.input, true, item.frame);
      }
      this.lastInput = plan.frames.at(-1)?.input ?? this.lastInput;
		this.epochRunning = true;
		this.checkpoints.requestFlush();
      this.status.publish("rollback", `已同步 · 回滚 ${through - rollback + 1} 帧`, "synced");
    } else {
      this.status.publish("stable", "网络稳定", "synced");
    }
    if ((message.frame! + 1) % this.config.netplayProfile.checkpointEveryFrames === 0) {
      this.checkpoints.queue(message.frame!, this.epoch);
    }
    this.requestAdvance();
    this.diagnostics?.onRetained?.(this.timeline.retained());
  }

  private async runFrame(input: CanonicalInput, suppressOutput: boolean, frame: number) {
    this.diagnostics?.onFrameStep?.({ frame, phase: "STARTED" });
    await this.bridge.runFrame(input, frame, suppressOutput);
    this.diagnostics?.onFrameStep?.({ frame, phase: "COMPLETED" });
  }

  private updateLockstepInputBuffer(frame: number, receivedAtMS: number) {
    const sent = this.lockstepInputSentAtMS.get(frame);
    this.lockstepInputSentAtMS.delete(frame);
    if (sent === undefined) {return;}
    // Inputs intentionally submitted ahead spend leadFrames in the local
    // lockstep pipeline. Remove that residence time so a larger buffer can
    // shrink again after the transport RTT recovers.
    const sampleMS = Math.max(0, receivedAtMS - sent.sentAtMS - sent.leadFrames * lockstepFrameDurationMS);
    const updated = updateLockstepBuffer({ frames: this.lockstepInputBufferFrames, roundTripMS: this.lockstepRoundTripMS, lowerTargetSamples: this.lockstepLowerTargetSamples }, sampleMS);
    this.lockstepInputBufferFrames = updated.frames;
    this.lockstepRoundTripMS = updated.roundTripMS;
    this.lockstepLowerTargetSamples = updated.lowerTargetSamples;
  }

  private async pauseAdvancement() {
    this.epochRunning = false;
    while (this.advancing) {await new Promise((resolve) => window.setTimeout(resolve, 0));}
  }

  private async pauseAtCanonicalBoundary(atFrame: number | undefined) {
    if (!Number.isSafeInteger(atFrame) || atFrame! < -1) {throw new Error("PROTOCOL_VIOLATION");}
    await this.pauseAdvancement();
    if (this.stopped) {return;}
    const targetNextFrame = atFrame! + 1;
    if (this.nextFrame < targetNextFrame) {throw new Error("NETPLAY_HISTORY_GAP");}
    if (this.config.netplayProfile.maxPredictionFrames === 0) {
      if (this.nextFrame !== targetNextFrame) {throw new Error("NETPLAY_HISTORY_GAP");}
      return;
    }
    if (atFrame === -1) {
      const initialState = this.timeline.stateAt(0);
      if (!initialState) {throw new Error("ROLLBACK_WINDOW_EXCEEDED");}
      await this.bridge.loadStateAndWait(initialState, 0);
    } else {
      const stateBefore = this.timeline.stateAt(atFrame!);
      const canonical = this.timeline.canonicalAt(atFrame!);
      if (!stateBefore || !canonical) {throw new Error("ROLLBACK_WINDOW_EXCEEDED");}
      // Native state load restores the core framebuffer but EmulatorJS does not
      // repaint its HTML canvas. Replaying this exact canonical frame restores
      // both the logical boundary and the visible paused frame on every peer.
      await this.bridge.loadStateAndWait(stateBefore, atFrame!);
      await this.bridge.runFrame(canonical, atFrame!, false);
    }
    this.nextFrame = targetNextFrame;
  }

  private async acceptHistory(message: ServerMessage, receivedAtMS: number) {
    for (const frame of message.canonical ?? []) {await this.acceptCanonical({ ...message, type: "CANONICAL", frame: frame.frame, occupiedSeatMask: frame.occupiedSeatMask, players: frame.players }, receivedAtMS);}
    this.send("HISTORY_APPLIED", { historyAppliedThrough: message.toFrame ?? -1 });
  }

  handleFocusLoss() {
    if (this.stopped) {return;}
    this.bridge.resetLocalControls();
  }

  end() {
    this.requestTerminalEnd(new Error("USER_EXIT"));
  }

  dispose() {
    this.finalizeEnded("CLIENT_DISPOSED", false);
  }

  private requestTerminalEnd(error: unknown) {
    if (this.stopped || this.terminalRequested) {return;}
    this.terminalRequested = true;
    this.terminalReason = terminalReason(error);
    this.epochRunning = false;
    this.bridge.resetLocalControls();
    this.sender.reset();
    this.status.publish("ending", this.terminalReason === "USER_EXIT" ? "正在结束联机会话…" : "联机运行异常，正在安全结束…", "warning");
    if (this.reconnectDeadline === 0) {this.reconnectDeadline = performance.now() + this.leaseMS;}
    const remainingMS = Math.max(0, this.reconnectDeadline - performance.now());
    this.endingTimer = window.setTimeout(() => this.finalizeEnded(this.terminalReason), remainingMS);
    try {
      this.send("END_REQUEST", { reason: this.terminalReason });
    } catch {
      this.handleTransportLoss();
    }
  }

  private finalizeEnded(reason: string, notify = true) {
    if (this.finalized) {return;}
    this.finalized = true;
    this.stopped = true;
    this.epochRunning = false;
    this.sender.reset();
    for (const timer of [this.reconnectTimer, this.reconnectDeadlineTimer, this.openTimer, this.endingTimer]) {
      if (timer !== null) {window.clearTimeout(timer);}
    }
    this.reconnectTimer = null;
    this.reconnectDeadlineTimer = null;
    this.openTimer = null;
    this.endingTimer = null;
    this.status.clear();
    document.removeEventListener("visibilitychange", this.resumeIfVisible);
    this.socketGeneration += 1;
    this.socket?.close(1000, reason);
    this.socket = null;
    void this.bridge.close().catch(() => undefined);
    if (notify) {
      this.diagnostics?.onEnded?.(reason);
      this.callbacks.onEnded(reason);
    }
  }

  private handleFailure(error: unknown) {
    if (this.stopped) {return;}
    const message = error instanceof Error ? error.message : "INTERNAL_ERROR";
    if (message === "NETPLAY_SOCKET_UNAVAILABLE") {
      this.handleTransportLoss();
      return;
    }
    this.requestTerminalEnd(error);
  }
	private dropConnectionForTest(durationMs: number) {
		if (process.env.NODE_ENV === "production") {return;}
		this.resumeBlocked = true;
		this.socket?.close(1000, "TEST_DISCONNECT");
		window.setTimeout(() => {
			this.resumeBlocked = false;
			this.scheduleReconnect();
		}, Math.max(0, Math.min(durationMs, 15_000)));
	}
}
