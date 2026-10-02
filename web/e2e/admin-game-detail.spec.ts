import { expect, test } from "@playwright/test";

test("ACC-UI-005 management media, read-only files and tag navigation", async ({ page }, testInfo) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  expect((await page.request.post("/api/v1/auth/login", {
    headers: { Origin: origin }, data: { username: "test", password: "test" },
  })).ok()).toBe(true);
  const response = await page.request.get("/api/v1/admin/games?limit=100");
  expect(response.ok()).toBe(true);
  const gameId: string = (await response.json()).items.find((game: { status: string }) => game.status === "PUBLISHED").gameId;
  await page.goto(`/admin/games/${gameId}`);
  await expect(page.getByRole("link", { name: "返回游戏管理", exact: true })).toBeVisible();
  const cover = page.getByRole("tab", { name: "封面", exact: true });
  const video = page.getByRole("tab", { name: "视频", exact: true });
  const headingHeights = await page.locator(".admin-game-primary-grid > .panel > .panel-head").evaluateAll((elements) => elements.map((element) => element.getBoundingClientRect().height));
  expect(headingHeights).toHaveLength(2);
  expect(Math.abs(headingHeights[0] - headingHeights[1])).toBeLessThan(1);
  await expect(cover).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".admin-game-media video")).toHaveCount(0);
  await cover.focus();
  await page.keyboard.press("End");
  await expect(video).toBeFocused();
  await expect(video).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("tabpanel", { name: "视频", exact: true })).toBeVisible();
  await page.locator(".admin-game-media").screenshot({ path: testInfo.outputPath("management-video.png") });
  await page.keyboard.press("Home");
  await expect(cover).toBeFocused();
  await expect(page.locator(".admin-game-media video")).toHaveCount(0);

  const files = page.getByRole("region", { name: "游戏文件", exact: true });
  await expect(files.getByText("SHA-256", { exact: true }).first()).toBeVisible();
  await expect(files.getByText("MD5", { exact: true }).first()).toBeVisible();
  await expect(files.getByText("CRC32", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "替换游戏文件" })).toHaveCount(0);
  await expect(page.getByText("技术详情", { exact: true })).toHaveCount(0);
  await expect(page.locator(".admin-game-hero-copy .tag-chips")).toHaveCount(0);
  await files.screenshot({ path: testInfo.outputPath("management-files.png") });

  const tagsResponse = await page.request.get("/api/v1/admin/tags?status=ACTIVE&limit=100");
  expect(tagsResponse.ok()).toBe(true);
  const activeTags = (await tagsResponse.json()).items as Array<{ tagId: string; name: string }>;
  const tagPicker = page.getByRole("combobox", { name: "标签", exact: true });
  await tagPicker.click();
  const manageTags = page.getByRole("link", { name: "前往标签管理", exact: true });
  if (activeTags.length) {
    // The next viewport shares the catalog populated by the prior tag tests.
    // Check the populated branch against real API facts rather than assuming
    // the entire multi-project run still has an empty tag dictionary.
    await expect(manageTags).toHaveCount(0);
    await expect(page.getByRole("listbox")).toBeVisible();
    const detailResponse = await page.request.get(`/api/v1/admin/games/${gameId}`);
    expect(detailResponse.ok()).toBe(true);
    const selected = new Set((await detailResponse.json()).tags.map((tag: { tagId: string }) => tag.tagId));
    await expect(page.getByRole("listbox").getByRole("option")).toHaveCount(activeTags.filter(tag => !selected.has(tag.tagId)).length);
    await tagPicker.press("Escape");
    await expect(tagPicker).toHaveAttribute("aria-expanded", "false");
    return;
  }
  await expect(manageTags).toBeVisible();
  await manageTags.click({ delay: 200 });
  await expect(page).toHaveURL(/\/admin\/tags$/);
  await page.goto(`/admin/games/${gameId}`);
  await page.getByRole("combobox", { name: "标签", exact: true }).click();
  await manageTags.focus();
  // Hold focus beyond the old input-blur timer before keyboard activation.
  await page.waitForTimeout(200);
  await expect(manageTags).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/admin\/tags$/);
});
