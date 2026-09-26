import { expect, test, type Page } from "@playwright/test";
import { expectPaletteContrast, expectServerImportStatsContrast } from "./palette-contrast-support";
import { expectChineseGlyphs, expectStatusTextCentered } from "./status-alignment-support";
import { expectSearchComposition } from "./search-control-support";
import { expectCardRadii } from "./card-radius-support";
import { expectHomeStates } from "./home-state-support";
import { expectHomeHero, expectNaturalHomeFlow } from "./home-layout-support";
import { evidencePath, noPageOverflow } from "./acceptance-support";

// Explicit page PNGs are the visual evidence; retain DOM/source traces without a duplicate 4K filmstrip.
test.use({ trace: { mode: "retain-on-failure", screenshots: false, snapshots: true, sources: true } });

async function navigateUIPage(page: Page, route: string) {
  const link = page.locator(`a[href="${route}"]:visible`).first();
  if (await link.count()) {
    // Exercise the app's own navigation and avoid reloading the dev runtime for every route.
    await Promise.all([
      page.waitForURL(new URL(route, page.url()).href),
      link.click(),
    ]);
  } else {
    await page.goto(route);
  }
}

async function expectControlStyles(page: Page, mobile: boolean) {
  const fields = await page.locator("select, input:not([type=checkbox], [type=radio], [type=file], [type=hidden], [type=range]), textarea").evaluateAll((elements) => elements
    .filter((element) => element.checkVisibility() && element.getBoundingClientRect().width > 1)
    .map((element) => {
      const style = getComputedStyle(element);
      return { name: element.getAttribute("aria-label") ?? element.getAttribute("placeholder") ?? element.tagName,
        font: style.fontSize, weight: style.fontWeight, height: element.getBoundingClientRect().height,
        radius: style.borderRadius, appearance: style.appearance, tag: element.tagName };
    }));
  for (const field of fields) {
    expect(field.font, field.name).toBe(mobile ? "16px" : "14px");
    expect(field.weight, field.name).toBe("400");
    // Search inputs live inside a 44px bordered wrapper.
    expect(field.height, field.name).toBeGreaterThanOrEqual(42);
    if (field.tag === "SELECT") {
      expect(field.radius, field.name).toBe("6px");
      expect(field.appearance, field.name).toBe("none");
    }
  }
  const buttons = await page.locator(".button").evaluateAll((elements) => elements
    .filter((element) => element.checkVisibility())
    .map((element) => {
      const style = getComputedStyle(element), box = element.getBoundingClientRect();
      return { text: element.textContent, font: style.fontSize, height: box.height, width: box.width,
        scrollWidth: element.scrollWidth, clientWidth: element.clientWidth, radius: style.borderRadius };
    }));
  for (const button of buttons) {
    expect(button.font, button.text ?? "button").toBe("14px");
    expect(button.height).toBeGreaterThanOrEqual(44);
    expect(button.radius).toBe("6px");
    expect(button.scrollWidth).toBeLessThanOrEqual(button.clientWidth + 1);
  }
  await noPageOverflow(page);
}

async function expectMobileFavorites(page: Page) {
  const navigation = page.getByRole("complementary", { name: "收藏导航" });
  await expect(navigation).toHaveCSS("position", "static");
  await expect(navigation.getByRole("button", { name: /全部收藏/ })).toBeHidden();
  await page.getByRole("button", { name: "展开收藏导航" }).click();
  await expect(navigation.getByRole("button", { name: /全部收藏/ })).toBeVisible();
  const navigationBox = (await navigation.boundingBox())!;
  const gridBox = await page.locator(".favorite-game-grid, .favorite-empty").boundingBox();
  if (gridBox) { expect(navigationBox.y + navigationBox.height).toBeLessThanOrEqual(gridBox.y); }
  await page.getByRole("button", { name: "折叠收藏导航" }).click();
  await expect(navigation.getByRole("button", { name: /全部收藏/ })).toBeHidden();
}

async function expectRouteComposition(page: Page, route: string, width: number) {
  await expect(page.locator(".page-header .eyebrow")).toHaveCount(0);
  if (width! >= 1440 && route === "/") {
    await expectHomeHero(page);
    await expectNaturalHomeFlow(page);
  }
  if (width! >= 1440 && route.startsWith("/games/")) {
    await expect(page.locator(".launch-panel-head .status")).toHaveCount(0);
    const cover = (await page.locator(".game-detail-poster").boundingBox())!;
    expect(cover.width / cover.height).toBeCloseTo(3 / 4, 2);
  }
  if (route.startsWith("/admin/imports/server/source/")) {
    for (const action of await page.locator(".source-review-action").all()) {
      await expect(action).toHaveClass(/button/);
      expect((await action.boundingBox())!.width).toBeLessThanOrEqual(160);
    }
    await expect(page.locator('.source-result-table > article > [role="cell"]:nth-child(3) small')).toHaveCount(0);
    const centers = await page.locator('.source-result-table > article').evaluateAll((rows) => rows.flatMap((row) => {
      const box = row.getBoundingClientRect();
      return [...row.children].map((cell) => { const rect = cell.getBoundingClientRect(); return Math.abs(rect.y + rect.height / 2 - box.y - box.height / 2); });
    }));
    for (const offset of centers) { expect(offset).toBeLessThanOrEqual(1); }

  }
  if (route === "/saves" && await page.locator(".save-library-card").count()) {
    const card = page.locator(".save-library-card").first();
    const action = card.getByRole("button", { name: "从这里继续", exact: true });
    if (await action.count()) {
      await action.click({ trial: true });
      const shot = (await card.locator(".save-library-shot").boundingBox())!;
      expect((await action.boundingBox())!.y).toBeGreaterThanOrEqual(shot.y + shot.height);
      await page.evaluate(() => scrollTo(0, 0));
    }
  }
}

async function expectImmersiveHeader(page: Page, width: number) {
  await page.goto("/immersive");
  await expect.poll(async () => {
    await page.keyboard.press("ArrowRight");
    return page.locator('[data-immersive-shell="true"]').getAttribute("data-controller-state");
  }).toBe("ready");
  const brand = page.locator('[data-immersive-shell="true"] > header > div');
  await expect(brand).toBeVisible();
  const metrics = await brand.locator(":scope > *").evaluateAll((elements) => elements.map((element) => {
    const style = getComputedStyle(element), box = element.getBoundingClientRect();
    return { font: style.fontSize, weight: style.fontWeight, spacing: style.letterSpacing, line: style.lineHeight, center: box.y + box.height / 2 };
  }));
  expect(metrics).toHaveLength(3);
  for (const metric of metrics) {
    expect(metric.font).toBe(width >= 2560 ? "22px" : "16px");
    expect(metric.weight).toBe("500");
    expect(metric.spacing).toBe("normal");
    expect(metric.line).toBe(metrics[0]!.line);
    expect(Math.abs(metric.center - metrics[0]!.center)).toBeLessThanOrEqual(1);
  }
  await noPageOverflow(page);
}

test("ACC-UI-011 shared typography, controls and responsive composition", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  page.setDefaultTimeout(12_000);
  await page.goto("/login");
  await expectChineseGlyphs(page);
  await expectSearchComposition(page);
  await expectPaletteContrast(page);
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  expect((await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } })).ok()).toBe(true);
  const response = await page.request.get("/api/v1/games?limit=1");
  expect(response.ok()).toBe(true);
  const gameId: string = (await response.json()).items[0].gameId;
  await page.setViewportSize({ width: testInfo.project.name === "chrome-4k-150" ? 2560 : 1440, height: 1440 });
  await expectHomeStates(page, testInfo);
  const userRoutes = ["/", "/library", `/games/${gameId}`, "/saves", "/favorites", "/recent", "/account"];
  const adminRoutes = ["/admin/imports", "/admin/games", `/admin/games/${gameId}`, "/admin/platform-instances", "/admin/imports/new", "/admin/imports/tasks", "/admin/imports/server", "/admin/reviews", "/admin/tags", "/admin/users", "/admin/bios", "/admin/storage"];
  await page.goto("/admin/imports/server");
  const sourceLink = page.locator('a[href^="/admin/imports/server/source/"]').first();
  if (await sourceLink.count()) { adminRoutes.push((await sourceLink.getAttribute("href"))!); }
  const sizes = testInfo.project.name === "chrome-4k-150" ? [[2560, 1440]] : [[1440, 1000], [390, 844], [320, 740]];
  for (const [width, height] of sizes) {
    await page.setViewportSize({ width: width!, height: height! });
    if (testInfo.project.name === "chrome-4k-150") {
      expect(await page.evaluate(() => devicePixelRatio)).toBe(1.5);
      const png = await page.screenshot();
      expect(png.readUInt32BE(16)).toBe(3840);
      expect(png.readUInt32BE(20)).toBe(2160);
    }
    for (const route of [...userRoutes, ...(width! >= 1440 ? adminRoutes : [])]) {
      await navigateUIPage(page, route);
      await expect(page.locator("main")).toBeVisible();
      await expect(page.locator(".loading-grid, .favorite-loading-shell")).toHaveCount(0);
      await expectControlStyles(page, width! < 768);
      await expectCardRadii(page);
      await expectPaletteContrast(page);
      await expectSearchComposition(page);
      await expectRouteComposition(page, route, width!);
      if (route === "/favorites" && width! < 768) { await expectMobileFavorites(page); }
      await page.screenshot({ path: evidencePath(testInfo, `ui-consistency-${width}-${route.replaceAll("/", "-") || "home"}.png`), fullPage: true });
    }
    if (width! >= 1440) {
      await expectImmersiveHeader(page, width!);
      await page.screenshot({ path: evidencePath(testInfo, `ui-consistency-${width}-immersive.png`), fullPage: true });
    }
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("/admin/platform-instances");
  await page.getByRole("button", { name: "新建游戏目录", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expectControlStyles(page, false);
  await expectPaletteContrast(page);
  await page.screenshot({ path: evidencePath(testInfo, "ui-consistency-directory-drawer.png"), fullPage: true });
  await page.keyboard.press("Escape");
  await page.goto("/library");
  await page.getByRole("combobox").first().focus();
  await expect(page.getByRole("combobox").first()).toBeFocused();
  expect(await page.getByRole("combobox").first().evaluate((element) => getComputedStyle(element).outlineStyle)).not.toBe("none");
});

test("ACC-UI-011 server import statistics stay readable over hero gradients", async ({ page }) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const response = await page.request.post("/api/v1/auth/login", {
    headers: { Origin: origin }, data: { username: "test", password: "test" },
  });
  expect(response.ok()).toBe(true);
  await page.goto("/admin/imports/server");
  await expectServerImportStatsContrast(page);
});


test("ACC-UI-011 status glyphs are centered without resizing the capsule", async ({ page }) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const response = await page.request.post("/api/v1/auth/login", {
    headers: { Origin: origin }, data: { username: "test", password: "test" },
  });
  expect(response.ok()).toBe(true);
  await page.goto("/admin/games");
  await expect(page.locator(".admin-game-table .status").first()).toBeVisible();
  await expectStatusTextCentered(page);
});
