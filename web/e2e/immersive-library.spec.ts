import { execFileSync } from "node:child_process";
import path from "node:path";
import { expect, test, type Locator, type Page } from "@playwright/test";
import axe from "axe-core";
import { currentEmulatorBrightRatio, evidencePath } from "./acceptance-support";
import { installGamepads, pressGamepad, setGamepadButtons, standardButton } from "./immersive-gamepad";

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
const audioPreferenceKey = "retrom:immersive:audio-preferences:v1";
const seededFolderId = "0198ff00-1000-7000-8000-000000000001";

function ensureImmersiveLibrarySeed() {
  const database = process.env.RETROM_E2E_DATABASE;
  expect(database, "RETROM_E2E_DATABASE must point to the temporary acceptance database").toBeTruthy();
  execFileSync(
    "python3",
    [path.resolve("../scripts/acceptance/seed-immersive-library.py"), database!],
    { stdio: "pipe" },
  );
}

type Destination = {
  destinationId: string;
  kind: "all" | "recent" | "favorites" | "saves" | "platform";
  name: string;
};

type SaveItem = {
  gameId: string;
  saveStateId: string;
  screenshotUrl: string;
};

type LibraryItem = {
  gameId: string;
  title: string;
  titleInitial: string;
};

type LibraryPage = {
  folders?: Array<{ folderId: string; name: string }>;
  items: LibraryItem[];
  nextCursor: string | null;
};

test.beforeEach(async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "chrome-1280", "Stateful immersive library cases run once.");
  await installGamepads(page);
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" },
    headers: { Origin: origin },
  });
  expect(response.ok()).toBe(true);
});

async function enterImmersive(page: Page) {
  await page.goto("/");
  await page.waitForTimeout(250);
  await pressGamepad(page, standardButton.a);
  const dialog = page.getByRole("alertdialog", { name: "进入沉浸模式？" });
  await expect(dialog).toBeVisible();
  await pressGamepad(page, standardButton.right);
  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(/\/immersive$/);
  await expect(page.locator('[data-immersive-shell="true"]')).toHaveAttribute("data-controller-state", "ready");
}

async function destinations(page: Page) {
  const response = await page.request.get("/api/v1/immersive/destinations");
  expect(response.ok()).toBe(true);
  return (await response.json() as { items: Destination[] }).items;
}

async function chooseDestination(page: Page, name: string) {
  const items = await destinations(page);
  const current = page.getByRole("option", { selected: true });
  for (let attempt = 0; attempt < items.length; attempt += 1) {
    if ((await current.textContent())?.includes(name)) {
      await page.waitForTimeout(180);
      return;
    }
    await pressGamepad(page, standardButton.right);
  }
  throw new Error(`IMMERSIVE_DESTINATION_NOT_SELECTABLE:${name}`);
}

async function openDestination(page: Page, name: string, path: RegExp) {
  await chooseDestination(page, name);
  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(path);
  await expect(page.getByRole("listbox", { name: "沉浸游戏列表" })).toBeVisible();
  await page.waitForTimeout(180);
}

async function selectOption(page: Page, text: string) {
  const options = page.getByRole("listbox", { name: "沉浸游戏列表" }).getByRole("option");
  const selected = page.getByRole("option", { selected: true });
  for (let attempt = 0; attempt < 300; attempt += 1) {
    if ((await selected.textContent())?.includes(text)) {return;}
    const labels = await options.allTextContents();
    const selectedLabel = await selected.textContent();
    const selectedIndex = labels.findIndex((label) => label === selectedLabel);
    const targetIndex = labels.findIndex((label) => label.includes(text));
    const direction = targetIndex >= 0 && targetIndex < selectedIndex ? standardButton.up : standardButton.down;
    await pressGamepad(page, direction, 0, 50, 130);
  }
  throw new Error(`IMMERSIVE_LIBRARY_OPTION_NOT_SELECTABLE:${text}`);
}

async function csrfToken(page: Page) {
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" },
    headers: { Origin: origin },
  });
  expect(response.ok()).toBe(true);
  return (await response.json() as { csrfToken: string }).csrfToken;
}

async function assignFolder(page: Page, csrf: string, gameId: string, folderId: string) {
  const response = await page.request.put(`/api/v1/favorites/${gameId}/folders`, {
    data: { folderIds: [folderId] },
    headers: { Origin: origin, "X-Retrom-Csrf": csrf },
  });
  expect(response.ok()).toBe(true);
}

async function emulatorFrame(page: Page) {
  const handle = await page.locator("iframe.player-frame").elementHandle();
  const frame = await handle?.contentFrame();
  if (!frame) {throw new Error("IMMERSIVE_EMULATOR_FRAME_UNAVAILABLE");}
  return frame;
}

async function launchSelectedGame(page: Page, saveStateId: string | null = null) {
  const configResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  const launchResponse = page.waitForResponse((response) =>
    response.request().method() === "POST" && /\/api\/v1\/launches$/.test(response.url()));
  await page.waitForTimeout(180);
  // This input replaces the top-level document. Releasing it through the old
  // execution context races with navigation; the new document's init script
  // starts from a fully released gamepad state.
  await setGamepadButtons(page, 0, [standardButton.a]);
  await page.waitForTimeout(140);
  const response = await launchResponse;
  if (response.status() !== 201) {
    throw new Error(`IMMERSIVE_LAUNCH_FAILED:${response.status()}:${await response.text()}`);
  }
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+\?experience=immersive$/);
  const configuration = await (await configResponse).json() as { stateUrl: string | null };
  expect(response.request().postDataJSON()).toMatchObject({ saveStateId });
  expect(configuration.stateUrl === null).toBe(saveStateId === null);
  await expect(page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas"))
    .toBeVisible({ timeout: 60_000 });
  return { configuration, frame: await emulatorFrame(page) };
}

async function openPlayerMenu(page: Page) {
  await pressGamepad(page, [standardButton.select, standardButton.start], 0, 80, 100);
  await pressGamepad(page, [standardButton.select, standardButton.start], 0, 80, 180);
  const menu = page.getByRole("dialog", { name: "游戏菜单" });
  await expect(menu).toBeVisible();
  return menu;
}

async function createSaveFromMenu(page: Page, menu: Locator, selectItem = true) {
  if (selectItem) {await pressGamepad(page, standardButton.right);}
  await expect(menu.getByRole("button", { name: "创建存档" })).toHaveAttribute("aria-current", "true");
  const response = page.waitForResponse((candidate) =>
    candidate.request().method() === "POST" && /\/runtime\/launches\/[^/]+\/save-states$/.test(candidate.url()));
  await pressGamepad(page, standardButton.a);
  expect((await response).status()).toBe(201);
  await expect(menu.getByRole("status")).toHaveText("存档已创建。", { timeout: 20_000 });
}

async function exitPlayer(page: Page, menu: Locator) {
  await expect(menu.getByRole("button", { name: "取消" })).toHaveAttribute("aria-current", "true");
  await pressGamepad(page, standardButton.right);
  await pressGamepad(page, standardButton.right);
  const finished = page.waitForResponse((response) =>
    response.request().method() === "POST" && /\/runtime\/launches\/[^/]+\/finish$/.test(response.url()));
  await pressGamepad(page, standardButton.a);
  expect((await finished).ok()).toBe(true);
  await expect(page).toHaveURL(/\/immersive\/(?:library|platforms)\//);
}

async function saves(page: Page) {
  const response = await page.request.get("/api/v1/saves?limit=100");
  expect(response.ok()).toBe(true);
  return (await response.json() as { items: SaveItem[] }).items;
}

async function readPagedGames(page: Page, path: string) {
  const items: LibraryItem[] = [];
  const pageSizes: number[] = [];
  let firstPage: LibraryPage | null = null;
  let cursor: string | null = null;
  for (let pageIndex = 0; pageIndex < 5; pageIndex += 1) {
    const separator = path.includes("?") ? "&" : "?";
    const response = await page.request.get(`${path}${separator}limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`);
    expect(response.ok()).toBe(true);
    const payload = await response.json() as LibraryPage;
    firstPage ??= payload;
    items.push(...payload.items);
    pageSizes.push(payload.items.length);
    cursor = payload.nextCursor;
    if (!cursor) {break;}
  }
  expect(cursor).toBeNull();
  expect(new Set(items.map((item) => item.gameId)).size).toBe(items.length);
  return { firstPage: firstPage!, items, pageSizes };
}

test("ACC-IMM-009 virtual libraries, custom folders and Y favorite stay controller navigable", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  ensureImmersiveLibrarySeed();
  const csrf = await csrfToken(page);
  await enterImmersive(page);
  const allGames = await readPagedGames(page, "/api/v1/immersive/libraries/all/games");
  expect(allGames.pageSizes[0]).toBe(50);
  expect(allGames.items.length).toBeGreaterThanOrEqual(53);
  const specialOrder = new Set([
    "# Symbol acceptance",
    "🎮 Emoji acceptance",
    "0 Numeric acceptance",
    "9 Numeric acceptance",
    "alpha lowercase acceptance",
    "Arcade uppercase acceptance",
    "打击者验收",
    "遊戲驗收",
  ]);
  expect(allGames.items.filter((item) => specialOrder.has(item.title)).map((item) => [item.title, item.titleInitial])).toEqual([
    ["# Symbol acceptance", "#"],
    ["🎮 Emoji acceptance", "#"],
    ["0 Numeric acceptance", "0"],
    ["9 Numeric acceptance", "9"],
    ["alpha lowercase acceptance", "A"],
    ["Arcade uppercase acceptance", "A"],
    ["打击者验收", "D"],
    ["遊戲驗收", "Y"],
  ]);
  await openDestination(page, "全部游戏", /\/immersive\/library\/all/);
  await selectOption(page, "Sudoku");
  const gameId = new URL(page.url()).searchParams.get("gameId");
  expect(gameId).toMatch(/^[0-9a-f-]{36}$/);

  await pressGamepad(page, standardButton.y);
  await expect(page.getByRole("status")).toHaveText("已收藏游戏。");
  await expect(page.getByRole("option", { selected: true }).getByLabel("已收藏")).toBeVisible();
  await page.route("**/api/v1/favorites/unfavorite", (route) => route.fulfill({
    status: 503,
    contentType: "application/json",
    body: JSON.stringify({ error: { message: "模拟收藏服务失败" } }),
  }));
  await pressGamepad(page, standardButton.y);
  await expect(page.getByRole("status")).toHaveText("模拟收藏服务失败");
  await expect(page.getByRole("option", { selected: true }).getByLabel("已收藏")).toBeVisible();
  await page.unroute("**/api/v1/favorites/unfavorite");
  await pressGamepad(page, standardButton.y);
  await expect(page.getByRole("status")).toHaveText("已取消收藏。");
  await expect(page.getByRole("option", { selected: true }).getByLabel("已收藏")).toHaveCount(0);
  await pressGamepad(page, standardButton.y);
  await expect(page.getByRole("status")).toHaveText("已收藏游戏。");
  await assignFolder(page, csrf, gameId!, seededFolderId);

  const recentGames = await readPagedGames(page, "/api/v1/immersive/libraries/recent/games");
  expect(recentGames.pageSizes[0]).toBe(50);
  expect(recentGames.items.length).toBeGreaterThanOrEqual(53);
  const favorites = await readPagedGames(page, "/api/v1/immersive/libraries/favorites/games");
  expect(favorites.pageSizes[0]).toBe(50);
  expect(favorites.items.length).toBeGreaterThanOrEqual(53);
  const seededFolder = favorites.firstPage.folders?.find((folder) => folder.name === "验收分页");
  expect(seededFolder).toBeTruthy();
  const folderGames = await readPagedGames(
    page,
    `/api/v1/immersive/libraries/favorites/games?folderId=${seededFolder!.folderId}`,
  );
  expect(folderGames.pageSizes).toEqual([50, 3]);
  const platformGames = await readPagedGames(page, "/api/v1/immersive/platforms/gba/games");
  expect(platformGames.pageSizes[0]).toBe(50);
  expect(platformGames.items.length).toBeGreaterThanOrEqual(53);

  await pressGamepad(page, standardButton.b);
  await chooseDestination(page, "最近游玩");
  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(/\/immersive\/library\/recent/);
  await selectOption(page, "Sudoku");
  await pressGamepad(page, standardButton.b);
  await openDestination(page, "收藏游戏", /\/immersive\/library\/favorites/);
  await expect(page.getByRole("option", { name: /验收分页/ })).toBeVisible();
  await expect(page.getByText("另一玩家私有", { exact: true })).toHaveCount(0);
  await selectOption(page, "Sudoku");
  await expect(page.getByRole("option", { selected: true })).toContainText("Sudoku");
  await selectOption(page, "验收分页");
  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(new RegExp(`folderId=${seededFolderId}`));
  await selectOption(page, "Sudoku");
  await expect(page.getByRole("option", { selected: true })).toContainText("Sudoku");
  await pressGamepad(page, standardButton.b);
  await pressGamepad(page, standardButton.b);
  await chooseDestination(page, "Game Boy Advance");
  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(/\/immersive\/platforms\/gba/);
  await page.screenshot({ path: evidencePath(testInfo, "immersive-library-favorites.png"), fullPage: true });
});

test("ACC-IMM-010 save library selects an older save, restores it and creates another", async ({ page }, testInfo) => {
  test.setTimeout(210_000);
  await enterImmersive(page);
  await openDestination(page, "全部游戏", /\/immersive\/library\/all/);
  await selectOption(page, "Sudoku");
  const gameId = new URL(page.url()).searchParams.get("gameId");
  expect(gameId).toBeTruthy();
  const baseline = (await saves(page)).filter((save) => save.gameId === gameId).length;
  await launchSelectedGame(page);
  let menu = await openPlayerMenu(page);
  await createSaveFromMenu(page, menu);
  await createSaveFromMenu(page, menu, false);
  await pressGamepad(page, standardButton.b);
  await expect(menu).toBeHidden();
  menu = await openPlayerMenu(page);
  await exitPlayer(page, menu);

  const initialSaves = (await saves(page)).filter((save) => save.gameId === gameId);
  expect(initialSaves).toHaveLength(baseline + 2);
  const saveTotal = initialSaves.length;
  await pressGamepad(page, standardButton.b);
  const screenshotResponse = page.waitForResponse((response) =>
    /\/content\/save-states\/[^/]+\/screenshot$/.test(response.url()) && response.status() === 200);
  await openDestination(page, "我的存档", /\/immersive\/library\/saves/);
  await selectOption(page, "Sudoku");
  await expect(page.getByText(`我的存档 · 1 / ${saveTotal}`)).toBeVisible();
  expect((await screenshotResponse).headers()["cache-control"]).toBe("private, no-store");
  const newestSaveId = new URL(page.url()).searchParams.get("saveStateId");
  expect(newestSaveId).toBeTruthy();
  await pressGamepad(page, standardButton.right);
  await expect(page.getByText(`我的存档 · 2 / ${saveTotal}`)).toBeVisible();
  const selectedSaveId = new URL(page.url()).searchParams.get("saveStateId");
  expect(selectedSaveId).toBeTruthy();
  expect(selectedSaveId).not.toBe(newestSaveId);
  const { configuration, frame } = await launchSelectedGame(page, selectedSaveId);
  expect(configuration.stateUrl).toMatch(/^\/runtime\/launches\/[0-9a-f-]+\/state$/);
  await expect.poll(() => currentEmulatorBrightRatio(page), { timeout: 10_000 }).toBeGreaterThan(0.01);
  expect(await frame.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0)).toBeGreaterThan(0);
  menu = await openPlayerMenu(page);
  await createSaveFromMenu(page, menu);
  await page.screenshot({ path: evidencePath(testInfo, "immersive-save-create.png"), fullPage: true });
  await pressGamepad(page, standardButton.b);
  await expect(menu).toBeHidden();
  menu = await openPlayerMenu(page);
  await exitPlayer(page, menu);
  expect((await saves(page)).filter((save) => save.gameId === gameId)).toHaveLength(baseline + 3);
});

test("ACC-IMM-011 Select menu persists audio preferences and BGM follows browse lifetime", async ({ page }, testInfo) => {
  test.setTimeout(150_000);
  await page.addInitScript(() => {
    const state = { playCalls: 0, pauseCalls: 0 };
    Object.defineProperty(window, "__retromE2EAudio", { configurable: true, value: state });
    Object.defineProperty(Element.prototype, "requestFullscreen", {
      configurable: true,
      value: () => {
        const calls = Number(localStorage.getItem("retrom:e2e:fullscreen-calls") ?? "0") + 1;
        localStorage.setItem("retrom:e2e:fullscreen-calls", String(calls));
        return calls === 2
          ? Promise.resolve()
          : Promise.reject(new DOMException("E2E denied", "NotAllowedError"));
      },
    });
    HTMLMediaElement.prototype.play = function () {
      if (!this.src.includes("/audio/immersive/insert-coin.ogg")) {return Promise.resolve();}
      state.playCalls = Number(localStorage.getItem("retrom:e2e:bgm-play-calls") ?? "0") + 1;
      localStorage.setItem("retrom:e2e:bgm-play-calls", String(state.playCalls));
      if (localStorage.getItem("retrom:e2e:immersive-audio-enabled") !== "true") {
        return Promise.reject(new DOMException("E2E autoplay denied", "NotAllowedError"));
      }
      return Promise.resolve();
    };
    HTMLMediaElement.prototype.pause = function () {
      if (!this.src.includes("/audio/immersive/insert-coin.ogg")) {return;}
      state.pauseCalls = Number(localStorage.getItem("retrom:e2e:bgm-pause-calls") ?? "0") + 1;
      localStorage.setItem("retrom:e2e:bgm-pause-calls", String(state.pauseCalls));
    };
  });
  await enterImmersive(page);
  await expect(page.getByText("背景音乐等待播放")).toBeVisible();
  await page.evaluate(() => localStorage.setItem("retrom:e2e:immersive-audio-enabled", "true"));
  await page.getByRole("button", { name: "启用背景音乐" }).click();
  await expect.poll(() => page.evaluate(() => Number(localStorage.getItem("retrom:e2e:bgm-play-calls") ?? "0")))
    .toBeGreaterThanOrEqual(2);

  await page.waitForTimeout(180);
  await pressGamepad(page, standardButton.select);
  const menu = page.getByRole("dialog", { name: "系统菜单" });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("meter", { name: "背景音乐音量" })).toHaveAttribute("aria-valuenow", "40");
  await expect(menu.getByRole("meter", { name: "游戏音量" })).toHaveAttribute("aria-valuenow", "100");
  await pressGamepad(page, standardButton.right);
  await pressGamepad(page, standardButton.down);
  await pressGamepad(page, standardButton.a);
  await pressGamepad(page, standardButton.down);
  await pressGamepad(page, standardButton.left);
  await pressGamepad(page, standardButton.down);
  await pressGamepad(page, standardButton.a);
  expect(JSON.parse(await page.evaluate((key) => localStorage.getItem(key)!, audioPreferenceKey))).toEqual({
    bgmVolume: 0.5,
    bgmMuted: true,
    gameVolume: 0.9,
    gameMuted: true,
  });
  await pressGamepad(page, standardButton.up);
  await pressGamepad(page, standardButton.up);
  await pressGamepad(page, standardButton.a);
  await pressGamepad(page, standardButton.down);
  await pressGamepad(page, standardButton.down);
  await pressGamepad(page, standardButton.down);
  await pressGamepad(page, standardButton.a);
  await expect(menu.getByRole("status")).toContainText("已进入全屏模式");
  await pressGamepad(page, standardButton.a);
  await expect(menu.getByRole("status")).toContainText("浏览器未允许全屏");
  expect(await page.evaluate(() => localStorage.getItem("retrom:e2e:fullscreen-calls"))).toBe("3");
  await page.evaluate(axe.source);
  const seriousViolations = await page.evaluate(async () => {
    const axeAPI = (window as typeof window & {
      axe: { run: (root: Document) => Promise<{ violations: Array<{ impact: string | null }> }> };
    }).axe;
    const result = await axeAPI.run(document);
    return result.violations.filter((item) => item.impact === "serious" || item.impact === "critical");
  });
  expect(seriousViolations).toEqual([]);
  await page.screenshot({ path: evidencePath(testInfo, "immersive-system-menu.png"), fullPage: true });
  await pressGamepad(page, standardButton.b);
  await expect(menu).toBeHidden();
  const audioBeforePlayer = await page.evaluate(() => ({
    pause: Number(localStorage.getItem("retrom:e2e:bgm-pause-calls") ?? "0"),
    play: Number(localStorage.getItem("retrom:e2e:bgm-play-calls") ?? "0"),
  }));

  await pressGamepad(page, standardButton.a);
  await expect(page).toHaveURL(/\/immersive\/library\/all/);
  await selectOption(page, "Sudoku");
  const { frame } = await launchSelectedGame(page);
  await expect(page.locator('audio[src="/audio/immersive/insert-coin.ogg"]')).toHaveCount(0);
  expect(await page.evaluate(() => Number(localStorage.getItem("retrom:e2e:bgm-pause-calls") ?? "0")))
    .toBeGreaterThan(audioBeforePlayer.pause);
  expect(await frame.evaluate(() => ({
    muted: window.EJS_emulator?.muted,
    volume: window.EJS_emulator?.volume,
  }))).toEqual({ muted: true, volume: 0.9 });
  const playerMenu = await openPlayerMenu(page);
  await exitPlayer(page, playerMenu);
  await expect(page.locator('audio[src="/audio/immersive/insert-coin.ogg"]')).toHaveCount(1);
  await expect.poll(() => page.evaluate(() => Number(localStorage.getItem("retrom:e2e:bgm-play-calls") ?? "0")))
    .toBeGreaterThan(audioBeforePlayer.play);
});
