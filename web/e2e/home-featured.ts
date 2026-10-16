import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import type { Schema } from "../lib/api/types";
import { request } from "./clean-refactor-support";

export async function verifyHomeFeatured(page: Page) {
  await page.goto("/");
  const home = await request<Schema<"Home">>(page, "/api/v1/home");
  const recent = home.recent[0];
  expect(recent).toBeDefined();
  const saves = await request<Schema<"SavePage">>(page, `/api/v1/saves?gameId=${recent.game.id}&sort=recent`);
  const save = saves.items[0]!;
  expect(save?.screenshotUrl).toBeTruthy();
  const hero = page.locator(".home-featured-panel");
  await expect(hero.getByRole("heading", { name: recent.game.title, exact: true })).toBeVisible();
  await expect(hero.getByRole("button", { name: "从存档继续", exact: true })).toBeVisible();
  await expect(hero.locator(".home-featured-meta time")).toHaveAttribute("datetime", new Date(save.updatedAtMs).toISOString());
  await expect(hero.locator(".home-featured-media")).toHaveAttribute("data-kind", "save");
  await expect(hero.locator("time")).toHaveCount(1);
  await expect(hero.locator(".home-featured-art")).toHaveAttribute("src", new URL(save.screenshotUrl!, page.url()).href);
  await page.evaluate(() => document.fonts.ready);
  const placement = await hero.evaluate(element => {
    const copy = element.querySelector(".home-featured-copy")!;
    const rect = (selector: string) => copy.querySelector(selector)!.getBoundingClientRect();
    return {
      flow: getComputedStyle(copy).display,
      actionGap: rect(".home-featured-actions").top - rect(".home-featured-meta").bottom,
      noteGap: rect(".home-launch-note").top - rect(".home-featured-actions").bottom,
      saveLinkGap: rect(".home-save-link").top - rect(".home-launch-note").bottom,
    };
  });
  expect(placement).toEqual({ flow: "block", actionGap: 24, noteGap: 8, saveLinkGap: 0 });
  // Narrow layouts intentionally hide this decorative area; lazy images there
  // need not request bytes, so network-error fallback is a desktop assertion.
  if ((page.viewportSize()?.width ?? 0) < 1280) {
    await expect(hero.locator(".home-featured-media")).toBeHidden();
    return;
  }
  const bounds = await hero.boundingBox();
  const actions = await hero.locator(".home-featured-actions").boundingBox();
  // A genuine failed image request must not move the copy/actions or remove the
  // available save. Only games with no saves use platform artwork.
  const imageURL = new URL(save.screenshotUrl!, page.url()).href;
  await page.route(imageURL, route => route.abort("failed"));
  try {
    await page.reload();
    await expect(hero.locator(".home-featured-media")).toHaveAttribute("data-kind", "save");
    await expect(hero.getByText("存档截图暂不可用")).toBeVisible();
    expect(await hero.boundingBox()).toEqual(bounds);
    expect(await hero.locator(".home-featured-actions").boundingBox()).toEqual(actions);
    await expect(hero.getByRole("button", { name: "从存档继续", exact: true })).toBeVisible();
    await expect(hero.getByRole("link", { name: "查看存档", exact: true })).toHaveAttribute("href", `/saves?gameId=${recent.game.id}`);
  } finally {
    await page.unroute(imageURL);
  }
}
