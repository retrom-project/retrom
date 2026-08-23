import axe from "axe-core";
import { expect, test, type Page } from "@playwright/test";
import {
  claimGamepad,
  focusedControl,
  installSyntheticGamepads,
  neutralGamepad,
  pressGamepadButton,
  pressGamepadCombination,
  pressGamepadDirection,
  setSyntheticGamepads,
  standardPad,
  unknownPad,
} from "./gamepad-support";

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";

type GameSummary = { gameId: string; title: string };

async function login(page: Page) {
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" },
    headers: { Origin: origin },
  });
  expect(response.ok()).toBe(true);
}

async function publishedGame(page: Page, title = "Sudoku") {
  const response = await page.request.get("/api/v1/games?limit=100");
  expect(response.ok()).toBe(true);
  const payload = await response.json() as { items: GameSummary[] };
  const game = payload.items.find((item) => item.title === title);
  expect(game, `published game ${title}`).toBeTruthy();
  return game!;
}

async function chooseNavigationRoute(page: Page, label: string) {
  await pressGamepadButton(page, 9);
  const panel = page.getByRole("dialog", { name: "用户导航" });
  await expect(panel).toBeVisible();
  const links = panel.getByRole("link");
  const labels = await links.allTextContents();
  const target = labels.findIndex((value) => value.trim() === label);
  expect(target).toBeGreaterThanOrEqual(0);
  const current = await links.evaluateAll((elements) => elements.findIndex(
    (element) => element === document.activeElement,
  ));
  expect(current).toBeGreaterThanOrEqual(0);
  const direction = target >= current ? "down" : "up";
  for (let index = 0; index < Math.abs(target - current); index += 1) {
    await pressGamepadDirection(page, direction);
  }
  await expect(links.nth(target)).toBeFocused();
  await pressGamepadButton(page, 0);
  await expect(panel).toBeHidden();
}

async function launchGameWithGamepad(page: Page, title = "Sudoku") {
  const game = await publishedGame(page, title);
  await page.goto(`/games/${game.gameId}`);
  const launch = page.getByRole("button", { name: /^(开始游戏|重新开始游戏)$/ });
  await pressGamepadButton(page, 0);
  await expect(page).toHaveURL(new RegExp(`/games/${game.gameId}$`));
  await neutralGamepad(page);
  await expect(launch).toBeFocused();
  await pressGamepadButton(page, 0);
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/, { timeout: 10_000 });
  const canvas = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas.ejs_canvas");
  await expect(canvas).toBeVisible({ timeout: 60_000 });
  await expect.poll(async () => {
    const frame = page.frames().find((candidate) => candidate !== page.mainFrame());
    return frame?.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0) ?? 0;
  }, { timeout: 60_000 }).toBeGreaterThan(0);
  await neutralGamepad(page);
  return { game, canvas };
}

async function launchSudokuWithGamepad(page: Page) {
  return launchGameWithGamepad(page);
}

async function movePlayerMenuFocus(
  page: Page,
  menu: ReturnType<Page["getByRole"]>,
  name: string | RegExp,
) {
  const target = menu.getByRole("menuitem", { name });
  for (let attempt = 0; attempt < 16 && !await target.evaluate(
    (element) => element === document.activeElement,
  ); attempt += 1) {
    await pressGamepadDirection(page, "down");
  }
  await expect(target).toBeFocused();
  return target;
}

async function openPlayerMenu(page: Page) {
  await pressGamepadButton(page, 16);
  const menu = page.getByRole("dialog", { name: "Retrom 菜单" });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: /继续(游戏|联机)/ })).toBeFocused();
  await neutralGamepad(page);
  return menu;
}

async function focusStructuredLibraryControl(page: Page) {
  for (let attempt = 0; attempt < 12; attempt += 1) {
    const focused = await page.evaluate(() => {
      const active = document.activeElement as HTMLElement | null;
      const toolbar = active?.closest(".library-toolbar");
      return {
        structured: Boolean(toolbar && (active instanceof HTMLSelectElement ||
          active?.getAttribute("aria-pressed") !== null)),
        tag: active?.tagName ?? "",
        label: active?.getAttribute("aria-label") ?? active?.textContent?.trim() ?? "",
      };
    });
    if (focused.structured) {return focused;}
    await pressGamepadDirection(page, "up");
  }
  throw new Error(`controller did not reach a structured library control: ${JSON.stringify(
    await focusedControl(page),
  )}`);
}

async function expectNoSeriousAxeViolations(page: Page) {
  await page.evaluate(axe.source);
  const violations = await page.evaluate(async () => {
    const result = await (window as typeof window & {
      axe: { run: (root: Document, options: object) => Promise<{
        violations: Array<{ id: string; impact: string | null }>;
      }> };
    }).axe.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((violation) =>
      violation.impact === "serious" || violation.impact === "critical");
  });
  expect(violations, `axe serious/critical violations: ${JSON.stringify(violations)}`).toEqual([]);
}

test.beforeEach(async ({ page }, testInfo) => {
  const responsive = testInfo.title.startsWith("ACC-GPAD-008");
  test.skip(!responsive && testInfo.project.name !== "chrome-1280", "Stateful controller cases run once.");
  await installSyntheticGamepads(page);
  await login(page);
});

test("ACC-GPAD-001 support scope, anonymous claim and neutral gate", async ({ page }) => {
  await page.goto("/");
  await setSyntheticGamepads(page, [unknownPad(0, [0])]);
  await page.waitForTimeout(100);
  await expect(page.locator("html")).not.toHaveAttribute("data-input-mode", "gamepad");

  await setSyntheticGamepads(page, [standardPad(0, [0])]);
  await page.waitForTimeout(60);
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByText("手柄已连接", { exact: true })).toBeVisible();
  await neutralGamepad(page);
  await expect(page.locator("html")).toHaveAttribute("data-input-mode", "gamepad");
  await expect(page.locator("body")).not.toContainText("Retrom synthetic standard controller");

  await pressGamepadButton(page, 9);
  await expect(page.getByRole("dialog", { name: "用户导航" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "用户导航" })).toBeHidden();

  for (const route of ["/account", "/admin/imports"]) {
    await page.goto(route);
    await pressGamepadButton(page, 9);
    await expect(page.getByRole("dialog", { name: "用户导航" })).toHaveCount(0);
  }

  await page.goto("/");
  await setSyntheticGamepads(page, [
    { ...standardPad(0), connected: false },
    standardPad(1, [0]),
  ]);
  await page.waitForTimeout(60);
  await neutralGamepad(page, 1);
  await expect(page.getByText("手柄已连接", { exact: true })).toBeVisible();
});

test("ACC-GPAD-002 controller navigates user routes and structured library controls", async ({ page }) => {
  await page.goto("/");
  await claimGamepad(page);
  await chooseNavigationRoute(page, "游戏库");
  await expect(page).toHaveURL(/\/library$/);
  await chooseNavigationRoute(page, "我的存档");
  await expect(page).toHaveURL(/\/saves$/);
  await chooseNavigationRoute(page, "我的收藏");
  await expect(page).toHaveURL(/\/favorites$/);
  await chooseNavigationRoute(page, "最近游玩");
  await expect(page).toHaveURL(/\/recent$/);
  await chooseNavigationRoute(page, "联机游玩");
  await expect(page).toHaveURL(/\/netplay$/);
  await pressGamepadButton(page, 1);
  await expect(page).toHaveURL(/\/recent$/);

  await page.goto("/library?q=Sudoku");
  await pressGamepadButton(page, 0);
  await neutralGamepad(page);
  expect((await focusedControl(page)).label).not.toContain("搜索游戏");
  const structured = await focusStructuredLibraryControl(page);
  const initialURL = page.url();
  await pressGamepadButton(page, 0);
  if (structured.tag === "SELECT") {
    await pressGamepadDirection(page, "down");
    await pressGamepadButton(page, 0);
  }
  await expect.poll(() => page.url()).not.toBe(initialURL);
  await expect(page).toHaveURL(/q=Sudoku/);

  await page.goto("/library?q=definitely-no-matching-game");
  await pressGamepadButton(page, 0);
  await neutralGamepad(page);
  const clear = page.getByRole("link", { name: /清除筛选|浏览全部游戏/ }).first();
  await expect(clear).toBeFocused();
  await pressGamepadButton(page, 0);
  await expect(page).toHaveURL(/\/library(?:\?.*)?$/);
  await expect(page).not.toHaveURL(/definitely-no-matching-game/);
});

test("ACC-GPAD-003 controller starts a real game and fullscreen denial is non-blocking", async ({ page }) => {
  test.setTimeout(120_000);
  await launchSudokuWithGamepad(page);
  const menu = await openPlayerMenu(page);
  const fullscreen = menu.getByRole("menuitem", { name: /(进入|退出)全屏/ });
  for (let attempt = 0; attempt < 12 && !await fullscreen.evaluate(
    (element) => element === document.activeElement,
  ); attempt += 1) {
    await pressGamepadDirection(page, "down");
  }
  await expect(fullscreen).toBeFocused();
  const fullscreenLabel = await fullscreen.getAttribute("aria-label");
  if (fullscreenLabel === "进入全屏") {
    await pressGamepadButton(page, 0);
    await expect.poll(async () => await page.evaluate(() => Boolean(document.fullscreenElement)) ||
      await page.getByText("浏览器未允许全屏，游戏仍会继续运行。").isVisible()).toBe(true);
  } else {
    expect(await page.evaluate(() => Boolean(document.fullscreenElement))).toBe(true);
  }
  await expect(page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas.ejs_canvas"))
    .toBeVisible();
});

test("ACC-GPAD-004 single-player menu controls save, settings and safe long-press exit", async ({ page }) => {
  test.setTimeout(180_000);
  await launchSudokuWithGamepad(page);
  const menu = await openPlayerMenu(page);
  await expect(page.getByText("已暂停", { exact: true })).toBeVisible();

  await setSyntheticGamepads(page, [standardPad(0, [2])]);
  await page.waitForTimeout(40);
  const filtered = await page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("body")
    .evaluate(() => navigator.getGamepads()[0]?.buttons[2]?.value ?? -1);
  expect(filtered).toBe(0);
  await neutralGamepad(page);

  await pressGamepadDirection(page, "down");
  await expect(menu.getByRole("menuitem", { name: "创建手动存档" })).toBeFocused();
  const saveResponse = page.waitForResponse((response) => response.request().method() === "POST" &&
    /\/runtime\/launches\/[^/]+\/save-states$/.test(response.url()));
  await pressGamepadButton(page, 0);
  expect((await saveResponse).status()).toBe(201);
  await expect(page.getByText("手动存档和截图已保存")).toBeVisible({ timeout: 20_000 });

  await pressGamepadDirection(page, "down");
  await expect(menu.getByRole("menuitem", { name: "画面、声音与高级设置" })).toBeFocused();
  await pressGamepadButton(page, 0);
  const settings = page.getByRole("region", { name: "模拟器设置工具栏" });
  const rendering = settings.getByRole("combobox", { name: "画面模式" });
  await expect(rendering).toBeFocused();
  const originalMode = await rendering.inputValue();
  await pressGamepadButton(page, 0);
  await pressGamepadDirection(page, "right");
  await pressGamepadButton(page, 0);
  await expect(rendering).not.toHaveValue(originalMode);
  await pressGamepadButton(page, 1);
  await expect(settings).toBeHidden();
  await pressGamepadButton(page, 1);
  await expect(menu).toBeHidden();
  await neutralGamepad(page);

  await setSyntheticGamepads(page, [standardPad(0, [16])]);
  await page.waitForTimeout(1_250);
  await setSyntheticGamepads(page, [standardPad(0)]);
  await page.waitForTimeout(50);
  const exit = page.getByRole("alertdialog", { name: /退出《.+》？/ });
  await expect(exit).toBeVisible();
  await expect(exit.getByRole("button", { name: "继续游戏" })).toBeFocused();
  await neutralGamepad(page);
  await pressGamepadButton(page, 0);
  await expect(exit).toBeHidden();
  await neutralGamepad(page);

  const reopen = await openPlayerMenu(page);
  await movePlayerMenuFocus(page, reopen, "退出游戏");
  await pressGamepadButton(page, 0);
  const confirmation = page.getByRole("alertdialog", { name: /退出《.+》？/ });
  await expect(confirmation.getByRole("button", { name: "继续游戏" })).toBeFocused();
  const danger = confirmation.getByRole("button", { name: "退出游戏" });
  for (let attempt = 0; attempt < 4 && !await danger.evaluate(
    (element) => element === document.activeElement,
  ); attempt += 1) {
    await pressGamepadDirection(page, "right");
  }
  await expect(danger).toBeFocused();
  await pressGamepadButton(page, 0);
  await expect(page).toHaveURL(new RegExp(`/games/${(await publishedGame(page)).gameId}$`));

  await launchGameWithGamepad(page, "pacman");
  await pressGamepadCombination(page);
  const arcadeMenu = page.getByRole("dialog", { name: "Retrom 菜单" });
  await expect(arcadeMenu).toBeVisible();
  await neutralGamepad(page);
  const filteredCombination = await page.frameLocator('iframe[title="Retrom EmulatorJS Player"]')
    .locator("body").evaluate(() => {
      const pad = navigator.getGamepads()[0];
      return [pad?.buttons[8]?.value, pad?.buttons[9]?.value];
    });
  expect(filteredCombination).toEqual([0, 0]);
  await pressGamepadButton(page, 1);
  await expect(arcadeMenu).toBeHidden();
});

test("ACC-GPAD-006 disconnect releases input and replacement waits for neutral", async ({ page }) => {
  test.setTimeout(120_000);
  await launchSudokuWithGamepad(page);
  const frame = page.frames().find((candidate) => candidate !== page.mainFrame());
  expect(frame).toBeTruthy();
  const before = await frame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);

  await setSyntheticGamepads(page, [{ ...standardPad(0), connected: false }]);
  const reconnect = page.getByRole("dialog", { name: "手柄重连" });
  await expect(reconnect).toBeVisible();
  await expect(reconnect.getByRole("button", { name: "继续游戏" })).toBeDisabled();
  await pressGamepadButton(page, 0, { index: 1, releaseMs: 0 });
  await expect(reconnect.getByRole("button", { name: "继续游戏" })).toBeDisabled();
  await neutralGamepad(page, 1);
  await expect(reconnect.getByRole("button", { name: "继续游戏" })).toBeEnabled();
  await pressGamepadButton(page, 0, { index: 1 });
  await expect(reconnect).toBeHidden();
  await neutralGamepad(page, 1);
  await expect.poll(async () => frame!.evaluate(
    () => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0,
  )).toBeGreaterThan(before + 5);
});

test("ACC-GPAD-007 adapter IDs and filtered Gamepad API are locked in the real launch", async ({ page }) => {
  test.setTimeout(180_000);
  const configurations: Array<Record<string, unknown>> = [];
  page.on("response", async (response) => {
    if (/\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.ok()) {
      configurations.push(await response.json() as Record<string, unknown>);
    }
  });
  await launchSudokuWithGamepad(page);
  expect(configurations).toHaveLength(1);
  expect(configurations[0]).toMatchObject({
    emulatorjsVersion: "4.2.3",
    playerAdapterId: "ejs-4.2.3-v3",
  });

  await setSyntheticGamepads(page, [standardPad(0, [8])]);
  await page.waitForTimeout(35);
  await setSyntheticGamepads(page, [standardPad(0, [8, 9])]);
  await page.waitForTimeout(35);
  const reserved = await page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("body")
    .evaluate(() => {
      const pad = navigator.getGamepads()[0];
      return [pad?.buttons[8]?.value, pad?.buttons[9]?.value, pad?.buttons[16]?.value];
    });
  expect(reserved).toEqual([0, 0, 0]);
  await neutralGamepad(page);
  await expect(page.getByRole("dialog", { name: "Retrom 菜单" })).toBeVisible();
});

test("ACC-GPAD-008 focus, reduced motion, keyboard fallback and responsive UI", async ({ page }, testInfo) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  await claimGamepad(page);
  await pressGamepadButton(page, 9);
  const panel = page.getByRole("dialog", { name: "用户导航" });
  await expect(panel).toBeVisible();
  const focusStyle = await panel.getByRole("link", { name: "首页" }).evaluate((element) => {
    const style = getComputedStyle(element);
    return { outlineWidth: style.outlineWidth, transitionDuration: style.transitionDuration };
  });
  expect(Number.parseFloat(focusStyle.outlineWidth)).toBeGreaterThanOrEqual(3);
  await expectNoSeriousAxeViolations(page);
  await page.screenshot({ path: testInfo.outputPath("controller-navigation.png"), fullPage: true });
  await page.keyboard.press("Escape");
  await expect(panel).toBeHidden();
  await page.keyboard.press("Tab");
  expect((await focusedControl(page)).tag).not.toBe("");
  await pressGamepadCombination(page);
  await expect(panel).toBeVisible();
});
