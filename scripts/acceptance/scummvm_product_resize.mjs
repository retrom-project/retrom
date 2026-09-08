import assert from "node:assert/strict";
import {writeFileSync} from "node:fs";
import {join} from "node:path";
import {expect} from "../../web/node_modules/@playwright/test/index.mjs";
import {revealScummvmToolbar} from "./scummvm_product_controls.mjs";

export async function resizePausedScummvm(page, directory) {
  await revealScummvmToolbar(page);
  await page.getByRole("button", {name: "暂停", exact: true}).click();
  await expect.poll(() => page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getState())).toBe("PAUSED");
  const canvas = page.frameLocator("iframe").locator("canvas");
  const evidence = [];
  for (const [width, height] of [[1920, 1080], [900, 600], [1440, 1000]]) {
    await page.setViewportSize({width, height});
    await expect.poll(() => canvas.evaluate((element) => {
      const gl = element.getContext("webgl2") ?? element.getContext("webgl");
      return gl && JSON.stringify([...gl.getParameter(gl.VIEWPORT)]) === JSON.stringify([0, 0, element.width, element.height]);
    }), {message: "SCUMMVM_PAUSED_VIEWPORT_STALE"}).toBe(true);
    // Read after browser presentation, not synchronously inside a native draw.
    await page.waitForTimeout(100);
    const url = await canvas.evaluate((element) => element.toDataURL("image/png"));
    writeFileSync(join(directory, `paused-resize-${width}-${height}.png`), Buffer.from(url.split(",")[1], "base64"));
    const frame = await readableScummvmImage(page, url);
    assert.equal(await page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getState()), "PAUSED");
    evidence.push({viewport: {width, height}, frame});
  }
  return evidence;
}

export async function readableScummvmImage(page, url) {
  let bytes;
  if (url.startsWith("data:image/png;base64,")) {bytes = Buffer.from(url.split(",")[1], "base64");}
  else {
    const response = await page.request.get(url);
    assert.equal(response.status(), 200);
    bytes = await response.body();
  }
  const stats = await page.evaluate(async (source) => {
    const image = await createImageBitmap(new Blob([Uint8Array.from(source)]));
    const canvas = document.createElement("canvas");
    canvas.width = image.width; canvas.height = image.height;
    const context = canvas.getContext("2d");
    context.drawImage(image, 0, 0); image.close();
    const {data, width, height} = context.getImageData(0, 0, canvas.width, canvas.height);
    let left = width, right = 0, top = height, bottom = 0, pixels = 0;
    for (let y = 0; y < height; y++) {
      for (let x = 0; x < width; x++) {
        const i = (y * width + x) * 4;
        if (data[i + 3] > 0 && Math.max(data[i], data[i + 1], data[i + 2]) > 24) {
          pixels++; left = Math.min(left, x); right = Math.max(right, x + 1);
          top = Math.min(top, y); bottom = Math.max(bottom, y + 1);
        }
      }
    }
    return {width, height, left, right, top, bottom, pixels};
  }, [...bytes]);
  assert(stats.pixels > stats.width * stats.height / 5, "SCUMMVM_SCREENSHOT_EMPTY_OR_SHRUNK");
  assert(Math.abs(stats.left - (stats.width - stats.right)) < stats.width / 20, "SCUMMVM_FRAME_SHIFTED_HORIZONTALLY");
  assert(Math.abs(stats.top - (stats.height - stats.bottom)) < stats.height / 6, "SCUMMVM_FRAME_SHIFTED_VERTICALLY");
  return stats;
}
