import { expect, type Page } from "@playwright/test";

export async function verifyRuntimePackInstallLayout(page: Page) {
  await page.goto("/admin/bios?tab=rpgmaker");
  const install = page.getByRole("button", { name: "安装运行包", exact: true });
  await expect(install).toBeVisible();
  const layout = await install.evaluate((button) => {
    const text = [...button.childNodes].find((node) => node.nodeType === Node.TEXT_NODE && node.textContent?.trim());
    if (!text) {throw new Error("Missing install label");}
    const range = document.createRange();
    range.selectNodeContents(text);
    const lines = [...range.getClientRects()];
    const bounds = button.getBoundingClientRect();
    const icon = button.querySelector("svg")?.getBoundingClientRect();
    return {
      lineCount: lines.length,
      inside: lines.every((line) => line.left >= bounds.left && line.right <= bounds.right),
      iconWidth: icon?.width ?? 0,
      centerOffset: icon && lines[0] ? Math.abs(icon.y + icon.height / 2 - lines[0].y - lines[0].height / 2) : null,
    };
  });
  expect(layout.lineCount).toBe(1);
  expect(layout.inside).toBe(true);
  expect(layout.iconWidth).toBe(18);
  expect(layout.centerOffset).not.toBeNull();
  expect(layout.centerOffset!).toBeLessThanOrEqual(2);
  await install.click();
  const dialog = page.getByRole("dialog", { name: "安装 RPG Maker 运行包" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(install).toBeFocused();
}
