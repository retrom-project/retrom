import { expect, test } from "@playwright/test";
import axe from "axe-core";
import { evidencePath, noPageOverflow } from "./acceptance-support";

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
const categoryOrder = [
  "GAME_CONTENT", "BIOS", "SAVES", "MEDIA", "WORKFLOW", "RUNTIME_SNAPSHOT",
  "SHARED_DURABLE", "OTHER_REFERENCED", "UNREFERENCED",
];

type StorageResponse = {
  scope: string;
  generatedAtMs: number;
  totals: { registeredBytes: string; protectedBytes: string; unreferencedBytes: string; blobCount: number };
  categories: Array<{ code: string; bytes: string; blobCount: number }>;
  details: {
    saveStates: { activeCount: number; deletedCount: number; stateReferenceBytes: string; screenshotReferenceBytes: string };
    cleanupCandidates: { blobCount: number; bytes: string };
  };
  excluded: string[];
};

test.beforeEach(async ({ page }) => {
  const login = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" }, headers: { Origin: origin },
  });
  expect(login.ok()).toBe(true);
});

test("ACC-STOR-001 registered CAS analysis is exact, private, responsive, and read-only", async ({ page }, testInfo) => {
  const response = await page.request.get("/api/v1/admin/storage-analysis");
  expect(response.status()).toBe(200);
  expect(response.headers()["cache-control"]).toBe("private, no-store");
  const body = await response.json() as StorageResponse;
  expect(body.scope).toBe("REGISTERED_CAS_PAYLOAD_V1");
  expect(body.categories.map((category) => category.code)).toEqual(categoryOrder);
  expect(body.excluded).toEqual([
    "DATABASE_FILES", "UPLOAD_PARTS", "JOB_SCRATCH", "DEPENDENCY_ROOT",
    "FILESYSTEM_OVERHEAD", "UNREGISTERED_ORPHANS", "VOLUME_FREE_SPACE",
  ]);
  const registered = BigInt(body.totals.registeredBytes);
  const protectedBytes = BigInt(body.totals.protectedBytes);
  const unreferenced = BigInt(body.totals.unreferencedBytes);
  expect(registered).toBeGreaterThan(0n);
  expect(protectedBytes + unreferenced).toBe(registered);
  expect(body.categories.reduce((sum, category) => sum + BigInt(category.bytes), 0n)).toBe(registered);
  expect(body.categories.reduce((sum, category) => sum + category.blobCount, 0)).toBe(body.totals.blobCount);
  const serialized = JSON.stringify(body);
  for (const forbidden of ["sha256", "blobId", "launchId", "capability", "originalFilename", "relativePath"]) {
    expect(serialized).not.toContain(forbidden);
  }
  expect((await page.request.get("/api/v1/admin/storage-analysis?scope=all")).status()).toBe(400);

  const initialViewport = page.viewportSize() ?? { width: 1280, height: 800 };
  const viewports = [{ width: 320, height: 568 }, { width: 768, height: 1024 }, initialViewport];
  for (const viewport of viewports) {
    await page.setViewportSize(viewport);
    await page.goto("/admin/storage");
    await expect(page.getByRole("heading", { name: "容量分析", exact: true })).toBeVisible();
    await expect(page.getByRole("region", { name: "按用途分析" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "仅计算已登记 CAS payload" })).toBeVisible();
    await expect(page.getByText("REGISTERED_CAS_PAYLOAD_V1", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: /清理|删除|回收/ })).toHaveCount(0);
    await noPageOverflow(page);
  }

  await page.setViewportSize(initialViewport);
  await page.goto("/admin/storage");
  const navigation = page.getByRole("navigation", { name: "主要导航" });
  const labels = await navigation.getByRole("link").allTextContents();
  expect(labels.slice(-2)).toEqual(["运行依赖", "容量分析"]);
  await expect(navigation.getByRole("link", { name: "容量分析" })).toHaveAttribute("aria-current", "page");
  const refreshResponse = page.waitForResponse((candidate) => candidate.url().endsWith("/api/v1/admin/storage-analysis"));
  await page.getByRole("button", { name: "刷新分析" }).click();
  await expect((await refreshResponse).status()).toBe(200);
  await expect(page.getByText(/^统计生成于 /)).toBeVisible();
  await page.evaluate(axe.source);
  const serious = await page.evaluate(async () => {
    const result = await window.axe.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
  });
  expect(serious).toEqual([]);
  await page.screenshot({ path: evidencePath(testInfo, "storage-analysis.png"), fullPage: true });
});
