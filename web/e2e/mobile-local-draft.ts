import { expect, type Page } from "@playwright/test";

export async function expectMobileLocalDraftNotice(page: Page) {
  const response = await page.request.get("/api/v1/auth/context");
  expect(response.ok()).toBe(true);
  const { user } = await response.json() as { user: { userId: string } };
  // This local banner fixture is discarded through the UI and never uploaded.
  await page.evaluate(async (userId) => {
    await new Promise<void>((resolve, reject) => {
      const request = indexedDB.open("retrom-game-save-drafts-v1", 1);
      request.onupgradeneeded = () => request.result.createObjectStore("drafts", { keyPath: ["userId", "launchId"] });
      request.onerror = () => reject(request.error);
      request.onsuccess = () => {
        const db = request.result;
        const tx = db.transaction("drafts", "readwrite");
        tx.objectStore("drafts").put({
          userId, launchId: "00000000-0000-4000-8000-000000000042", title: "手机草稿提示测试",
          restored: false, updatedAtMs: 1000,
          payload: { checkpoint: { bytes: Uint8Array.of(1), format: "native-v1", metadata: null },
            screenshot: null, source: "GAME_SAVE", requestId: "phone-draft-layout", name: "测试草稿" },
        });
        tx.oncomplete = () => { db.close(); resolve(); };
        tx.onerror = () => { db.close(); reject(tx.error); };
      };
    });
  }, user.userId);
  await page.reload();
  const notice = page.getByRole("complementary", { name: "未提交的本地游戏存档" });
  await expect(notice).toBeVisible();
  for (const viewport of [{ width: 390, height: 844 }, { width: 844, height: 390 }]) {
    await page.setViewportSize(viewport);
    const banner = await notice.boundingBox();
    const nav = await page.getByRole("navigation", { name: "手机主导航" }).boundingBox();
    expect(banner!.y).toBeGreaterThanOrEqual(0);
    expect(banner!.y + banner!.height).toBeLessThanOrEqual(nav!.y - 8);
    const action = await notice.getByRole("button", { name: "处理本地草稿" }).boundingBox();
    expect(action!.height).toBeGreaterThanOrEqual(44);
  }
  await notice.getByRole("button", { name: "处理本地草稿" }).click();
  const dialog = page.getByRole("alertdialog", { name: "处理未提交的游戏存档" });
  await expect(dialog).toContainText("不是关闭页面时的即时进度");
  await dialog.getByRole("button", { name: "丢弃草稿" }).click();
  await expect(notice).toBeHidden();
  await page.setViewportSize({ width: 390, height: 844 });
}
