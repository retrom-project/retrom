import { createHash, randomBytes, randomUUID } from "node:crypto";
import { LocalRelay } from "../src/local-relay.js";
import { assertSlot } from "../src/protocol.js";

const DIGEST_PATTERN = /^[0-9a-f]{64}$/;
const ROOM_ID_PATTERN = /^[a-zA-Z0-9_-]{1,80}$/;
const TRANSFER_ID_PATTERN = /^[a-zA-Z0-9_-]{1,100}$/;
const MAX_STATE_BYTES = 1024 * 1024;

function token() {
  return randomBytes(24).toString("base64url");
}

function timer(callback, delayMs) {
  const handle = setTimeout(callback, delayMs);
  handle.unref?.();
  return handle;
}

export class NetplayHub {
  #leaseMs;
  #maxRooms;
  #roomTtlMs;
  #rooms = new Map();

  constructor({ leaseMs = 10000, roomTtlMs = 300000, maxRooms = 100 } = {}) {
    if (!Number.isInteger(leaseMs) || leaseMs < 100) throw new TypeError("leaseMs is too small");
    if (!Number.isInteger(roomTtlMs) || roomTtlMs < leaseMs) throw new TypeError("roomTtlMs is too small");
    if (!Number.isInteger(maxRooms) || maxRooms < 1) throw new TypeError("maxRooms must be positive");
    this.#leaseMs = leaseMs;
    this.#roomTtlMs = roomTtlMs;
    this.#maxRooms = maxRooms;
  }

  createRoom({ profileDigest }) {
    if (!DIGEST_PATTERN.test(profileDigest)) throw new TypeError("Invalid profile digest");
    if (this.#rooms.size >= this.#maxRooms) throw new Error("Room capacity reached");
    const roomId = randomUUID();
    const room = {
      expiresAtMs: 0,
      expiryTimer: null,
      joinTokens: [token(), token()],
      leases: new Map(),
      pendingStates: new Map(),
      profileDigest,
      reconnects: 0,
      relay: new LocalRelay(),
      roomId,
      stateTransfers: 0
    };
    this.#rooms.set(roomId, room);
    this.#touch(room);
    return {
      roomId,
      profileDigest,
      expiresAtMs: room.expiresAtMs,
      participants: room.joinTokens.map((joinToken, slot) => ({ slot, joinToken }))
    };
  }

  getRoomStatus(roomId) {
    const room = this.#rooms.get(roomId);
    if (!room) return null;
    return {
      roomId,
      profileDigest: room.profileDigest,
      connectedSlots: [...room.leases.values()].filter((lease) => lease.connection).map((lease) => lease.slot),
      expiresAtMs: room.expiresAtMs,
      metrics: this.#metrics(room)
    };
  }

  connect({
    roomId,
    slot,
    joinToken,
    resumeToken = null,
    profileDigest,
    afterFrame = 0,
    afterHashFrame = 0,
    connection
  }) {
    if (!ROOM_ID_PATTERN.test(roomId)) throw new TypeError("Invalid room id");
    assertSlot(slot);
    if (!Number.isSafeInteger(afterFrame) || afterFrame < 0) throw new TypeError("Invalid replay frame");
    if (!Number.isSafeInteger(afterHashFrame) || afterHashFrame < 0) throw new TypeError("Invalid hash replay frame");
    const room = this.#rooms.get(roomId);
    if (!room) throw new Error("Room does not exist or expired");
    if (joinToken !== room.joinTokens[slot]) throw new Error("Invalid room participant token");
    if (profileDigest !== room.profileDigest) throw new Error("Core/content profile does not match the room");

    let lease = room.leases.get(slot);
    const resumed = Boolean(lease);
    if (lease) {
      if (lease.connection) throw new Error(`Slot ${slot} is already connected`);
      if (resumeToken !== lease.resumeToken) throw new Error("Invalid resume token");
      clearTimeout(lease.expiryTimer);
      lease.expiryTimer = null;
      lease.connection = connection;
    } else {
      if (resumeToken) throw new Error("Unexpected resume token");
      lease = {
        connection,
        disconnectRelay: null,
        expiryTimer: null,
        resumeToken: token(),
        slot
      };
      lease.disconnectRelay = room.relay.connect(slot, {
        onFrame: (frame) => {
          lease.connection?.sendJson(frame);
          lease.connection?.sendJson({ type: "metrics", metrics: this.#metrics(room) });
        },
        onHashResult: (result) => lease.connection?.sendJson(result),
        onPause: (event) => lease.connection?.sendJson({ type: "pause", ...event }),
        onState: async (event) => {
          if (!lease.connection) throw new Error(`Slot ${slot} is disconnected`);
          lease.connection.sendJson({
            type: "state",
            transferId: event.transferId,
            frame: event.frame,
            digest: event.digest,
            coreDigest: event.coreDigest,
            state: Buffer.from(event.state).toString("base64")
          });
        }
      });
      room.leases.set(slot, lease);
    }

    let disconnected = false;
    const disconnect = () => {
      if (disconnected || lease.connection !== connection) return;
      disconnected = true;
      lease.connection = null;
      this.#broadcast(room, { type: "pause", reason: "peer-disconnected", slot });
      lease.expiryTimer = timer(() => this.#expireLease(room, lease), this.#leaseMs);
      this.#touch(room);
    };
    connection.onMessage = (text) => {
      void this.#handleMessage(room, lease, connection, text);
    };
    connection.onClose = disconnect;
    connection.sendJson({
      type: "hello",
      roomId,
      slot,
      resumeToken: lease.resumeToken,
      resumed,
      leaseMs: this.#leaseMs
    });
    if (resumed) {
      room.reconnects += 1;
      void room.relay.replay(slot, { afterFrame, afterHashFrame }).then(() => {
        this.#broadcast(room, { type: "resume", reason: "peer-disconnected", slot });
      }).catch((error) => {
        const message = error instanceof Error ? error.message : String(error);
        connection.sendJson({ type: "error", message });
        connection.close(1011, message);
      });
    }
    this.#touch(room);
    return disconnect;
  }

  close() {
    for (const room of this.#rooms.values()) this.#destroyRoom(room, "Server closing");
    this.#rooms.clear();
  }

  #broadcast(room, message) {
    for (const lease of room.leases.values()) lease.connection?.sendJson(message);
  }

  #metrics(room) {
    return {
      ...room.relay.getMetrics(),
      reconnects: room.reconnects,
      stateTransfers: room.stateTransfers
    };
  }

  #expireLease(room, lease) {
    if (lease.connection || room.leases.get(lease.slot) !== lease) return;
    lease.disconnectRelay();
    room.leases.delete(lease.slot);
    for (const [transferId, transfer] of room.pendingStates) {
      if (transfer.from === lease.slot || transfer.to === lease.slot) room.pendingStates.delete(transferId);
    }
    this.#touch(room);
  }

  #touch(room) {
    clearTimeout(room.expiryTimer);
    room.expiresAtMs = Date.now() + this.#roomTtlMs;
    room.expiryTimer = timer(() => {
      this.#destroyRoom(room, "Room expired");
      this.#rooms.delete(room.roomId);
    }, this.#roomTtlMs);
  }

  #destroyRoom(room, reason) {
    clearTimeout(room.expiryTimer);
    for (const lease of room.leases.values()) {
      clearTimeout(lease.expiryTimer);
      lease.connection?.close(1001, reason);
    }
    room.relay.close();
    room.leases.clear();
    room.pendingStates.clear();
  }

  async #handleMessage(room, lease, connection, text) {
    try {
      const message = JSON.parse(text);
      if (!message || typeof message !== "object") throw new TypeError("Message must be an object");
      if (message.type === "contribution") {
        await room.relay.sendContribution({ slot: lease.slot, frame: message.frame, values: message.values });
      } else if (message.type === "hash") {
        await room.relay.sendHash({ slot: lease.slot, frame: message.frame, digest: message.digest });
      } else if (message.type === "state") {
        this.#acceptState(room, lease.slot, message);
      } else if (message.type === "state-applied") {
        this.#acceptStateApplied(room, lease.slot, message);
      } else {
        throw new TypeError(`Unsupported message type: ${String(message.type)}`);
      }
      this.#touch(room);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      connection.sendJson({ type: "error", message });
      connection.close(1008, message);
    }
  }

  #acceptState(room, slot, message) {
    if (slot !== 0) throw new Error("Only the authority may publish a savestate");
    if (!TRANSFER_ID_PATTERN.test(message.transferId)) throw new TypeError("Invalid state transfer id");
    if (!Number.isSafeInteger(message.frame) || message.frame < 0) throw new TypeError("Invalid state frame");
    if (!DIGEST_PATTERN.test(message.digest)) throw new TypeError("Invalid savestate digest");
    if (!DIGEST_PATTERN.test(message.coreDigest)) throw new TypeError("Invalid core state digest");
    if (typeof message.state !== "string") throw new TypeError("Invalid savestate payload");
    const state = Buffer.from(message.state, "base64");
    if (state.length === 0 || state.length > MAX_STATE_BYTES) throw new Error("Savestate payload is too large");
    const digest = createHash("sha256").update(state).digest("hex");
    if (digest !== message.digest) throw new Error("Savestate payload digest mismatch");
    const existing = room.pendingStates.get(message.transferId);
    if (existing && existing.digest !== digest) throw new Error("State transfer id changed payload");
    const receiver = room.leases.get(1);
    if (!receiver?.connection) throw new Error("Savestate receiver is disconnected");
    room.pendingStates.set(message.transferId, { digest, from: 0, to: 1 });
    receiver.connection.sendJson({
      type: "state",
      transferId: message.transferId,
      frame: message.frame,
      digest,
      coreDigest: message.coreDigest,
      state: message.state
    });
  }

  #acceptStateApplied(room, slot, message) {
    if (!TRANSFER_ID_PATTERN.test(message.transferId)) throw new TypeError("Invalid state transfer id");
    const pending = room.pendingStates.get(message.transferId);
    if (!pending || pending.to !== slot) throw new Error("Unknown state transfer acknowledgement");
    if (message.digest !== pending.digest) throw new Error("Savestate acknowledgement digest mismatch");
    const sender = room.leases.get(pending.from);
    if (!sender?.connection) throw new Error("Savestate sender is disconnected");
    room.pendingStates.delete(message.transferId);
    room.stateTransfers += 1;
    sender.connection.sendJson({
      type: "state-applied",
      transferId: message.transferId,
      digest: message.digest
    });
  }
}
