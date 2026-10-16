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
  const save = saves.items.find(item => item.restorable)!;
  expect(save?.screenshotUrl).toBeTruthy();
  const hero = page.locator(".home-featured-panel");
  await expect(hero.getByRole("heading", { name: recent.game.title, exact: true })).toBeVisible();
  await expect(hero.getByRole("button", { name: "从存档继续", exact: true })).toBeVisible();
  await expect(hero.locator(".home-featured-meta time")).toHaveAttribute("datetime", new Date(recent.lastPlayedAtMs).toISOString());
  await expect(hero.locator(".home-featured-media")).toHaveAttribute("data-kind", "save");
  // Narrow layouts intentionally hide this decorative area; lazy images there
  // need not request bytes, so network-error fallback is a desktop assertion.
  if ((page.viewportSize()?.width ?? 0) < 1280) {
    await expect(hero.locator(".home-featured-media")).toBeHidden();
    return;
  }
  const bounds = await hero.boundingBox();
  const actions = await hero.locator(".home-featured-actions").boundingBox();
  // A genuine failed image request must not move the copy/actions or remove the
  // available save. The legal NES fixture has no other game media to fall back to.
  const imageURL = new URL(save.screenshotUrl!, page.url()).href;
  await page.route(imageURL, route => route.abort("failed"));
  try {
    await page.reload();
    await expect(hero.locator(".home-featured-media")).toHaveAttribute("data-kind", "platform");
    expect(await hero.boundingBox()).toEqual(bounds);
    expect(await hero.locator(".home-featured-actions").boundingBox()).toEqual(actions);
    await expect(hero.getByRole("button", { name: "从存档继续", exact: true })).toBeVisible();
    await expect(hero.getByRole("link", { name: "查看存档", exact: true })).toHaveAttribute("href", `/saves?gameId=${recent.game.id}`);
  } finally {
    await page.unroute(imageURL);
  }
}
