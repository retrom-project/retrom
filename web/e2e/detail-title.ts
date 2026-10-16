import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import type { Schema } from "../lib/api/types";
import { request } from "./clean-refactor-support";
import { verifyGameDetailLayout } from "./game-detail-layout";

export async function verifyDetailTitle(page: Page, gameId: string) {
  const original = await request<Schema<"GameDetail">>(page, `/api/v1/games/${gameId}`);
  async function rename(title: string) {
    const detail = await request<Schema<"GameDetail">>(page, `/api/v1/games/${gameId}`);
    const game = detail.game;
    await request(page, `/api/v1/admin/games/${gameId}`, {
      version: game.version, platformInstanceId: game.platformInstanceId, title,
      description: game.description, developer: game.developer, publisher: game.publisher,
      genre: game.genre, players: game.players, releaseYear: game.releaseYear,
      tagIds: game.tags.map(tag => tag.id), runtimeConfig: detail.runtimeConfig,
    }, "PATCH");
    await page.reload();
    await expect(page.getByRole("heading", { level: 1, name: title, exact: true })).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
  }
  async function geometry() {
    return page.locator(".game-detail-hero").evaluate(hero => {
      const box = (element: Element) => {
        const { x, y, width, height } = element.getBoundingClientRect();
        return { x, y, width, height };
      };
      return [box(hero), ...[".game-detail-poster-shell", ".game-detail-tags", ".game-detail-feature-preview", ".launch-panel"]
        .flatMap(selector => { const element = hero.querySelector(selector); return element ? [box(element)] : []; })];
    });
  }
  try {
    await rename("NES");
    await verifyGameDetailLayout(page);
    const before = await geometry();
    await expect(page.locator(".game-detail-title")).not.toHaveAttribute("tabindex");
    await rename("超长游戏标题 Long Adventure — ".repeat(6));
    await verifyGameDetailLayout(page);
    expect(await geometry()).toEqual(before);
    await verifyTitleScrolling(page);
  } finally {
    await rename(original.game.title);
  }
}

export async function verifyTitleScrolling(page: Page) {
  const title = page.locator(".game-detail-title");
  const offset = () => title.evaluate(element => element.scrollLeft);
  await expect(title).toHaveAttribute("tabindex", "0");
  await page.mouse.move(0, 0);
  await page.waitForTimeout(1100);
  expect(await offset()).toBe(0);
  if (await page.evaluate(() => matchMedia("(pointer: coarse)").matches)) {
    const box = (await title.boundingBox())!;
    const session = await page.context().newCDPSession(page);
    const x = box.x + box.width - 12, y = box.y + box.height / 2;
    await session.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x, y }] });
    for (let step = 1; step <= 5; step++) {
      await session.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ x: x - step * 20, y }] });
    }
    await session.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
    await expect.poll(offset).toBeGreaterThan(10);
    await session.detach();
    return;
  }
  await title.hover();
  await expect.poll(offset).toBeGreaterThan(10);
  await page.mouse.move(0, 0);
  await expect.poll(offset).toBe(0);
  await title.focus();
  await expect.poll(offset).toBeGreaterThan(10);
  await title.evaluate(element => element.blur());
  await expect.poll(offset).toBe(0);
  await page.emulateMedia({ reducedMotion: "reduce" });
  await title.hover();
  await page.waitForTimeout(1100);
  expect(await offset()).toBe(0);
  await title.focus();
  await page.keyboard.press("ArrowRight");
  await expect.poll(offset).toBeGreaterThan(0);
  await title.evaluate(element => element.blur());
  await page.mouse.move(0, 0);
  await page.emulateMedia({ reducedMotion: "no-preference" });
}
