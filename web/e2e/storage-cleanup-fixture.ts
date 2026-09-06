import { createHash, randomUUID } from "node:crypto";
import { expect, type APIRequestContext } from "@playwright/test";

// Own marker bytes exercise upload/import/discard ownership; no game is launched.
export async function seedStorageCleanupCandidate(api: APIRequestContext, origin: string) {
  const login = await api.post("/api/v1/auth/login", { data: { username: "test", password: "test" }, headers: { Origin: origin } });
  expect(login.ok()).toBe(true);
  const { csrfToken } = await login.json() as { csrfToken: string };
  const headers = () => ({ Origin: origin, "X-Retrom-Csrf": csrfToken, "Idempotency-Key": randomUUID() });
  const instances = await api.get("/api/v1/admin/platform-instances");
  expect(instances.ok()).toBe(true);
  const { items } = await instances.json() as { items: Array<{ id: string; platformId: string }> };
  const target = items.find((item) => item.platformId === "gba");
  expect(target).toBeTruthy();
  const bytes = Buffer.from("Retrom storage cleanup UI fixture v1");
  const uploadResponse = await api.post("/api/v1/admin/uploads", {
    headers: headers(), data: { purpose: "GENERAL", sourceType: "FILES", files: [{ clientFileId: "marker", relativePath: "storage-cleanup.gba", sizeBytes: bytes.length }] },
  });
  expect(uploadResponse.ok()).toBe(true);
  const upload = await uploadResponse.json() as { uploadId: string; files: Array<{ fileId: string }> };
  const part = await api.put(`/api/v1/admin/uploads/${upload.uploadId}/files/${upload.files[0].fileId}/parts/0`, {
    headers: { ...headers(), "Content-Type": "application/octet-stream", "Content-Range": `bytes 0-${bytes.length - 1}/${bytes.length}`, "Content-Digest": `sha-256=:${createHash("sha256").update(bytes).digest("base64")}:` }, data: bytes,
  });
  expect(part.ok()).toBe(true);
  const current = await api.get(`/api/v1/admin/uploads/${upload.uploadId}`);
  const complete = await api.post(`/api/v1/admin/uploads/${upload.uploadId}/complete`, { headers: { ...headers(), "If-Match": current.headers().etag } });
  expect(complete.ok()).toBe(true);
  await expect.poll(async () => (await (await api.get(`/api/v1/admin/uploads/${upload.uploadId}`)).json()).state).toBe("COMPLETE");
  const imported = await api.post("/api/v1/admin/imports", {
    headers: headers(), data: { uploadId: upload.uploadId, targetPlatformInstanceId: target!.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: [] },
  });
  expect(imported.ok()).toBe(true);
  const { importJobId } = await imported.json() as { importJobId: string };
  await expect.poll(async () => (await (await api.get(`/api/v1/admin/imports/${importJobId}`)).json()).state).toBe("REVIEW_PENDING");
  const discarded = await api.post(`/api/v1/admin/import-batches/IMPORT/${importJobId}/discard`, { headers: headers(), data: {} });
  expect(discarded.ok()).toBe(true);
  await expect.poll(async () => {
    const analysis = await api.get("/api/v1/admin/storage-analysis");
    return (await analysis.json()).details.cleanupCandidates.blobCount;
  }).toBeGreaterThan(0);
}
