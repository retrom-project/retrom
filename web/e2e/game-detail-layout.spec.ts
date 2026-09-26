import { execFileSync } from "node:child_process";
import path from "node:path";
import { expect, test } from "@playwright/test";
import { evidencePath, noPageOverflow, pngDimensions } from "./acceptance-support";
import {uiLayoutState} from "./ui-layout-state";

test.beforeEach(() => uiLayoutState("isolate"));
test.afterEach(() => uiLayoutState("restore"));

test("ACC-UI-003 detail populated, missing screenshot and expanded states", async ({ browser }, testInfo) => {
  test.setTimeout(90_000);
  const database = process.env.RETROM_E2E_DATABASE;
  expect(database, "requires the disposable acceptance database").toBeTruthy();
  const gameId = execFileSync("python3", [path.resolve("../scripts/acceptance/seed-ui-detail.py"), database!], { encoding: "utf8" }).trim();
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  for (const [width, height, dpr] of [[2560, 1440, 1.5], [390, 844, 1]]) {
    const context = await browser.newContext({ viewport: { width, height }, deviceScaleFactor: dpr, baseURL: origin });
    const page = await context.newPage();
    try {
      expect((await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } })).ok()).toBe(true);
      await page.goto(`/games/${gameId}`);
      await expect(page.locator(".game-detail-save-card")).toHaveCount(3);
      await expect(page.locator(".game-detail-saves-actions")).toContainText("共 4 份");
      const missingShot = page.locator(".game-detail-save-media").filter({ has: page.getByRole("img", { name: "存档截图无预览图" }) });
      await expect(missingShot).toHaveCount(1);
      await expect(missingShot.locator(".save-library-size")).toBeVisible();
      await noPageOverflow(page);
      const image = await page.screenshot({ path: evidencePath(testInfo, `detail-populated-${width}.png`) });
      expect(pngDimensions(image)).toEqual({ width: width * dpr, height: height * dpr });
      if (width === 2560) {
        const cards = await page.locator(".game-detail-save-card").evaluateAll((items) => items.map((item) => {
          const card = item.getBoundingClientRect(), shot = item.querySelector(".game-detail-save-media")!.getBoundingClientRect();
          return { top: card.top, bottom: card.bottom, fraction: shot.width / card.width, ratio: shot.width / shot.height };
        }));
        for (const card of cards) {expect(card.top).toBe(cards[0].top); expect(card.bottom).toBeLessThan(height); expect(card.fraction).toBeCloseTo(.42, 1); expect(card.ratio).toBeCloseTo(16 / 9, 2);}
      }
      const hero = await page.locator(".game-detail-hero").evaluate((element) => element.getBoundingClientRect().height);
      await page.getByRole("button", { name: "展开完整简介" }).click();
      await expect(page.locator(".game-detail-description")).toContainText("最后一段：完整简介应随页面滚动。");
      await expect(page.locator(".game-detail-description")).toHaveCSS("overflow-y", "visible");
      expect(await page.locator(".game-detail-hero").evaluate((element) => element.getBoundingClientRect().height)).toBe(hero);
      await page.getByRole("button", { name: "收起简介" }).click();
      const previewLayout = () => page.evaluate(() => [".game-detail-feature-preview", ".game-detail-hero", ".game-detail-overview"].map((selector) => {
        const rect = document.querySelector(selector)!.getBoundingClientRect();
        return { top: rect.top + window.scrollY, height: rect.height };
      }));
      const saveLayout = await previewLayout();
      const toggleWidth = (await page.getByRole("button", { name: "查看视频", exact: true }).boundingBox())!.width;
      await page.getByRole("button", { name: "查看视频", exact: true }).click();
      await expect(page.locator(".game-detail-video-stage")).toBeVisible();
      expect(await previewLayout()).toEqual(saveLayout);
      expect((await page.getByRole("button", { name: "查看最近存档", exact: true }).boundingBox())!.width).toBe(toggleWidth);
      await page.getByRole("button", { name: "查看最近存档", exact: true }).click();
      expect(await previewLayout()).toEqual(saveLayout);
      await page.locator(".game-detail-feature-shot").click();
      await expect(page.getByRole("dialog")).toHaveCount(0);
      await page.locator(".game-detail-save-media").first().click();
      await expect(page.getByRole("dialog")).toHaveCount(0);
      const all = page.getByRole("button", { name: "查看全部存档" });
      await all.click();
      const drawer = page.getByRole("dialog", { name: "全部存档" });
      await expect(drawer.getByRole("article")).toHaveCount(4);
      await drawer.locator(".game-detail-drawer-shot").first().click();
      await expect(page.getByRole("dialog")).toHaveCount(1);
      await expect(drawer.getByRole("button", { name: "关闭全部存档" })).toBeInViewport();
      if (width === 390) {
        const layers = await page.evaluate(() => [".game-detail-save-drawer", ".game-detail-drawer-backdrop", ".mobile-launch-dock"].map((selector) => Number(getComputedStyle(document.querySelector(selector)!).zIndex)));
        expect(layers[0]).toBeGreaterThan(layers[1]);
        expect(layers[1]).toBeGreaterThan(layers[2]);
      }
      await page.keyboard.press("Escape");
      await expect(all).toBeFocused();
      await page.locator(".game-detail-title-row h1").evaluate((element) => {element.textContent = "很长的游戏名称 🎮 ".repeat(15);});
      await noPageOverflow(page);
      await page.screenshot({ path: evidencePath(testInfo, `detail-long-title-${width}.png`), fullPage: true });
    } finally { await context.close(); }
  }
});
