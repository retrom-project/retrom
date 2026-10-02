import { expect, type Page } from "@playwright/test";
import type { SourceItemList } from "../features/server-import/source-import-model";

export async function expectSourceHandoffDuringCleanup(page: Page, sourceImportId: string) {
  const pattern = `**/api/v1/admin/source-imports/${sourceImportId}/items?**`;
  let initialSnapshot = true;
  let itemReads = 0;
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/admin/source-imports/${sourceImportId}/items?limit=20`);
    const result = await response.json() as SourceItemList;
    return result.items[0].payloadState;
  }).toBe("RELEASED");
  await page.route(pattern, async (route) => {
    itemReads += 1;
    const response = await route.fetch();
    expect(response.ok()).toBe(true);
    const result = await response.json() as SourceItemList;
    expect(result.items).toHaveLength(1);
    expect(result.items[0].payloadState).toBe("RELEASED");
    if (initialSnapshot) {
      // Model a snapshot taken before the separate cleanup job finished. The
      // following reads use the actual source result; no stored data is changed.
      initialSnapshot = false;
      result.items[0].payloadState = "RELEASING";
      result.items[0].media = { cover: "MISSING", video: "MISSING" };
    }
    await route.fulfill({ response, json: result });
  });
  try {
    await page.getByRole("button", { name: "应用筛选" }).click();
    const results = page.getByRole("table", { name: "游戏导入结果" });
    await expect(results).toContainText("已移交审核");
    await expect(results).not.toContainText("封面 MISSING");
    await expect(results).not.toContainText("视频 MISSING");
    await expect(results).not.toContainText("导入临时副本已清理");
    await expect.poll(() => itemReads, { timeout: 12_000 }).toBeGreaterThanOrEqual(2);
    await expect(results).toContainText("已移交审核");
    await expect(results).not.toContainText("封面 MISSING");
    await expect(results).not.toContainText("视频 MISSING");
  } finally {
    await page.unroute(pattern);
  }
}
