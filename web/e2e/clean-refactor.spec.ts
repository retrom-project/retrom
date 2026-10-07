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
    page.getByRole("button", { name: "保存", exact: true }),
  ).toBeEnabled({ timeout: 60_000 });
  await revealPlayerControls(page);
  await page.getByRole("button", { name: "全屏", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "退出全屏", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "退出全屏", exact: true }).click();
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
  await page.getByRole("button", { name: "保存", exact: true }).click();
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
    page.getByRole("button", { name: "保存", exact: true }),
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
