import { expect, test, type Page } from "@playwright/test";
import type { FavoriteGame } from "../features/favorites/favorite-api";

function favorite(index: number): FavoriteGame {
  return {
    gameId: `01980000-0000-7000-8000-00000000000${index}`,
    title: `收藏布局示例 ${index} · 一个需要省略显示的较长游戏名称`,
    status: "PUBLISHED", availability: "PUBLISHED", coverUrl: null,
    platform: { id: "gba", name: "Game Boy Advance" },
    platformInstance: { id: "one", name: "掌机游戏" },
    defaultCore: { id: "mgba", name: "mGBA" },
    releaseYear: null, createdAtMs: 1000, lastPlayedAtMs: null, tags: [],
    favorite: { favoritedAtMs: 1000 - index, folderIds: [] },
  };
}

// Deterministic list-response layout regression. Real favorite writes and
// navigation use the product acceptance flow; this case changes no user data.
test("ACC-UI-005 home favorites fit the sidebar with stable space for zero to three games", async ({ page }, testInfo) => {
  test.setTimeout(90_000);
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const login = await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } });
  expect(login.ok()).toBe(true);
  let items: FavoriteGame[] = [];
  await page.route("**/api/v1/favorites?*", async (route) => {
    const url = new URL(route.request().url());
    expect(url.searchParams.get("sort")).toBe("FAVORITED_DESC");
    expect(url.searchParams.get("limit")).toBe("3");
    await route.fulfill({ json: { generatedAtMs: 1000, summary: { favoriteCount: items.length, uncategorizedCount: items.length, folderCount: 0 }, folders: [], platforms: [], totalCount: items.length, items, nextCursor: null } });
  });
  await page.goto("/");
  const section = page.getByRole("region", { name: "收藏的游戏", exact: true });
  await expect(section.getByText("把喜欢的游戏留在这里")).toBeVisible();
  await expect(section.getByRole("link", { name: "浏览游戏库" })).toHaveAttribute("href", "/library");
  await expect(section.locator(".home-favorite-game")).toHaveCount(0);
  items = [favorite(1), favorite(2), favorite(3)];
  await page.reload();
  await expect(section.locator(".home-favorite-game")).toHaveCount(3);
  await expect(section.getByRole("link", { name: "查看全部" })).toHaveAttribute("href", "/favorites");
  const sizes = testInfo.project.name === "chrome-1280" ? [[1280, 800], [1920, 950], [2086, 920], [1920, 900]] : [[2560, 1440], [2560, 1360], [2560, 1300], [2560, 1200], [2560, 1100], [3840, 2160]];
  for (const [width, height] of sizes) {
    await page.setViewportSize({ width: width!, height: height! });
    const layout = await section.evaluate((element) => {
      const panel = element.closest(".home-quick-panel")!.getBoundingClientRect();
      const rows = [...element.querySelectorAll(".home-favorite-game")].map((game) => game.getBoundingClientRect());
      return { sectionContained: element.getBoundingClientRect().bottom <= panel.bottom, contained: rows.every((row) => row.top >= panel.top && row.bottom <= panel.bottom && row.right <= panel.right), overlap: rows.some((row, index) => index > 0 && row.top < rows[index - 1]!.bottom), pageWidth: document.documentElement.scrollWidth, viewport: innerWidth, height: element.getBoundingClientRect().height, bottomGap: panel.bottom - rows.at(-1)!.bottom, coverHeight: element.querySelector(".home-favorite-cover")!.getBoundingClientRect().height };
    });
    await expectHomeLinkStyles(page);
    expect(layout.contained).toBe(true);
    expect(layout.sectionContained).toBe(true);
    expect(layout.overlap).toBe(false);
    expect(layout.pageWidth).toBeLessThanOrEqual(layout.viewport);
    if (width! >= 1800) {expect(layout.bottomGap).toBeLessThanOrEqual(24);}
    if (width === 2560 && height! >= 1300) {expect(layout.coverHeight).toBeGreaterThanOrEqual(49);}
    const fullLayout = await quickLayout(page);
    for (const count of [0, 1, 2]) {
      items = [favorite(1), favorite(2), favorite(3)].slice(0, count);
      await page.reload();
      if (count === 0) {await expect(section.getByRole("link", { name: "浏览游戏库" })).toBeVisible();}
      else {await expect(section.locator(".home-favorite-game")).toHaveCount(count);}
      if (width === 2560 && height === 1360 && count < 2) {
        await page.locator(".home-quick-panel").screenshot({ path: testInfo.outputPath(`quick-${count}.png`), scale: "css" });
      }
      const sparseLayout = await quickLayout(page);
      for (let i = 0; i < fullLayout.length; i++) {
        expect(Math.abs(sparseLayout[i]! - fullLayout[i]!)).toBeLessThanOrEqual(1);
      }
    }
    items = [favorite(1), favorite(2), favorite(3)];
    await page.reload();
    await expect(section.locator(".home-favorite-game")).toHaveCount(3);
  }
  await expectAccountMenu(page);
  await expectPlatformPin(page);
});

async function expectHomeLinkStyles(page: Page) {
  const styles = await page.locator(".home-favorites-head a, .home-section-head > a").evaluateAll((links) => links.map((link) => {
    const style = getComputedStyle(link);
    return { color: style.color, size: style.fontSize, weight: style.fontWeight };
  }));
  expect(styles).toHaveLength(4);
  for (const style of styles) {expect(style).toEqual(styles[0]);}
}

async function expectPlatformPin(page: Page) {
  const card = page.locator(".home-platform-card").first();
  const pin = card.getByRole("button");
  const header = page.locator(".home-quick-panel .home-panel-head");
  await header.click();
  await expect(pin).toHaveCSS("opacity", "0");
  await card.hover();
  await expect(pin).toHaveCSS("opacity", "1");
  await expect(pin).toHaveText("📌");
  const position = await pin.evaluate((button) => {
    const bounds = button.getBoundingClientRect();
    const cardBounds = button.closest("article")!.getBoundingClientRect();
    return { top: bounds.top - cardBounds.top, right: cardBounds.right - bounds.right };
  });
  expect(position.top).toBeLessThanOrEqual(10);
  expect(position.right).toBeLessThanOrEqual(10);
  await header.click();
  await pin.focus();
  await expect(pin).toHaveCSS("opacity", "1");
  await pin.press("Enter");
  await header.click();
  await expect(pin).toHaveAttribute("aria-pressed", "true");
  await expect(pin).toHaveCSS("opacity", "1");
  await page.reload();
  await expect(pin).toHaveAttribute("aria-pressed", "true");
  await expect(pin).toHaveCSS("opacity", "1");
  await pin.click();
  await header.click();
  await expect(pin).toHaveAttribute("aria-pressed", "false");
  await expect(pin).toHaveCSS("opacity", "0");
}

async function quickLayout(page: Page) {
  return page.locator(".home-quick-link, .home-favorites").evaluateAll((elements) => elements.flatMap((element) => {
    const rect = element.getBoundingClientRect();
    return [rect.y, rect.height];
  }));
}

async function expectAccountMenu(page: Page) {
  await page.locator(".account-menu summary").click();
  const menu = page.locator(".account-menu-popover");
  await expect(menu.locator("svg")).toHaveCount(2);
  const positions = await menu.locator("a, button").evaluateAll((items) => items.map((item) => {
    const icon = item.querySelector("svg")!.getBoundingClientRect();
    const text = [...item.childNodes].find((node) => node.nodeType === Node.TEXT_NODE && node.textContent?.trim())!;
    const range = document.createRange();
    range.selectNode(text);
    return { textLeft: range.getBoundingClientRect().left, iconWidth: icon.width, iconHeight: icon.height };
  }));
  expect(positions[0]).toEqual(positions[1]);
  const account = menu.getByRole("link", { name: "账户设置" });
  await account.focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/account$/);
  await expect(page.getByRole("heading", { name: "账户设置" })).toBeVisible();
  await page.goto("/");
  await expect(page.locator(".home-quick-panel")).toBeVisible();
}
