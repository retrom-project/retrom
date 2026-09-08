import {expect, test, type Frame} from "@playwright/test";
import {installGamepads, setGamepadButtons} from "./immersive-gamepad";
import {runtimeFrameCount} from "./runtime-provider-support";

test("ACC-RUN-013 input diagnostics preserve live input and restore observers", async ({page}, testInfo) => {
  test.setTimeout(180_000);
  await installGamepads(page);
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const login = await page.request.post("/api/v1/auth/login", {data: {username: "test", password: "test"}, headers: {Origin: origin}});
  expect(login.ok()).toBe(true);
  await page.goto("/library");
  await page.locator(".library-game-card").filter({hasText: "Sudoku"}).getByRole("link").first().click();
  await page.getByRole("button", {name: "开始游戏"}).click();
  await expect(page.locator(".player-loading")).toBeHidden({timeout: 30_000});
  const canvas = page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas");
  await expect(canvas).toBeVisible({timeout: 30_000});
  const frame = page.frames().find((candidate) => candidate !== page.mainFrame())!;
  await saveInputIdentity(frame);
  await canvas.click();
  await page.mouse.move(20, 20);
  const debug = page.getByRole("button", {name: "调试信息", exact: true});
  await debug.click();
  const panel = page.getByRole("complementary", {name: "运行调试信息"});
  await expect(panel).toBeVisible();
  await expect(panel.getByRole("button", {name: "关闭调试信息面板"})).toHaveCount(0);
  await expect(page.locator(".player-shell")).not.toHaveClass(/is-paused/);
  expect(await panel.evaluate((element) => getComputedStyle(element).backgroundColor)).toBe("rgba(14, 19, 28, 0.62)");
  expect(await panel.evaluate((element) => getComputedStyle(element).backdropFilter)).toBe("none");
  const count = await runtimeFrameCount(page);
  await expect.poll(() => runtimeFrameCount(page)).toBeGreaterThan(count + 10);
  const support = panel.getByText("手柄取样观测", {exact: true}).locator("..").locator("dd");
  if (!process.env.RETROM_PFB_DIAGNOSTICS_REQUIRED && await support.textContent() === "未接入") {
    await expect(support).toHaveText("未接入");
    await debug.click();
    await expect(panel).toBeHidden();
    expect(await inputRestored(frame)).toBe(true);
    return;
  }
  await expect(support).toHaveText("已开启");
  await panel.getByText("最近输入记录", {exact: true}).click();
  await canvas.focus();
  await page.keyboard.down("k"); await page.waitForTimeout(30); await page.keyboard.up("k");
  await expect(panel.locator(".player-input-history")).toContainText("KeyK 松开");
  await expect(panel.locator(".player-input-history")).toContainText("已投递");
  await setGamepadButtons(page, 0, [0]); await page.waitForTimeout(50); await setGamepadButtons(page, 0, []);
  await expect(panel.locator(".player-input-history")).toContainText("Button 0 松开");
  expect(await panel.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return document.elementFromPoint(rect.left + 20, rect.top + 20)?.tagName;
  })).toBe("IFRAME");
  await panel.getByText("最近输入记录", {exact: true}).click();
  await page.screenshot({path: testInfo.outputPath("input-overlay.png")});
  await debug.click();
  await expect(panel).toBeHidden();
  expect(await inputRestored(frame)).toBe(true);
  await page.setViewportSize({width: 960, height: 600});
  await page.mouse.move(20, 20);
  await expect(debug).toBeVisible();
  await debug.click(); await expect(panel).toBeVisible(); await debug.click();
  await page.getByRole("button", {name: "更多操作", exact: true}).click();
  await expect(page.getByRole("menuitem", {name: /调试信息|查看快捷键/})).toHaveCount(0);
});

type InputWindow = Window & {
  EJS_emulator?: {gameManager?: {simulateInput?: unknown}};
  __inputBefore?: {getGamepads: unknown; simulateInput: unknown};
};

async function saveInputIdentity(frame: Frame) {
  await frame.evaluate(() => {
    const scope = window as InputWindow;
    scope.__inputBefore = {getGamepads: navigator.getGamepads, simulateInput: scope.EJS_emulator?.gameManager?.simulateInput};
  });
}

async function inputRestored(frame: Frame) {
  return frame.evaluate(() => {
    const scope = window as InputWindow;
    return navigator.getGamepads === scope.__inputBefore?.getGamepads &&
      scope.EJS_emulator?.gameManager?.simulateInput === scope.__inputBefore?.simulateInput;
  });
}
