"use strict";
let configuration = null;
let connection = null;
let sequence = 0;
const pending = new Map();
const runId = new URL(self.location.href).pathname.split("/")[3];
self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});
self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});
self.addEventListener("message", (event) => {
  if (
    event.data?.type !== "CONNECT" ||
    event.ports.length !== 2 ||
    !event.source?.url
  ) {
    return;
  }
  const source = new URL(event.source.url);
  if (
    source.origin !== self.location.origin ||
    source.pathname !== `/__retrom/runtime-isolation/${runId}/shell.html` ||
    event.data.configuration?.runId !== runId
  ) {
    return;
  }
  bind(event.ports[0], event.data.configuration);
  event.ports[1].postMessage({ type: "CONNECTED" });
  event.ports[1].close();
});
function bind(port, input) {
  configuration = input;
  connection?.close();
  connection = port;
  connection.onmessage = ({ data }) => {
    const request = pending.get(data?.id);
    if (!request || data?.type === "READ_STARTED") {
      return;
    }
    pending.delete(data.id);
    clearTimeout(request.timer);
    request.resolve(data);
  };
  connection.start();
}
let reconnecting = null;
async function reconnect() {
  if (reconnecting) {
    return reconnecting;
  }
  reconnecting = reconnectClient().finally(() => {
    reconnecting = null;
  });
  return reconnecting;
}
async function reconnectClient() {
  const clients = await self.clients.matchAll({ type: "window" });
  const client = clients.find((item) =>
    new URL(item.url).pathname.startsWith(`/run/${runId}/`),
  );
  if (!client) {
    throw new Error("CONTENT_IO_UNAVAILABLE");
  }
  const content = new MessageChannel();
  const response = new MessageChannel();
  const ready = new Promise((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new Error("CONTENT_IO_RECONNECT_TIMEOUT")),
      10000,
    );
    response.port1.onmessage = (event) => {
      if (event.data?.runId !== runId) {
        return;
      }
      clearTimeout(timer);
      bind(content.port1, event.data);
      response.port1.close();
      resolve();
    };
  });
  client.postMessage({ type: "RETROM_WEB_CONTENT_RECONNECT", v: 1 }, [
    content.port2,
    response.port2,
  ]);
  await ready;
}
self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  const prefix = `/run/${runId}/`;
  if (url.origin !== self.location.origin || !url.pathname.startsWith(prefix)) {
    return;
  }
  let path;
  try {
    path = decodeURIComponent(url.pathname.slice(prefix.length));
  } catch {
    event.respondWith(new Response(null, { status: 400 }));
    return;
  }
  if (
    path.split("/").some((part) => ["", ".", ".."].includes(part)) ||
    path.includes("\\")
  ) {
    event.respondWith(new Response(null, { status: 404 }));
    return;
  }
  event.respondWith(
    serve(event.request, path).catch(() => new Response(null, { status: 502 })),
  );
});
async function request(body) {
  if (!connection || !configuration) {
    await reconnect();
  }
  if (!connection || !configuration || pending.size >= 128) {
    return Promise.reject(new Error("CONTENT_IO_UNAVAILABLE"));
  }
  const id = ++sequence;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      connection = null;
      reject(new Error("CONTENT_IO_TIMEOUT"));
    }, 15000);
    pending.set(id, { resolve, reject, timer });
    connection.postMessage({ ...body, id });
  });
}
async function block(path, offset, length) {
  const reply = await request({
    type: "READ",
    admission: true,
    path,
    offset,
    length,
  });
  if (
    reply.status !== 200 ||
    !(reply.bytes instanceof Uint8Array) ||
    reply.bytes.length !== length
  ) {
    throw new Error("CONTENT_IO_READ_FAILED");
  }
  return reply.bytes;
}
function stream(path, offset, length) {
  let position = offset;
  return new ReadableStream(
    {
      async pull(controller) {
        try {
          if (position === offset + length) {
            controller.close();
            return;
          }
          const count = Math.min(262144, offset + length - position);
          const bytes = await block(path, position, count);
          position += count;
          controller.enqueue(bytes);
        } catch (error) {
          controller.error(error);
        }
      },
    },
    { highWaterMark: 0 },
  );
}
function range(header, size) {
  if (header === null) {
    return { offset: 0, length: size, partial: false };
  }
  const match = /^bytes=(\d*)-(\d*)$/.exec(header);
  if (!match || (!match[1] && !match[2]) || size === 0) {
    return null;
  }
  const first = match[1]
    ? Number(match[1])
    : Math.max(0, size - Number(match[2]));
  const last =
    match[1] && match[2] ? Math.min(size - 1, Number(match[2])) : size - 1;
  if (
    !Number.isSafeInteger(first) ||
    !Number.isSafeInteger(last) ||
    first < 0 ||
    first >= size ||
    last < first
  ) {
    return null;
  }
  return { offset: first, length: last - first + 1, partial: true };
}
async function entryBytes(path, size) {
  if (size > 2 * 1024 * 1024) {
    throw new Error("CONTENT_IO_ENTRY_BOUNDS");
  }
  const bytes = new Uint8Array(size);
  for (let offset = 0; offset < size; offset += 262144) {
    bytes.set(
      await block(path, offset, Math.min(262144, size - offset)),
      offset,
    );
  }
  const text = new TextDecoder("utf-8", { fatal: true })
    .decode(bytes)
    .replace(/<base(?=[\s/>])(?:[^"'<>]|"[^"]*"|'[^']*')*>/gi, "");
  const head = /<head(?=[\s>])(?:[^"'<>]|"[^"]*"|'[^']*')*>/i.exec(text);
  if (!head) {
    throw new Error("CONTENT_IO_ENTRY_INVALID");
  }
  const root = `/run/${runId}/${path
    .slice(0, path.lastIndexOf("/") + 1)
    .split("/")
    .map(encodeURIComponent)
    .join("/")}`;
  const relay = `(${relaySource.toString()})(${JSON.stringify(configuration).replace(/</g, "\\u003c")})`;
  const injected = `<base href="${root}"><script>${relay}</script><script>${configuration.bridge.replace(/<\/script/gi, "<\\/script")}</script>`;
  const end = head.index + head[0].length;
  return new TextEncoder().encode(
    text.slice(0, end) + injected + text.slice(end),
  );
}
async function serve(input, path) {
  if (
    !["GET", "HEAD"].includes(input.method) ||
    input.destination === "serviceworker"
  ) {
    return new Response(null, { status: 404 });
  }
  const stat = await request({ type: "STAT", path });
  if (stat.status !== 200) {
    return new Response(null, { status: stat.status === 404 ? 404 : 502 });
  }
  const entry = path === configuration.entryFile;
  const bytes = entry ? await entryBytes(path, stat.sizeBytes) : null;
  const size = bytes?.length ?? stat.sizeBytes;
  const selected = range(input.headers.get("Range"), size);
  const headers = new Headers({
    "Content-Type": stat.mediaType,
    "X-Content-Type-Options": "nosniff",
    "Cache-Control": "no-store",
    "Cross-Origin-Resource-Policy": entry ? "cross-origin" : "same-origin",
    "Cross-Origin-Embedder-Policy": "require-corp",
    "Accept-Ranges": "bytes",
  });
  if (!selected) {
    headers.set("Content-Range", `bytes */${size}`);
    return new Response(null, { status: 416, headers });
  }
  headers.set("Content-Length", String(selected.length));
  if (selected.partial) {
    headers.set(
      "Content-Range",
      `bytes ${selected.offset}-${selected.offset + selected.length - 1}/${size}`,
    );
  }
  headers.set(
    "Content-Security-Policy",
    `default-src 'self' blob: data:; script-src 'self' 'unsafe-inline' 'unsafe-eval' blob:; connect-src 'self' blob:; worker-src 'self' blob:; style-src 'self' 'unsafe-inline'; base-uri 'self'; object-src 'none'; frame-ancestors ${configuration.parentOrigin}`,
  );
  if (entry) {
    headers.set("Cross-Origin-Opener-Policy", "same-origin");
    headers.set("Referrer-Policy", "no-referrer");
  }
  const body =
    input.method === "HEAD"
      ? null
      : bytes
        ? bytes.slice(selected.offset, selected.offset + selected.length)
        : stream(stat.path, selected.offset, selected.length);
  return new Response(body, { status: selected.partial ? 206 : 200, headers });
}

function relaySource(input) {
  navigator.serviceWorker.addEventListener("message", (event) => {
    if (
      event.source !== navigator.serviceWorker.controller ||
      event.data?.type !== "RETROM_WEB_CONTENT_RECONNECT" ||
      event.ports.length !== 2
    ) {
      return;
    }
    parent.postMessage(
      { type: "RETROM_WEB_CONTENT_CONNECT", v: 1 },
      input.parentOrigin,
      [event.ports[0]],
    );
    event.ports[1].postMessage(input);
    event.ports[1].close();
  });
}
