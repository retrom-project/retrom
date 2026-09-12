import assert from "node:assert/strict";
import {join} from "node:path";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
export async function installMiniBIOS(client, directory) {
  const catalog = await client.json("GET", "/api/v1/admin/bios?scope=FULL_CATALOG&coreId=gbe_plus&limit=100");
  const requirements = catalog.items.filter(item => item.coreId === "gbe_plus");
  assert.equal(requirements.length, 1, "POKEMINI_BIOS_CATALOG_MISSING");
  for (const requirement of requirements) {
    if (requirement.status === "MATCHED") {continue;}
    assert.equal(requirement.activeInstallation, null, "POKEMINI_ACCEPTANCE_WILL_NOT_REPLACE_BIOS");
    const uploadId = await client.upload(singleFile(join(directory, requirement.logicalName)), "FILES", "GENERAL");
    const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
    const response = await client.raw("POST", `/api/v1/admin/bios/${requirement.id}/installations`, {
      headers: {...client.writeHeaders(), "If-Match": `"v${requirement.version}"`},
      data: {uploadFileId: upload.files[0].fileId},
    });
    assert.equal(response.status(), 201, "POKEMINI_BIOS_INSTALL_FAILED");
  }
}
export async function importMini(client, filename) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=pokemini&limit=100");
  const instance = platforms.items.find(item => item.enabled && item.defaultCoreId === "gbe_plus");
  assert.ok(instance, "POKEMINI_PLATFORM_MISSING");
  const uploadId = await client.upload(singleFile(filename), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
  });
  return reviewForImport(client, imported.importJobId);
}
