/** Exercise only normal HTTP against a fresh release image and its private fixture. */
import { createHash, randomUUID } from "node:crypto";
import { readFile } from "node:fs/promises";

const origin = "https://retrom.example.com";
const base = "http://127.0.0.1:8080";
let cookie = "";
let csrf = "";

async function request(path, method = "GET", input) {
  const response = await fetch(base + path, {
    method,
    headers: { Origin: origin, Cookie: cookie, "X-Retrom-Csrf": csrf,
      ...(input === undefined ? {} : { "Content-Type": "application/json" }) },
    ...(input === undefined ? {} : { body: JSON.stringify(input) })
  });
  if (!response.ok) throw new Error(`IMAGE_HTTP_FAILED:${method}:${path}:${response.status}`);
  const setCookie = response.headers.getSetCookie();
  if (setCookie.length) cookie = setCookie[0].split(";")[0];
  const text = await response.text();
  return text ? JSON.parse(text) : null;
}

const before = await request("/api/v1/auth/context");
if (before.initialized || before.user !== null) throw new Error("IMAGE_DATABASE_NOT_EMPTY");
const initialized = await request("/api/v1/auth/initialize", "POST", {
  username: "image-admin", displayName: "Image verification", password: "Image-" + randomUUID()
});
csrf = initialized.csrfToken;
const empty = await request("/api/v1/admin/platform-instances");
if (empty.items.length) throw new Error("IMAGE_DIRECTORY_SEED");
const directory = await request("/api/v1/admin/platform-instances", "POST", {
  platformId: "nes", name: "Image NES", slug: "image-nes", description: "",
  coreIds: ["fceumm"], defaultCoreId: "fceumm", enabled: true
});
const source = { rootId: "image", relativePath: "browser", format: "pegasus" };
const inspected = await request("/api/v1/admin/game-scans/inspect", "POST", source);
if (inspected.items.length !== 1) throw new Error("IMAGE_SOURCE_INSPECTION_FAILED");
const scan = await request("/api/v1/admin/game-scans", "POST", {
  ...source, mappings: [{ sourceKey: inspected.items[0].key, platformInstanceId: directory.id, tagIds: [] }]
});
let complete = false;
for (let attempt = 0; attempt < 120; attempt += 1) {
  const scans = await request("/api/v1/admin/scans?limit=100");
  const current = scans.items.find(item => item.id === scan.id);
  if (current?.status === "completed") {
    if (current.importedCount !== 1 || current.failedCount !== 0) throw new Error("IMAGE_SCAN_COUNTS_INVALID");
    complete = true;
    break;
  }
  if (["failed", "interrupted", "cancelled"].includes(current?.status)) throw new Error("IMAGE_SCAN_FAILED");
  await new Promise(resolve => setTimeout(resolve, 250));
}
if (!complete) throw new Error("IMAGE_SCAN_TIMEOUT");
const reviews = await request(`/api/v1/admin/reviews?platformInstanceId=${directory.id}&limit=1`);
const game = reviews.items[0];
if (!game) throw new Error("IMAGE_GAME_MISSING");
await request(`/api/v1/admin/reviews/${game.id}/approve`, "POST", { version: game.version });
const run = await request("/api/v1/runs", "POST", { gameId: game.id, purpose: "play", coreId: "fceumm" });
if (run.envelope.runtime.checkpoint.semantics !== "INSTANT") throw new Error("IMAGE_SEMANTICS_MISSING");
const resource = run.envelope.resources.find(item => typeof item.url === "string");
if (!resource) throw new Error("IMAGE_RESOURCE_MISSING");
const response = await fetch(base + new URL(resource.url, origin).pathname, {
  headers: { Cookie: cookie, "If-Match": `"sha256-${resource.sha256}"`, Range: "bytes=0-15" }
});
if (response.status !== 206 || (await response.arrayBuffer()).byteLength !== 16) throw new Error("IMAGE_RANGE_FAILED");
for (const name of ["shell.html", "service-worker.js"]) {
  const response = await fetch(`${base}/__retrom/runtime-isolation/${run.id}/${name}`, {
    headers: { Host: `${run.id}.sub.retrom.example.com` }
  });
  if (response.status !== 200 || (await response.text()).length === 0 || response.headers.has("set-cookie")) {
    throw new Error("IMAGE_ISOLATION_ASSET_FAILED");
  }
  if (response.headers.get("Cross-Origin-Embedder-Policy") !== "require-corp") throw new Error("IMAGE_ISOLATION_HEADER_FAILED");
  if (name === "service-worker.js" && response.headers.get("Service-Worker-Allowed") !== `/run/${run.id}/`) {
    throw new Error("IMAGE_SERVICE_WORKER_SCOPE_FAILED");
  }
}
const providers = process.env.RETROM_PROVIDER_ROOT;
const active = JSON.parse(await readFile(`${providers}/active.json`, "utf8"));
let checkedBridge = false;
for (const provider of active.providers) {
  const installed = `${providers}/installed/${provider.installationPath}`;
  const integrity = JSON.parse(await readFile(`${installed}/integrity.json`, "utf8"));
  const bridge = integrity.files.find(file => file.path.endsWith("/bridge.js"));
  if (!bridge) continue;
  const response = await fetch(`${base}/runtime/providers/${provider.providerId}/${provider.bundleSha256}/${bridge.path}`, {
    headers: { Cookie: cookie }
  });
  if (response.status !== 200) throw new Error("IMAGE_BRIDGE_ROUTE_FAILED");
  const bytes = Buffer.from(await response.arrayBuffer());
  if (bytes.length !== bridge.sizeBytes || createHash("sha256").update(bytes).digest("hex") !== bridge.sha256) {
    throw new Error("IMAGE_BRIDGE_IDENTITY_FAILED");
  }
  checkedBridge = true;
  break;
}
if (!checkedBridge) throw new Error("IMAGE_BRIDGE_MISSING");
await request(`/api/v1/runs/${run.id}`, "DELETE");
console.log(JSON.stringify({ initialized: true, imported: 1, managedRange: 206,
  isolatedAssets: 2, authenticatedBridge: true, checkpointSemantics: "INSTANT", runClosed: true }));
