import assert from "node:assert/strict";
import {join} from "node:path";

export async function verifyGamepadCursor(page, canvas, {defaultEnabled, directory, protocol = "mouse"}) {
  await canvas.evaluate(element => {
    const events = [];
    element.ownerDocument.defaultView.__cursorEvents = events;
    for (const type of ["keydown", "mousedown", "mouseup", "mousemove", "click", "pointerdown", "pointerup", "pointermove"]) {
      element.addEventListener(type, event => events.push({type, buttons: event.buttons, x: event.clientX, y: event.clientY}));
    }
  });
  await openMenu(page);
  const control = page.getByRole("menuitemcheckbox", {name: /手柄光标/u});
  assert.equal(await control.getAttribute("aria-checked"), String(defaultEnabled));
  if (!defaultEnabled) {await control.click();}
  await page.screenshot({path: join(directory, "cursor-standard-menu.png")});
  await resume(page);
  await input(page, "axis", 0, 0.6);
  await input(page, "axis", 0, 0);
  // A Ruffle canvas is inside a shadow root; query the owning document directly.
  assert.equal(await canvas.evaluate(element => {
    const nodes = element.ownerDocument.querySelectorAll("[data-gamepad-cursor]");
    return nodes.length === 1 && !nodes[0].hidden;
  }), true, "CURSOR_SINGLE_VISIBLE");
  await input(page, "button", 0, true);
  await input(page, "axis", 0, 0.6);
  await input(page, "axis", 0, 0);
  await input(page, "button", 0, false);
  const events = await canvas.evaluate(element => element.ownerDocument.defaultView.__cursorEvents);
  const move = protocol === "pointer" ? "pointermove" : "mousemove";
  const down = protocol === "pointer" ? "pointerdown" : "mousedown";
  assert.ok(events.some(event => event.type === down), "CURSOR_CLICK_DELIVERED");
  assert.ok(events.some(event => event.type === move && event.buttons === 1), "CURSOR_DRAG_BUTTONS");
  assert.equal(events.filter(event => event.type === "keydown").length, 0, "CURSOR_NO_KEYBOARD_DUPLICATION");
  await openMenu(page);
  await control.click();
  await resume(page);
  assert.equal(await canvas.evaluate(element => element.ownerDocument.querySelector("[data-gamepad-cursor]").hidden), true);
  const before = await canvas.evaluate(element => element.ownerDocument.defaultView.__cursorEvents.length);
  await input(page, "axis", 0, 0.8); await input(page, "axis", 0, 0);
  const disabled = await canvas.evaluate((element, from) => element.ownerDocument.defaultView.__cursorEvents.slice(from), before);
  assert.equal(disabled.filter(event => event.type === move).length, 0, "CURSOR_DISABLED_STOPS_MOVEMENT");
  if (defaultEnabled) {await openMenu(page); await control.click(); await resume(page);}
  return {singleCursor: true, dragging: true, exclusiveMapping: true, toggle: true};
}

async function openMenu(page) {
  await page.mouse.move(20, 8);
  await page.getByRole("button", {name: "更多操作", exact: true}).click();
  await page.getByRole("menuitemcheckbox", {name: /手柄光标/u}).waitFor();
}

async function resume(page) {
  await page.getByRole("button", {name: "更多操作", exact: true}).click();
  const button = page.getByRole("button", {name: "继续游戏", exact: true});
  if (await button.isVisible()) {await button.click();}
  await page.waitForFunction(() => window.__RETROM_E2E_RUNTIME_V1__?.getState() === "RUNNING");
  await page.mouse.move(20, 200);
  await page.waitForTimeout(180);
}

async function input(page, kind, index, value) {
  for (const frame of page.frames()) {
    await frame.evaluate(({kind, index, value}) => globalThis.__retromTestGamepad?.[kind](index, value), {kind, index, value});
  }
  await page.waitForTimeout(100);
}
