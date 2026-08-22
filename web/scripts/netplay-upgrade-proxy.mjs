import { request as requestHTTP, Server } from "node:http";

const installed = Symbol.for("retrom.netplay.upgrade-proxy.installed");
const netplaySocketPath = /^\/runtime\/netplay\/rooms\/[^/?]+\/socket(?:\?.*)?$/;
const backend = new URL(process.env.NEXT_BACKEND_ORIGIN ?? "http://127.0.0.1:8080");
if (backend.protocol !== "http:" || backend.username || backend.password || backend.pathname !== "/" || backend.search || backend.hash) {
  throw new Error("NEXT_BACKEND_ORIGIN_INVALID");
}

function responseHead(response) {
  const headers = [];
  for (let index = 0; index < response.rawHeaders.length; index += 2) {
    headers.push(`${response.rawHeaders[index]}: ${response.rawHeaders[index + 1]}`);
  }
  return `HTTP/${response.httpVersion} ${response.statusCode} ${response.statusMessage || "Unknown"}\r\n${headers.join("\r\n")}\r\n\r\n`;
}

function rejectProxy(socket) {
  if (!socket.destroyed) {
    socket.end("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\nContent-Length: 0\r\n\r\n");
  }
}

function proxyNetplayUpgrade(incoming, clientSocket, clientHead) {
  const upstream = requestHTTP({
    hostname: backend.hostname,
    port: backend.port,
    method: incoming.method,
    path: incoming.url,
    headers: incoming.headers,
  });
  upstream.once("upgrade", (response, backendSocket, backendHead) => {
    clientSocket.write(responseHead(response));
    if (backendHead.length > 0) {clientSocket.write(backendHead);}
    if (clientHead.length > 0) {backendSocket.write(clientHead);}
    backendSocket.pipe(clientSocket);
    clientSocket.pipe(backendSocket);
  });
  upstream.once("response", (response) => {
    clientSocket.write(responseHead(response));
    response.pipe(clientSocket);
  });
  upstream.once("error", () => rejectProxy(clientSocket));
  clientSocket.once("error", () => upstream.destroy());
  upstream.end();
}

if (!Server.prototype[installed]) {
  const addListener = Server.prototype.on;
  Server.prototype.on = function on(event, listener) {
    if (event !== "upgrade") {return addListener.call(this, event, listener);}
    return addListener.call(this, event, function upgrade(request, socket, head) {
      if (request.url && netplaySocketPath.test(request.url)) {
        proxyNetplayUpgrade(request, socket, head);
        return;
      }
      return listener.call(this, request, socket, head);
    });
  };
  Object.defineProperty(Server.prototype, installed, { value: true });
}
