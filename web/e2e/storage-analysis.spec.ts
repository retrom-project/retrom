import { expectPhoneAdminNotice } from "./admin-phone-support";
import { expect, test } from "@playwright/test";
import axe from "axe-core";
import { evidencePath, noPageOverflow } from "./acceptance-support";
import { seedStorageCleanupCandidate } from "./storage-cleanup-fixture";

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
const categoryOrder = [
  "GAME_CONTENT", "BIOS", "SAVES", "MEDIA", "WORKFLOW", "PENDING_DELETE",
];

type StorageResponse = {
  scope: string;
  generatedAtMs: number;
  totals: { registeredBytes: string; retainedBytes: string; pendingDeleteBytes: string; fileCount: number };
  categories: Array<{ code: string; bytes: string; fileCount: number }>;
  details: {
    saveStates: { activeCount: number; deletedCount: number; stateBytes: string; screenshotBytes: string };
    cleanupCandidates: { fileCount: number; bytes: string };
  };
  excluded: string[];
};

test.beforeEach(async ({ page }) => {
  const login = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" }, headers: { Origin: origin },
  });
  expect(login.ok()).toBe(true);
});

test("ACC-STOR-001 registered file analysis is exact, private, responsive, and exposes guarded cleanup", async ({ page }, testInfo) => {
  await seedStorageCleanupCandidate(page.request, origin);
  const response = await page.request.get("/api/v1/admin/storage-analysis");
  expect(response.status()).toBe(200);
  expect(response.headers()["cache-control"]).toBe("private, no-store");
  const body = await response.json() as StorageResponse;
  expect(body.scope).toBe("OWNED_FILES_V1");
  expect(body.categories.map((category) => category.code)).toEqual(categoryOrder);
  expect(body.excluded).toEqual([
    "DATABASE_FILES", "UPLOAD_PARTS", "JOB_SCRATCH", "DEPENDENCY_ROOT",
    "FILESYSTEM_OVERHEAD", "UNREGISTERED_ORPHANS", "VOLUME_FREE_SPACE",
  ]);
  const registered = BigInt(body.totals.registeredBytes);
  const retainedBytes = BigInt(body.totals.retainedBytes);
  const unreferenced = BigInt(body.totals.pendingDeleteBytes);
  expect(registered).toBeGreaterThan(0n);
  expect(retainedBytes + unreferenced).toBe(registered);
  expect(body.categories.reduce((sum, category) => sum + BigInt(category.bytes), 0n)).toBe(registered);
  expect(body.categories.reduce((sum, category) => sum + category.fileCount, 0)).toBe(body.totals.fileCount);
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
    if (viewport.width < 768) {
      await expectPhoneAdminNotice(page);
      await expect(page.getByRole("button", { name: "立即清理" })).toHaveCount(0);
      await noPageOverflow(page);
      await page.screenshot({ path: evidencePath(testInfo, "storage-phone-notice.png"), fullPage: true });
      continue;
    }
    await expect(page.getByRole("heading", { name: "容量分析", exact: true })).toBeVisible();
    await expect(page.getByRole("region", { name: "按用途分析" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "仅计算已登记文件" })).toBeVisible();
    await expect(page.getByText("OWNED_FILES_V1", { exact: true })).toBeVisible();
    const cleanup = page.getByRole("button", { name: "立即清理" });
    await expect(cleanup).toBeVisible();
    if (await cleanup.isEnabled()) {
      await cleanup.click();
      const dialog = page.getByRole("alertdialog", { name: "立即删除待清理文件？" });
      await expect(dialog).toContainText("这会提交待删除文件并重试失败任务");
      await expect(dialog.getByRole("textbox")).toHaveCount(0);
      await dialog.getByRole("button", { name: "取消" }).click();
    }
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
  const cleanupButton = page.getByRole("button", { name: "立即清理", exact: true });
  await expect(cleanupButton).toBeEnabled();
  await cleanupButton.click();
  await page.getByRole("alertdialog").getByRole("button", { name: "立即清理", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText(/立即清理已完成/);
  await expect(page.getByLabel("清理候选大小，精确值 0 bytes", { exact: true })).toBeVisible();
  const candidates = page.locator(".storage-details article").filter({ hasText: "删除队列" });
  await expect(candidates).toContainText("0 个文件");
  await expect(page.getByLabel("等待回收，精确值 0 bytes", { exact: true })).toBeVisible();
  await expect(cleanupButton).toBeDisabled();
  await page.evaluate(axe.source);
  const serious = await page.evaluate(async () => {
    const result = await window.axe.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
  });
  expect(serious).toEqual([]);
  await page.screenshot({ path: evidencePath(testInfo, "storage-analysis.png"), fullPage: true });
});
