import { expect, type Page } from "@playwright/test";
import { test } from "./clean-refactor-fixtures";
import type { Schema } from "../lib/api/types";

export async function verifyLibraryCollectionDialog(
  page: Page,
  game: Schema<"Game">,
  folder: Schema<"FavoriteFolder">,
) {
  await page.goto("/library");
  const trigger = page.getByRole("button", {
    name: `整理${game.title}的收藏夹`, exact: true,
  });
  await trigger.click();
  const dialog = page.getByRole("alertdialog", { name: "整理收藏夹", exact: true });
  const cancel = dialog.getByRole("button", { name: "取消", exact: true });
  const save = dialog.getByRole("button", { name: "保存分类", exact: true });
  await expect(cancel).toBeFocused();
  // Test the real card's hover/stacking context and the fixed phone navigation.
  // Visibility alone does not detect an element painted over the save button.
  for (const action of [cancel, save]) {
    await expect(action).toBeInViewport({ ratio: 1 });
    await action.click({ trial: true, timeout: 5000 });
  }
  const backgroundCovered = await page.locator(".library-game-card .favorite-heart").evaluateAll(
    (hearts) => hearts.every((heart) => {
      const rect = heart.getBoundingClientRect();
      const x = rect.x + rect.width / 2;
      const y = rect.y + rect.height / 2;
      if (x < 0 || x >= innerWidth || y < 0 || y >= innerHeight) { return true; }
      const target = document.elementFromPoint(x, y);
      return !!target?.closest(".dialog-backdrop");
    }),
  );
  expect(backgroundCovered).toBe(true);
  await page.screenshot({ path: test.info().outputPath("library-collection-dialog.png") });
  const viewport = page.viewportSize();
  if (!viewport) { throw new Error("Collection dialog requires an explicit viewport"); }
  await page.setViewportSize({ ...viewport, height: 320 });
  for (const action of [cancel, save]) {
    await expect(action).toBeInViewport({ ratio: 1 });
    await action.click({ trial: true, timeout: 5000 });
  }
  await page.setViewportSize(viewport);
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await trigger.click();
  await dialog.getByLabel(folder.name, { exact: true }).check();
  await save.click();
  await expect(dialog).toHaveCount(0);
}
