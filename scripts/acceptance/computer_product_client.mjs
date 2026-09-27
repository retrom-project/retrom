import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import {proofDigest} from "./content_io_case_proof.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";

export async function computerSource(path, role = "game") {
  const bytes = await readFile(path);
  return {role, sha256: proofDigest(bytes), sizeBytes: bytes.length};
}

export async function installComputerBios(client, core, files) {
  const catalog = await client.json("GET", `/api/v1/admin/bios?scope=FULL_CATALOG&coreId=${core}&limit=100`);
  const sources = [];
  for (const [logicalName, path] of Object.entries(files)) {
    const item = catalog.items.find(row => row.logicalName === logicalName && row.enabled);
    assert.ok(item, `COMPUTER_BIOS_REQUIREMENT_MISSING:${logicalName}`);
    sources.push({logicalName, ...await computerSource(path, "external")});
    if (item.activeInstallation) continue; // The Launch's actual identities are checked separately.
    const uploadId = await client.upload(singleFile(path), "FILES", "GENERAL");
    const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
    await client.json("POST", `/api/v1/admin/bios/${item.id}/installations`, {
      headers: {...client.writeHeaders(), "If-Match": `"v${item.version}"`}, expected: 201,
      data: {uploadFileId: upload.files[0].fileId},
    });
  }
  return sources;
}

export async function importComputer(client, platform, core, path) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const catalog = await client.json("GET", `/api/v1/admin/platform-instances?platformId=${platform}&limit=100`);
  const instance = catalog.items.find(row => row.enabled && row.defaultCoreId === core);
  assert.ok(instance, "COMPUTER_PLATFORM_MISSING");
  const uploadId = await client.upload(singleFile(path), "FILES", "GENERAL");
  const job = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
  });
  const review = await reviewForImport(client, job.importJobId);
  return {...review, importJobId: job.importJobId};
}
