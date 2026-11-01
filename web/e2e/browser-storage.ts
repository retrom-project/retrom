import { expect } from "@playwright/test";
import type { Page, TestInfo } from "@playwright/test";

async function contentSnapshot(page: Page) {
  return page.evaluate(async () => {
    const database = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open("retrom-content-io-v1");
      request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error);
    });
    try {
      return await new Promise<{objects: number; bytes: number}>((resolve, reject) => {
        const tx = database.transaction(["objects", "generations"], "readonly");
        const count = tx.objectStore("objects").count(), generations = tx.objectStore("generations").getAll();
        tx.oncomplete = () => resolve({objects: count.result,
          bytes: (generations.result as Array<{committedBytes: number}>).reduce((sum, generation) => sum + generation.committedBytes, 0)});
        tx.onabort = () => reject(tx.error);
      });
    } finally {database.close();}
  });
}

export async function verifyBrowserStorage(page: Page, info: TestInfo, saveId: string) {
  const original = page.url(), before = await contentSnapshot(page);
  expect(before.objects).toBeGreaterThan(0); expect(before.bytes).toBeGreaterThan(0);
  const saved = await page.request.get(`/api/v1/saves/${saveId}/payload`);
  expect(saved.ok()).toBe(true);
  const savedBytes = await saved.body();
  const pwa = await page.evaluate(async () => {
    localStorage.setItem("retrom-storage-acceptance", "keep preference");
    return (await caches.keys()).filter(key => key.startsWith("retrom-pwa-")).sort();
  });
  await page.goto("/account");
  const panel = page.getByRole("region", { name: "本浏览器存储", exact: true });
  const clear = panel.getByRole("button", { name: "清理资源缓存", exact: true });
  await expect(clear).toBeEnabled(); await panel.scrollIntoViewIfNeeded();
  await expect(panel.getByText(/站点本地占用/)).toBeVisible();
  await expect(panel.getByText(/资源缓存有效数据/)).toBeVisible();
  await expect(panel.getByText("暂不可用", {exact: true})).toHaveCount(0);
  await page.screenshot({path: info.outputPath("browser-storage-populated.png"), fullPage: true});
  await clear.click();
  const dialog = page.getByRole("alertdialog");
  await expect(dialog.getByRole("button", {name: "取消", exact: true})).toBeFocused();
  await page.screenshot({path: info.outputPath("browser-storage-confirm.png")});
  await dialog.getByRole("button", {name: "取消", exact: true}).click();
  await expect(clear).toBeFocused(); expect(await contentSnapshot(page)).toEqual(before);
  await clear.click();
  await dialog.getByRole("button", {name: "清理缓存", exact: true}).click();
  await expect(panel.getByRole("status").filter({hasText: /^已清理 [1-9]/u})).toBeVisible();
  await expect(panel.getByText("0B · 0 项", {exact: true})).toBeVisible();
  await expect(clear).toBeDisabled();
  expect(await contentSnapshot(page)).toEqual({objects: 0, bytes: 0});
  expect(await page.evaluate(() => localStorage.getItem("retrom-storage-acceptance"))).toBe("keep preference");
  expect(await page.evaluate(async () => (await caches.keys()).filter(key => key.startsWith("retrom-pwa-")).sort())).toEqual(pwa);
  const retained = await page.request.get(`/api/v1/saves/${saveId}/payload`);
  expect(retained.ok()).toBe(true); expect(await retained.body()).toEqual(savedBytes);
  await page.screenshot({path: info.outputPath("browser-storage-empty.png"), fullPage: true});
  await page.goBack(); await expect(page).toHaveURL(original);
}
