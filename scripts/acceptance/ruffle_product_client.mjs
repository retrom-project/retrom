import {withRuffleRunMovie} from "./ruffle_run_movie.mjs";
import assert from "node:assert/strict";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
export async function importRuffleMovie(client, filename) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=flash&limit=100");
  const instance = platforms.items.find(item => item.enabled && item.defaultCoreId === "ruffle");
  assert.ok(instance, "RUFFLE_PLATFORM_MISSING");
  const uploadId = await withRuffleRunMovie(filename, wrapped => client.upload(singleFile(wrapped), "FILES", "GENERAL"));
  const imported = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
  });
  return reviewForImport(client, imported.importJobId);
}
