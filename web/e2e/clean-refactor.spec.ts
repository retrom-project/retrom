import { expect } from "@playwright/test";
import { test } from "./clean-refactor-fixtures";
import type { Schema } from "../lib/api/types";
import { nesFrame, nesPlayerCounter, sendNesInput } from "./nes-playback";
import {
  instantConflict,
  newInstantSave,
  overwriteInstantSave,
} from "./instant-save-choices";
import {
  acceptanceTitle,
  fixtureGame,
  leavePlayer,
  login,
  prepareLibrary,
  request,
  revealPlayerControls,
  verifyLibraryFilters,
} from "./clean-refactor-support";

test("shared review, real NES save and new-instance restore", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await login(page);
  const game = await prepareLibrary(page);
  await page.goto(`/games/${game.id}`);
  await expect(
    page.getByRole("heading", { name: game.title, exact: true }).first(),
  ).toBeVisible();
  const start = page.getByRole("button", { name: "重新开始游戏", exact: true });
  if (!(await start.isVisible())) {
    await page.getByRole("button", { name: "启动选项", exact: true }).click();
  }
  await start.click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+\?/u);
  await expect(
    page.getByRole("button", { name: /^(创建存档|保存)$/u }),
  ).toBeEnabled({ timeout: 60_000 });
  await revealPlayerControls(page);
  const toolbarFullscreen = page.getByRole("button", { name: "全屏", exact: true });
  if (await toolbarFullscreen.isVisible()) {
    await toolbarFullscreen.click();
    await expect(page.getByRole("button", { name: "退出全屏", exact: true })).toBeVisible();
    await page.getByRole("button", { name: "退出全屏", exact: true }).click();
  } else {
    await page.getByRole("button", { name: "更多操作", exact: true }).click();
    await page.getByRole("menuitem", { name: "在更多操作中进入全屏", exact: true }).click();
    await expect(page.getByRole("menuitem", { name: "在更多操作中退出全屏", exact: true })).toBeVisible();
    await page.getByRole("menuitem", { name: "在更多操作中退出全屏", exact: true }).click();
    const menu = page.getByRole("menu", { name: "Player 更多操作", exact: true });
    await menu.getByRole("button", { name: "关闭更多操作", exact: true }).click();
    await expect(menu).toHaveCount(0);
  }
  const firstFrame = await nesFrame(page);
  await revealPlayerControls(page);
  const pause = page.getByRole("button", { name: "暂停", exact: true });
  await expect
    .poll(() =>
      pause.evaluate((element) => {
        const box = element.getBoundingClientRect();
        const target = document.elementFromPoint(
          box.x + box.width / 2,
          box.y + box.height / 2,
        );
        return target !== null && element.contains(target);
      }),
    )
    .toBe(true);
  await pause.click();
  await expect(
    page.getByRole("button", { name: "继续", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "继续", exact: true }).click();
  const playerCounter = await sendNesInput(page, firstFrame);
  await expect
    .poll(async () =>
      page.locator(".player-toolbar").evaluate((element) =>
        element.getBoundingClientRect().bottom,
      ),
    )
    .toBeLessThanOrEqual(0);
  await revealPlayerControls(page);
  const persisted = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/saves") &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: /^(创建存档|保存)$/u }).click();
  const firstResponse = await persisted;
  expect(firstResponse.status()).toBe(200);
  const firstSave = (await firstResponse.json()) as Schema<"Save">;
  await expect(
    page.getByRole("status").filter({ hasText: "存档已同步。" }),
  ).toBeVisible();
  const newSave = await newInstantSave(page, firstSave);
  const latestSave = await overwriteInstantSave(page, newSave);
  await leavePlayer(page);
  await page.goto(`/saves?gameId=${game.id}`);
  await expect(
    page.getByRole("heading", { name: acceptanceTitle, exact: true }).first(),
  ).toBeVisible();
  const saves = await request<Schema<"SavePage">>(
    page,
    `/api/v1/saves?gameId=${game.id}`,
  );
  expect(saves.items.length).toBeGreaterThan(0);
  expect(saves.items[0].restorable).toBe(true);
  await page
    .locator(".save-library-card")
    .first()
    .getByRole("button", { name: "从这里继续", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: /^(创建存档|保存)$/u }),
  ).toBeEnabled({ timeout: 60_000 });
  await expect(page.getByText("游戏无法运行", { exact: true })).toHaveCount(0);
  const restoredFrame = await nesFrame(page);
  expect(await nesPlayerCounter(restoredFrame)).toBe(playerCounter);
  const resumedCounter = await sendNesInput(page, restoredFrame);
  expect(resumedCounter).not.toBe(playerCounter);
  await page.waitForTimeout(7_000);
  expect(restoredFrame.isDetached()).toBe(false);
  expect(await nesPlayerCounter(restoredFrame)).toBe(resumedCounter);
  await instantConflict(page, latestSave);
  await leavePlayer(page);
  expect(errors).toEqual([]);
});

test("populated pages keep their viewport and functional favorite classification", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await login(page);
  const game = await fixtureGame(page);
  await request(page, `/api/v1/favorites/${game.id}`, { folderIds: [] }, "PUT");
  const folders = await request<Schema<"FolderList">>(
    page,
    "/api/v1/favorite-folders",
  );
  const folderName = `Browser ${test.info().project.name}`;
  const folder =
    folders.items.find((item) => item.name === folderName) ??
    (await request<Schema<"FavoriteFolder">>(page, "/api/v1/favorite-folders", {
      name: folderName,
    }));
  await page.goto("/favorites");
  await page
    .locator(".favorite-game-card")
    .filter({ has: page.locator(`a[href="/games/${game.id}"]`) })
    .getByRole("button", {
      name: `整理${acceptanceTitle}的收藏夹`,
      exact: true,
    })
    .click();
  await page
    .getByRole("alertdialog")
    .getByLabel(folder.name, { exact: true })
    .check();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "保存分类", exact: true })
    .click();
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  const classified = await request<Schema<"GamePage">>(
    page,
    `/api/v1/favorites?folderId=${folder.id}`,
  );
  expect(classified.items.some((item) => item.id === game.id)).toBe(true);
  const routes = [
    "/",
    "/library",
    "/favorites",
    "/saves",
    "/recent",
    "/admin/imports",
    "/admin/imports/server",
    "/admin/reviews",
    "/admin/games",
    "/admin/platform-instances",
    "/admin/tags",
    "/admin/bios",
    "/admin/users",
    "/account",
    "/me",
    "/immersive",
    `/games/${game.id}`,
  ];
  for (const route of routes) {
    await page.goto(route);
    await expect(page.locator("main, .immersive-shell").first()).toBeAttached();
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
    await page.screenshot({
      path: test
        .info()
        .outputPath(`${route.replace(/[^a-z0-9]/gu, "-") || "home"}.png`),
    });
  }
  expect(errors).toEqual([]);
});

test("save search, kinds and game choice use server filters", async ({
  page,
}) => {
  await login(page);
  await verifyLibraryFilters(page);
  await page.goto("/saves");
  await page
    .getByRole("searchbox", { name: "搜索存档", exact: true })
    .fill("no-save-matches-this-acceptance-query");
  await expect(
    page.getByRole("heading", { name: "还没有存档", exact: true }),
  ).toBeVisible();
  const empty = await request<Schema<"SavePage">>(
    page,
    "/api/v1/saves?q=no-save-matches-this-acceptance-query",
  );
  expect(empty.total).toBe(0);
  expect(empty.gameCount).toBe(0);
  await page.getByRole("searchbox", { name: "搜索存档", exact: true }).fill("");
  await page.getByRole("button", { name: "游戏", exact: true }).click();
  await page
    .getByRole("searchbox", { name: "查找游戏", exact: true })
    .fill(acceptanceTitle);
  const choice = page.getByRole("combobox", { name: "游戏筛选", exact: true });
  const game = await fixtureGame(page);
  await expect(
    choice.getByRole("option", {
      name: `${acceptanceTitle} · Browser acceptance`,
      exact: true,
    }),
  ).toBeAttached();
  await choice.selectOption(game.id);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page
    .getByRole("combobox", { name: "存档类型", exact: true })
    .selectOption("checkpoint");
  await page
    .getByRole("combobox", { name: "排列", exact: true })
    .selectOption("title");
  await expect(page.locator(".save-library-card").first()).toBeVisible();
  const filtered = await request<Schema<"SavePage">>(
    page,
    `/api/v1/saves?gameId=${game.id}&kind=checkpoint&sort=title`,
  );
  expect(filtered.gameCount).toBe(1);
  expect(
    filtered.items.every(
      (save) => save.kind === "checkpoint" && save.game.id === game.id,
    ),
  ).toBe(true);
});

test("immersive audio and keyboard controls retain their behavior", async ({ page }) => {
  await login(page);
  await page.goto("/immersive");
  const viewport = page.viewportSize();
  if (viewport && (viewport.width < 960 || viewport.height < 540 || viewport.height >= viewport.width)) {
    await expect(page.getByRole("heading", { name: "沉浸模式需要横屏大屏", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "返回普通首页", exact: true })).toBeVisible();
    return;
  }
  await expect(page.getByRole("heading", { name: "等待手柄", exact: true })).toBeVisible();
  await page.keyboard.press("ArrowDown");
  await expect(page.getByRole("heading", { name: "今天想玩哪个平台？", exact: true })).toBeVisible();
  const audio = page.locator("audio[data-immersive-bgm]");
  const audioNode = await audio.elementHandle();
  expect(audioNode).not.toBeNull();
  const enable = page.getByRole("button", { name: "启用背景音乐", exact: true });
  await expect.poll(async () => await enable.isVisible() || await audio.evaluate((element) => !(element as HTMLAudioElement).paused)).toBe(true);
  if (await enable.isVisible()) { await enable.click(); }
  const initialTime = await audio.evaluate((element) => (element as HTMLAudioElement).currentTime);
  await expect.poll(() => audio.evaluate((element) => (element as HTMLAudioElement).currentTime)).toBeGreaterThan(initialTime);
  await page.keyboard.press("s");
  const menu = page.getByRole("dialog", { name: "系统菜单", exact: true });
  await expect(menu).toBeVisible();
  await menu.getByRole("button", { name: "背景音乐音量提高", exact: true }).click();
  await expect.poll(() => audio.evaluate((element) => (element as HTMLAudioElement).volume)).toBe(0.5);
  await menu.getByRole("button", { name: "背景音乐静音", exact: true }).click();
  await expect.poll(() => audio.evaluate((element) => (element as HTMLAudioElement).muted && (element as HTMLAudioElement).paused)).toBe(true);
  await menu.getByRole("button", { name: "背景音乐静音", exact: true }).click();
  await expect.poll(() => audio.evaluate((element) => !(element as HTMLAudioElement).muted && !(element as HTMLAudioElement).paused)).toBe(true);
  await page.keyboard.press("Tab");
  await expect.poll(() => menu.evaluate((element) => element.contains(document.activeElement))).toBe(true);
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);
  await page.keyboard.press("s");
  await menu.getByRole("button", { name: "退出沉浸模式", exact: true }).click();
  await expect(page).toHaveURL(/\/$/u);
  await expect(audio).toHaveCount(0);
  expect(await audioNode!.evaluate((element) => (element as HTMLAudioElement).paused)).toBe(true);
});


test("immersive player owns its iframe shortcut and returns to the selected game", async ({ page }) => {
  test.setTimeout(120_000);
  await login(page);
  const game = await fixtureGame(page);
  const returnQuery = new URLSearchParams({ view: "games", destination: "all", entry: game.id, offset: "0", folder: "" });
  await page.goto(`/immersive?${returnQuery}`);
  const viewport = page.viewportSize();
  if (viewport && viewport.width < 960) {
    await expect(page.getByRole("heading", { name: "沉浸模式需要横屏大屏", exact: true })).toBeVisible();
    return;
  }
  await expect(page.getByRole("heading", { name: "等待手柄", exact: true })).toBeVisible();
  await page.keyboard.press("ArrowLeft");
  await page.getByRole("button", { name: "开始游戏", exact: true }).click();
  await expect(page).toHaveURL(/\/play\//u);
  const frame = await nesFrame(page);
  await expect(page.locator(".player-toolbar")).toHaveCount(0);
  await frame.locator("canvas").click({ position: { x: 10, y: 10 } });
  await page.keyboard.press("m");
  const menu = page.getByRole("dialog", { name: "游戏菜单", exact: true });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("button", { name: "取消", exact: true })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);
  await expect.poll(() => page.evaluate(() => document.activeElement?.tagName)).toBe("IFRAME");
  await page.keyboard.press("m");
  await expect(menu).toBeVisible();
  await menu.getByRole("button", { name: "退出游戏", exact: true }).click();
  await expect(page).toHaveURL(/\/immersive\?/u);
  expect(new URL(page.url()).searchParams.get("entry")).toBe(game.id);
  await expect(page.getByRole("heading", { name: "等待手柄", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { pressed: true }).filter({ hasText: game.title })).toBeVisible();
});
