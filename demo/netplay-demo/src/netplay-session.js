import { LocalRelay } from "./local-relay.js";
import { LockstepClient } from "./lockstep-client.js";
import { sha256Bytes } from "./state-hash.js";

function stableJson(value) {
  if (Array.isArray(value)) return `[${value.map(stableJson).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stableJson(value[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

export class NetplaySession {
  #bridges;
  #clients = [];
  #disconnect = [];
  #failed = false;
  #hashes = new Map();
  #inputTimers = new Set();
  #onState;
  #onStatus;
  #relay;
  #report;
  #state = "idle";
  #targetFrame;

  constructor({
    bridges,
    inputDelay,
    latencyMs,
    hashEvery,
    targetFrame,
    relay = new LocalRelay({ latencyMs }),
    transport = "local",
    onStatus = () => {},
    onState = () => {}
  }) {
    if (!Array.isArray(bridges) || bridges.length !== 2) throw new TypeError("Two emulator bridges are required");
    this.#bridges = bridges;
    this.#targetFrame = targetFrame;
    this.#onStatus = onStatus;
    this.#onState = onState;
    this.#relay = relay;
    this.#relay.setLatency(latencyMs);
    this.#report = {
      state: "idle",
      transport,
      inputDelay,
      latencyMs,
      hashEvery,
      targetFrame,
      profileDigest: null,
      profile: null,
      initialStateDigest: null,
      preSyncStateDigests: null,
      stateSeedMode: null,
      initialStateBytes: null,
      stateCaptureMs: null,
      stateTransferMs: null,
      hashCheckpoints: [],
      desyncs: 0,
      startedAtMs: null,
      completedAtMs: null,
      error: null
    };

    this.#clients = bridges.map((emulator, slot) => new LockstepClient({
      slot,
      emulator,
      inputDelay,
      hashEvery,
      targetFrame,
      sendContribution: (contribution) => this.#relay.sendContribution(contribution),
      sendHash: (hash) => this.#relay.sendHash(hash),
      onStatus: (metrics) => this.#handleClientStatus(slot, metrics),
      onError: (error) => this.#fail(error)
    }));
  }

  async start() {
    this.#setState("synchronizing");
    try {
      const profiles = this.#bridges.map((bridge) => bridge.profile);
      const fingerprints = profiles.map(stableJson);
      if (fingerprints[0] !== fingerprints[1]) throw new Error("Emulator profiles do not match");
      this.#report.profile = profiles[0];
      this.#report.profileDigest = await sha256Bytes(new TextEncoder().encode(fingerprints[0]));

      await Promise.all(this.#bridges.map((bridge) => bridge.waitForFrames(120)));
      const alignmentFrame = Math.max(...this.#bridges.map((bridge) => bridge.getFrame())) + 30;
      await Promise.all(this.#bridges.map((bridge) => bridge.pauseAtFrame(alignmentFrame)));
      this.#bridges.forEach((bridge) => bridge.resetInputs());
      const captures = this.#bridges.map((bridge) => {
        const startedAt = performance.now();
        const state = bridge.captureState();
        return { state, durationMs: performance.now() - startedAt };
      });
      const authorityState = captures[0].state;
      this.#report.stateCaptureMs = captures.map(({ durationMs }) => durationMs);
      this.#report.preSyncStateDigests = await Promise.all(captures.map(({ state }) => sha256Bytes(state)));
      if (this.#report.preSyncStateDigests[0] === this.#report.preSyncStateDigests[1]) {
        this.#report.stateSeedMode = "cold-start-aligned";
      } else {
        this.#report.stateSeedMode = "savestate-transfer";
        const transferStartedAt = performance.now();
        await this.#bridges[1].loadStateAndWait(authorityState);
        this.#report.stateTransferMs = performance.now() - transferStartedAt;
      }
      const initialDigests = await Promise.all([
        sha256Bytes(authorityState),
        sha256Bytes(this.#bridges[1].captureState())
      ]);
      if (initialDigests[0] !== initialDigests[1]) {
        throw new Error("Canonical savestate did not round-trip into the right instance");
      }
      this.#report.initialStateBytes = authorityState.byteLength;
      this.#report.initialStateDigest = initialDigests[0];

      for (let slot = 0; slot < this.#clients.length; slot += 1) {
        const client = this.#clients[slot];
        this.#disconnect.push(this.#relay.connect(slot, {
          onFrame: (frame) => client.receiveCanonical(frame),
          onHashResult: (result) => this.#handleHashResult(slot, result),
          onPause: ({ reason }) => client.pause(reason),
          onError: (error) => client.fail(error)
        }));
      }

      await Promise.all(this.#clients.map((client) => client.prime()));
      await Promise.all(this.#clients.map((client) => client.waitForCanonical(1)));
      this.#clients.forEach((client) => client.attach(alignmentFrame));
      this.#clients.forEach((client) => client.start());
      this.#report.startedAtMs = Date.now();
      this.#setState("running");
    } catch (error) {
      this.#fail(error);
      throw error;
    }
  }

  setLatency(latencyMs) {
    this.#relay.setLatency(latencyMs);
    this.#report.latencyMs = latencyMs;
    this.#emitStatus();
  }

  pause() {
    this.#clients.forEach((client) => client.pause("manual"));
    if (!this.#failed) this.#setState("paused");
  }

  resume() {
    this.#clients.forEach((client) => client.resume("manual"));
    if (!this.#failed) this.#setState("running");
  }

  hashNow() {
    const nextFrame = Math.max(...this.#clients.map((client) => client.getMetrics().netFrame)) + 1;
    this.#clients.forEach((client) => client.scheduleHash(nextFrame));
    return nextFrame;
  }

  simulateLocalInput(slot, control, value) {
    const bridge = this.#bridges[slot];
    if (!bridge) throw new TypeError(`Invalid local slot ${slot}`);
    bridge.simulateLocalInput(control, value);
  }

  press(slot, control, durationMs = 120) {
    this.simulateLocalInput(slot, control, 1);
    const timer = setTimeout(() => {
      this.#inputTimers.delete(timer);
      this.simulateLocalInput(slot, control, 0);
    }, durationMs);
    this.#inputTimers.add(timer);
  }

  getReport() {
    return structuredClone({
      ...this.#report,
      state: this.#state,
      relay: this.#relay.getMetrics(),
      clients: this.#clients.map((client) => client.getMetrics()),
      capabilities: this.#bridges.map((bridge) => bridge.capabilities)
    });
  }

  cleanup() {
    for (const timer of this.#inputTimers) clearTimeout(timer);
    this.#inputTimers.clear();
    this.#clients.forEach((client) => client.cleanup());
    while (this.#disconnect.length) this.#disconnect.pop()();
    this.#relay.close();
    if (this.#state !== "complete" && this.#state !== "failed") this.#setState("closed");
  }

  #handleClientStatus(slot, metrics) {
    this.#onStatus({ slot, metrics, report: this.getReport() });
  }

  #handleHashResult(slot, result) {
    this.#clients[slot].receiveHashResult(result);
    const observed = this.#hashes.get(result.frame) ?? new Set();
    observed.add(slot);
    this.#hashes.set(result.frame, observed);
    if (observed.size !== 2) return;
    this.#hashes.delete(result.frame);
    this.#report.hashCheckpoints.push({
      frame: result.frame,
      matched: result.matched,
      digest: result.matched ? result.digests[0] : null,
      digests: result.matched ? undefined : [...result.digests]
    });
    if (!result.matched) {
      this.#report.desyncs += 1;
      this.#fail(new Error(`State hash mismatch at frame ${result.frame}`));
      return;
    }
    if (result.frame === this.#targetFrame) {
      this.#report.completedAtMs = Date.now();
      this.#setState("complete");
    } else {
      this.#emitStatus();
    }
  }

  #fail(error) {
    if (this.#failed) return;
    this.#failed = true;
    const normalized = error instanceof Error ? error : new Error(String(error));
    this.#report.error = normalized.message;
    this.#clients.forEach((client) => client.pause("session-failed"));
    this.#setState("failed");
  }

  #setState(state) {
    this.#state = state;
    this.#report.state = state;
    this.#onState({ state, report: this.getReport() });
    this.#emitStatus();
  }

  #emitStatus() {
    this.#onStatus({ slot: null, metrics: null, report: this.getReport() });
  }
}
