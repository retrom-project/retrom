import assert from "node:assert/strict";
import {createServer, request as upstreamRequest} from "node:http";
import {connect} from "node:net";
import {join} from "node:path";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";

// Playwright's request context needs the same localhost routing as Chromium.
export async function px68kLocalProxy(baseUrl) {
  const origin = new URL(baseUrl);
  if (origin.protocol !== "http:" || !origin.hostname.endsWith(".localhost")) {
    return {contextOptions: {}, close: async () => {}};
  }
  const sockets = new Set();
  const server = createServer((request, response) => {
    const target = new URL(request.url, origin);
    if (target.origin !== origin.origin) {response.writeHead(403).end(); return;}
    const upstream = upstreamRequest({hostname: "127.0.0.1", port: origin.port || 80,
      method: request.method, path: target.pathname + target.search, headers: {...request.headers, host: origin.host}}, remote => {
      response.writeHead(remote.statusCode, remote.headers); remote.pipe(response);
    });
    upstream.on("error", () => {if (!response.headersSent) {response.writeHead(502);} response.end();});
    request.pipe(upstream);
  });
  server.on("connect", (request, socket, head) => {
    if (request.url !== origin.host) {socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"); return;}
    const upstream = connect(Number(origin.port || 80), "127.0.0.1", () => {
      socket.write("HTTP/1.1 200 Connection Established\r\n\r\n");
      if (head.length) {upstream.write(head);} socket.pipe(upstream); upstream.pipe(socket);
    });
    sockets.add(upstream); upstream.on("close", () => sockets.delete(upstream));
    upstream.on("error", () => socket.destroy());
  });
  server.on("connection", socket => {socket.on("error", () => socket.destroy()); sockets.add(socket); socket.on("close", () => sockets.delete(socket));});
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  return {contextOptions: {proxy: {server: `http://127.0.0.1:${server.address().port}`}},
    close: async () => {for (const socket of sockets) {socket.destroy();} await new Promise(resolve => server.close(resolve));}};
}
export async function installPX68KBIOS(client, directory) {
  const catalog = await client.json("GET", "/api/v1/admin/bios?scope=FULL_CATALOG&coreId=px68k&limit=100");
  const requirements = catalog.items.filter(item => item.coreId === "px68k");
  assert.equal(requirements.length, 2, "PX68K_BIOS_CATALOG_MISSING");
  for (const requirement of requirements) {
    if (requirement.status === "MATCHED") {continue;}
    assert.equal(requirement.activeInstallation, null, "PX68K_ACCEPTANCE_WILL_NOT_REPLACE_BIOS");
    const uploadId = await client.upload(singleFile(join(directory, requirement.logicalName)), "FILES", "GENERAL");
    const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
    const response = await client.raw("POST", `/api/v1/admin/bios/${requirement.id}/installations`, {
      headers: {...client.writeHeaders(), "If-Match": `"v${requirement.version}"`},
      data: {uploadFileId: upload.files[0].fileId},
    });
    assert.equal(response.status(), 201, "PX68K_BIOS_INSTALL_FAILED");
  }
}
export async function importPX68KDisk(client, filename) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=x68000&limit=100");
  const instance = platforms.items.find(item => item.enabled && item.defaultCoreId === "px68k");
  assert.ok(instance, "PX68K_PLATFORM_MISSING");
  const uploadId = await client.upload(singleFile(filename), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
  });
  return reviewForImport(client, imported.importJobId);
}
export async function px68kCanvas(page) {
  for (let attempt = 0; attempt < 300; attempt++) {
    for (const frame of page.frames()) {
      const canvas = frame.locator('canvas[aria-label="px68k game"]');
      if (await canvas.isVisible()) {await canvas.click(); return canvas;}
    }
    const text = await page.locator("body").innerText();
    if (/PX68K_[A-Z_]+|RUNTIME_FAILED/u.test(text)) {throw Error(text.match(/PX68K_[A-Z_]+|RUNTIME_FAILED/u)[0]);}
    await page.waitForTimeout(100);
  }
  throw Error("PX68K_CANVAS_TIMEOUT");
}
export async function canvasDigest(canvas) {
  return canvas.evaluate(async element => {
    const data = element.getContext("2d").getImageData(0, 0, element.width, element.height).data;
    const colors = new Set(); for (let i = 0; i < data.length; i += 4) {colors.add(`${data[i]},${data[i+1]},${data[i+2]}`);}
    const hash = await crypto.subtle.digest("SHA-256", data);
    return {width: element.width, height: element.height, colors: colors.size,
      sha256: Array.from(new Uint8Array(hash), byte => byte.toString(16).padStart(2, "0")).join("")};
  });
}
export async function waitPX68KGame(canvas) {
  let stable = 0;
  for (let attempt = 0; attempt < 60; attempt++) {
    stable = (await canvasDigest(canvas)).colors > 4 ? stable + 1 : 0;
    if (stable >= 3) {return;}
    await canvas.page().waitForTimeout(1000);
  }
  throw Error("PX68K_GAME_BOOT_TIMEOUT");
}
