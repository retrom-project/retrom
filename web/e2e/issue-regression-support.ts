import {createHash} from "node:crypto";
import {readFileSync} from "node:fs";
import path from "node:path";
import {expect, test as base, type Page} from "@playwright/test";
import type {SourceImportSummary} from "../features/server-import/source-import-model";

export async function login(page: Page) {
  const response = await page.request.post("/api/v1/auth/login", {data: {username: "test", password: "test"},
    headers: {Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000"}});
  expect(response.ok()).toBe(true);
  const {csrfToken} = await response.json() as {csrfToken: string};
  return {Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000", "X-Retrom-Csrf": csrfToken};
}

export const test = base.extend<{sourceDrafts: string[]}>({
  sourceDrafts: async ({page}, runTest) => {
    const ids: string[] = [];
    await runTest(ids);
    const headers = await login(page);
    for (const id of ids) {
      const response = await page.request.get(`/api/v1/admin/source-imports/${id}`);
      expect(response.ok()).toBe(true);
      const plan = await response.json() as SourceImportSummary;
      expect(plan.state).toBe("AWAITING_MAPPING");
      const removed = await page.request.delete(`/api/v1/admin/source-imports/${id}`, {
        headers: {...headers, "If-Match": `"v${plan.version}"`, "Idempotency-Key": crypto.randomUUID()},
      });
      expect(removed.status(), await removed.text()).toBe(204);
      expect((await page.request.get(`/api/v1/admin/source-imports/${id}`)).status()).toBe(404);
    }
  },
});

export async function createOrdinaryImport(page: Page, headers: Record<string, string>) {
  const bytes = readFileSync(path.join(process.cwd(), "../testdata/public-roms/gba-smoke/gba-smoke.gba"));
  const writeHeaders = () => ({...headers, "Idempotency-Key": crypto.randomUUID()});
  const created = await page.request.post("/api/v1/admin/uploads", {headers: writeHeaders(),
    data: {sourceType: "FILES", files: [{clientFileId: "timezone", relativePath: "Timezone.gba", sizeBytes: bytes.length}]}});
  expect(created.ok(), await created.text()).toBe(true);
  const upload = await created.json() as {uploadId: string; files: {fileId: string}[]};
  const part = await page.request.put(`/api/v1/admin/uploads/${upload.uploadId}/files/${upload.files[0].fileId}/parts/0`, {
    headers: {...headers, "Content-Type": "application/octet-stream", "Content-Range": `bytes 0-${bytes.length - 1}/${bytes.length}`,
      "Content-Digest": `sha-256=:${createHash("sha256").update(bytes).digest("base64")}:`}, data: bytes,
  });
  expect(part.ok(), await part.text()).toBe(true);
  const current = await page.request.get(`/api/v1/admin/uploads/${upload.uploadId}`);
  const completed = await page.request.post(`/api/v1/admin/uploads/${upload.uploadId}/complete`, {
    headers: {...writeHeaders(), "If-Match": current.headers().etag},
  });
  expect(completed.status(), await completed.text()).toBe(202);
  const {jobId} = await completed.json() as {jobId: string};
  await expect.poll(async () => {
    const job = await (await page.request.get(`/api/v1/admin/jobs/${jobId}`)).json() as {state: string};
    return job.state;
  }, {timeout: 30_000}).toBe("SUCCEEDED");
  const directories = await (await page.request.get("/api/v1/admin/platform-instances")).json() as {
    items: {id: string; platformId: string; defaultCoreId: string}[];
  };
  const directory = directories.items.find(item => item.platformId === "gba" && item.defaultCoreId === "mgba");
  expect(directory).toBeTruthy();
  const imported = await page.request.post("/api/v1/admin/imports", {headers: writeHeaders(),
    data: {uploadId: upload.uploadId, targetPlatformInstanceId: directory!.id, metadataProvider: "NONE", tagIds: []}});
  expect(imported.status(), await imported.text()).toBe(202);
  const {importJobId} = await imported.json() as {importJobId: string};
  const detail = await page.request.get(`/api/v1/admin/imports/${importJobId}`);
  expect(detail.ok()).toBe(true);
  return await detail.json() as {id: string; createdAtMs: number};
}
