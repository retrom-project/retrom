import {expect, type Page} from "@playwright/test";

/** ACC-RUN-002: read-only diagnostics must not block an explicit surface resume. */
export async function verifyPlayerSurfaceResume(page: Page, verifyRuntime?: (paused: boolean) => Promise<void>) {
  const panel = page.getByRole("complementary", {name: "运行调试信息"});
  const overlay = page.getByRole("button", {name: "继续游戏", exact: true});
  await expect(panel).toBeVisible();
  for (const activation of ["background", "pill", "keyboard"] as const) {
    await page.getByRole("button", {name: "暂停", exact: true}).click();
    await expect(page.locator(".player-shell")).toHaveClass(/is-paused/);
    await expect(panel.getByText("暂停", {exact: true})).toBeVisible();
    await verifyRuntime?.(true);
    if (activation === "background") {await overlay.click({position: {x: 100, y: 200}});}
    else if (activation === "pill") {await overlay.getByText("已暂停", {exact: true}).click();}
    else {await overlay.focus(); await page.keyboard.press("Enter");}
    await expect(page.locator(".player-shell")).not.toHaveClass(/is-paused/);
    await expect(overlay).toBeHidden();
    await expect(panel).toBeVisible();
    await expect(panel.getByText("运行中", {exact: true})).toBeVisible();
    await expect(page.locator(".player-toolbar")).toHaveClass(/is-visible/);
    await verifyRuntime?.(false);
  }
}
