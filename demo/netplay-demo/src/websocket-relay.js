import { assertFrame, assertSlot, canonicalFrame, normalizeSnapshot } from "./protocol.js";

export class WebSocketRelay {
  #closed = false;
  #connections = new Map();
  #latencyMs;
  #metrics = {
    lastCanonicalFrame: 0,
    nonNeutralFrames: 0,
    inputTransitions: 0,
    pendingFrames: 0,
    retainedCanonicalFrames: 0
  };
  #roomId;
  #timers = new Set();

  constructor({ latencyMs = 0, roomId = crypto.randomUUID() } = {}) {
    if (!/^[a-zA-Z0-9_-]{1,80}$/.test(roomId)) throw new TypeError("Invalid room id");
    this.#roomId = roomId;
    this.setLatency(latencyMs);
  }

  connect(slot, handlers) {
    assertSlot(slot);
    if (this.#closed) throw new Error("Relay is closed");
    if (this.#connections.has(slot)) throw new Error(`Slot ${slot} is already connected`);
    if (typeof handlers?.onFrame !== "function" || typeof handlers?.onHashResult !== "function") {
      throw new TypeError("Relay handlers are incomplete");
    }

    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const url = new URL(`${scheme}//${location.host}/netplay`);
    url.searchParams.set("room", this.#roomId);
    url.searchParams.set("slot", String(slot));
    const socket = new WebSocket(url);
    let resolveReady;
    let rejectReady;
    const ready = new Promise((resolve, reject) => {
      resolveReady = resolve;
      rejectReady = reject;
    });
    const entry = { handlers, ready, rejectReady, resolveReady, socket, welcomed: false };
    this.#connections.set(slot, entry);

    socket.addEventListener("message", (event) => this.#receive(slot, event.data));
    socket.addEventListener("error", () => this.#fail(slot, new Error(`WebSocket slot ${slot} failed`)));
    socket.addEventListener("close", () => {
      if (this.#closed || !this.#connections.has(slot)) return;
      this.#fail(slot, new Error(`WebSocket slot ${slot} closed`));
    });
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
    await this.#send(slot, { type: "contribution", frame, values: normalizeSnapshot(values) });
  }

  async sendHash({ slot, frame, digest }) {
    assertSlot(slot);
    assertFrame(frame);
    if (!/^[0-9a-f]{64}$/.test(digest)) throw new TypeError("Invalid SHA-256 digest");
    await this.#send(slot, { type: "hash", frame, digest });
  }

  disconnect(slot) {
    const entry = this.#connections.get(slot);
    if (!entry) return;
    this.#connections.delete(slot);
    entry.rejectReady(new Error(`WebSocket slot ${slot} disconnected`));
    entry.socket.close(1000, "Client disconnected");
  }

  close() {
    this.#closed = true;
    for (const timer of this.#timers) clearTimeout(timer);
    this.#timers.clear();
    for (const slot of [...this.#connections.keys()]) this.disconnect(slot);
  }

  getMetrics() {
    return { ...this.#metrics, roomId: this.#roomId };
  }

  async #send(slot, message) {
    const entry = this.#connections.get(slot);
    if (!entry) throw new Error(`Slot ${slot} is not connected`);
    await entry.ready;
    await this.#schedule(() => {
      if (entry.socket.readyState !== WebSocket.OPEN) throw new Error(`WebSocket slot ${slot} is not open`);
      entry.socket.send(JSON.stringify(message));
    });
  }

  #receive(slot, raw) {
    const entry = this.#connections.get(slot);
    if (!entry) return;
    try {
      const message = JSON.parse(raw);
      if (message.type === "hello") {
        if (message.slot !== slot || message.roomId !== this.#roomId) throw new Error("WebSocket welcome mismatch");
        entry.welcomed = true;
        entry.resolveReady();
        return;
      }
      if (!entry.welcomed) throw new Error("WebSocket message arrived before welcome");
      if (message.type === "frame") {
        const frame = canonicalFrame(message.frame, message.players);
        void this.#schedule(() => entry.handlers.onFrame(frame)).catch((error) => this.#fail(slot, error));
      } else if (message.type === "hash-result") {
        assertFrame(message.frame);
        if (typeof message.matched !== "boolean"
          || !Array.isArray(message.digests)
          || message.digests.length !== 2
          || message.digests.some((digest) => !/^[0-9a-f]{64}$/.test(digest))) {
          throw new TypeError("Invalid hash result");
        }
        void this.#schedule(() => entry.handlers.onHashResult(message)).catch((error) => this.#fail(slot, error));
      } else if (message.type === "metrics") {
        this.#metrics = { ...this.#metrics, ...message.metrics };
      } else if (message.type === "pause") {
        void this.#schedule(() => entry.handlers.onPause?.(message)).catch((error) => this.#fail(slot, error));
      } else if (message.type === "error") {
        throw new Error(`Relay rejected slot ${slot}: ${message.message}`);
      } else {
        throw new TypeError(`Unsupported relay message: ${String(message.type)}`);
      }
    } catch (error) {
      this.#fail(slot, error instanceof Error ? error : new Error(String(error)));
    }
  }

  #fail(slot, error) {
    const entry = this.#connections.get(slot);
    if (!entry) return;
    if (!entry.welcomed) entry.rejectReady(error);
    entry.handlers.onError?.(error);
  }

  #schedule(callback) {
    if (this.#closed) return Promise.reject(new Error("Relay is closed"));
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.#timers.delete(timer);
        try {
          resolve(callback());
        } catch (error) {
          reject(error);
        }
      }, this.#latencyMs / 2);
      this.#timers.add(timer);
    });
  }
}
