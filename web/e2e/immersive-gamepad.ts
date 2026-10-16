import type { Page } from "@playwright/test";

type ControllerWindow = Window & {
  retromTestButtons?: number[];
  retromTestPressedRead?: boolean;
  retromTestReleaseOnRead?: boolean;
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
        if (window === host && pressed.length > 0) {
          host.retromTestPressedRead = true;
          if (host.retromTestReleaseOnRead) { host.retromTestButtons = []; }
        }
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

export async function holdController(page: Page, buttons: number[], releaseOnRead = false) {
  await page.evaluate(({ pressed, releaseOnRead }) => {
    const host = window as ControllerWindow;
    host.retromTestButtons = pressed;
    host.retromTestNeutralReads = undefined;
    host.retromTestPressedRead = false;
    host.retromTestReleaseOnRead = releaseOnRead;
  }, { pressed: buttons, releaseOnRead });
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
  // Deliver one sampled press. Trace snapshots can otherwise stretch a Node
  // timer past the repeat delay; release inside the browser, not over the wire.
  await holdController(page, buttons, true);
  await page.waitForFunction(() => (window as ControllerWindow).retromTestPressedRead === true, undefined, { timeout: 5_000 });
  await settleController(page);
}
