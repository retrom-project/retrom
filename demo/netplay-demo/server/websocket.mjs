import { createHash } from "node:crypto";

const WEBSOCKET_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";
const MAX_PAYLOAD_BYTES = 1024 * 1024;

function encodeFrame(opcode, payload = Buffer.alloc(0)) {
  const length = payload.length;
  let header;
  if (length < 126) {
    header = Buffer.from([0x80 | opcode, length]);
  } else if (length <= 0xffff) {
    header = Buffer.alloc(4);
    header[0] = 0x80 | opcode;
    header[1] = 126;
    header.writeUInt16BE(length, 2);
  } else {
    header = Buffer.alloc(10);
    header[0] = 0x80 | opcode;
    header[1] = 127;
    header.writeBigUInt64BE(BigInt(length), 2);
  }
  return Buffer.concat([header, payload]);
}

export class WebSocketConnection {
  #buffer = Buffer.alloc(0);
  #closed = false;
  #closeNotified = false;
  #onClose = () => {};
  #onMessage = () => {};
  #socket;

  constructor(socket, head = Buffer.alloc(0)) {
    this.#socket = socket;
    socket.setNoDelay(true);
    socket.on("data", (chunk) => this.#receive(chunk));
    socket.on("close", () => this.#notifyClose());
    socket.on("error", () => {
      this.#closed = true;
      this.#notifyClose();
    });
    if (head.length) this.#receive(head);
  }

  set onMessage(handler) {
    this.#onMessage = typeof handler === "function" ? handler : () => {};
  }

  set onClose(handler) {
    this.#onClose = typeof handler === "function" ? handler : () => {};
  }

  sendJson(value) {
    if (this.#closed) return;
    this.#socket.write(encodeFrame(0x1, Buffer.from(JSON.stringify(value))));
  }

  close(code = 1000, reason = "") {
    if (this.#closed) return;
    const reasonBytes = Buffer.from(reason).subarray(0, 123);
    const payload = Buffer.alloc(2 + reasonBytes.length);
    payload.writeUInt16BE(code, 0);
    reasonBytes.copy(payload, 2);
    this.#closed = true;
    this.#socket.end(encodeFrame(0x8, payload));
  }

  #receive(chunk) {
    if (this.#closed) return;
    this.#buffer = Buffer.concat([this.#buffer, chunk]);
    try {
      while (this.#parseFrame()) {}
    } catch (error) {
      this.close(1002, error instanceof Error ? error.message : "Protocol error");
    }
  }

  #parseFrame() {
    if (this.#buffer.length < 2) return false;
    const first = this.#buffer[0];
    const second = this.#buffer[1];
    if ((first & 0x80) === 0) throw new Error("Fragmented frames are unsupported");
    if ((first & 0x70) !== 0) throw new Error("Reserved WebSocket bits are set");
    if ((second & 0x80) === 0) throw new Error("Client frames must be masked");

    const opcode = first & 0x0f;
    let length = second & 0x7f;
    let offset = 2;
    if (length === 126) {
      if (this.#buffer.length < 4) return false;
      length = this.#buffer.readUInt16BE(2);
      offset = 4;
    } else if (length === 127) {
      if (this.#buffer.length < 10) return false;
      const value = this.#buffer.readBigUInt64BE(2);
      if (value > BigInt(MAX_PAYLOAD_BYTES)) throw new Error("WebSocket payload is too large");
      length = Number(value);
      offset = 10;
    }
    if (length > MAX_PAYLOAD_BYTES) throw new Error("WebSocket payload is too large");
    if (this.#buffer.length < offset + 4 + length) return false;

    const mask = this.#buffer.subarray(offset, offset + 4);
    const payload = Buffer.from(this.#buffer.subarray(offset + 4, offset + 4 + length));
    this.#buffer = this.#buffer.subarray(offset + 4 + length);
    for (let index = 0; index < payload.length; index += 1) {
      payload[index] ^= mask[index % 4];
    }

    if (opcode === 0x1) {
      const text = new TextDecoder("utf-8", { fatal: true }).decode(payload);
      this.#onMessage(text);
    } else if (opcode === 0x8) {
      this.close();
    } else if (opcode === 0x9) {
      this.#socket.write(encodeFrame(0xA, payload));
    } else if (opcode !== 0xA) {
      throw new Error(`Unsupported WebSocket opcode ${opcode}`);
    }
    return this.#buffer.length >= 2;
  }

  #notifyClose() {
    if (this.#closeNotified) return;
    this.#closeNotified = true;
    this.#onClose();
  }
}

export function acceptWebSocket(request, socket, head) {
  const key = request.headers["sec-websocket-key"];
  const version = request.headers["sec-websocket-version"];
  if (request.headers.upgrade?.toLowerCase() !== "websocket"
    || request.headers.connection?.toLowerCase().split(/\s*,\s*/).includes("upgrade") !== true
    || typeof key !== "string"
    || version !== "13") {
    socket.end("HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n");
    return null;
  }
  const accept = createHash("sha1").update(`${key}${WEBSOCKET_GUID}`).digest("base64");
  socket.write([
    "HTTP/1.1 101 Switching Protocols",
    "Upgrade: websocket",
    "Connection: Upgrade",
    `Sec-WebSocket-Accept: ${accept}`,
    "\r\n"
  ].join("\r\n"));
  return new WebSocketConnection(socket, head);
}
