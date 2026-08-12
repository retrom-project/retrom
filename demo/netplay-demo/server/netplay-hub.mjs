import { LocalRelay } from "../src/local-relay.js";
import { assertSlot } from "../src/protocol.js";

const ROOM_ID_PATTERN = /^[a-zA-Z0-9_-]{1,80}$/;

export class NetplayHub {
  #rooms = new Map();

  connect({ roomId, slot, connection }) {
    if (!ROOM_ID_PATTERN.test(roomId)) throw new TypeError("Invalid room id");
    assertSlot(slot);
    let room = this.#rooms.get(roomId);
    if (!room) {
      room = { relay: new LocalRelay(), peers: new Map() };
      this.#rooms.set(roomId, room);
    }
    if (room.peers.has(slot)) throw new Error(`Slot ${slot} is already connected`);

    const disconnectRelay = room.relay.connect(slot, {
      onFrame: (frame) => {
        connection.sendJson(frame);
        connection.sendJson({ type: "metrics", metrics: room.relay.getMetrics() });
      },
      onHashResult: (result) => connection.sendJson(result),
      onPause: (event) => connection.sendJson({ type: "pause", ...event })
    });
    room.peers.set(slot, connection);
    let disconnected = false;
    const disconnect = () => {
      if (disconnected) return;
      disconnected = true;
      disconnectRelay();
      room.peers.delete(slot);
      if (room.peers.size === 0) {
        room.relay.close();
        this.#rooms.delete(roomId);
      }
    };

    connection.onMessage = (text) => {
      void this.#handleMessage(room, slot, connection, text);
    };
    connection.onClose = disconnect;
    connection.sendJson({ type: "hello", roomId, slot });
    return disconnect;
  }

  close() {
    for (const room of this.#rooms.values()) {
      for (const connection of room.peers.values()) connection.close(1001, "Server closing");
      room.relay.close();
    }
    this.#rooms.clear();
  }

  async #handleMessage(room, slot, connection, text) {
    try {
      const message = JSON.parse(text);
      if (!message || typeof message !== "object") throw new TypeError("Message must be an object");
      if (message.type === "contribution") {
        await room.relay.sendContribution({ slot, frame: message.frame, values: message.values });
      } else if (message.type === "hash") {
        await room.relay.sendHash({ slot, frame: message.frame, digest: message.digest });
      } else {
        throw new TypeError(`Unsupported message type: ${String(message.type)}`);
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      connection.sendJson({ type: "error", message });
      connection.close(1008, message);
    }
  }
}
