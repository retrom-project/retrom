import { mkdirSync } from "node:fs";
import path from "node:path";
import { expect, type Locator, type Page, type TestInfo } from "@playwright/test";

export function evidencePath(testInfo: TestInfo, name: string) {
  const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!caseDirectory) {return testInfo.outputPath(name);}
  const screenshots = path.join(caseDirectory, "screenshots");
  mkdirSync(screenshots, { recursive: true });
  return path.join(screenshots, `${testInfo.project.name}-${name}`);
}

export function pngDimensions(contents: Buffer) {
  expect(contents.subarray(1, 4).toString("ascii")).toBe("PNG");
  return { width: contents.readUInt32BE(16), height: contents.readUInt32BE(20) };
}

export async function noPageOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
}

export async function expectNoTextArrowsInInteractiveControls(page: Page) {
  const labels = await page.locator("a, button").evaluateAll((elements) => elements
    .filter((element) => {
      const style = getComputedStyle(element);
      const rectangle = element.getBoundingClientRect();
      return style.display !== "none" && style.visibility !== "hidden" && rectangle.width > 0 && rectangle.height > 0;
    })
    .map((element) => element.textContent?.trim() ?? "")
    .filter((label) => label.includes("→")));
  expect(labels, "可见按钮和链接文案不应追加箭头字符").toEqual([]);
}

export async function expectHomeCoverRatios(page: Page) {
  const groups = [
    ["最近玩的游戏", '.home-featured-cover'],
    ["最近游玩", '[data-home-layer="2"] .home-recent-cover'],
    ["最新添加", '[data-home-layer="3"] .home-recent-cover'],
  ] as const;
  let measuredCoverCount = 0;
  for (const [label, selector] of groups) {
    const sizes = await page.locator(selector).evaluateAll((elements) => elements.map((element) => {
      const rectangle = element.getBoundingClientRect();
      const existingImage = element.querySelector("img");
      const image = existingImage ?? document.createElement("img");
      if (!existingImage) {element.append(image);}
      const objectFit = getComputedStyle(image).objectFit;
      if (!existingImage) {image.remove();}
      return { width: rectangle.width, height: rectangle.height, objectFit };
    }));
    measuredCoverCount += sizes.length;
    for (const size of sizes) {
      expect(size.height, `${label}封面高度应大于零`).toBeGreaterThan(0);
      expect(Math.abs(size.width / size.height - 5 / 7), `${label}封面宽高比应为 5:7`).toBeLessThanOrEqual(0.01);
      expect(size.objectFit, `${label}封面图片应等比裁切填满容器`).toBe("cover");
    }
  }
  expect(measuredCoverCount, "首页验收夹具应包含至少一张可测量封面").toBeGreaterThan(0);
}

async function screenshotBrightRatio(page: Page, screenshot: Buffer | null) {
  if (!screenshot?.length) {return 0;}
  return page.evaluate(async (encoded) => {
    const binary = atob(encoded);
    const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
    const bitmap = await createImageBitmap(new Blob([bytes], { type: "image/png" }));
    const sample = document.createElement("canvas");
    sample.width = 64; sample.height = 64;
    const context = sample.getContext("2d", { alpha: false });
    if (!context) {return 0;}
    context.drawImage(bitmap, 0, 0, sample.width, sample.height);
    bitmap.close();
    const pixels = context.getImageData(0, 0, sample.width, sample.height).data;
    let brightPixels = 0;
    for (let index = 0; index < pixels.length; index += 4) {
      if ((pixels[index] + pixels[index + 1] + pixels[index + 2]) / 3 > 8) {brightPixels += 1;}
    }
    return brightPixels / (pixels.length / 4);
  }, screenshot.toString("base64"));
}

export async function locatorBrightRatio(page: Page, locator: Locator) {
  return screenshotBrightRatio(page, await locator.screenshot({ timeout: 1_000 }).catch(() => null));
}

export async function currentEmulatorBrightRatio(page: Page) {
  const canvas = page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas");
  return locatorBrightRatio(page, canvas);
}

export type HorizontalGaps = { left: number; right: number };

export async function pageCanvasGaps(page: Page, targetSelector = ".page-header"): Promise<HorizontalGaps> {
  const measurement = await page.evaluate((selector) => {
    const appBody = document.querySelector<HTMLElement>(".app-body");
    const content = document.querySelector<HTMLElement>(".content");
    const target = document.querySelector<HTMLElement>(selector);
    if (!appBody || !content || !target) {return null;}
    const appBodyRect = appBody.getBoundingClientRect();
    const contentRect = content.getBoundingClientRect();
    const targetRect = target.getBoundingClientRect();
    const contentStyle = getComputedStyle(content);
    const contentLeft = contentRect.left + Number.parseFloat(contentStyle.paddingLeft);
    const contentRight = contentRect.right - Number.parseFloat(contentStyle.paddingRight);
    return { canvasWidth: contentRight - contentLeft, targetLeftDelta: targetRect.left - contentLeft, targetRightDelta: contentRight - targetRect.right, left: targetRect.left - appBodyRect.left, right: appBodyRect.right - targetRect.right };
  }, targetSelector);
  expect(measurement).not.toBeNull();
  if (!measurement) {throw new Error("ACCEPTANCE_LAYOUT_MEASUREMENT_UNAVAILABLE");}
  expect(Math.abs(measurement.targetLeftDelta)).toBeLessThanOrEqual(1);
  expect(Math.abs(measurement.targetRightDelta)).toBeLessThanOrEqual(1);
  expect(measurement.canvasWidth).toBeLessThanOrEqual(2321);
  return { left: measurement.left, right: measurement.right };
}
