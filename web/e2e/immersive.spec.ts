import { expect, test, type Frame, type Page, type TestInfo } from "@playwright/test";
import axe from "axe-core";
import { currentEmulatorBrightRatio, evidencePath, noPageOverflow } from "./acceptance-support";
import {
  installGamepads,
  pressGamepad,
  setGamepadButtons,
  setGamepadConnected,
  standardButton,
} from "./immersive-gamepad";

type PlatformItem = {
  featuredGames: Array<{
    coverUrl: string | null;
    gameId: string;
    lastPlayedAtMs: number | null;
    title: string;
  }>;
  gameCount: number;
  lastPlayedAtMs: number | null;
  platformId: string;
  platformName: string;
};

type GameItem = {
  defaultCore: { id: string; name: string };
  gameId: string;
  platformInstance: { id: string; name: string };
  title: string;
};

test.beforeEach(async ({ page }, testInfo) => {
  test.skip(testInfo.title.includes("ACC-IMM-007") ? false : testInfo.project.name !== "chrome-1280");
  await installGamepads(page, testInfo.title.includes("ACC-IMM-006") ? 2 : 1);
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" },
    headers: { Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000" },
  });
  expect(response.ok()).toBe(true);
});

async function claimEntry(page: Page) {
  await page.goto("/");
  await expect(page.locator(".app-frame")).toBeVisible();
  await page.waitForTimeout(250);
  await pressGamepad(page, standardButton.a);
  const dialog = page.getByRole("alertdialog", { name: "进入沉浸模式？" });
  await expect(dialog).toBeVisible();
  const takeover = page.locator('[data-immersive-entry="true"]');
  await expect(takeover).toBeVisible();
  const takeoverBox = await takeover.boundingBox();
  const viewport = page.viewportSize();
  expect(takeoverBox).toEqual({ x: 0, y: 0, width: viewport?.width, height: viewport?.height });
  await expect(dialog.getByRole("button", { name: "取消" })).toBeFocused();
  return dialog;
}

async function enterImmersive(page: Page) {
  const dialog = await claimEntry(page);
  await pressGamepad(page, standardButton.right);
  await expect(dialog.getByRole("button", { name: "进入沉浸模式" })).toBeFocused();
  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(/\/immersive$/);
  await expect(page.locator('[data-immersive-shell="true"]')).toBeVisible();
}

async function platformItems(page: Page) {
  const response = await page.request.get("/api/v1/immersive/platforms");
  expect(response.ok()).toBe(true);
  return (await response.json() as { items: PlatformItem[] }).items;
}

async function choosePlatform(page: Page, platformName: string) {
  const items = await platformItems(page);
  for (let attempt = 0; attempt < items.length; attempt += 1) {
    const current = page.getByRole("option", { selected: true });
    if ((await current.textContent())?.includes(platformName)) {
      await page.waitForTimeout(180);
      return;
    }
    await pressGamepad(page, standardButton.right);
  }
  throw new Error(`IMMERSIVE_PLATFORM_NOT_SELECTABLE:${platformName}`);
}

async function openPlatform(page: Page, platformName: string) {
  await enterImmersive(page);
  await choosePlatform(page, platformName);
  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(/\/immersive\/platforms\/[a-z0-9-]+/);
  await expect(page.getByRole("listbox", { name: new RegExp(`${platformName} 游戏`) })).toBeVisible();
  await page.waitForTimeout(180);
}

async function selectGameForCore(page: Page, coreId: string) {
  const match = /\/immersive\/platforms\/([^?]+)/.exec(page.url());
  if (!match) {throw new Error("IMMERSIVE_PLATFORM_ID_UNAVAILABLE");}
  const response = await page.request.get(`/api/v1/immersive/platforms/${match[1]}/games?limit=50`);
  expect(response.ok()).toBe(true);
  const games = (await response.json() as { items: GameItem[] }).items;
  const index = games.findIndex((game) => game.defaultCore.id === coreId);
  expect(index, `immersive game for ${coreId}`).toBeGreaterThanOrEqual(0);
  for (let position = 0; position < index; position += 1) {
    await pressGamepad(page, standardButton.down);
  }
  const selected = page.getByRole("option", { selected: true });
  await expect(selected).toContainText(games[index]!.title);
  return { game: games[index]!, games, index };
}

async function selectGameByTitle(page: Page, title: string) {
  const selected = page.getByRole("option", { selected: true });
  if ((await selected.textContent())?.includes(title)) {return;}
  await setGamepadButtons(page, 0, [standardButton.down]);
  try {
    await expect.poll(
      async () => (await selected.textContent())?.includes(title) ?? false,
      { intervals: [50], timeout: 20_000 },
    ).toBe(true);
  } finally {
    await setGamepadButtons(page, 0, []);
  }
  await page.waitForTimeout(180);
}

async function launchSelectedGame(page: Page) {
  const configResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+\?experience=immersive$/);
  const configuration = await (await configResponse).json() as {
    mode: string;
    playerAdapterId: string;
    returnTo: string;
    stateUrl: string | null;
  };
  expect(configuration).toMatchObject({ mode: "single", playerAdapterId: "ejs-4.2.3-v3", stateUrl: null });
  await expect(page.locator(".player-toolbar")).toHaveCount(0);
  const canvas = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas.ejs_canvas");
  await expect(canvas).toBeVisible({ timeout: 60_000 });
  const frame = await emulatorFrame(page);
  return { canvas, configuration, frame };
}

async function emulatorFrame(page: Page) {
  const handle = await page.locator('iframe[title="Retrom EmulatorJS Player"]').elementHandle();
  const frame = await handle?.contentFrame();
  if (!frame) {throw new Error("IMMERSIVE_EMULATOR_FRAME_UNAVAILABLE");}
  return frame;
}

function frameNumber(frame: Frame) {
  return frame.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
}

async function openPlayerMenu(page: Page) {
  await pressGamepad(page, [standardButton.select, standardButton.start], 0, 80, 100);
  await expect(page.getByRole("dialog", { name: "游戏菜单" })).toHaveCount(0);
  await pressGamepad(page, [standardButton.select, standardButton.start], 0, 80, 180);
  const menu = page.getByRole("dialog", { name: "游戏菜单" });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("button", { name: "取消" })).toHaveAttribute("aria-current", "true");
  return menu;
}

async function exitFromPlayerMenu(page: Page) {
  const finished = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/finish$/.test(response.url()) && response.request().method() === "POST");
  await pressGamepad(page, standardButton.right);
  await pressGamepad(page, standardButton.a);
  expect((await finished).ok()).toBe(true);
  await expect(page).toHaveURL(/\/immersive\/platforms\/[a-z0-9-]+\?gameId=[0-9a-f-]+$/);
}

test("ACC-IMM-001 home gamepad entry defaults to cancel and stays isolated", async ({ page }, testInfo) => {
  const dialog = await claimEntry(page);
  await page.screenshot({ path: evidencePath(testInfo, "immersive-entry-confirmation.png"), fullPage: true });
  await pressGamepad(page, standardButton.b);
  await expect(dialog).toBeHidden();
  await expect(page).toHaveURL(/\/$/);

  await page.goto("/library");
  await pressGamepad(page, standardButton.a);
  await expect(page.getByRole("alertdialog", { name: "进入沉浸模式？" })).toHaveCount(0);
  await expect(page).toHaveURL(/\/library$/);

  await enterImmersive(page);
  await expect(page.getByRole("navigation", { name: "主要导航" })).toHaveCount(0);
  if (!await page.evaluate(() => Boolean(document.fullscreenElement))) {
    await expect(page.getByRole("button", { name: /进入全屏/ })).toBeVisible();
  }
  await page.screenshot({ path: evidencePath(testInfo, "immersive-entry.png"), fullPage: true });
});

test("ACC-IMM-002 platform carousel uses real counts and controller-only navigation", async ({ page }, testInfo) => {
  const items = await platformItems(page);
  expect(items.length).toBeGreaterThanOrEqual(2);
  await enterImmersive(page);
  const current = page.getByRole("option", { selected: true });
  const horizontalKey = page.getByLabel("左右方向键");
  const keyBox = await horizontalKey.boundingBox();
  const iconBox = await horizontalKey.locator("svg").boundingBox();
  expect(keyBox?.width).toBe(keyBox?.height);
  expect(iconBox?.width).toBeLessThan(keyBox?.width ?? 0);
  expect(iconBox?.height).toBeLessThan(keyBox?.height ?? 0);
  await expect(current).toContainText(items[0]!.platformName);
  await expect(current).toContainText(`${items[0]!.gameCount} 款游戏`);
  let coverStack = page.getByLabel(`${items[0]!.platformName} 最近游戏封面`);
  expect(items[0]!.featuredGames).toHaveLength(3);
  expect(items[0]!.featuredGames.every((game) => game.coverUrl !== null)).toBe(true);
  await expect(coverStack.locator("figure")).toHaveCount(3);
  await expect(page.locator('[data-platform-cover-stack="true"]')).toHaveCount(1);
  const initialFront = await coverStack.locator('[data-cover-slot="0"]').getAttribute("data-game-id");
  await expect.poll(
    () => coverStack.locator('[data-cover-slot="0"]').getAttribute("data-game-id"),
    { timeout: 4_000 },
  ).not.toBe(initialFront);
  await page.waitForTimeout(180);

  await pressGamepad(page, standardButton.left);
  const carousel = page.getByRole("listbox", { name: "游戏平台" });
  await expect(carousel).toHaveAttribute("data-direction", "left");
  await expect(carousel).toHaveAttribute("data-selected-index", String(items.length - 1));
  const animationName = await current.evaluate((element) => getComputedStyle(element).animationName);
  expect(animationName).not.toBe("none");
  await expect.poll(async () => current.evaluate((element) => getComputedStyle(element).animationDuration)).toBe("0.32s");
  await expect(current).toContainText(items.at(-1)!.platformName);
  coverStack = page.getByLabel(`${items.at(-1)!.platformName} 最近游戏封面`);
  await expect(coverStack.locator("figure")).toHaveCount(Math.min(3, items.at(-1)!.featuredGames.length));
  const coverStackBox = await coverStack.boundingBox();
  expect((coverStackBox?.width ?? 0) / (coverStackBox?.height ?? 1)).toBeCloseTo(5 / 7, 1);
  await page.waitForTimeout(360);
  const currentAppearance = await current.evaluate((element) => {
    const style = getComputedStyle(element);
    return { opacity: Number(style.opacity), transform: style.transform };
  });
  const adjacentAppearance = await page.getByRole("option", { selected: false }).first().evaluate((element) => {
    const style = getComputedStyle(element);
    return { opacity: Number(style.opacity), transform: style.transform };
  });
  expect(currentAppearance.opacity).toBeGreaterThan(0.95);
  expect(adjacentAppearance.opacity).toBeLessThan(0.5);
  expect(adjacentAppearance.transform).not.toBe(currentAppearance.transform);
  await expect(page.getByTestId("platform-position-indicator")).toHaveAttribute(
    "style",
    new RegExp(`translateX\\(${(items.length - 1) * 100}%\\)`),
  );
  await pressGamepad(page, standardButton.right);
  await expect(current).toContainText(items[0]!.platformName);
  await page.screenshot({ path: evidencePath(testInfo, "immersive-platform-carousel.png"), fullPage: true });

  await pressGamepad(page, standardButton.b);
  const exit = page.getByRole("alertdialog", { name: "退出沉浸模式？" });
  await expect(exit).toBeVisible();
  await pressGamepad(page, standardButton.a);
  await expect(exit).toBeHidden();
  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(new RegExp(`/immersive/platforms/${items[0]!.platformId}`));
  await page.screenshot({ path: evidencePath(testInfo, "immersive-platform.png"), fullPage: true });
});

test("ACC-IMM-003 game browser renders authorized cover video and description fallback", async ({ page }, testInfo) => {
  test.setTimeout(60_000);
  await enterImmersive(page);
  await choosePlatform(page, "Game Boy Advance");
  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(/\/immersive\/platforms\/gba/);
  const listbox = page.getByRole("listbox", { name: /Game Boy Advance 游戏/ });
  await expect(listbox).toBeVisible();
  await expect(page.getByLabel("上下方向键").locator("svg")).toBeVisible();
  await expect(page.getByLabel("左右方向键").locator("svg")).toBeVisible();
  await selectGameByTitle(page, "Sudoku");
  const selected = listbox.getByRole("option", { selected: true });
  await expect(selected).toContainText("Sudoku");
  await expect(page.getByRole("img", { name: "Sudoku 封面" })).toBeVisible();
  await expect(page.locator("video")).toHaveCount(1, { timeout: 5_000 });
  const mediaHeights = await page.locator('[data-immersive-description="true"]').evaluate(() => {
    const poster = document.querySelector<HTMLElement>('[data-immersive-poster="true"]');
    const video = document.querySelector<HTMLElement>('[data-immersive-video-panel="true"]');
    return { poster: poster?.getBoundingClientRect().height ?? 0, video: video?.getBoundingClientRect().height ?? 0 };
  });
  expect(mediaHeights.poster).toBeGreaterThan(0);
  expect(Math.abs(mediaHeights.poster - mediaHeights.video)).toBeLessThanOrEqual(1);
  const description = page.locator('[data-immersive-description="true"]');
  await expect(description).toContainText("Retrom 的项目自有测试游戏");
  const descriptionMetrics = await description.evaluate((element) => ({
    clientHeight: element.clientHeight,
    overflowY: getComputedStyle(element).overflowY,
    scrollHeight: element.scrollHeight,
  }));
  expect(descriptionMetrics.overflowY).toBe("auto");
  expect(descriptionMetrics.scrollHeight).toBeGreaterThan(descriptionMetrics.clientHeight);
  await description.evaluate((element) => {element.scrollTop = element.scrollHeight;});
  expect(await description.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  await pressGamepad(page, standardButton.b);
  await expect(page).toHaveURL(/\/immersive\?platformId=gba$/);
  await page.screenshot({ path: evidencePath(testInfo, "immersive-game-list.png"), fullPage: true });
});

test("ACC-IMM-004 real GBA launch advances frames and returns to the selected game", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  await openPlatform(page, "Game Boy Advance");
  await selectGameByTitle(page, "Sudoku");
  const { frame } = await launchSelectedGame(page);
  const initialFrame = await frameNumber(frame);
  await expect.poll(() => frameNumber(frame), { timeout: 10_000 }).toBeGreaterThan(initialFrame + 30);
  await expect.poll(() => currentEmulatorBrightRatio(page), { timeout: 10_000 }).toBeGreaterThan(0.01);
  await page.screenshot({ path: evidencePath(testInfo, "immersive-gba-running.png"), fullPage: true });
  await openPlayerMenu(page);
  await exitFromPlayerMenu(page);
  await expect(page.getByRole("status").filter({ hasText: "等待手柄" })).toHaveCount(0);
  await expect(page.getByRole("option", { selected: true })).toContainText("Sudoku");
});

test("ACC-IMM-005 reserved chord pauses, continues and exits without save writes", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  const saveWrites: string[] = [];
  page.on("request", (request) => {
    if (/\/runtime\/launches\/[^/]+\/save-states$/.test(request.url())) {saveWrites.push(request.url());}
  });
  await openPlatform(page, "Game Boy Advance");
  const { frame } = await launchSelectedGame(page);

  await setGamepadButtons(page, 0, [standardButton.select]);
  await page.waitForTimeout(140);
  expect(await frame.evaluate(() => navigator.getGamepads()[0]?.buttons[8]?.pressed)).toBe(true);
  await setGamepadButtons(page, 0, []);
  await page.waitForTimeout(180);

  const menu = await openPlayerMenu(page);
  expect(await frame.evaluate(() => window.EJS_emulator?.paused)).toBe(true);
  await pressGamepad(page, standardButton.b);
  await expect(menu).toBeHidden();
  const resumedAt = await frameNumber(frame);
  await expect.poll(() => frameNumber(frame), { timeout: 10_000 }).toBeGreaterThan(resumedAt + 10);

  await openPlayerMenu(page);
  await page.screenshot({ path: evidencePath(testInfo, "immersive-pause-menu.png"), fullPage: true });
  await exitFromPlayerMenu(page);
  expect(saveWrites).toEqual([]);
});

test("ACC-IMM-006 Arcade keeps P2 input and gives menu ownership only to the active pad", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  await openPlatform(page, "Arcade");
  const selectedGame = await selectGameForCore(page, "mame2003");
  const { frame } = await launchSelectedGame(page);
  await expect.poll(() => currentEmulatorBrightRatio(page), { timeout: 10_000 }).toBeGreaterThan(0.001);

  await setGamepadButtons(page, 1, [standardButton.a]);
  await page.waitForTimeout(80);
  expect(await frame.evaluate(() => navigator.getGamepads()[1]?.buttons[0]?.pressed)).toBe(true);
  await setGamepadButtons(page, 1, []);
  await page.waitForTimeout(180);
  await pressGamepad(page, standardButton.select, 0, 140, 180);
  await pressGamepad(page, standardButton.start, 0, 140, 180);

  await openPlayerMenu(page);
  const filtered = await frame.evaluate(() => Array.from(navigator.getGamepads()).map((gamepad) =>
    gamepad ? gamepad.buttons.some((button) => button.pressed || button.value >= 0.5) : false));
  expect(filtered).toEqual([false, false]);
  await page.screenshot({ path: evidencePath(testInfo, "immersive-arcade-menu.png"), fullPage: true });
  await exitFromPlayerMenu(page);
  expect(selectedGame.games.length).toBeGreaterThan(1);
  const shell = page.locator('[data-immersive-shell="true"]');
  expect(await page.evaluate(() => ({
    focused: document.hasFocus(),
    gamepads: Array.from(navigator.getGamepads()).filter(Boolean).length,
  }))).toEqual({ focused: true, gamepads: 2 });
  await expect(shell).toHaveAttribute("data-controller-state", "ready", { timeout: 2_000 });
  const move = selectedGame.index === selectedGame.games.length - 1 ? "up" : "down";
  const targetIndex = selectedGame.index + (move === "up" ? -1 : 1);
  await pressGamepad(page, standardButton[move]);
  await expect(page).toHaveURL(new RegExp(`gameId=${selectedGame.games[targetIndex]!.gameId}`));
});

test("ACC-IMM-007 immersive shell has no overflow and no serious accessibility violations", async ({ page }, testInfo: TestInfo) => {
  const hydrationErrors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error" && message.text().includes("hydrated")) {hydrationErrors.push(message.text());}
  });
  await enterImmersive(page);
  if (testInfo.project.name === "chrome-1280") {
    await setGamepadConnected(page, 0, false);
    await expect(page.getByRole("status")).toContainText("等待手柄");
    await setGamepadConnected(page, 0, true);
    await pressGamepad(page, standardButton.a);
    await expect(page.getByRole("status").filter({ hasText: "等待手柄" })).toBeHidden();
  }
  await noPageOverflow(page);
  await page.evaluate(axe.source);
  const violations = await page.evaluate(async () => {
    const axeAPI = (window as typeof window & {
      axe: { run: (root: Document, options: object) => Promise<{ violations: Array<{ impact: string | null }> }> };
    }).axe;
    const result = await axeAPI.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((item) => item.impact === "serious" || item.impact === "critical");
  });
  expect(violations).toEqual([]);
  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect(page.locator('[data-immersive-shell="true"]')).toBeVisible();
  expect(hydrationErrors).toEqual([]);
  await page.screenshot({ path: evidencePath(testInfo, "immersive-accessibility.png"), fullPage: true });
});

test("ACC-IMM-008 ordinary Player and navigation remain outside immersive mode", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  await page.goto("/library?q=Sudoku");
  const game = page.locator(".library-game-card").filter({ hasText: "Sudoku" });
  await expect(game).toHaveCount(1);
  await game.getByRole("link").first().click();
  const configResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  const config = await (await configResponse).json() as { playerAdapterId: string };
  expect(config.playerAdapterId).toBe("ejs-4.2.3-v3");
  await expect(page.locator(".player-toolbar")).toBeVisible();
  await expect(page.getByRole("dialog", { name: "游戏菜单" })).toHaveCount(0);
  await page.screenshot({ path: evidencePath(testInfo, "standard-player-regression.png"), fullPage: true });
});
