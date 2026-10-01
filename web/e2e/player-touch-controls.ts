import {expect, type Page} from "@playwright/test";

export async function expectCircularDirectionInput(page: Page) {
  const pad = page.frameLocator("iframe.player-frame").locator(".ejs_dpad_main");
  await expect(pad).toBeVisible();
  await expect(pad).toHaveCSS("border-radius", "50%");
  await expect(pad.locator(".ejs_dpad_vertical")).toBeHidden();
  const observations = await pad.evaluate((element) => {
    const frameWindow = element.ownerDocument.defaultView!;
    const manager = (frameWindow as Window & {EJS_emulator?: {gameManager: {
      simulateInput: (player: number, button: number, value: number) => void;
    }}}).EJS_emulator?.gameManager;
    if (!manager) {throw new Error("EmulatorJS input manager unavailable");}
    const original = manager.simulateInput;
    const events: number[][] = [];
    manager.simulateInput = function (player, button, value) {
      events.push([player, button, value]); original.call(this, player, button, value);
    };
    const bounds = element.getBoundingClientRect();
    const centerX = bounds.left + bounds.width / 2, centerY = bounds.top + bounds.height / 2;
    const dispatch = (type: string, x: number, y: number) => {
      const points = type === "touchend" || type === "touchcancel" ? [] : [new Touch({
        identifier: 1, target: element, clientX: centerX + x, clientY: centerY + y,
      })];
      element.dispatchEvent(new TouchEvent(type, {bubbles: true, cancelable: true, targetTouches: points, touches: points}));
      return events.splice(0);
    };
    try {
      const directions = [[0, -30], [0, 30], [-30, 0], [30, 0], [30, -30]].map(([x, y]) => {
        const down = dispatch("touchstart", x, y);
        const move = dispatch("touchmove", x, y);
        const thumb = getComputedStyle(element, "::after").transform;
        const up = dispatch("touchend", x, y);
        return {down, move, up, thumb};
      });
      dispatch("touchstart", 30, -30);
      const cancel = dispatch("touchcancel", 0, 0);
      return {directions, cancel, centered: getComputedStyle(element, "::after").transform,
        baseSize: getComputedStyle(element, "::before").width, thumbSize: getComputedStyle(element, "::after").width};
    } finally {manager.simulateInput = original;}
  });
  const released = [4, 5, 6, 7].map(button => [0, button, 0]);
  for (const [index, buttons] of [[4], [5], [6], [7], [4, 7]].entries()) {
    const expected = [4, 5, 6, 7].map(button => [0, button, Number(buttons.includes(button))]);
    expect(observations.directions[index].down).toEqual(expected);
    expect(observations.directions[index].move).toEqual(expected);
    expect(observations.directions[index].up).toEqual(released);
    expect(observations.directions[index].thumb).not.toBe("matrix(1, 0, 0, 1, 0, 0)");
  }
  expect(observations.cancel).toEqual(released);
  expect(observations.centered).toBe("matrix(1, 0, 0, 1, 0, 0)");
  expect(observations.baseSize).toBe("100px");
  expect(observations.thumbSize).toBe("50px");
}
