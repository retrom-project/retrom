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
