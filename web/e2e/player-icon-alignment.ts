import {expect, type Page} from "@playwright/test";

export async function expectPlayerIconsCentered(page: Page) {
  await expect(page.getByRole("button", {name: "调试信息"})).toHaveCount(0);
  await expect(page.locator("#player-debug-panel")).toHaveCount(0);
  for (const name of ["创建存档"]) {
    const button = page.getByRole("button", {name, exact: true});
    await expect(button).toBeVisible();
    // Read both boxes in one frame while the toolbar reveal animation is running.
    const offset = await button.evaluate(element => {
      const bounds = element.getBoundingClientRect();
      const icon = element.querySelector("svg")!.getBoundingClientRect();
      return {x: Math.abs(icon.x + icon.width / 2 - bounds.x - bounds.width / 2),
        y: Math.abs(icon.y + icon.height / 2 - bounds.y - bounds.height / 2)};
    });
    expect(offset.x, name).toBeLessThan(1);
    expect(offset.y, name).toBeLessThan(1);
  }
}
