import { expect } from "@playwright/test";
import type { Page, TestInfo } from "@playwright/test";
import type { Schema } from "../lib/api/types";
import { request } from "./clean-refactor-support";

async function importGames(page: Page, source: string) {
  const directories = await request<Schema<"DirectoryList">>(page, "/api/v1/admin/platform-instances");
  const directory = directories.items.find(item => item.slug === "browser-rpg-entry") ??
    await request<Schema<"Directory">>(page, "/api/v1/admin/platform-instances", {
      name: "Browser RPG entry", slug: "browser-rpg-entry", description: "Public MIT MV fixture",
      platformId: "rpgmaker", defaultCoreId: "rpgmaker", coreIds: ["rpgmaker"], enabled: true,
    });
  const path = `${source}/import`;
  const inspection = await request<{ items: Schema<"SourceEntry">[] }>(page,
    "/api/v1/admin/game-scans/inspect", { path, format: "pegasus" });
  const scan = await request<Schema<"ScanProgress">>(page, "/api/v1/admin/game-scans", {
    path, format: "pegasus",
    mappings: inspection.items.map(item => ({ sourceKey: item.key, platformInstanceId: directory.id, tagIds: [] })),
  });
  await expect.poll(async () => {
    const scans = await request<Schema<"ScanList">>(page, "/api/v1/admin/scans");
    const progress = scans.items.find(item => item.id === scan.id);
    expect(progress?.failedCount).toBe(0);
    return progress?.status;
  }).toBe("completed");
  const reviews = await request<Schema<"GamePage">>(page,
    `/api/v1/admin/reviews?platformInstanceId=${directory.id}`);
  expect(reviews.items).toHaveLength(2);
  return reviews.items;
}

async function expectFieldLayout(page: Page) {
  const entry = page.getByRole("textbox", { name: "入口文件", exact: true })
    .or(page.getByRole("combobox", { name: "入口文件", exact: true }));
  const engine = page.getByRole("combobox", { name: "RPG Maker 版本", exact: true });
  const [left, right] = await Promise.all([entry.boundingBox(), engine.boundingBox()]);
  expect(left).not.toBeNull();
  expect(right).not.toBeNull();
  // Shared mobile controls use a larger font; compare against their natural box,
  // so an adjacent validation message cannot stretch either control.
  const naturalHeight = await entry.evaluate(element => {
    const style = getComputedStyle(element);
    const content = parseFloat(style.lineHeight) + parseFloat(style.paddingTop) + parseFloat(style.paddingBottom)
      + parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth);
    return Math.max(parseFloat(style.minHeight), content);
  });
  expect(left!.height).toBe(naturalHeight);
  expect(right!.height).toBe(naturalHeight);
  if (page.viewportSize()!.width >= 768) {
    expect(Math.abs(left!.y - right!.y)).toBeLessThan(1);
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
}

export async function verifyRPGEntrySelection(page: Page, info: TestInfo) {
  const source = process.env.RETROM_BROWSER_RPG_SOURCE_PATH;
  if (!source) { throw new Error("The public RPG entry fixture path is required."); }
  const games = await importGames(page, source);
  const single = games.find(game => game.title === "Browser RPG single")!;
  const multiple = games.find(game => game.title === "Browser RPG multiple")!;
  const detailPath = `/api/v1/admin/reviews/${multiple.id}`;
  try {
    await page.goto(`/admin/reviews/${single.id}`);
    const input = page.getByRole("textbox", { name: "入口文件", exact: true });
    await expect(input).toHaveValue("index.html");
    await expect(input).toHaveAttribute("readonly", "");
    await expect(page.getByText("已识别唯一入口。", { exact: true })).toHaveCount(0);
    await input.scrollIntoViewIfNeeded();
    await expectFieldLayout(page);
    await page.screenshot({ path: info.outputPath("rpg-entry-single.png") });

    // A multiline validation message must not stretch the adjacent version selector.
    await page.getByRole("combobox", { name: "RPG Maker 版本", exact: true }).selectOption("RPGMZ");
    await expect(page.getByText("未找到可用入口，请检查游戏文件和 RPG Maker 版本。")).toBeVisible();
    await expectFieldLayout(page);
    await page.screenshot({ path: info.outputPath("rpg-entry-invalid.png") });

    await page.goto(`/admin/reviews/${multiple.id}`);
    const select = page.getByRole("combobox", { name: "入口文件", exact: true });
    await expect(select).toBeEnabled();
    await expect(select.locator("option")).toHaveText(["请选择入口", "index.html", "www/index.html"]);
    await expect(page.getByRole("textbox", { name: "入口文件", exact: true })).toHaveCount(0);
    await select.selectOption("www/index.html");
    await expectFieldLayout(page);
    await select.focus();
    await page.screenshot({ path: info.outputPath("rpg-entry-multiple.png") });
    const saved = page.waitForResponse(response =>
      new URL(response.url()).pathname === detailPath && response.request().method() === "PATCH");
    await page.getByRole("button", { name: "保存更改", exact: true }).click();
    expect((await saved).status()).toBe(200);
    await page.reload();
    await expect(select).toHaveValue("www/index.html");
  } finally {
    for (const game of games) {
      const path = `/api/v1/admin/reviews/${game.id}`;
      const detail = await request<Schema<"GameDetail">>(page, path);
      await request(page, `${path}/discard`, { version: detail.game.version });
    }
  }
}
