import {expect, type Page} from "@playwright/test";

export async function expectPlayerIconsCentered(page: Page) {
  await expect(page.getByRole("button", {name: "调试信息"})).toHaveCount(0);
  await expect(page.locator("#player-debug-panel")).toHaveCount(0);
  await expectPlayerBackButton(page);
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

export async function expectPlayerBackButton(page: Page) {
  const button = page.getByRole("button", {name: "返回并退出游戏", exact: true});
  await expect(button).toBeVisible();
  const layout = await button.evaluate(element => {
    const bounds = element.getBoundingClientRect();
    const icon = element.querySelector("svg")!.getBoundingClientRect();
    const style = getComputedStyle(element);
    const title = element.parentElement!.querySelector(".player-game-meta")!.getBoundingClientRect();
    return {width: bounds.width, height: bounds.height,
      background: style.backgroundColor, border: style.borderTopColor,
      x: Math.abs(icon.x + icon.width / 2 - bounds.x - bounds.width / 2),
      y: Math.abs(icon.y + icon.height / 2 - bounds.y - bounds.height / 2),
      title: Math.abs(icon.y + icon.height / 2 - title.y - title.height / 2)};
  });
  expect(layout.background).toBe("rgba(0, 0, 0, 0)");
  expect(layout.border).toBe("rgba(0, 0, 0, 0)");
  expect(layout.width).toBe(44);
  expect(layout.height).toBe(44);
  expect(layout.x).toBeLessThan(1);
  expect(layout.y).toBeLessThan(1);
  expect(layout.title).toBeLessThan(1);
}
