import { assertFrame, assertSlot, canonicalFrame, normalizeSnapshot } from "./protocol.js";
import { sha256Bytes } from "./state-hash.js";

const DIGEST_PATTERN = /^[0-9a-f]{64}$/;

function bytesToBase64(bytes) {
  let binary = "";
  for (let offset = 0; offset < bytes.byteLength; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return btoa(binary);
}

function base64ToBytes(value) {
  const binary = atob(value);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

export class WebSocketRelay {
  #closed = false;
  #connections = new Map();
  #latencyMs;
  #metrics = {
    lastCanonicalFrame: 0,
    nonNeutralFrames: 0,
    inputTransitions: 0,
    pendingFrames: 0,
    retainedCanonicalFrames: 0,
    stateTransfers: 0,
    reconnects: 0,
    lastClose: null
  };
  #participants = null;
  #pendingStates = new Map();
  #profileDigest = null;
  #roomId = null;
  #timers = new Set();

  constructor({ latencyMs = 0 } = {}) {
    this.setLatency(latencyMs);
  }

  async initialize(profileDigest) {
    if (!DIGEST_PATTERN.test(profileDigest)) throw new TypeError("Invalid profile digest");
    if (this.#roomId) {
      if (profileDigest !== this.#profileDigest) throw new Error("Relay profile cannot change");
      return;
    }
    const response = await fetch("/api/rooms", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ profileDigest })
    });
    const room = await response.json();
    if (!response.ok) throw new Error(room.error ?? `Room creation failed with ${response.status}`);
    if (!Array.isArray(room.participants) || room.participants.length !== 2) {
      throw new Error("Room service returned invalid participant credentials");
    }
    this.#profileDigest = profileDigest;
    this.#roomId = room.roomId;
    this.#participants = room.participants;
  }

  connect(slot, handlers) {
    assertSlot(slot);
    if (this.#closed) throw new Error("Relay is closed");
    if (!this.#roomId || !this.#participants) throw new Error("Relay must be initialized before connecting");
    if (this.#connections.has(slot)) throw new Error(`Slot ${slot} is already connected`);
    if (typeof handlers?.onFrame !== "function"
      || typeof handlers?.onHashResult !== "function"
      || typeof handlers?.onState !== "function") {
      throw new TypeError("Relay handlers are incomplete");
    }
    const entry = {
      attempt: 0,
      awaitingResume: false,
      closed: false,
      contributions: new Map(),
      disconnectedAt: null,
      everWelcomed: false,
      handlers,
      hashes: new Map(),
      lastCanonicalFrame: 0,
      lastHashFrame: 0,
      online: null,
      reconnectTimer: null,
      rejectOnline: null,
      resolveOnline: null,
      resumeToken: null,
      socket: null,
      states: new Map(),
      welcomed: false
    };
    this.#resetOnline(entry);
    this.#connections.set(slot, entry);
    this.#open(slot, entry);
    return () => this.disconnect(slot);
  }

  setLatency(latencyMs) {
    if (!Number.isFinite(latencyMs) || latencyMs < 0 || latencyMs > 2000) {
      throw new TypeError("latencyMs must be between 0 and 2000");
    }
    this.#latencyMs = latencyMs;
  }

  get latencyMs() {
    return this.#latencyMs;
  }

  async sendContribution({ slot, frame, values }) {
    assertSlot(slot);
    assertFrame(frame);
    const message = { type: "contribution", frame, values: normalizeSnapshot(values) };
    const entry = this.#entry(slot);
    entry.contributions.set(frame, message);
    await this.#send(slot, message);
  }

  async sendHash({ slot, frame, digest }) {
    assertSlot(slot);
    assertFrame(frame);
    if (!DIGEST_PATTERN.test(digest)) throw new TypeError("Invalid SHA-256 digest");
    const message = { type: "hash", frame, digest };
    const entry = this.#entry(slot);
    entry.hashes.set(frame, message);
    await this.#send(slot, message);
  }

  async sendState({ slot, frame, state, digest, coreDigest }) {
    assertSlot(slot);
    if (!Number.isSafeInteger(frame) || frame < 0) throw new TypeError("Invalid state frame");
    if (!(state instanceof Uint8Array) || state.byteLength === 0 || state.byteLength > 1024 * 1024) {
      throw new TypeError("Savestate must be a non-empty Uint8Array of at most 1 MiB");
    }
    if (!DIGEST_PATTERN.test(digest)) throw new TypeError("Invalid savestate digest");
    if (!DIGEST_PATTERN.test(coreDigest)) throw new TypeError("Invalid core state digest");
    const transferId = crypto.randomUUID();
    const message = { type: "state", transferId, frame, digest, coreDigest, state: bytesToBase64(state) };
    const entry = this.#entry(slot);
    entry.states.set(transferId, message);
    const acknowledgement = new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.#pendingStates.delete(transferId);
        reject(new Error(`Savestate transfer ${transferId} timed out`));
      }, 15000);
      this.#pendingStates.set(transferId, { digest, reject, resolve, slot, timeout });
    });
    try {
      await this.#send(slot, message);
      return await acknowledgement;
    } catch (error) {
      const pending = this.#pendingStates.get(transferId);
      if (pending) {
        clearTimeout(pending.timeout);
        this.#pendingStates.delete(transferId);
      }
      entry.states.delete(transferId);
      throw error;
    }
  }

  dropConnection(slot) {
    const entry = this.#entry(slot);
    entry.socket?.close(4001, "Fault injection");
  }

  disconnect(slot) {
    const entry = this.#connections.get(slot);
    if (!entry) return;
    entry.closed = true;
    clearTimeout(entry.reconnectTimer);
    entry.rejectOnline(new Error(`WebSocket slot ${slot} disconnected`));
    entry.socket?.close(1000, "Client disconnected");
    this.#connections.delete(slot);
  }

  close() {
    this.#closed = true;
    for (const timer of this.#timers) clearTimeout(timer);
    this.#timers.clear();
    for (const pending of this.#pendingStates.values()) {
      clearTimeout(pending.timeout);
      pending.reject(new Error("Relay closed during savestate transfer"));
    }
    this.#pendingStates.clear();
    for (const slot of [...this.#connections.keys()]) this.disconnect(slot);
  }

  getMetrics() {
    return {
      ...this.#metrics,
      roomId: this.#roomId,
      connections: [...this.#connections.entries()].map(([slot, entry]) => ({
        slot,
        online: entry.welcomed,
        reconnectAttempt: entry.attempt
      }))
    };
  }

  #entry(slot) {
    const entry = this.#connections.get(slot);
    if (!entry) throw new Error(`Slot ${slot} is not connected`);
    return entry;
  }

  #resetOnline(entry) {
    entry.online = new Promise((resolve, reject) => {
      entry.resolveOnline = resolve;
      entry.rejectOnline = reject;
    });
  }

  #open(slot, entry) {
    if (this.#closed || entry.closed) return;
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const url = new URL(`${scheme}//${location.host}/netplay`);
    url.searchParams.set("room", this.#roomId);
    url.searchParams.set("slot", String(slot));
    url.searchParams.set("token", this.#participants[slot].joinToken);
    url.searchParams.set("profile", this.#profileDigest);
    if (entry.resumeToken) url.searchParams.set("resume", entry.resumeToken);
    url.searchParams.set("after", String(entry.lastCanonicalFrame));
    url.searchParams.set("hafter", String(entry.lastHashFrame));
    const socket = new WebSocket(url);
    entry.socket = socket;
    socket.addEventListener("message", (event) => {
      if (entry.socket === socket) this.#receive(slot, event.data);
    });
    socket.addEventListener("close", (event) => {
      if (entry.socket !== socket || entry.closed || this.#closed) return;
      entry.socket = null;
      entry.welcomed = false;
      entry.awaitingResume = true;
      this.#metrics.lastClose = { slot, code: event.code, reason: event.reason };
      if (event.code === 1008) {
        this.#fail(slot, new Error(`WebSocket slot ${slot} was rejected`));
        return;
      }
      if (entry.disconnectedAt === null) entry.disconnectedAt = Date.now();
      entry.handlers.onPause?.({ reason: "transport-reconnecting", slot });
      this.#resetOnline(entry);
      this.#scheduleReconnect(slot, entry);
    });
  }

  #scheduleReconnect(slot, entry) {
    if (Date.now() - entry.disconnectedAt > 12000) {
      this.#fail(slot, new Error(`WebSocket slot ${slot} reconnect lease expired`));
      return;
    }
    const delay = Math.min(1000, 100 * (2 ** Math.min(entry.attempt, 4)));
    entry.attempt += 1;
    entry.reconnectTimer = setTimeout(() => this.#open(slot, entry), delay);
  }

  async #send(slot, message) {
    const entry = this.#entry(slot);
    const deadline = Date.now() + 12000;
    while (Date.now() < deadline) {
      await entry.online;
      const sent = await this.#schedule(() => {
        if (entry.socket?.readyState !== WebSocket.OPEN || !entry.welcomed) return false;
        entry.socket.send(JSON.stringify(message));
        return true;
      });
      if (sent) return;
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
    throw new Error(`WebSocket slot ${slot} could not send within its reconnect lease`);
  }

  #receive(slot, raw) {
    const entry = this.#connections.get(slot);
    if (!entry) return;
    try {
      const message = JSON.parse(raw);
      if (message.type === "hello") {
        if (message.slot !== slot || message.roomId !== this.#roomId || typeof message.resumeToken !== "string") {
          throw new Error("WebSocket welcome mismatch");
        }
        const reconnected = entry.everWelcomed;
        entry.resumeToken = message.resumeToken;
        entry.welcomed = true;
        entry.everWelcomed = true;
        entry.disconnectedAt = null;
        entry.attempt = 0;
        entry.resolveOnline();
        if (reconnected) {
          this.#metrics.reconnects += 1;
        }
        if (reconnected) void this.#flush(slot, entry).catch((error) => this.#fail(slot, error));
        return;
      }
      if (!entry.welcomed) throw new Error("WebSocket message arrived before welcome");
      if (message.type === "frame") {
        const frame = canonicalFrame(message.frame, message.players);
        entry.lastCanonicalFrame = Math.max(entry.lastCanonicalFrame, frame.frame);
        entry.contributions.delete(frame.frame);
        void this.#schedule(() => entry.handlers.onFrame(frame)).catch((error) => this.#fail(slot, error));
      } else if (message.type === "hash-result") {
        assertFrame(message.frame);
        if (typeof message.matched !== "boolean"
          || !Array.isArray(message.digests)
          || message.digests.length !== 2
          || message.digests.some((digest) => !DIGEST_PATTERN.test(digest))) {
          throw new TypeError("Invalid hash result");
        }
        entry.hashes.delete(message.frame);
        entry.lastHashFrame = Math.max(entry.lastHashFrame, message.frame);
        void this.#schedule(() => entry.handlers.onHashResult(message)).catch((error) => this.#fail(slot, error));
      } else if (message.type === "state") {
        void this.#receiveState(slot, entry, message).catch((error) => this.#fail(slot, error));
      } else if (message.type === "state-applied") {
        this.#receiveStateApplied(entry, message);
      } else if (message.type === "metrics") {
        this.#metrics = { ...this.#metrics, ...message.metrics };
      } else if (message.type === "pause") {
        void this.#schedule(() => entry.handlers.onPause?.(message)).catch((error) => this.#fail(slot, error));
      } else if (message.type === "resume") {
        void this.#schedule(() => {
          if (entry.awaitingResume) {
            entry.awaitingResume = false;
            entry.handlers.onResume?.({ reason: "transport-reconnecting", slot });
          }
          entry.handlers.onResume?.(message);
        }).catch((error) => this.#fail(slot, error));
      } else if (message.type === "error") {
        throw new Error(`Relay rejected slot ${slot}: ${message.message}`);
      } else {
        throw new TypeError(`Unsupported relay message: ${String(message.type)}`);
      }
    } catch (error) {
      this.#fail(slot, error instanceof Error ? error : new Error(String(error)));
    }
  }

  async #receiveState(slot, entry, message) {
    if (typeof message.transferId !== "string"
      || !DIGEST_PATTERN.test(message.digest)
      || !DIGEST_PATTERN.test(message.coreDigest)) {
      throw new TypeError("Invalid savestate transfer metadata");
    }
    const state = base64ToBytes(message.state);
    if (state.byteLength === 0 || state.byteLength > 1024 * 1024) throw new Error("Invalid savestate size");
    if (await sha256Bytes(state) !== message.digest) throw new Error("Savestate transfer digest mismatch");
    await this.#schedule(() => entry.handlers.onState({
      frame: message.frame,
      state,
      digest: message.digest,
      coreDigest: message.coreDigest
    }));
    await this.#send(slot, {
      type: "state-applied",
      transferId: message.transferId,
      digest: message.digest
    });
  }

  #receiveStateApplied(entry, message) {
    const pending = this.#pendingStates.get(message.transferId);
    if (!pending || pending.digest !== message.digest) throw new Error("Invalid savestate acknowledgement");
    clearTimeout(pending.timeout);
    this.#pendingStates.delete(message.transferId);
    entry.states.delete(message.transferId);
    this.#metrics.stateTransfers += 1;
    pending.resolve({ digest: message.digest });
  }

  async #flush(slot, entry) {
    for (const message of [...entry.contributions.values()].sort((left, right) => left.frame - right.frame)) {
      await this.#send(slot, message);
    }
    for (const message of [...entry.hashes.values()].sort((left, right) => left.frame - right.frame)) {
      await this.#send(slot, message);
    }
    for (const message of entry.states.values()) await this.#send(slot, message);
  }

  #fail(slot, error) {
    const entry = this.#connections.get(slot);
    if (!entry) return;
    entry.rejectOnline(error);
    entry.handlers.onError?.(error);
  }

  #schedule(callback) {
    if (this.#closed) return Promise.reject(new Error("Relay is closed"));
    return new Promise((resolve, reject) => {
      const handle = setTimeout(() => {
        this.#timers.delete(handle);
        try {
          resolve(callback());
        } catch (error) {
          reject(error);
        }
      }, this.#latencyMs / 2);
      this.#timers.add(handle);
    });
  }
}
