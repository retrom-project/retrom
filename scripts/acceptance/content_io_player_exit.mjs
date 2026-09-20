import assert from "node:assert/strict";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

export async function performContentIOPlayerExit(page, base, launch, action) {
  const finished = page.waitForResponse(response => response.request().method() === "POST" &&
    new URL(response.url()).pathname === `/runtime/launches/${launch.launchId}/finish`, {timeout: 30000})
    .then(response => ({response}), error => ({error}));
  const result = await action(), observed = await finished;
  if (observed.error) throw observed.error;
  assert.equal(observed.response.status(), 200);
  await page.waitForURL(`${base}${launch.returnTo}`);
  assert.equal(page.frames().length, 1); assert.equal(page.workers().length, 0);
  return result;
}
export async function exitContentIOPlayer(page, base, launch, semantics = "INSTANT") {
  assert.ok(["INSTANT", "GAME_SAVE"].includes(semantics));
  return performContentIOPlayerExit(page, base, launch, async () => {
    await revealPreviewToolbar(page);
    await page.getByRole("button", {name: "返回并退出游戏", exact: true}).click();
    const name = semantics === "GAME_SAVE" ? /^(直接退出|继续退出)$/u : "退出游戏";
    await page.getByRole("alertdialog", {name: "退出游戏？"}).getByRole("button", {name, exact: true}).click();
  });
}
