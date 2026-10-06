import { createHash, randomUUID } from "node:crypto";
import { expect, type Page } from "@playwright/test";

export async function createReviewFixture(page: Page, name: string, csrfToken: string) {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const headers = { Origin: origin, "X-Retrom-Csrf": csrfToken, "Idempotency-Key": randomUUID() };
  const directories = await page.request.get("/api/v1/admin/platform-instances");
  const target = (await directories.json() as { items: Array<{ id: string; platformId: string; defaultCoreId: string }> }).items.find((entry) => entry.platformId === "gba" && entry.defaultCoreId === "mgba");
  if (!target) {throw new Error("GBA review directory missing");}
  // Project-owned bytes create an independent pending review without launching a core.
  const bytes = Buffer.from(`Retrom pending review fixture ${name}`);
  const created = await page.request.post("/api/v1/admin/uploads", { headers, data: { sourceType: "FILES", files: [{ clientFileId: "review", relativePath: "review-fixture.gba", sizeBytes: bytes.length }] } });
  expect(created.ok()).toBe(true);
  const upload = await created.json() as { uploadId: string; files: Array<{ fileId: string }> };
  const part = await page.request.put(`/api/v1/admin/uploads/${upload.uploadId}/files/${upload.files[0].fileId}/parts/0`, {
    headers: { ...headers, "Content-Type": "application/octet-stream", "Content-Range": `bytes 0-${bytes.length - 1}/${bytes.length}`, "Content-Digest": `sha-256=:${createHash("sha256").update(bytes).digest("base64")}:` }, data: bytes,
  });
  expect(part.ok()).toBe(true);
  const current = await page.request.get(`/api/v1/admin/uploads/${upload.uploadId}`);
  const complete = await page.request.post(`/api/v1/admin/uploads/${upload.uploadId}/complete`, { headers: { ...headers, "If-Match": current.headers().etag } });
  expect(complete.ok()).toBe(true);
  const job = await complete.json() as { jobId: string };
  await expect.poll(async () => (await (await page.request.get(`/api/v1/admin/jobs/${job.jobId}`)).json() as { state: string }).state).toBe("SUCCEEDED");
  const imported = await page.request.post("/api/v1/admin/imports", { headers: { ...headers, "Idempotency-Key": randomUUID() }, data: { uploadId: upload.uploadId, targetPlatformInstanceId: target.id, metadataProvider: "NONE", tagIds: [], contentMode: "STANDARD" } });
  expect(imported.ok()).toBe(true);
  const batch = await imported.json() as { importJobId: string };
  let items: Array<{ itemId: string }> = [];
  await expect.poll(async () => {
    items = (await (await page.request.get(`/api/v1/admin/reviews?importJobId=${batch.importJobId}`)).json() as { items: Array<{ itemId: string }> }).items;
    return items.length;
  }).toBe(1);
  return items[0].itemId;
}
