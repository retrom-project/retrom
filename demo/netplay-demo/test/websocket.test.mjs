import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";
import { WebSocketConnection } from "../server/websocket.mjs";

class FakeSocket extends EventEmitter {
  writes = [];
  ended = null;
  setNoDelay() {}
  write(value) { this.writes.push(Buffer.from(value)); }
  end(value) {
    this.ended = value ? Buffer.from(value) : Buffer.alloc(0);
    this.emit("close");
  }
}

function clientFrame(opcode, payload, { finished = true } = {}) {
  const bytes = Buffer.from(payload);
  if (bytes.length > 125) throw new Error("test helper only creates short frames");
  const mask = Buffer.from([0x12, 0x34, 0x56, 0x78]);
  const masked = Buffer.from(bytes);
  for (let index = 0; index < masked.length; index += 1) masked[index] ^= mask[index % 4];
  return Buffer.concat([
    Buffer.from([(finished ? 0x80 : 0) | opcode, 0x80 | bytes.length]),
    mask,
    masked
  ]);
}

test("server reassembles masked fragmented text messages", () => {
  const socket = new FakeSocket();
  const connection = new WebSocketConnection(socket);
  const messages = [];
  connection.onMessage = (message) => messages.push(message);

  socket.emit("data", clientFrame(0x1, '{"type":', { finished: false }).subarray(0, 5));
  socket.emit("data", Buffer.concat([
    clientFrame(0x1, '{"type":', { finished: false }).subarray(5),
    clientFrame(0x0, '"hash"}', { finished: true })
  ]));

  assert.deepEqual(messages, ['{"type":"hash"}']);
  assert.equal(socket.ended, null);
});

test("server closes unmasked client frames with a protocol error", () => {
  const socket = new FakeSocket();
  new WebSocketConnection(socket);
  socket.emit("data", Buffer.from([0x81, 0x02, 0x7b, 0x7d]));
  assert.equal(socket.ended[0], 0x88);
  assert.equal(socket.ended.readUInt16BE(2), 1002);
});
