import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { NetplayHub } from "../server/netplay-hub.mjs";
import { acceptWebSocket } from "../server/websocket.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const reservedPorts = new Set([3000, 8080]);
const mimeTypes = new Map([
  [".css", "text/css; charset=utf-8"],
  [".data", "application/octet-stream"],
  [".html", "text/html; charset=utf-8"],
  [".js", "text/javascript; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".mjs", "text/javascript; charset=utf-8"],
  [".wasm", "application/wasm"],
  [".zip", "application/zip"]
]);

function resolveRequest(requestUrl) {
  const pathname = decodeURIComponent(new URL(requestUrl, "http://local.invalid").pathname);
  const relative = pathname === "/" ? "index.html" : pathname.replace(/^\/+/, "");
  const candidate = path.resolve(root, relative);
  if (candidate !== root && !candidate.startsWith(`${root}${path.sep}`)) return null;
  return candidate;
}

export function validateListenOptions({ host, port }) {
  if (typeof host !== "string" || host.length === 0) throw new TypeError("host is required");
  if (!Number.isInteger(port) || port < 0 || port > 65535) {
    throw new TypeError("port must be an integer between 0 and 65535");
  }
  if (reservedPorts.has(port)) {
    throw new RangeError(`Port ${port} is reserved for the main Retrom applications`);
  }
  return { host, port };
}

export function startServer({ host = "127.0.0.1", port = 4174 } = {}) {
  validateListenOptions({ host, port });
  const hub = new NetplayHub();
  const server = http.createServer(async (request, response) => {
    if (request.method !== "GET" && request.method !== "HEAD") {
      response.writeHead(405, { Allow: "GET, HEAD" });
      response.end();
      return;
    }
    const filePath = resolveRequest(request.url ?? "/");
    if (!filePath) {
      response.writeHead(400);
      response.end("Invalid path");
      return;
    }
    try {
      const info = await stat(filePath);
      if (!info.isFile()) throw new Error("Not a file");
      response.writeHead(200, {
        "Cache-Control": "no-store",
        "Content-Length": info.size,
        "Content-Type": mimeTypes.get(path.extname(filePath)) ?? "application/octet-stream",
        "Cross-Origin-Opener-Policy": "same-origin",
        "Cross-Origin-Embedder-Policy": "require-corp"
      });
      if (request.method === "HEAD") response.end();
      else createReadStream(filePath).pipe(response);
    } catch {
      response.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
      response.end("Not found");
    }
  });
  server.on("upgrade", (request, socket, head) => {
    let connection = null;
    try {
      const url = new URL(request.url ?? "/", "http://local.invalid");
      const slotText = url.searchParams.get("slot");
      const expectedOrigin = `http://${request.headers.host}`;
      if (request.headers.origin !== expectedOrigin
        || url.pathname !== "/netplay"
        || !/^[01]$/.test(slotText ?? "")) {
        socket.end("HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n");
        return;
      }
      const roomId = url.searchParams.get("room") ?? "";
      connection = acceptWebSocket(request, socket, head);
      if (connection) hub.connect({ roomId, slot: Number(slotText), connection });
    } catch (error) {
      const message = error instanceof Error ? error.message : "Invalid WebSocket request";
      if (connection) connection.close(1008, message);
      else if (!socket.destroyed) socket.end("HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n");
    }
  });
  server.on("close", () => hub.close());

  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, host, () => {
      const address = server.address();
      if (!address || typeof address === "string") return reject(new Error("Unexpected listen address"));
      resolve({ server, origin: `http://${host}:${address.port}` });
    });
  });
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    const host = process.env.HOST ?? "127.0.0.1";
    const port = Number.parseInt(process.env.PORT ?? "4174", 10);
    if (process.argv.includes("--check-config")) {
      validateListenOptions({ host, port });
      console.log(`Listen configuration: http://${host}:${port}`);
    } else {
      const { origin } = await startServer({ host, port });
      console.log(`Retrom netplay demo: ${origin}`);
    }
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
