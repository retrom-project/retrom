import {expect, test} from "@playwright/test";
import type {Page} from "@playwright/test";
import {nesFrame} from "./nes-playback";

export async function verifyDesktopPlayerControls(page: Page) {
  const desktop = await page.evaluate(() => matchMedia("(hover: hover) and (pointer: fine)").matches);
  if (!desktop) { return; }
  const frame = await nesFrame(page);
  const frameNumber = () => frame.evaluate(() => {
    const browser = window as Window & {EJS_emulator?: {gameManager?: {getFrameNum: () => number}}};
    const manager = browser.EJS_emulator?.gameManager;
    if (!manager) { throw new Error("The actual NES runtime is unavailable."); }
    return manager.getFrameNum();
  });
  const toolbar = page.locator(".player-toolbar");
  const paused = page.locator(".player-pause-overlay");
  await page.mouse.move(200, 300);
  await expect.poll(() => toolbar.evaluate(element => element.getBoundingClientRect().bottom)).toBeLessThanOrEqual(0);
  const runningFrame = await frameNumber();
  await page.mouse.move(20, 2);
  await expect(toolbar).toHaveClass(/is-visible/u);
  await expect(toolbar).toBeInViewport({ratio: 1});
  await expect(paused).toHaveCount(0);
  await page.screenshot({path: test.info().outputPath("player-desktop-hover.png")});
  await page.mouse.move(200, 300);
  await expect.poll(() => toolbar.evaluate(element => element.getBoundingClientRect().bottom)).toBeLessThanOrEqual(0);
  await expect.poll(frameNumber).toBeGreaterThan(runningFrame);
  await page.mouse.move(20, 2);
  await expect(toolbar).toHaveClass(/is-visible/u);
  await expect(toolbar).toBeInViewport({ratio: 1});
  await page.mouse.click(120, 24);
  await expect(paused).toBeVisible();
  await page.mouse.move(200, 300);
  await expect.poll(() => toolbar.evaluate(element => element.getBoundingClientRect().bottom)).toBeLessThanOrEqual(0);
  await page.screenshot({path: test.info().outputPath("player-desktop-paused-hidden.png")});
  const frozen = await frameNumber();
  await page.waitForTimeout(350);
  expect(await frameNumber()).toBe(frozen);
  await page.mouse.move(20, 2);
  await expect(toolbar).toHaveClass(/is-visible/u);
  await expect(toolbar).toBeInViewport({ratio: 1});
  await page.getByRole("button", {name: "暂停", exact: true}).click();
  await expect(paused).toBeVisible();
  expect(await frameNumber()).toBe(frozen);
  const more = page.getByRole("button", {name: "更多操作", exact: true});
  const bounds = await more.boundingBox();
  if (!bounds) { throw new Error("The visible player menu control is missing."); }
  // The persistent hover edge must sit below the toolbar, including the upper part of each control.
  await page.mouse.click(bounds.x + bounds.width / 2, bounds.y + 1);
  await expect(page.getByRole("menu", {name: "Player 更多操作", exact: true})).toBeVisible();
  await page.mouse.move(200, 300);
  await paused.click();
  await expect(paused).toHaveCount(0);
  await expect(page.getByRole("menu", {name: "Player 更多操作", exact: true})).toHaveCount(0);
  await expect.poll(frameNumber).toBeGreaterThan(frozen);
}
