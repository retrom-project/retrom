import { expect, test } from "@playwright/test";
import type { FavoritePage } from "../features/favorites/favorite-api";

test("ACC-FAV-001 floating navigation and compact card controls remain usable", async ({ page }, testInfo) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  expect((await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } })).ok()).toBe(true);
  await page.goto("/favorites");
  const sidebar = page.locator(".sidebar");
  await expect(sidebar.locator(".account-copy")).not.toContainText("@");
  const menuSizes = await sidebar.evaluate((element) => {
    const rect = (selector: string) => element.querySelector(selector)!.getBoundingClientRect();
    return { nav: rect(".nav-link").height, account: rect(".sidebar-account-row").height, context: element.querySelector(".context-switch")?.getBoundingClientRect().height, avatar: rect(".account-initial").height };
  });
  expect(menuSizes.account).toBe(menuSizes.nav);
  if (menuSizes.context) {expect(menuSizes.context).toBe(menuSizes.nav);}
  expect(menuSizes.avatar).toBe(26);
  const fixture: FavoritePage = {
    generatedAtMs: 1000, summary: { favoriteCount: 2, folderCount: 1, uncategorizedCount: 0 },
    folders: [{ folderId: "folder", name: "想玩", visibleGameCount: 2, version: 1, createdAtMs: 1000, updatedAtMs: 1000 }],
    platforms: [], totalCount: 2, nextCursor: null,
    items: [1, 2].map((index) => ({ gameId: `layout-${index}`, title: `收藏示例 ${index}`, status: "PUBLISHED", availability: "PUBLISHED", platform: { id: "gba", name: "Game Boy Advance" }, platformInstance: { id: "gba", name: "掌机游戏" }, defaultCore: { id: "mgba", name: "mGBA" }, coverUrl: null, releaseYear: 2000, createdAtMs: 1000, lastPlayedAtMs: null, tags: [{ tagId: "tag", name: "掌机精选" }], favorite: { favoritedAtMs: 1000, folderIds: ["folder"] } })),
  };
  await page.route("**/api/v1/favorites?*", (route) => route.fulfill({ json: fixture }));
  await page.goto("/favorites");
  await page.getByRole("combobox", { name: "排序方式" }).selectOption("TITLE_ASC");
  const card = page.locator(".favorite-game-card").first();
  await expect(card.getByRole("heading", { name: "收藏示例 1" })).toBeVisible();
  await expect(card.locator(".tag-chips, .favorite-tags")).toHaveCount(0);
  await expect(page.locator(".favorite-view-head")).toHaveCount(0);
  const navigation = page.getByRole("complementary", { name: "收藏导航" });
  const start = (await navigation.boundingBox())!;
  const toolbar = (await page.locator(".favorite-toolbar").boundingBox())!;
  expect(start.y).toBeGreaterThan(toolbar.y + toolbar.height);
  expect(start.x).toBeGreaterThan(toolbar.x + toolbar.width / 2);
  const drag = page.getByRole("button", { name: "移动收藏导航" });
  await drag.hover(); await page.mouse.down();
  await page.mouse.move(start.x - 100, start.y + 100, { steps: 5 }); await page.mouse.up();
  const moved = (await navigation.boundingBox())!;
  expect(moved.x).toBeLessThan(start.x - 50);
  await drag.focus(); await page.keyboard.press("ArrowDown");
  expect((await navigation.boundingBox())!.y).toBeCloseTo(moved.y + 20, 0);
  await page.getByRole("button", { name: "折叠收藏导航" }).click();
  expect((await navigation.boundingBox())!.height).toBeLessThan(moved.height);
  await expect(navigation.getByRole("button", { name: /全部收藏/ })).toHaveCount(0);
  await page.getByRole("button", { name: "展开收藏导航" }).click();
  await expect(navigation.getByRole("button", { name: /全部收藏/ })).toBeVisible();
  await page.getByRole("button", { name: "批量整理", exact: true }).click();
  await expect(card.locator(".favorite-heart")).toBeHidden();
  await expect(card.locator(".favorite-select")).toBeVisible();
  await page.getByRole("button", { name: "完成整理", exact: true }).click();
  await expect(card.locator(".favorite-heart")).toBeVisible();
  for (const width of [testInfo.project.use.viewport?.width ?? 1280, 390]) {
    await page.setViewportSize({ width, height: 844 });
    await expect(drag).toBeVisible();
    await page.getByRole("button", { name: "折叠收藏导航" }).click();
    await expect(navigation.getByRole("button", { name: /全部收藏/ })).toHaveCount(0);
    await page.getByRole("button", { name: "展开收藏导航" }).click();
    await expect.poll(() => navigation.evaluate((element) => {
      const box = element.getBoundingClientRect();
      return box.x >= 8 && box.y >= 8 && box.right <= innerWidth - 7 && box.bottom <= innerHeight - 7;
    })).toBe(true);
    const poster = card.locator(".favorite-poster");
    const beforeTitle = await poster.locator("small, :scope > span").evaluateAll((elements) => elements.map((element) => element.getBoundingClientRect().y));
    await poster.locator("strong").evaluate((element) => {element.textContent = "过长标题验证固定排版".repeat(40);});
    expect(await poster.locator("small, :scope > span").evaluateAll((elements) => elements.map((element) => element.getBoundingClientRect().y))).toEqual(beforeTitle);
    await expect(poster.locator("strong")).toHaveCSS("-webkit-line-clamp", width === 390 ? "2" : "3");
    const positions = await card.evaluate((element) => {
      const cover = element.querySelector(".favorite-game-cover")!.getBoundingClientRect();
      const heart = element.querySelector(".favorite-heart")!.getBoundingClientRect();
      const menu = element.querySelector(".favorite-manage")!.getBoundingClientRect();
      return { heartGap: heart.x - cover.x, menuGap: cover.right - menu.right, deltaY: menu.y - heart.y, deltaHeight: menu.height - heart.height };
    });
    expect(positions.heartGap).toBeCloseTo(9, 0);
    expect(positions.menuGap).toBeCloseTo(9, 0);
    expect(positions.deltaY).toBe(0); expect(positions.deltaHeight).toBe(0);
    await page.screenshot({ path: testInfo.outputPath(`favorites-${width}.png`), scale: "css" });
  }
});
