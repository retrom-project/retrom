import {
  PLAYER_COUNT,
  assertFrame,
  assertSlot,
  canonicalFrame,
  neutralSnapshot,
  normalizeInputValue,
  sameSnapshot
} from "./protocol.js";
import { sha256CoreState } from "./state-hash.js";

function samePlayers(left, right) {
  return left.every((snapshot, slot) => sameSnapshot(snapshot, right[slot]));
}

export class RollbackClient {
  #applied = new Map();
  #blockers = new Set(["not-started"]);
  #canonical = new Map();
  #cleanup = [];
  #confirmedFrame = 0;
  #contributed = new Set();
  #currentFrame = 0;
  #emulator;
  #failed = false;
  #hashEvery;
  #hashSent = new Set();
  #inFrameHook = false;
  #lastAppliedFrame = 0;
  #lastConfirmedRemote = [...neutralSnapshot()];
  #lastHash = null;
  #localIntent = [...neutralSnapshot()];
  #maxPredictionFrames;
  #maxRollbackFrames;
  #onError;
  #onStatus;
  #pendingCheckpoint = null;
  #pendingRollbackFrame = null;
  #replaying = false;
  #resyncs = 0;
  #rollbackFrames = 0;
  #rollbacks = 0;
  #scheduledHashes = new Set();
  #sendContribution;
  #sendHash;
  #slot;
  #stallCount = 0;
  #stateBefore = new Map();
  #targetFrame;

  constructor({
    slot,
    emulator,
    inputDelay,
    hashEvery,
    targetFrame,
    maxRollbackFrames = 120,
    sendContribution,
    sendHash,
    onStatus = () => {},
    onError = () => {}
  }) {
    this.#slot = assertSlot(slot);
    if (!emulator) throw new TypeError("emulator is required");
    if (!Number.isInteger(inputDelay) || inputDelay < 1 || inputDelay > 30) {
      throw new TypeError("inputDelay must be between 1 and 30 frames");
    }
    if (!Number.isInteger(hashEvery) || hashEvery < 1) {
      throw new TypeError("hashEvery must be a positive integer");
    }
    if (!Number.isInteger(maxRollbackFrames) || maxRollbackFrames < inputDelay) {
      throw new TypeError("maxRollbackFrames must cover the prediction window");
    }
    this.#emulator = emulator;
    this.#hashEvery = hashEvery;
    this.#maxPredictionFrames = inputDelay;
    this.#maxRollbackFrames = maxRollbackFrames;
    this.#onError = onError;
    this.#onStatus = onStatus;
    this.#sendContribution = sendContribution;
    this.#sendHash = sendHash;
    this.#targetFrame = assertFrame(targetFrame);
    this.#scheduledHashes.add(this.#targetFrame);
  }

  attach() {
    this.#cleanup.push(this.#emulator.installInputCapture((control, value) => {
      this.captureLocalInput(control, value);
    }));
    this.#cleanup.push(this.#emulator.installFrameHook(() => {
      if (this.#replaying) return;
      try {
        this.#onFrameEnd();
      } catch (error) {
        this.#fail(error);
      }
    }));
    this.#stateBefore.set(1, this.#emulator.captureState());
    this.#emitStatus();
  }

  start() {
    if (this.#failed) throw new Error("Cannot start a failed rollback client");
    this.#blockers.delete("not-started");
    this.#prepareNextFrame();
    this.#refreshExecution();
  }

  receiveCanonical(frame) {
    const normalized = canonicalFrame(frame.frame, frame.players);
    this.#canonical.set(normalized.frame, normalized);
    while (this.#canonical.has(this.#confirmedFrame + 1)) {
      this.#confirmedFrame += 1;
      this.#lastConfirmedRemote = [
        ...this.#canonical.get(this.#confirmedFrame).players[1 - this.#slot]
      ];
    }

    const applied = this.#applied.get(normalized.frame);
    if (applied && !samePlayers(applied.players, normalized.players)) {
      if (normalized.frame <= this.#currentFrame) this.#requestRollback(normalized.frame);
      else {
        this.#emulator.applyInputs(normalized.players, { force: true });
        this.#applied.set(normalized.frame, normalized);
      }
    }
    this.#blockers.delete("prediction-window");
    this.#tryFinalizeCheckpoint();
    this.#prepareNextFrame();
    this.#refreshExecution();
  }

  receiveHashResult(result) {
    const blocker = `hash:${result.frame}`;
    if (!this.#blockers.has(blocker)) return;
    if (!result.matched) {
      this.#blockers.add(`resync:${result.frame}`);
      this.#refreshExecution();
      return;
    }
      this.#lastHash = { frame: result.frame, digest: result.digests[this.#slot] };
      this.#blockers.delete(blocker);
      this.#blockers.delete(`checkpoint:${result.frame}`);
      if (this.#pendingCheckpoint === result.frame) this.#pendingCheckpoint = null;
      this.#prepareNextFrame();
    this.#refreshExecution();
  }

  async applyResync(frame, state, coreDigest) {
    assertFrame(frame);
    this.pause(`resync:${frame}`);
    const currentCoreDigest = await sha256CoreState(this.#emulator.captureState());
    const result = currentCoreDigest === coreDigest
      ? { changed: false, coreAlreadyMatched: true, stateBytes: state.byteLength }
      : await this.#emulator.loadStateAndWait(state);
    if (result.changed) {
      this.#emulator.applyInputs(this.#predictionFor(frame + 1).players, { force: true });
    }
    this.#stateBefore.set(frame + 1, this.#emulator.captureState());
    this.#currentFrame = frame;
    this.#lastAppliedFrame = frame;
    this.#resyncs += 1;
    this.#trimHistory();
    this.#emitStatus();
    return result;
  }

  completeResync(frame, digest) {
    this.#lastHash = { frame, digest };
    this.#blockers.delete(`hash:${frame}`);
    this.#blockers.delete(`checkpoint:${frame}`);
    this.#blockers.delete(`resync:${frame}`);
    this.#pendingCheckpoint = null;
    this.#prepareNextFrame();
    this.#refreshExecution();
  }

  captureLocalInput(control, value) {
    if (!Number.isInteger(control) || control < 0 || control >= this.#localIntent.length) return;
    this.#localIntent[control] = normalizeInputValue(value);
    this.#emitStatus();
  }

  scheduleHash(frame) {
    this.#scheduledHashes.add(assertFrame(frame));
  }

  pause(reason = "manual") {
    this.#blockers.add(reason);
    this.#refreshExecution();
  }

  resume(reason = "manual") {
    this.#blockers.delete(reason);
    this.#prepareNextFrame();
    this.#refreshExecution();
  }

  fail(error) {
    this.#fail(error);
  }

  getMetrics() {
    return {
      slot: this.#slot,
      emulatorFrame: this.#emulator.getFrame?.() ?? null,
      netFrame: this.#currentFrame,
      confirmedFrame: this.#confirmedFrame,
      predictionDepth: Math.max(0, this.#currentFrame - this.#confirmedFrame),
      bufferDepth: Math.max(0, this.#confirmedFrame - this.#currentFrame),
      waitingFrame: this.#blockers.has("prediction-window") ? this.#currentFrame + 1 : null,
      stallCount: this.#stallCount,
      rollbackCount: this.#rollbacks,
      rollbackFrames: this.#rollbackFrames,
      resyncCount: this.#resyncs,
      replay: this.#emulator.getReplayMetrics?.() ?? null,
      blockers: [...this.#blockers].sort(),
      lastHash: this.#lastHash,
      localIntent: [...this.#localIntent],
      failed: this.#failed
    };
  }

  cleanup() {
    this.#blockers.add("cleanup");
    this.#emulator.pause();
    while (this.#cleanup.length) this.#cleanup.pop()();
    this.#canonical.clear();
    this.#applied.clear();
    this.#stateBefore.clear();
  }

  #onFrameEnd() {
    if (this.#inFrameHook) return;
    this.#inFrameHook = true;
    try {
    if (this.#failed || this.#blockers.has("not-started")) return;
    const completedFrame = this.#currentFrame + 1;
    if (this.#lastAppliedFrame !== completedFrame) {
      throw new Error(`Net frame ${completedFrame} ran without predicted or canonical input`);
    }
    this.#currentFrame = completedFrame;
    this.#stateBefore.set(completedFrame + 1, this.#emulator.captureState());
    this.#trimHistory();

    if (completedFrame % this.#hashEvery === 0 || this.#scheduledHashes.has(completedFrame)) {
      this.#scheduledHashes.delete(completedFrame);
      this.#pendingCheckpoint = completedFrame;
      this.#blockers.add(`checkpoint:${completedFrame}`);
    }
    if (completedFrame >= this.#targetFrame) this.#blockers.add("target-reached");
    this.#refreshExecution();
    this.#tryFinalizeCheckpoint();
    this.#prepareNextFrame();
    this.#refreshExecution();
    } finally {
      this.#inFrameHook = false;
    }
  }

  #predictionFor(frame) {
    const known = this.#canonical.get(frame);
    if (known) return known;
    const players = Array.from({ length: PLAYER_COUNT }, () => [...neutralSnapshot()]);
    players[this.#slot] = [...this.#localIntent];
    players[1 - this.#slot] = [...this.#lastConfirmedRemote];
    return canonicalFrame(frame, players);
  }

  #prepareNextFrame() {
    if (this.#failed
      || this.#replaying
      || this.#pendingCheckpoint !== null
      || this.#blockers.has("not-started")) return;
    const nextFrame = this.#currentFrame + 1;
    if (nextFrame > this.#targetFrame) return;
    this.#contribute(nextFrame);
    if (nextFrame > this.#confirmedFrame + this.#maxPredictionFrames) {
      if (!this.#blockers.has("prediction-window")) this.#stallCount += 1;
      this.#blockers.add("prediction-window");
      return;
    }
    if (this.#lastAppliedFrame === nextFrame) return;
    const prediction = this.#predictionFor(nextFrame);
    this.#emulator.applyInputs(prediction.players);
    this.#applied.set(nextFrame, prediction);
    this.#lastAppliedFrame = nextFrame;
    this.#blockers.delete("prediction-window");
  }

  #contribute(frame) {
    if (this.#contributed.has(frame)) return;
    this.#contributed.add(frame);
    void this.#sendContribution({
      slot: this.#slot,
      frame,
      values: [...this.#localIntent]
    }).catch((error) => this.#fail(error));
  }

  #requestRollback(frame) {
    this.#pendingRollbackFrame = this.#pendingRollbackFrame === null
      ? frame
      : Math.min(this.#pendingRollbackFrame, frame);
    this.#blockers.add("rollback");
    this.#refreshExecution();
    void this.#performRollbacks().catch((error) => this.#fail(error));
  }

  async #performRollbacks() {
    if (this.#replaying || this.#failed) return;
    this.#replaying = true;
    try {
      while (this.#pendingRollbackFrame !== null) {
        const firstFrame = this.#pendingRollbackFrame;
        this.#pendingRollbackFrame = null;
        const targetFrame = this.#currentFrame;
        const state = this.#stateBefore.get(firstFrame);
        if (!state) throw new Error(`Rollback frame ${firstFrame} fell outside retained history`);
        const replayCount = targetFrame - firstFrame + 1;
        if (replayCount > this.#maxRollbackFrames) {
          throw new Error(`Rollback distance ${replayCount} exceeds ${this.#maxRollbackFrames} frames`);
        }

        this.#emulator.beginReplay();
        try {
          await this.#emulator.loadStateAndWait(state);
          for (let frame = firstFrame; frame <= targetFrame; frame += 1) {
            const corrected = this.#canonical.get(frame) ?? this.#applied.get(frame);
            if (!corrected) throw new Error(`Replay input for frame ${frame} is unavailable`);
            this.#emulator.applyInputs(corrected.players, { force: true });
            await this.#emulator.runExactFrame();
            this.#applied.set(frame, corrected);
            this.#stateBefore.set(frame + 1, this.#emulator.captureState());
          }
        } finally {
          this.#emulator.endReplay(replayCount);
        }
        this.#lastAppliedFrame = targetFrame;
        this.#rollbacks += 1;
        this.#rollbackFrames += replayCount;

        for (let frame = firstFrame; frame <= targetFrame; frame += 1) {
          const canonical = this.#canonical.get(frame);
          const applied = this.#applied.get(frame);
          if (canonical && applied && !samePlayers(canonical.players, applied.players)) {
            this.#pendingRollbackFrame = this.#pendingRollbackFrame === null
              ? frame
              : Math.min(this.#pendingRollbackFrame, frame);
          }
        }
      }
    } finally {
      this.#replaying = false;
      this.#blockers.delete("rollback");
    }
    this.#tryFinalizeCheckpoint();
    this.#prepareNextFrame();
    this.#refreshExecution();
  }

  #tryFinalizeCheckpoint() {
    const frame = this.#pendingCheckpoint;
    if (frame === null || this.#hashSent.has(frame) || this.#replaying) return;
    if (this.#confirmedFrame < frame || this.#pendingRollbackFrame !== null) return;
    const blocker = `hash:${frame}`;
    this.#hashSent.add(frame);
    this.#blockers.add(blocker);
    void (async () => {
      try {
        const digest = await sha256CoreState(this.#emulator.captureState());
        await this.#sendHash({ slot: this.#slot, frame, digest });
      } catch (error) {
        this.#fail(error);
      }
    })();
  }

  #trimHistory() {
    const cutoff = this.#currentFrame - this.#maxRollbackFrames;
    for (const frame of this.#stateBefore.keys()) {
      if (frame < cutoff) this.#stateBefore.delete(frame);
    }
    for (const frame of this.#applied.keys()) {
      if (frame < cutoff) this.#applied.delete(frame);
    }
    for (const frame of this.#canonical.keys()) {
      if (frame < cutoff) this.#canonical.delete(frame);
    }
  }

  #refreshExecution() {
    if (this.#blockers.size === 0) this.#emulator.resume();
    else this.#emulator.pause();
    this.#emitStatus();
  }

  #emitStatus() {
    this.#onStatus(this.getMetrics());
  }

  #fail(error) {
    if (this.#failed) return;
    this.#failed = true;
    this.#blockers.add("failed");
    this.#emulator.pause();
    this.#emitStatus();
    this.#onError(error instanceof Error ? error : new Error(String(error)));
  }
}
