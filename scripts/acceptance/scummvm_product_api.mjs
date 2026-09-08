import {withScummvmRunArchive} from "./scummvm_product_source.mjs";
import assert from "node:assert/strict";
import {createProductClient, reviewForImport, singleFile} from "./rpgmaker_security_upload.mjs";

export async function scummvmClient(context, baseUrl) {
  const response = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
    headers: {Origin: baseUrl},
    data: {username: process.env.RETROM_ACCEPTANCE_USERNAME, password: process.env.RETROM_ACCEPTANCE_PASSWORD},
  });
  assert.equal(response.status(), 200);
  return createProductClient(context, baseUrl, (await response.json()).csrfToken);
}

export async function importScummvm(client, archive) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {},
  });
  const instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=scummvm&limit=100");
  const instance = instances.items.find((item) => item.enabled && item.defaultCoreId === "scummvm");
  assert(instance);
  const uploadId = await withScummvmRunArchive(archive, (wrapped) => client.upload(singleFile(wrapped), "FILES", "PROJECT"));
  const imported = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "SCUMMVM_PROJECT", tagIds: []},
  });
  return reviewForImport(client, imported.importJobId, {attempts: 1200});
}

export async function approveScummvm(client, itemId) {
  const snapshot = await client.raw("GET", `/api/v1/admin/reviews/${itemId}`);
  assert.equal(snapshot.status(), 200);
  assert(snapshot.headers().etag);
  return client.json("POST", `/api/v1/admin/reviews/${itemId}/approve`, {
    headers: {...client.writeHeaders(), "If-Match": snapshot.headers().etag}, data: {}, expected: 201,
  });
}

export function trackScummvmTraffic(page) {
  const responses = [];
  page.on("response", (response) => {
    const path = new URL(response.url()).pathname;
    if (response.request().method() === "GET" && (/\/plugins\//u.test(path) || path.startsWith("/runtime/content/project/"))) {
      responses.push({path, status: response.status(), range: response.request().headers().range ?? null});
    }
  });
  return responses;
}

export function assertScummvmTraffic(first, restored, engineId) {
  const plugins = first.filter((item) => item.path.includes("/plugins/"));
  assert(plugins.length > 0, "SCUMMVM_PLUGIN_REQUEST_MISSING");
  assert(plugins.every((item) => item.path.endsWith(`/lib${engineId}.so`) && item.status === 206));
  const content = (items) => items.filter((item) => item.path.startsWith("/runtime/content/project/"));
  const initial = content(first), resumed = content(restored);
  assert(initial.some((item) => item.status === 206 && item.range), "SCUMMVM_LAZY_CONTENT_UNOBSERVED");
  assert(resumed.some((item) => item.path.endsWith("/index.json") && item.status === 200));
  assert.equal(resumed.filter((item) => item.range).length, 0, "SCUMMVM_CONTENT_CACHE_NOT_REUSED");
  const identities = new Set([...initial, ...resumed].map((item) => item.path.split("/")[4]));
  assert.equal(identities.size, 1);
  return {engineId, contentDigest: [...identities][0], firstPluginResponses: plugins.length,
    firstBlockResponses: initial.filter((item) => item.range).length, restoredBlockResponses: 0};
}
