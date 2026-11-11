import {expect, test, type Frame, type Page} from "@playwright/test";
import {nesFrame, nesPlayerCounter} from "./nes-playback";
import {revealPlayerControls} from "./clean-refactor-support";

/** Exercise the real provider, both restart choices, and progress carried into a new instance. */
export async function verifyTouchStickMode(page: Page, frame: Frame): Promise<Frame> {
  if (test.info().project.name !== "chrome-mobile") {return frame;}
  const before = await nesPlayerCounter(frame);
  await openMenu(page);
  const floating = page.getByRole("menuitem", {name: "摇杆模式：浮动摇杆", exact: true});
  await expect(floating).toBeVisible();
  await floating.click();
  const dialog = page.getByRole("alertdialog");
  await expect(dialog).toContainText("需要重启游戏才能生效");
  await expect(dialog).toContainText("立即重启会保留当前进度");
  await expect(page.getByRole("menu", {name: "Player 更多操作"})).toHaveCount(0);
  await page.screenshot({path: test.info().outputPath("touch-stick-restart.jpg"), type: "jpeg", quality: 95});
  await dialog.getByRole("button", {name: "继续游玩", exact: true}).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator(".player-pause-overlay")).toHaveCount(0);
  expect(frame.isDetached()).toBe(false);
  expect(await nesPlayerCounter(frame)).toBe(before);

  await openMenu(page);
  const fixedPending = page.getByRole("menuitem", {name: "摇杆模式：固定摇杆，下次进入游戏生效", exact: true});
  await expect(fixedPending).toBeVisible();
  await page.screenshot({path: test.info().outputPath("touch-stick-pending.jpg"), type: "jpeg", quality: 95});
  // Returning to this launch's original choice needs no restart prompt.
  await fixedPending.click();
  await expect(floating).toBeVisible();
  await expect(dialog).toHaveCount(0);
  await floating.click();
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", {name: "立即重启", exact: true}).click();
  await expect.poll(() => frame.isDetached()).toBe(true);
  const restarted = await nesFrame(page);
  expect(await nesPlayerCounter(restarted)).toBe(before);
  await openMenu(page);
  await expect(page.getByRole("menuitem", {name: "摇杆模式：固定摇杆", exact: true})).toBeVisible();
  await expect(page.getByText("下次进入游戏生效", {exact: true})).toHaveCount(0);
  await page.screenshot({path: test.info().outputPath("touch-stick-applied.jpg"), type: "jpeg", quality: 95});
  await page.locator(".player-menu-backdrop").click({position: {x: 10, y: 10}});
  await expect(page.locator(".player-pause-overlay")).toHaveCount(0);
  return restarted;
}

async function openMenu(page: Page) {
  await revealPlayerControls(page);
  await page.getByRole("button", {name: "更多操作", exact: true}).click();
  await expect(page.getByRole("menu", {name: "Player 更多操作"})).toBeVisible();
  await expect(page.locator(".player-pause-overlay")).toBeVisible();
}
