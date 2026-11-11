import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";
import type { Schema } from "../lib/api/types";
import { verifyGameDetailLayout } from "./game-detail-layout";

export async function verifyDetailDirectoryLink(page: Page, game: Schema<"Game">) {
  const link = page.locator("a.game-detail-eyebrow");
  await expect(link).toHaveText(game.directoryName);
  await page.screenshot({ path: test.info().outputPath("detail-directory-link.png") });
  await link.click();
  await page.waitForURL(/\/library\?/u);
  expect(new URL(page.url()).searchParams.get("platformInstanceId")).toBe(game.platformInstanceId);
  await expect(page.locator(`a[href="/games/${game.id}"]`).first()).toBeVisible();
  const directory = page.getByRole("combobox", { name: "游戏目录" });
  if (await directory.isVisible()) {
    await expect(directory).toHaveValue(game.platformInstanceId);
  }
  await page.screenshot({ path: test.info().outputPath("library-directory-filter.png") });
  await page.goBack();
  await page.waitForURL(new RegExp(`/games/${game.id}$`, "u"));
  await expect(page.getByRole("heading", { level: 1, name: game.title, exact: true })).toBeVisible();
  await verifyGameDetailLayout(page);
}
