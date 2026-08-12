import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";
import { NetplayHub } from "../server/netplay-hub.mjs";
import { neutralSnapshot } from "../src/protocol.js";

const tick = (delay = 10) => new Promise((resolve) => setTimeout(resolve, delay));
const profileDigest = "a".repeat(64);

class FakeConnection {
  messages = [];
  onClose = () => {};
  onMessage = () => {};

  sendJson(message) { this.messages.push(structuredClone(message)); }
  receive(message) { this.onMessage(JSON.stringify(message)); }
  close() { this.onClose(); }
}

function connectRoom(hub, room, peers, resumeTokens = []) {
  peers.forEach((connection, slot) => hub.connect({
    roomId: room.roomId,
    slot,
    joinToken: room.participants[slot].joinToken,
    resumeToken: resumeTokens[slot] ?? null,
    profileDigest,
    connection
  }));
}

test("room credentials gate canonical input, state transfer, and hash comparison", async () => {
  const hub = new NetplayHub();
  const room = hub.createRoom({ profileDigest });
  const peers = [new FakeConnection(), new FakeConnection()];
  connectRoom(hub, room, peers);
  const pressed = [...neutralSnapshot()];
  pressed[3] = 1;

  peers[0].receive({ type: "contribution", frame: 1, values: pressed });
  peers[1].receive({ type: "contribution", frame: 1, values: neutralSnapshot() });
  await tick();
  for (const peer of peers) {
    const frame = peer.messages.find((message) => message.type === "frame");
    assert.deepEqual(frame.players[0], pressed);
    assert.equal(peer.messages.find((message) => message.type === "metrics").metrics.nonNeutralFrames, 1);
  }

  const state = Buffer.from([1, 2, 3, 4, 5]);
  const digest = createHash("sha256").update(state).digest("hex");
  peers[0].receive({
    type: "state",
    transferId: "state-1",
    frame: 1,
    digest,
    coreDigest: "f".repeat(64),
    state: state.toString("base64")
  });
  await tick();
  assert.equal(peers[1].messages.find((message) => message.type === "state").digest, digest);
  assert.equal(peers[1].messages.find((message) => message.type === "state").coreDigest, "f".repeat(64));
  peers[1].receive({ type: "state-applied", transferId: "state-1", digest });
  await tick();
  assert.equal(peers[0].messages.find((message) => message.type === "state-applied").digest, digest);

  peers[0].receive({ type: "hash", frame: 1, digest });
  peers[1].receive({ type: "hash", frame: 1, digest });
  await tick();
  assert.equal(peers[0].messages.find((message) => message.type === "hash-result").matched, true);
  hub.close();
});

test("a disconnected participant reclaims its leased slot with the opaque resume token", async () => {
  const hub = new NetplayHub({ leaseMs: 100, roomTtlMs: 1000 });
  const room = hub.createRoom({ profileDigest });
  const peers = [new FakeConnection(), new FakeConnection()];
  connectRoom(hub, room, peers);
  const hello = peers[0].messages.find((message) => message.type === "hello");
  peers[0].receive({ type: "contribution", frame: 1, values: neutralSnapshot() });
  peers[1].receive({ type: "contribution", frame: 1, values: neutralSnapshot() });
  peers[0].close();
  assert.equal(peers[1].messages.find((message) => message.type === "pause").reason, "peer-disconnected");
  await tick();

  const replacement = new FakeConnection();
  hub.connect({
    roomId: room.roomId,
    slot: 0,
    joinToken: room.participants[0].joinToken,
    resumeToken: hello.resumeToken,
    profileDigest,
    afterFrame: 0,
    connection: replacement
  });
  await tick();
  assert.equal(replacement.messages.find((message) => message.type === "hello").resumed, true);
  assert.equal(replacement.messages.find((message) => message.type === "frame").frame, 1);
  assert.ok(
    replacement.messages.findIndex((message) => message.type === "frame")
      < replacement.messages.findIndex((message) => message.type === "resume"),
    "history must be delivered before simulation resumes"
  );
  assert.equal(peers[1].messages.find((message) => message.type === "resume").reason, "peer-disconnected");
  hub.close();
});

test("room rejects guessed slots, wrong profiles, and expired resume leases", async () => {
  const hub = new NetplayHub({ leaseMs: 100, roomTtlMs: 500 });
  const room = hub.createRoom({ profileDigest });
  assert.throws(() => hub.connect({
    roomId: room.roomId,
    slot: 0,
    joinToken: "guessed",
    profileDigest,
    connection: new FakeConnection()
  }), /participant token/);
  assert.throws(() => hub.connect({
    roomId: room.roomId,
    slot: 0,
    joinToken: room.participants[0].joinToken,
    profileDigest: "b".repeat(64),
    connection: new FakeConnection()
  }), /profile/);

  const first = new FakeConnection();
  hub.connect({
    roomId: room.roomId,
    slot: 0,
    joinToken: room.participants[0].joinToken,
    profileDigest,
    connection: first
  });
  first.close();
  await tick(130);
  assert.throws(() => hub.connect({
    roomId: room.roomId,
    slot: 0,
    joinToken: room.participants[0].joinToken,
    resumeToken: "expired",
    profileDigest,
    connection: new FakeConnection()
  }), /Unexpected resume token/);
  hub.close();
});

test("room registry enforces capacity and expires inactive rooms", async () => {
  const hub = new NetplayHub({ leaseMs: 100, roomTtlMs: 100, maxRooms: 1 });
  const room = hub.createRoom({ profileDigest });
  assert.throws(() => hub.createRoom({ profileDigest }), /capacity/);
  assert.ok(hub.getRoomStatus(room.roomId));
  await tick(130);
  assert.equal(hub.getRoomStatus(room.roomId), null);
  assert.doesNotThrow(() => hub.createRoom({ profileDigest }));
  hub.close();
});
