import type { Page } from "@playwright/test";

type ControllerWindow = Window & {
  retromTestButtons?: number[];
  retromTestNeutralReads?: { first: number; last: number };
};

export async function installStandardController(page: Page) {
  await page.addInitScript(() => {
    const host = window.top as ControllerWindow;
    Object.defineProperty(navigator, "getGamepads", {
      configurable: true,
      value: () => {
        let pressed: number[];
        try { pressed = host.retromTestButtons ?? []; } catch { return []; }
        if (window === host && pressed.length === 0) {
          const now = Number(document.timeline.currentTime ?? performance.now());
          host.retromTestNeutralReads ??= { first: now, last: now };
          host.retromTestNeutralReads.last = now;
        }
        return [{ id: "Browser acceptance standard gamepad", connected: true, mapping: "standard", index: 0,
          timestamp: performance.now(), axes: [0, 0, 0, 0],
          buttons: Array.from({ length: 17 }, (_, index) => ({ pressed: pressed.includes(index), touched: pressed.includes(index), value: pressed.includes(index) ? 1 : 0 })),
        }, null, null, null];
      },
    });
  });
}

export async function holdController(page: Page, buttons: number[]) {
  await page.evaluate((pressed) => {
    const host = window as ControllerWindow;
    host.retromTestButtons = pressed;
    host.retromTestNeutralReads = undefined;
  }, buttons);
}

export async function settleController(page: Page) {
  await holdController(page, []);
  // The menu's neutral guard uses sampled animation-frame time. A Node timer
  // can expire before Chromium has delivered enough frames to arm that guard.
  await page.waitForFunction(() => {
    const reads = (window as ControllerWindow).retromTestNeutralReads;
    return reads !== undefined && reads.last - reads.first >= 200;
  }, undefined, { timeout: 5_000 });
}

export async function pressController(page: Page, buttons: number[]) {
  await holdController(page, buttons);
  await page.waitForTimeout(100);
  await holdController(page, []);
  await page.waitForTimeout(200);
}
