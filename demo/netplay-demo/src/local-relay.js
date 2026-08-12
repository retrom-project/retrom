import {
  PLAYER_COUNT,
  assertFrame,
  assertSlot,
  canonicalFrame,
  normalizeSnapshot,
  sameSnapshot
} from "./protocol.js";

function sameCanonical(left, right) {
  return left.frame === right.frame
    && left.players.every((snapshot, slot) => sameSnapshot(snapshot, right.players[slot]));
}

export class LocalRelay {
  #canonical = new Map();
  #clients = new Map();
  #hashes = new Map();
  #latencyMs;
  #maxFutureFrames;
  #pending = new Map();
  #timers = new Set();
  #closed = false;
  #inputTransitions = 0;
  #lastCanonicalFrame = 0;
  #lastContributionBySlot = Array.from({ length: PLAYER_COUNT }, () => 0);
  #lastPlayers = null;
  #nonNeutralFrames = 0;

  constructor({ latencyMs = 0, maxFutureFrames = 120 } = {}) {
    this.setLatency(latencyMs);
    if (!Number.isInteger(maxFutureFrames) || maxFutureFrames < 1) {
      throw new TypeError("maxFutureFrames must be a positive integer");
    }
    this.#maxFutureFrames = maxFutureFrames;
  }

  connect(slot, handlers) {
    assertSlot(slot);
    if (this.#closed) throw new Error("Relay is closed");
    if (this.#clients.has(slot)) throw new Error(`Slot ${slot} is already connected`);
    if (typeof handlers?.onFrame !== "function" || typeof handlers?.onHashResult !== "function") {
      throw new TypeError("Relay handlers are incomplete");
    }
    this.#clients.set(slot, handlers);
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
    const snapshot = normalizeSnapshot(values);
    await this.#schedule(() => this.#acceptContribution(slot, frame, snapshot));
  }

  async sendHash({ slot, frame, digest }) {
    assertSlot(slot);
    assertFrame(frame);
    if (!/^[0-9a-f]{64}$/.test(digest)) throw new TypeError("Invalid SHA-256 digest");
    await this.#schedule(() => this.#acceptHash(slot, frame, digest));
  }

  disconnect(slot) {
    if (!this.#clients.delete(slot)) return;
    for (const handlers of this.#clients.values()) {
      handlers.onPause?.({ reason: "peer-disconnected", slot });
    }
  }

  close() {
    this.#closed = true;
    for (const timer of this.#timers) clearTimeout(timer);
    this.#timers.clear();
    this.#clients.clear();
    this.#pending.clear();
    this.#hashes.clear();
  }

  getMetrics() {
    return {
      lastCanonicalFrame: this.#lastCanonicalFrame,
      nonNeutralFrames: this.#nonNeutralFrames,
      inputTransitions: this.#inputTransitions,
      pendingFrames: this.#pending.size,
      retainedCanonicalFrames: this.#canonical.size
    };
  }

  #acceptContribution(slot, frame, snapshot) {
    if (this.#closed) throw new Error("Relay is closed");
    if (!this.#clients.has(slot)) throw new Error(`Slot ${slot} is not connected`);
    if (frame > this.#lastCanonicalFrame + this.#maxFutureFrames) {
      throw new RangeError(`Frame ${frame} exceeds the future window`);
    }

    const published = this.#canonical.get(frame);
    if (published) {
      if (!sameSnapshot(published.players[slot], snapshot)) {
        throw new Error(`Canonical frame ${frame} is immutable`);
      }
      return;
    }

    const contributions = this.#pending.get(frame) ?? Array.from({ length: PLAYER_COUNT });
    const previous = contributions[slot];
    if (previous && !sameSnapshot(previous, snapshot)) {
      throw new Error(`Slot ${slot} changed its contribution for frame ${frame}`);
    }
    if (previous) return;
    if (frame <= this.#lastContributionBySlot[slot]) {
      throw new RangeError(`Slot ${slot} contribution frame ${frame} is not monotonic`);
    }
    contributions[slot] = snapshot;
    this.#lastContributionBySlot[slot] = frame;
    this.#pending.set(frame, contributions);
    if (contributions.every(Boolean)) this.#publishFrame(frame, contributions);
  }

  #publishFrame(frame, contributions) {
    const canonical = canonicalFrame(frame, contributions);
    const previous = this.#canonical.get(frame);
    if (previous && !sameCanonical(previous, canonical)) {
      throw new Error(`Canonical frame ${frame} changed after publication`);
    }
    if (canonical.players.some((snapshot) => snapshot.some((value) => value !== 0))) {
      this.#nonNeutralFrames += 1;
    }
    if (this.#lastPlayers) {
      for (let slot = 0; slot < PLAYER_COUNT; slot += 1) {
        for (let control = 0; control < canonical.players[slot].length; control += 1) {
          if (canonical.players[slot][control] !== this.#lastPlayers[slot][control]) {
            this.#inputTransitions += 1;
          }
        }
      }
    } else {
      this.#inputTransitions = canonical.players.reduce(
        (count, snapshot) => count + snapshot.filter((value) => value !== 0).length,
        0
      );
    }
    this.#lastPlayers = canonical.players;
    this.#canonical.set(frame, canonical);
    this.#pending.delete(frame);
    this.#lastCanonicalFrame = Math.max(this.#lastCanonicalFrame, frame);
    this.#trimFrames();
    for (const handlers of this.#clients.values()) {
      void this.#schedule(() => handlers.onFrame(canonical));
    }
  }

  #acceptHash(slot, frame, digest) {
    if (!this.#clients.has(slot)) throw new Error(`Slot ${slot} is not connected`);
    const hashes = this.#hashes.get(frame) ?? Array.from({ length: PLAYER_COUNT });
    if (hashes[slot] && hashes[slot] !== digest) {
      throw new Error(`Slot ${slot} changed its hash for frame ${frame}`);
    }
    hashes[slot] = digest;
    this.#hashes.set(frame, hashes);
    if (!hashes.every(Boolean)) return;

    const result = Object.freeze({
      type: "hash-result",
      frame,
      matched: hashes[0] === hashes[1],
      digests: Object.freeze([...hashes])
    });
    this.#hashes.delete(frame);
    for (const handlers of this.#clients.values()) {
      void this.#schedule(() => handlers.onHashResult(result));
    }
  }

  #trimFrames() {
    const cutoff = this.#lastCanonicalFrame - 600;
    for (const frame of this.#canonical.keys()) {
      if (frame < cutoff) this.#canonical.delete(frame);
    }
  }

  #schedule(callback) {
    if (this.#closed) return Promise.reject(new Error("Relay is closed"));
    const oneWayDelay = this.#latencyMs / 2;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.#timers.delete(timer);
        try {
          resolve(callback());
        } catch (error) {
          reject(error);
        }
      }, oneWayDelay);
      this.#timers.add(timer);
    });
  }
}
