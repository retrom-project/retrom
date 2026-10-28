import { expect, type Locator, type Page } from "@playwright/test";
import { test } from "./clean-refactor-fixtures";
import { login, prepareLibrary, revealPlayerControls } from "./clean-refactor-support";
import { ensurePlayerLandscape } from "./nes-playback";

async function expectActionsToFit(dialog: Locator, count = 3) {
  await expect(dialog).toBeInViewport({ ratio: 1 });
  const actions = dialog.locator(".dialog-actions > button");
  await expect(actions).toHaveCount(count);
  for (const action of await actions.all()) {
    await expect(action).toBeInViewport({ ratio: 1 });
    const layout = await action.evaluate((element) => {
      const bounds = element.getBoundingClientRect();
      const panel = element.closest('[role="alertdialog"]')!.getBoundingClientRect();
      return {
        contained: bounds.top >= panel.top && bounds.bottom <= panel.bottom,
        height: bounds.height,
        reachable: [bounds.top + 2, bounds.bottom - 2].every((y) =>
          element.contains(document.elementFromPoint(bounds.left + bounds.width / 2, y)),
        ),
      };
    });
    expect(layout.contained).toBe(true);
    expect(layout.height).toBeGreaterThanOrEqual(44);
    expect(layout.reachable).toBe(true);
    if (await action.isEnabled()) {
      await action.click({ trial: true });
    }
  }
}

async function verifyScrollableConfirmation(page: Page, gameId: string) {
  await page.goto(`/admin/games/${gameId}`);
  await page.getByRole("button", { name: "替换游戏内容", exact: true }).click();
  const dialog = page.getByRole("alertdialog", { name: "替换游戏内容", exact: true });
  await expectActionsToFit(dialog, 2);
  const body = dialog.locator(".dialog-impact");
  await expect.poll(() => body.evaluate((element) => element.scrollHeight - element.clientHeight))
    .toBeGreaterThan(0);
  await body.evaluate((element) => { element.scrollTop = element.scrollHeight; });
  await expectActionsToFit(dialog, 2);
  await page.screenshot({ path: test.info().outputPath("confirmation-scrollable-body.png") });
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toHaveCount(0);
}

export async function verifyPlayerExitDialogLayout(page: Page) {
  await login(page);
  const game = await prepareLibrary(page);
  await page.goto(`/games/${game.id}`);
  await expect(page.getByRole("heading", { level: 1, name: game.title, exact: true })).toBeVisible();
  const start = page.getByRole("button", { name: "重新开始游戏", exact: true });
  if (!(await start.isVisible())) {
    await page.getByRole("button", { name: "启动选项", exact: true }).click();
    await page.getByRole("dialog", { name: "启动选项", exact: true })
      .getByRole("button", { name: "重新开始游戏", exact: true }).click();
  } else {
    await start.click();
  }
  await page.waitForURL(/\/play\//u);
  await ensurePlayerLandscape(page);
  await expect(page.getByRole("button", { name: /^(创建存档|保存)$/u })).toBeEnabled({ timeout: 60_000 });
  const viewport = page.viewportSize();
  if (!viewport) { throw new Error("Player dialog requires an explicit viewport"); }
  await revealPlayerControls(page);
  await page.getByRole("button", { name: "返回并退出游戏", exact: true }).click();
  const dialog = page.getByRole("alertdialog", { name: "退出游戏？", exact: true });
  const cancel = dialog.getByRole("button", { name: "取消", exact: true });
  await expect(cancel).toBeFocused();
  await page.screenshot({ path: test.info().outputPath("player-exit-original-viewport.png") });
  await expectActionsToFit(dialog);
  for (const [width, height] of [[760, 346], [700, 320], [640, 291], [568, 280]]) {
    await page.setViewportSize({ width, height });
    await expect.poll(() => page.evaluate(() => ({ width: innerWidth, height: innerHeight })))
      .toEqual({ width, height });
    await page.screenshot({ path: test.info().outputPath(`player-exit-${width}x${height}.png`) });
    await expectActionsToFit(dialog);
  }
  await cancel.click();
  await expect(dialog).toHaveCount(0);
  await revealPlayerControls(page);
  await page.getByRole("button", { name: "返回并退出游戏", exact: true }).click();
  await expect(cancel).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(dialog.getByRole("button", { name: "退出游戏", exact: true })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await revealPlayerControls(page);
  await page.getByRole("button", { name: "返回并退出游戏", exact: true }).click();
  await dialog.getByRole("button", { name: "退出游戏", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/games/${game.id}$`, "u"));
  await verifyScrollableConfirmation(page, game.id);
  await page.setViewportSize(viewport);
}
