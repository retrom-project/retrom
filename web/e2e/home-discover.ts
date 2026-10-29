import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";
import type { Schema } from "../lib/api/types";

export async function verifyHomeDiscover(page: Page) {
  const response = page.waitForResponse(value => {
    const url = new URL(value.url());
    return url.pathname === "/api/v1/games" && url.searchParams.get("sort") === "random";
  });
  await page.goto("/");
  const sampleResponse = await response;
  expect(sampleResponse.ok()).toBe(true);
  const sample: Schema<"GamePage"> = await sampleResponse.json();
  const phone = await page.locator(".phone-home").count() > 0;
  expect(sample.limit).toBe(phone ? 6 : 12);
  expect(sample.offset).toBe(0);
  expect(sample.items).toHaveLength(Math.min(sample.total, sample.limit));
  expect(new Set(sample.items.map(game => game.id)).size).toBe(sample.items.length);
  expect(sample.items.every(game => game.status === "published")).toBe(true);
  const section = page.getByRole("region", { name: "发现游戏" });
  await expect(section.getByRole("heading", { name: "发现游戏", exact: true })).toBeVisible();
  await expect(section.getByRole("link", { name: "查看全部" })).toHaveAttribute("href", "/library");
  const cards = section.locator(phone ? ".phone-game-card" : ".home-recent-card");
  await expect(cards).toHaveCount(sample.items.length);
  expect(await cards.evaluateAll(elements => elements.map(element => element.getAttribute("href"))))
    .toEqual(sample.items.map(game => `/games/${game.id}`));
  if (!phone) {
    const headings = await page.locator(".home-page > .home-layer h2").allTextContents();
    expect(headings.indexOf("最近玩过")).toBeLessThan(headings.indexOf("发现游戏"));
    expect(headings.indexOf("发现游戏")).toBeLessThan(headings.indexOf("一直喜欢的"));
  }
  await page.screenshot({ path: test.info().outputPath("home-discovery.png"), fullPage: true });
}
