import type { Page } from "@playwright/test";

type ControllerWindow = Window & { retromTestButtons?: number[] };

export async function installStandardController(page: Page) {
  await page.addInitScript(() => {
    const host = window.top as ControllerWindow;
    Object.defineProperty(navigator, "getGamepads", {
      configurable: true,
      value: () => {
        let pressed: number[];
        try { pressed = host.retromTestButtons ?? []; } catch { return []; }
        return [{ id: "Browser acceptance standard gamepad", connected: true, mapping: "standard", index: 0,
          timestamp: performance.now(), axes: [0, 0, 0, 0],
          buttons: Array.from({ length: 17 }, (_, index) => ({ pressed: pressed.includes(index), touched: pressed.includes(index), value: pressed.includes(index) ? 1 : 0 })),
        }, null, null, null];
      },
    });
  });
}

export async function holdController(page: Page, buttons: number[]) {
  await page.evaluate((pressed) => { (window as ControllerWindow).retromTestButtons = pressed; }, buttons);
}

export async function pressController(page: Page, buttons: number[]) {
  await holdController(page, buttons);
  await page.waitForTimeout(100);
  await holdController(page, []);
  await page.waitForTimeout(200);
}
