import assert from "node:assert/strict";
import test from "node:test";
import { LocalRelay } from "../src/local-relay.js";
import { neutralSnapshot } from "../src/protocol.js";

const tick = () => new Promise((resolve) => setTimeout(resolve, 5));

function connectPair(relay, frames, hashes = []) {
  return [0, 1].map((slot) => relay.connect(slot, {
    onFrame: (frame) => frames[slot].push(frame),
    onHashResult: (result) => hashes.push({ slot, result })
  }));
}

test("publishes one immutable canonical frame after both slots contribute", async () => {
  const relay = new LocalRelay();
  const frames = [[], []];
  const disconnect = connectPair(relay, frames);
  const neutral = neutralSnapshot();
  const pressed = [...neutral];
  pressed[8] = 1;

  await Promise.all([
    relay.sendContribution({ slot: 0, frame: 1, values: pressed }),
    relay.sendContribution({ slot: 1, frame: 1, values: neutral })
  ]);
  await tick();

  assert.equal(frames[0].length, 1);
  assert.equal(frames[1].length, 1);
  assert.deepEqual(frames[0][0].players[0], pressed);
  assert.deepEqual(frames[1][0], frames[0][0]);
  assert.deepEqual(relay.getMetrics(), {
    lastCanonicalFrame: 1,
    nonNeutralFrames: 1,
    inputTransitions: 1,
    pendingFrames: 0,
    retainedCanonicalFrames: 1,
    stateTransfers: 0,
    reconnects: 0
  });
  await relay.sendContribution({ slot: 0, frame: 1, values: pressed });
  await assert.rejects(
    relay.sendContribution({ slot: 0, frame: 1, values: neutral }),
    /immutable/
  );

  disconnect.forEach((close) => close());
  relay.close();
});

test("transfers a bounded savestate to the other logical client", async () => {
  const relay = new LocalRelay();
  const received = [];
  [0, 1].forEach((slot) => relay.connect(slot, {
    onFrame: () => {},
    onHashResult: () => {},
    onState: async (event) => received.push({ slot, event })
  }));
  const state = new Uint8Array([4, 2, 3]);
  await relay.sendState({
    slot: 0,
    frame: 0,
    state,
    digest: "d".repeat(64),
    coreDigest: "e".repeat(64)
  });
  assert.equal(received.length, 1);
  assert.equal(received[0].slot, 1);
  assert.deepEqual(received[0].event.state, state);
  assert.equal(received[0].event.coreDigest, "e".repeat(64));
  assert.equal(relay.getMetrics().stateTransfers, 1);
  relay.close();
});

test("rejects forged slots, changed duplicates, and oversized future frames", async () => {
  const relay = new LocalRelay({ maxFutureFrames: 10 });
  const frames = [[], []];
  connectPair(relay, frames);
  const neutral = neutralSnapshot();
  const changed = [...neutral];
  changed[0] = 1;

  await assert.rejects(
    relay.sendContribution({ slot: 2, frame: 1, values: neutral }),
    /slot/i
  );
  await relay.sendContribution({ slot: 0, frame: 2, values: neutral });
  await assert.rejects(
    relay.sendContribution({ slot: 0, frame: 2, values: changed }),
    /changed its contribution/
  );
  await assert.rejects(
    relay.sendContribution({ slot: 0, frame: 1, values: neutral }),
    /not monotonic/
  );
  await assert.rejects(
    relay.sendContribution({ slot: 1, frame: 11, values: neutral }),
    /future window/
  );
  relay.close();
});

test("compares both state hashes at the same frame", async () => {
  const relay = new LocalRelay();
  const frames = [[], []];
  const hashes = [];
  connectPair(relay, frames, hashes);
  const digestA = "a".repeat(64);
  const digestB = "b".repeat(64);

  await Promise.all([
    relay.sendHash({ slot: 0, frame: 120, digest: digestA }),
    relay.sendHash({ slot: 1, frame: 120, digest: digestB })
  ]);
  await tick();

  assert.equal(hashes.length, 2);
  assert.equal(hashes[0].result.matched, false);
  assert.deepEqual(hashes[0].result.digests, [digestA, digestB]);
  relay.close();
});
