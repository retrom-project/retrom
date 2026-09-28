"use strict";
let connection = null;
let reconnecting = null;
let sequence = 0;
const pending = new Map();
function bindConnection(port) {
  connection?.close(); connection = port;
  port.onmessage = ({data}) => {
    const request = pending.get(data?.id);
    if (!request) return;
    pending.delete(data.id); clearTimeout(request.timer);
    request.resolve(data);
  };
  port.start();
}
async function reconnect() {
  if (connection) return;
  if (reconnecting) return reconnecting;
  reconnecting = (async () => {
    const windows = await self.clients.matchAll({type: "window", includeUncontrolled: true});
    const client = windows.find(item => [configuration.entry, configuration.bootstrap].includes(new URL(item.url).pathname));
    if (!client) throw new Error("CONTENT_IO_CLIENT_UNAVAILABLE");
    const pair = new MessageChannel();
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => {pair.port1.close(); reject(new Error("CONTENT_IO_TIMEOUT"));}, 10000);
      pair.port1.onmessage = ({data}) => {
        if (data?.type !== "READY") return;
        clearTimeout(timer); bindConnection(pair.port1); resolve();
      };
      client.postMessage({type: "RETROM_WEB_CONTENT_RECONNECT"}, [pair.port2]);
    });
  })().finally(() => {reconnecting = null;});
  return reconnecting;
}
async function contentRequest(body) {
  await reconnect();
  const id = ++sequence;
  if (pending.size >= 128) throw new Error("CONTENT_IO_BUSY");
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id); connection?.close(); connection = null;
      reject(new Error("CONTENT_IO_TIMEOUT"));
    }, 15000);
    pending.set(id, {resolve, reject, timer});
    connection.postMessage({...body, id});
  });
}
async function readBlock(path, offset, length) {
  const reply = await contentRequest({type: "READ", path, offset, length});
  if (reply.status !== 200 || !(reply.bytes instanceof Uint8Array) || reply.bytes.length !== length) throw new Error("CONTENT_IO_READ_FAILED");
  return reply.bytes;
}
function contentStream(path, offset, length) {
  let position = offset;
  let cancelled = false;
  return new ReadableStream({
    async pull(controller) {
      try {
        if (position === offset + length) {controller.close(); return;}
        const count = Math.min(262144, offset + length - position);
        const bytes = await readBlock(path, position, count);
        if (cancelled) return;
        position += count; controller.enqueue(bytes);
      } catch (error) {if (!cancelled) controller.error(error);}
    },
    cancel() {cancelled = true;},
  }, {highWaterMark: 0});
}
async function readSmall(path, size) {
  if (size > 2097152) throw new Error("CONTENT_IO_BOUNDS");
  const result = new Uint8Array(size);
  for (let offset = 0; offset < size; offset += 262144) {
    result.set(await readBlock(path, offset, Math.min(262144, size - offset)), offset);
  }
  return result;
}
