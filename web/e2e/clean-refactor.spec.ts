import { writeFile } from "node:fs/promises";
import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { test } from "./clean-refactor-fixtures";
import type { Schema } from "../lib/api/types";
import { nesFrame, nesPlayerCounter, sendNesInput } from "./nes-playback";
import { holdController, installStandardController, pressController } from "./immersive-gamepad";
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

async function expectSaveContentToFit(page: Page) {
  await expect(page.locator(".save-library-group").first()).toBeVisible();
  const content = await page.locator("main").evaluate((element) => {
    const bounds = element.getBoundingClientRect();
    const style = getComputedStyle(element);
    return {
      left: bounds.left + parseFloat(style.paddingLeft),
      right: bounds.right - parseFloat(style.paddingRight),
    };
  });
  const selectors = [
    ".save-library-groups",
    ".save-library-group",
    ".save-library-group-head",
    ".save-library-group-main",
    ".save-library-group-main h2",
    ".save-library-group-main p",
    ".save-library-group-meta",
    ".save-library-group-meta a",
    ".save-library-grid",
    ".save-library-card",
    ".save-library-shot",
    ".save-library-menu-button",
    ".save-library-resume .button",
  ];
  const items = await page.locator(selectors.join(", ")).evaluateAll((elements) =>
    elements.filter((element) => element.getClientRects().length > 0).map((element) => {
      const bounds = element.getBoundingClientRect();
      const parent = element.parentElement;
      if (!parent) { throw new Error("Save content has no layout parent"); }
      const parentBounds = parent.getBoundingClientRect();
      const style = getComputedStyle(parent);
      return {
        label: `${element.tagName}.${element.className}`,
        left: bounds.left,
        right: bounds.right,
        width: bounds.width,
        parentLeft: parentBounds.left + parseFloat(style.borderLeftWidth) + parseFloat(style.paddingLeft),
        parentRight: parentBounds.right - parseFloat(style.borderRightWidth) - parseFloat(style.paddingRight),
      };
    }),
  );
  const boundsPath = test.info().outputPath("save-layout-bounds.json");
  await writeFile(boundsPath, JSON.stringify({ content, items }, null, 2));
  await test.info().attach("save-layout-bounds", {
    path: boundsPath,
    contentType: "application/json",
  });
  await page.screenshot({ path: test.info().outputPath("save-groups-containment.png") });
  for (const item of items) {
    expect(item.width, item.label).toBeGreaterThan(0);
    expect(item.left, item.label).toBeGreaterThanOrEqual(Math.max(content.left, item.parentLeft) - 1);
    expect(item.right, item.label).toBeLessThanOrEqual(Math.min(content.right, item.parentRight) + 1);
  }
}

test("shared review, real NES save and new-instance restore", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await login(page);
  const game = await prepareLibrary(page);
  await page.goto(`/library?q=${encodeURIComponent(game.title)}`);
  await page.locator(`a[href="/games/${game.id}"]`).first().click();
  await expect(page).toHaveURL(new RegExp(`/games/${game.id}$`, "u"));
  await expect(
    page.getByRole("heading", { level: 1, name: game.title, exact: true }),
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
  await page.getByRole("button", { name: "更多操作", exact: true }).click();
  const surfaceMenu = page.getByRole("menu", { name: "Player 更多操作", exact: true });
  await expect(surfaceMenu).toBeVisible();
  if (await page.locator(".player-menu-backdrop").isVisible()) {
    await page.locator(".player-menu-backdrop").click({ position: { x: 10, y: 10 } });
  } else {
    await firstFrame.locator("canvas").click();
  }
  await expect(surfaceMenu).toHaveCount(0);
  await expect(page.locator(".player-game-meta")).toHaveCount(0);
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
  await expect(page).toHaveURL(new RegExp(`/games/${game.id}$`, "u"));
  await page.goBack();
  await expect(page).toHaveURL(/\/library\?/u);
  expect(new URL(page.url()).searchParams.get("q")).toBe(game.title);
  if (page.viewportSize()!.width >= 768) {
    const played = page.locator(".library-game-card")
      .filter({ has: page.locator(`a[href="/games/${game.id}"]`) })
      .locator(".library-game-played");
    await expect(played.locator("time")).toHaveText(/^\d{2}\/\d{2} \d{2}:\d{2}$/u);
    await expect(played.locator("time")).toHaveAttribute("title", /\d{4}年/u);
    expect(await played.evaluate((row) => {
      const label = row.querySelector("span")!;
      const time = row.querySelector("time")!;
      const labelBox = label.getBoundingClientRect();
      const timeBox = time.getBoundingClientRect();
      return {
        labelSingleLine: labelBox.height <= parseFloat(getComputedStyle(label).lineHeight) + 0.5,
        timeSingleLine: timeBox.height <= parseFloat(getComputedStyle(time).lineHeight) + 0.5,
        timeNotClipped: time.scrollWidth <= time.parentElement!.clientWidth,
        withinRow: timeBox.right <= row.getBoundingClientRect().right + 0.5,
        separated: labelBox.right <= timeBox.left,
      };
    })).toEqual({ labelSingleLine: true, timeSingleLine: true, timeNotClipped: true, withinRow: true, separated: true });
  }
  await page.goForward();
  await expect(page).toHaveURL(new RegExp(`/games/${game.id}$`, "u"));
  await expect(page.getByText("CONTEXT_EXPIRED", { exact: false })).toHaveCount(0);
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
  await expect(page).toHaveURL(new RegExp(`/saves\\?gameId=${game.id}$`, "u"));
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`/games/${game.id}$`, "u"));
  await page.goForward();
  await expect(page).toHaveURL(new RegExp(`/saves\\?gameId=${game.id}$`, "u"));
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
    if (route === "/saves") {
      await expectSaveContentToFit(page);
    }
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
  await installStandardController(page);
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
  await pressController(page, [12]);
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
  await expect(menu.getByRole("button", { name: "取消", exact: true })).toBeFocused();
  await holdController(page, []);
  // Opening the menu intentionally requires 120ms of neutral controller input.
  await page.waitForTimeout(200);
  await pressController(page, [15]);
  await pressController(page, [15]);
  await expect(menu.getByRole("button", { name: "退出游戏", exact: true })).toBeFocused();
  await holdController(page, [0]);
  await expect(page).toHaveURL(/\/immersive\?/u);
  await page.waitForTimeout(400);
  await expect(page).toHaveURL(/\/immersive\?/u);
  await holdController(page, []);
  await page.waitForTimeout(200);
  await expect.poll(() => page.evaluate(() => document.hasFocus())).toBe(true);
  expect(new URL(page.url()).searchParams.get("entry")).toBe(game.id);
  await expect(page.getByRole("heading", { name: "等待手柄", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { pressed: true }).filter({ hasText: game.title })).toBeVisible();
  await pressController(page, [1]);
  await expect(page.getByRole("region", { name: "游戏平台", exact: true })).toBeVisible();
  const position = page.locator('[aria-label^="第 "]');
  const originalPosition = await position.getAttribute("aria-label");
  await pressController(page, [15]);
  await expect(position).not.toHaveAttribute("aria-label", originalPosition!);
  await pressController(page, [14]);
  await pressController(page, [0]);
  await expect(page.getByRole("button", { name: "开始游戏", exact: true })).toBeVisible();
  await pressController(page, [0]);
  await expect(page).toHaveURL(/\/play\//u);
  const nextFrame = await nesFrame(page);
  await nextFrame.locator("canvas").click({ position: { x: 10, y: 10 } });
  await page.keyboard.press("m");
  await menu.getByRole("button", { name: "退出游戏", exact: true }).click();
  await expect(page).toHaveURL(/\/immersive\?/u);
});
