import assert from "node:assert/strict";
import test from "node:test";
import { NetplayHub } from "../server/netplay-hub.mjs";
import { neutralSnapshot } from "../src/protocol.js";

const tick = () => new Promise((resolve) => setTimeout(resolve, 10));

class FakeConnection {
  messages = [];
  onClose = () => {};
  onMessage = () => {};

  sendJson(message) {
    this.messages.push(structuredClone(message));
  }

  receive(message) {
    this.onMessage(JSON.stringify(message));
  }

  close() {
    this.onClose();
  }
}

test("authoritative hub canonicalizes two WebSocket peers and compares hashes", async () => {
  const hub = new NetplayHub();
  const peers = [new FakeConnection(), new FakeConnection()];
  peers.forEach((connection, slot) => hub.connect({ roomId: "test-room", slot, connection }));
  const pressed = [...neutralSnapshot()];
  pressed[3] = 1;

  peers[0].receive({ type: "contribution", frame: 1, values: pressed });
  peers[1].receive({ type: "contribution", frame: 1, values: neutralSnapshot() });
  await tick();

  for (const peer of peers) {
    const frame = peer.messages.find((message) => message.type === "frame");
    assert.deepEqual(frame.players[0], pressed);
    const metrics = peer.messages.find((message) => message.type === "metrics").metrics;
    assert.equal(metrics.nonNeutralFrames, 1);
    assert.equal(metrics.inputTransitions, 1);
  }

  const digest = "c".repeat(64);
  peers[0].receive({ type: "hash", frame: 1, digest });
  peers[1].receive({ type: "hash", frame: 1, digest });
  await tick();
  assert.equal(peers[0].messages.find((message) => message.type === "hash-result").matched, true);
  peers[0].close();
  assert.equal(peers[1].messages.find((message) => message.type === "pause").reason, "peer-disconnected");
  peers[1].close();

  const replacement = new FakeConnection();
  assert.doesNotThrow(() => hub.connect({ roomId: "test-room", slot: 0, connection: replacement }));
  hub.close();
});

test("authoritative hub rejects a duplicate slot in one room", () => {
  const hub = new NetplayHub();
  hub.connect({ roomId: "duplicate-room", slot: 0, connection: new FakeConnection() });
  assert.throws(
    () => hub.connect({ roomId: "duplicate-room", slot: 0, connection: new FakeConnection() }),
    /already connected/
  );
  hub.close();
});
