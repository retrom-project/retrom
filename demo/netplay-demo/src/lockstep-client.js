import { assertFrame, assertSlot, neutralSnapshot, normalizeInputValue } from "./protocol.js";
import { sha256Bytes } from "./state-hash.js";

export class LockstepClient {
  #baseFrame = 0;
  #blockers = new Set(["not-started"]);
  #canonical = new Map();
  #cleanup = [];
  #currentFrame = 0;
  #emulator;
  #failed = false;
  #hashEvery;
  #inputDelay;
  #lastAppliedFrame = 0;
  #lastHash = null;
  #localIntent = [...neutralSnapshot()];
  #onError;
  #onStatus;
  #scheduledHashes = new Set();
  #sendContribution;
  #sendHash;
  #slot;
  #stallCount = 0;
  #targetFrame;
  #waiters = new Map();

  constructor({
    slot,
    emulator,
    inputDelay,
    hashEvery,
    targetFrame,
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
    this.#targetFrame = assertFrame(targetFrame);
    this.#emulator = emulator;
    this.#inputDelay = inputDelay;
    this.#hashEvery = hashEvery;
    this.#sendContribution = sendContribution;
    this.#sendHash = sendHash;
    this.#onStatus = onStatus;
    this.#onError = onError;
    this.#scheduledHashes.add(targetFrame);
  }

  attach(baseFrame) {
    if (!Number.isSafeInteger(baseFrame) || baseFrame < 0) throw new TypeError("Invalid base frame");
    this.#baseFrame = baseFrame;
    this.#cleanup.push(this.#emulator.installInputCapture((control, value) => {
      this.captureLocalInput(control, value);
    }));
    this.#cleanup.push(this.#emulator.installFrameHook((rawFrame) => {
      try {
        this.#onFrameEnd(rawFrame);
      } catch (error) {
        this.#fail(error);
      }
    }));
    this.#emitStatus();
  }

  async prime() {
    const contributions = [];
    for (let frame = 1; frame <= this.#inputDelay + 1; frame += 1) {
      contributions.push(this.#contribute(frame));
    }
    await Promise.all(contributions);
  }

  start() {
    if (this.#failed) throw new Error("Cannot start a failed lockstep client");
    this.#blockers.delete("not-started");
    this.#applyNextFrame();
    if (this.#lastAppliedFrame !== 1) throw new Error("Canonical frame 1 is unavailable");
    this.#refreshExecution();
  }

  receiveCanonical(frame) {
    assertFrame(frame.frame);
    this.#canonical.set(frame.frame, frame);
    const waiters = this.#waiters.get(frame.frame) ?? [];
    this.#waiters.delete(frame.frame);
    for (const resolve of waiters) resolve(frame);
    if (!this.#blockers.has("not-started")) this.#applyNextFrame();
    this.#refreshExecution();
  }

  receiveHashResult(result) {
    const blocker = `hash:${result.frame}`;
    if (!this.#blockers.has(blocker)) return;
    if (!result.matched) {
      this.#fail(new Error(`State hash mismatch at net frame ${result.frame}`));
      return;
    }
    this.#lastHash = { frame: result.frame, digest: result.digests[this.#slot] };
    this.#blockers.delete(blocker);
    this.#refreshExecution();
  }

  waitForCanonical(frame) {
    assertFrame(frame);
    const existing = this.#canonical.get(frame);
    if (existing) return Promise.resolve(existing);
    return new Promise((resolve) => {
      const waiters = this.#waiters.get(frame) ?? [];
      waiters.push(resolve);
      this.#waiters.set(frame, waiters);
    });
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
    this.#refreshExecution();
  }

  fail(error) {
    this.#fail(error);
  }

  getMetrics() {
    const futureFrames = [...this.#canonical.keys()].filter((frame) => frame > this.#currentFrame);
    const maxFutureFrame = futureFrames.length ? Math.max(...futureFrames) : this.#currentFrame;
    return {
      slot: this.#slot,
      emulatorFrame: this.#baseFrame + this.#currentFrame,
      netFrame: this.#currentFrame,
      lastAppliedFrame: this.#lastAppliedFrame,
      bufferDepth: Math.max(0, maxFutureFrame - this.#currentFrame),
      waitingFrame: this.#blockers.has("waiting-input") ? this.#currentFrame + 1 : null,
      stallCount: this.#stallCount,
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
    this.#waiters.clear();
    this.#canonical.clear();
  }

  #onFrameEnd(rawFrame) {
    if (this.#failed || this.#blockers.has("not-started")) return;
    const netFrame = rawFrame - this.#baseFrame;
    if (netFrame <= this.#currentFrame) return;
    if (netFrame !== this.#currentFrame + 1) {
      throw new Error(`Frame discontinuity: expected ${this.#currentFrame + 1}, received ${netFrame}`);
    }
    if (this.#lastAppliedFrame !== netFrame) {
      throw new Error(`Frame ${netFrame} ran without canonical input`);
    }
    this.#currentFrame = netFrame;
    void this.#contribute(netFrame + this.#inputDelay + 1).catch((error) => this.#fail(error));

    if (netFrame % this.#hashEvery === 0 || this.#scheduledHashes.has(netFrame)) {
      this.#scheduledHashes.delete(netFrame);
      this.#captureHash(netFrame);
    }
    if (netFrame >= this.#targetFrame) this.#blockers.add("target-reached");
    this.#applyNextFrame();
    this.#refreshExecution();
  }

  #applyNextFrame() {
    if (this.#failed) return;
    const nextFrame = this.#currentFrame + 1;
    if (this.#lastAppliedFrame === nextFrame) return;
    if (this.#lastAppliedFrame !== this.#currentFrame) {
      this.#fail(new Error(`Cannot apply frame ${nextFrame} after ${this.#lastAppliedFrame}`));
      return;
    }
    const frame = this.#canonical.get(nextFrame);
    if (!frame) {
      if (!this.#blockers.has("waiting-input")) this.#stallCount += 1;
      this.#blockers.add("waiting-input");
      return;
    }
    this.#emulator.applyInputs(frame.players);
    this.#lastAppliedFrame = nextFrame;
    this.#canonical.delete(nextFrame);
    this.#blockers.delete("waiting-input");
  }

  async #captureHash(frame) {
    const blocker = `hash:${frame}`;
    this.#blockers.add(blocker);
    this.#refreshExecution();
    try {
      const state = this.#emulator.captureState();
      const digest = await sha256Bytes(state);
      await this.#sendHash({ slot: this.#slot, frame, digest });
    } catch (error) {
      this.#fail(error);
    }
  }

  #contribute(frame) {
    return this.#sendContribution({
      slot: this.#slot,
      frame,
      values: [...this.#localIntent]
    });
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
