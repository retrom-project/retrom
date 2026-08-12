import assert from "node:assert/strict";
import test from "node:test";
import { LocalRelay } from "../src/local-relay.js";
import { LockstepClient } from "../src/lockstep-client.js";

const tick = () => new Promise((resolve) => setTimeout(resolve, 3));

class FakeEmulator {
  constructor(baseFrame) {
    this.baseFrame = baseFrame;
    this.rawFrame = baseFrame;
    this.playing = false;
    this.applied = [];
    this.inputCapture = null;
    this.frameHook = null;
  }

  installInputCapture(callback) {
    this.inputCapture = callback;
    return () => { this.inputCapture = null; };
  }

  installFrameHook(callback) {
    this.frameHook = callback;
    return () => { this.frameHook = null; };
  }

  applyInputs(players) {
    this.applied.push({ frameToRun: this.rawFrame - this.baseFrame + 1, players: structuredClone(players) });
  }

  captureState() {
    return new Uint8Array([this.rawFrame - this.baseFrame, 19, 87, 4]);
  }

  pause() { this.playing = false; }
  resume() { this.playing = true; }

  runFrame() {
    if (!this.playing) return false;
    this.rawFrame += 1;
    this.frameHook(this.rawFrame);
    return true;
  }
}

test("schedules both local intents into the same future canonical frame", async () => {
  const relay = new LocalRelay();
  const emulators = [new FakeEmulator(100), new FakeEmulator(100)];
  const errors = [];
  const clients = emulators.map((emulator, slot) => new LockstepClient({
    slot,
    emulator,
    inputDelay: 3,
    hashEvery: 5,
    targetFrame: 10,
    sendContribution: (message) => relay.sendContribution(message),
    sendHash: (message) => relay.sendHash(message),
    onError: (error) => errors.push(error)
  }));
  [0, 1].forEach((slot) => relay.connect(slot, {
    onFrame: (frame) => clients[slot].receiveCanonical(frame),
    onHashResult: (result) => clients[slot].receiveHashResult(result)
  }));

  clients.forEach((client) => client.attach(100));
  await Promise.all(clients.map((client) => client.prime()));
  await Promise.all(clients.map((client) => client.waitForCanonical(1)));
  clients.forEach((client) => client.start());
  clients[0].captureLocalInput(8, 1);
  clients[1].captureLocalInput(0, 1);

  let safety = 0;
  while (clients.some((client) => client.getMetrics().netFrame < 10) && safety < 200) {
    emulators.forEach((emulator) => emulator.runFrame());
    await tick();
    safety += 1;
  }
  await tick();

  assert.equal(errors.length, 0);
  assert.deepEqual(clients.map((client) => client.getMetrics().netFrame), [10, 10]);
  assert.equal(clients[0].getMetrics().lastHash.frame, 10);
  assert.equal(clients[1].getMetrics().lastHash.frame, 10);
  for (const emulator of emulators) {
    const frameFive = emulator.applied.find((entry) => entry.frameToRun === 5);
    assert.equal(frameFive.players[0][8], 1);
    assert.equal(frameFive.players[1][0], 1);
    assert.equal(emulator.playing, false);
  }
  clients.forEach((client) => client.cleanup());
  for (const emulator of emulators) {
    assert.equal(emulator.inputCapture, null);
    assert.equal(emulator.frameHook, null);
  }
  relay.close();
});
