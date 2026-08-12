import assert from "node:assert/strict";
import test from "node:test";
import { RollbackClient } from "../src/rollback-client.js";
import { canonicalFrame, neutralSnapshot } from "../src/protocol.js";

const tick = () => new Promise((resolve) => setTimeout(resolve, 5));

class FakeEmulator {
  constructor() {
    this.applied = [neutralSnapshot(), neutralSnapshot()];
    this.frameHook = null;
    this.inputCapture = null;
    this.playing = false;
    this.rawFrame = 100;
    this.replay = { active: false, frames: 0, mutedRuns: 0, runs: 0, totalMs: 0 };
    this.state = 7;
  }

  installInputCapture(callback) {
    this.inputCapture = callback;
    return () => { this.inputCapture = null; };
  }

  installFrameHook(callback) {
    this.frameHook = callback;
    return () => { this.frameHook = null; };
  }

  applyInputs(players) { this.applied = structuredClone(players); }
  captureState() { return new Uint8Array(new Uint32Array([this.state]).buffer); }
  getFrame() { return this.rawFrame; }
  getReplayMetrics() { return { ...this.replay }; }
  pause() { this.playing = false; }
  resume() { this.playing = true; }

  async loadStateAndWait(bytes) {
    this.state = new Uint32Array(new Uint8Array(bytes).buffer.slice(0))[0];
    return { changed: true, stateBytes: bytes.byteLength };
  }

  beginReplay() {
    this.replay.active = true;
    this.replay.runs += 1;
    this.replay.mutedRuns += 1;
  }

  endReplay(frames) {
    this.replay.active = false;
    this.replay.frames += frames;
  }

  async runExactFrame() { this.#advance(); }
  runFrame() {
    if (!this.playing) return false;
    this.#advance();
    return true;
  }

  #advance() {
    const inputWeight = this.applied.reduce(
      (total, snapshot, slot) => total + snapshot.reduce((sum, value, control) => sum + value * (slot + 1) * (control + 1), 0),
      0
    );
    this.state = (Math.imul(this.state, 33) + inputWeight + 17) >>> 0;
    this.rawFrame += 1;
    this.frameHook?.(this.rawFrame);
  }
}

test("late remote input rewinds to a retained state and replays with output muted", async () => {
  const emulator = new FakeEmulator();
  const errors = [];
  const contributions = [];
  const client = new RollbackClient({
    slot: 0,
    emulator,
    inputDelay: 8,
    hashEvery: 10,
    targetFrame: 10,
    sendContribution: async (message) => contributions.push(message),
    sendHash: async () => {},
    onError: (error) => errors.push(error)
  });
  client.attach();
  client.start();
  assert.equal(emulator.runFrame(), true);
  assert.equal(emulator.runFrame(), true);
  assert.equal(emulator.runFrame(), true);

  const remotePressed = [...neutralSnapshot()];
  remotePressed[8] = 1;
  client.receiveCanonical(canonicalFrame(1, [neutralSnapshot(), remotePressed]));
  for (let count = 0; count < 20 && client.getMetrics().rollbackCount === 0; count += 1) await tick();

  const metrics = client.getMetrics();
  assert.equal(errors.length, 0);
  assert.equal(metrics.netFrame, 3);
  assert.equal(metrics.rollbackCount, 1);
  assert.equal(metrics.rollbackFrames, 3);
  assert.equal(metrics.replay.runs, 1);
  assert.equal(metrics.replay.mutedRuns, 1);
  assert.equal(metrics.replay.frames, 3);
  assert.deepEqual(contributions.map(({ frame }) => frame), [1, 2, 3, 4]);
  client.cleanup();
  assert.equal(emulator.frameHook, null);
  assert.equal(emulator.inputCapture, null);
});

test("prediction is bounded until canonical input advances", async () => {
  const emulator = new FakeEmulator();
  const client = new RollbackClient({
    slot: 0,
    emulator,
    inputDelay: 2,
    hashEvery: 10,
    targetFrame: 10,
    sendContribution: async () => {},
    sendHash: async () => {}
  });
  client.attach();
  client.start();
  assert.equal(emulator.runFrame(), true);
  assert.equal(emulator.runFrame(), true);
  assert.equal(emulator.runFrame(), false);
  assert.equal(client.getMetrics().waitingFrame, 3);
  client.receiveCanonical(canonicalFrame(1, [neutralSnapshot(), neutralSnapshot()]));
  await tick();
  assert.equal(emulator.runFrame(), true);
  client.cleanup();
});
