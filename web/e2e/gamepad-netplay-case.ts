import { expect, type Page } from "@playwright/test";
import {
  claimGamepad,
  neutralGamepad,
  pressGamepadButton,
  pressGamepadDirection,
  setSyntheticGamepads,
  standardPad,
} from "./gamepad-support";
import { diagnosticEvents } from "./netplay-checkpoints";

type ControllerSession = Readonly<{
  hostPage: Page;
  guestPage: Page;
  roomId: string;
  consoleErrors: readonly string[];
}>;

export async function verifyGamepadNetplayOwnership(
  session: ControllerSession,
  waitForFrame: (page: Page, frame: number) => Promise<void>,
) {
  await Promise.all([claimGamepad(session.hostPage), claimGamepad(session.guestPage)]);

  await pressGamepadButton(session.hostPage, 16);
  const hostMenu = session.hostPage.getByRole("dialog", { name: "Retrom 菜单" });
  await expect(hostMenu).toBeVisible();
  await expect(hostMenu).toContainText("联机 P1 · 暂停由房主控制");
  const hostPauseOverlay = session.hostPage.locator(".player-pause-overlay");
  const guestPauseOverlay = session.guestPage.locator(".player-pause-overlay");
  await Promise.all([
    expect(hostPauseOverlay).toHaveClass(/is-visible/, { timeout: 30_000 }),
    expect(guestPauseOverlay).toHaveClass(/is-visible/, { timeout: 30_000 }),
  ]);
  await setSyntheticGamepads(session.hostPage, [standardPad(0, [2])]);
  await session.hostPage.waitForTimeout(40);
  expect(await session.hostPage.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("body")
    .evaluate(() => navigator.getGamepads()[0]?.buttons[2]?.value ?? -1)).toBe(0);
  await neutralGamepad(session.hostPage);
  const resumeResponse = session.hostPage.waitForResponse((response) =>
    response.request().method() === "POST" && /\/sessions\/[^/]+\/resume$/.test(response.url()));
  await pressGamepadButton(session.hostPage, 1);
  await neutralGamepad(session.hostPage);
  await expect(hostMenu).toBeHidden();
  expect((await resumeResponse).status()).toBe(202);
  await expect(hostPauseOverlay).not.toHaveClass(/is-visible/, { timeout: 30_000 });

  const beforeP2Menu = Math.max(...(await diagnosticEvents(session.hostPage))
    .filter((event) => event.kind === "canonical").map((event) => event.frame ?? -1));
  await pressGamepadButton(session.guestPage, 16);
  const guestMenu = session.guestPage.getByRole("dialog", { name: "Retrom 菜单" });
  await expect(guestMenu).toBeVisible();
  await expect(guestMenu).toContainText("联机 P2 · 游戏仍由 P1 继续");
  await expect(guestMenu.getByRole("menuitem", { name: /创建手动存档|高级设置/ })).toHaveCount(0);
  await waitForFrame(session.hostPage, beforeP2Menu + 30);
  await pressGamepadButton(session.guestPage, 1);
  await neutralGamepad(session.guestPage);
  await expect(guestMenu).toBeHidden();

  await setSyntheticGamepads(session.hostPage, [standardPad(0, [16])]);
  await session.hostPage.waitForTimeout(1_250);
  await neutralGamepad(session.hostPage);
  const exit = session.hostPage.getByRole("alertdialog", { name: /退出《.+》？/ });
  await expect(exit).toBeVisible();
  await expect(exit.getByRole("button", { name: "继续联机" })).toBeFocused();
  await pressGamepadDirection(session.hostPage, "right");
  await expect(exit.getByRole("button", { name: "结束联机" })).toBeFocused();
  await setSyntheticGamepads(session.hostPage, [standardPad(0, [0])]);
  await Promise.all([
    expect(session.hostPage).toHaveURL(new RegExp(`/netplay/rooms/${session.roomId}$`), { timeout: 30_000 }),
    expect(session.guestPage).toHaveURL(new RegExp(`/netplay/rooms/${session.roomId}$`), { timeout: 30_000 }),
  ]);
  expect(session.consoleErrors).toEqual([]);
}
