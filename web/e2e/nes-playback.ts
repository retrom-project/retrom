import { expect } from "@playwright/test";
import type { Frame, Page } from "@playwright/test";

type EmulatorWindow = Window & {
  EJS_emulator?: {
    gameManager?: { getState: () => Uint8Array; getFrameNum: () => number };
  };
};

export async function nesFrame(page: Page) {
  await ensurePlayerLandscape(page);
  const surface = page.locator(".player-runtime-mount iframe");
  await expect(surface).toBeVisible({ timeout: 60_000 });
  const frame = await (await surface.elementHandle())?.contentFrame();
  if (!frame) {
    throw new Error("The NES runtime frame is missing.");
  }
  await expect(frame.locator("canvas").first()).toBeVisible();
  await expect
    .poll(() =>
      frame.evaluate(() => {
        const emulator = (window as EmulatorWindow).EJS_emulator;
        return emulator?.gameManager?.getFrameNum() ?? 0;
      }),
    )
    .toBeGreaterThan(0);
  return frame;
}

export async function ensurePlayerLandscape(page: Page) {
  if (await page.evaluate(() => /Android.*Mobile|iPhone|iPod/iu.test(navigator.userAgent) && !screen.orientation.type.startsWith("landscape"))) {
    await expect(page.getByRole("region", {name: "横屏游玩提示"})).toBeVisible();
    await expect(page.locator(".player-runtime-mount iframe")).toHaveCount(0);
    await page.setViewportSize({width: 844, height: 390});
    const protocol = await page.context().newCDPSession(page);
    await protocol.send("Emulation.setDeviceMetricsOverride", {width: 844, height: 390, mobile: true,
      deviceScaleFactor: 1, screenWidth: 844, screenHeight: 390, screenOrientation: {type: "landscapePrimary", angle: 90}});
    await expect(page.getByRole("region", {name: "横屏游玩提示"})).toHaveCount(0);
  }
}

// FCEUmm native state serializes its 2 KiB CPU RAM under RAM\0 + u32LE size.
// The project-owned NES fixture increments zero-page $03 only for P1 input.
export async function nesPlayerCounter(frame: Frame) {
  return frame.evaluate(() => {
    const bytes = (
      window as EmulatorWindow
    ).EJS_emulator?.gameManager?.getState();
    if (!bytes) {
      throw new Error("The actual FCEUmm checkpoint is unavailable.");
    }
    const tag = [82, 65, 77, 0, 0, 8, 0, 0];
    const offsets: number[] = [];
    for (
      let offset = 0;
      offset + tag.length + 0x800 <= bytes.length;
      offset++
    ) {
      if (tag.every((value, index) => bytes[offset + index] === value)) {
        offsets.push(offset);
      }
    }
    if (offsets.length !== 1) {
      throw new Error(
        "The FCEUmm checkpoint must contain one complete RAM block.",
      );
    }
    return bytes[offsets[0] + tag.length + 3];
  });
}

export async function sendNesInput(page: Page, frame: Frame) {
  const before = await nesPlayerCounter(frame);
  await frame.locator("canvas").first().click();
  await page.keyboard.down("d");
  try {
    await expect.poll(() => nesPlayerCounter(frame)).not.toBe(before);
  } finally {
    await page.keyboard.up("d");
  }
  const after = await nesPlayerCounter(frame);
  expect(after).toBeGreaterThan(0);
  return after;
}
