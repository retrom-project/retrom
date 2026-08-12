import { LocalRelay } from "./local-relay.js";
import { RollbackClient } from "./rollback-client.js";
import { sha256Bytes, sha256CoreState } from "./state-hash.js";

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
  #resyncing = new Set();
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
      syncModel: "bounded-prediction-rollback",
      inputDelay,
      latencyMs,
      hashEvery,
      targetFrame,
      profileDigest: null,
      profile: null,
      initialStateDigest: null,
      preSyncStateDigests: null,
      stateSeedMode: null,
      stateLoadEvidence: null,
      initialStateBytes: null,
      stateCaptureMs: null,
      stateTransferMs: null,
      hashCheckpoints: [],
      desyncs: 0,
      resyncs: 0,
      resyncEvents: [],
      resyncLoadEvidence: [],
      reconnectEvents: 0,
      startedAtMs: null,
      completedAtMs: null,
      error: null
    };

    this.#clients = bridges.map((emulator, slot) => new RollbackClient({
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
      await this.#relay.initialize(this.#report.profileDigest);

      await Promise.all(this.#bridges.map((bridge) => bridge.waitForFrames(120)));
      const alignmentFrame = Math.max(...this.#bridges.map((bridge) => bridge.getFrame())) + 30;
      await Promise.all(this.#bridges.map((bridge) => bridge.pauseAtFrame(alignmentFrame)));
      this.#bridges.forEach((bridge) => bridge.resetInputs());

      for (let slot = 0; slot < this.#clients.length; slot += 1) {
        const client = this.#clients[slot];
        this.#disconnect.push(this.#relay.connect(slot, {
          onFrame: (frame) => client.receiveCanonical(frame),
          onHashResult: (result) => this.#handleHashResult(slot, result),
          onState: (event) => this.#handleState(slot, event),
          onPause: (event) => this.#handleTransportPause(slot, event),
          onResume: (event) => this.#handleTransportResume(slot, event),
          onError: (error) => client.fail(error)
        }));
      }

      const captureStartedAt = performance.now();
      const authorityState = this.#bridges[0].captureState();
      this.#report.stateCaptureMs = [performance.now() - captureStartedAt];
      this.#bridges[1].injectUntrackedInput(0, 3, 1);
      await this.#bridges[1].runExactFrame();
      await this.#bridges[1].waitForPause();
      this.#bridges[1].injectUntrackedInput(0, 3, 0);
      const divergentState = this.#bridges[1].captureState();
      this.#report.stateCaptureMs.push(performance.now() - captureStartedAt - this.#report.stateCaptureMs[0]);
      this.#report.preSyncStateDigests = await Promise.all([
        sha256Bytes(authorityState),
        sha256Bytes(divergentState)
      ]);
      if (this.#report.preSyncStateDigests[0] === this.#report.preSyncStateDigests[1]) {
        throw new Error("State-load proof could not create a divergent receiver state");
      }

      this.#report.stateSeedMode = "savestate-transfer-from-divergent-state";
      this.#report.initialStateBytes = authorityState.byteLength;
      this.#report.initialStateDigest = this.#report.preSyncStateDigests[0];
      const transferStartedAt = performance.now();
      await this.#relay.sendState({
        slot: 0,
        frame: 0,
        state: authorityState,
        digest: this.#report.initialStateDigest,
        coreDigest: await sha256CoreState(authorityState)
      });
      this.#report.stateTransferMs = performance.now() - transferStartedAt;
      const initialDigests = await Promise.all(this.#bridges.map((bridge) => sha256Bytes(bridge.captureState())));
      if (initialDigests[0] !== initialDigests[1]) {
        throw new Error("Canonical savestate did not settle in the receiving instance");
      }

      this.#clients.forEach((client) => client.attach());
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

  async injectDesync(slot = 1, control = 3) {
    const bridge = this.#bridges[slot];
    if (!bridge || this.#state !== "running") throw new Error("A running session is required for fault injection");
    if (!Number.isInteger(control) || control < 0 || control >= 24) {
      throw new TypeError("Fault-injection control must be between 0 and 23");
    }
    const reason = "core-divergence-fault";
    this.#clients.forEach((client) => client.pause(reason));
    try {
      await Promise.all(this.#bridges.map((candidate) => candidate.waitForPause()));
      bridge.injectUntrackedInput(0, control, 1);
      await bridge.runExactFrame({ suppressHook: true });
      await bridge.waitForPause();
      bridge.injectUntrackedInput(0, control, 0);
      const checkpoint = this.hashNow();
      return { checkpoint, fault: "untracked-core-frame" };
    } catch (error) {
      this.#fail(error);
      throw error;
    } finally {
      bridge.injectUntrackedInput(0, control, 0);
      this.#clients.forEach((client) => client.resume(reason));
    }
  }

  dropConnection(slot = 1) {
    if (typeof this.#relay.dropConnection !== "function") throw new Error("Transport does not support reconnect fault injection");
    this.#relay.dropConnection(slot);
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

  async #handleState(slot, event) {
    if (slot !== 1) throw new Error("Only the non-authority client accepts transferred state");
    if (this.#state === "synchronizing") {
      const result = await this.#bridges[slot].loadStateAndWait(event.state);
      if (await sha256Bytes(this.#bridges[slot].captureState()) !== event.digest) {
        throw new Error("Initial savestate acknowledgement digest mismatch");
      }
      this.#report.stateLoadEvidence = result;
      return result;
    }
    const result = await this.#clients[slot].applyResync(event.frame, event.state, event.coreDigest);
    this.#report.resyncLoadEvidence.push({ frame: event.frame, ...result });
    return result;
  }

  #handleHashResult(slot, result) {
    this.#clients[slot].receiveHashResult(result);
    const observed = this.#hashes.get(result.frame) ?? new Set();
    observed.add(slot);
    this.#hashes.set(result.frame, observed);
    if (observed.size !== 2) return;
    this.#hashes.delete(result.frame);
    const checkpoint = {
      frame: result.frame,
      matched: result.matched,
      digest: result.matched ? result.digests[0] : null,
      digests: result.matched ? undefined : [...result.digests],
      resynced: false
    };
    this.#report.hashCheckpoints.push(checkpoint);
    if (!result.matched) {
      this.#report.desyncs += 1;
      void this.#resync(result.frame, checkpoint).catch((error) => this.#fail(error));
      return;
    }
    if (result.frame === this.#targetFrame) {
      this.#report.completedAtMs = Date.now();
      this.#setState("complete");
    } else {
      this.#emitStatus();
    }
  }

  async #resync(frame, checkpoint) {
    if (this.#resyncing.has(frame)) return;
    this.#resyncing.add(frame);
    this.#setState("resynchronizing");
    try {
      const state = this.#bridges[0].captureState();
      const digest = await sha256Bytes(state);
      const coreDigest = await sha256CoreState(state);
      const startedAt = performance.now();
      await this.#relay.sendState({ slot: 0, frame, state, digest, coreDigest });
      const settled = await Promise.all(this.#bridges.map((bridge) => sha256CoreState(bridge.captureState())));
      if (settled[0] !== coreDigest || settled[1] !== coreDigest) {
        throw new Error(`Savestate resync failed at frame ${frame}`);
      }
      this.#clients.forEach((client) => client.completeResync(frame, coreDigest));
      checkpoint.resynced = true;
      checkpoint.resyncDigest = coreDigest;
      this.#report.resyncs += 1;
      this.#report.resyncEvents.push({ frame, bytes: state.byteLength, durationMs: performance.now() - startedAt });
      if (frame === this.#targetFrame) {
        this.#report.completedAtMs = Date.now();
        this.#setState("complete");
      } else {
        this.#setState("running");
      }
    } finally {
      this.#resyncing.delete(frame);
    }
  }

  #handleTransportPause(slot, event) {
    this.#clients[slot].pause(event.reason);
    if (!this.#failed && !["synchronizing", "complete"].includes(this.#state)) {
      this.#setState("reconnecting");
    }
  }

  #handleTransportResume(slot, event) {
    this.#clients[slot].resume(event.reason);
    this.#report.reconnectEvents += 1;
    if (this.#state === "reconnecting") this.#setState("running");
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
