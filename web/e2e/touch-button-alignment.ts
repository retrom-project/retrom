import { expect } from "@playwright/test";
import type { Frame } from "@playwright/test";

export async function verifyTouchButtonAlignment(frame: Frame) {
  const buttons = frame.locator(".ejs_virtualGamepad_button");
  await expect(buttons.first()).toBeVisible();
  const metrics = await buttons.evaluateAll((elements) => elements.map((button) => {
    const bounds = button.getBoundingClientRect();
    const range = document.createRange();
    range.selectNodeContents(button);
    const text = range.getBoundingClientRect();
    const style = getComputedStyle(button);
    const context = document.createElement("canvas").getContext("2d")!;
    context.font = style.font;
    const ink = context.measureText(button.textContent ?? "");
    // Range gives the font box; Canvas metrics locate the visible glyphs within it.
    const baseline = text.top + ink.fontBoundingBoxAscent;
    return {
      label: button.textContent,
      width: bounds.width,
      height: bounds.height,
      horizontal: text.left + text.width / 2 - bounds.left - bounds.width / 2,
      vertical: baseline + (ink.actualBoundingBoxDescent - ink.actualBoundingBoxAscent) / 2 - bounds.top - bounds.height / 2,
    };
  }));
  expect(metrics.map(({ label }) => label)).toEqual(expect.arrayContaining(["A", "B", "开始", "选择", "加速", "减速"]));
  for (const item of metrics) {
    expect(item.width, item.label ?? "button").toBeGreaterThan(0);
    expect(item.height, item.label ?? "button").toBeGreaterThan(0);
    expect(Math.abs(item.horizontal), `${item.label} horizontal center`).toBeLessThanOrEqual(0.75);
    expect(Math.abs(item.vertical), `${item.label} visible text center`).toBeLessThanOrEqual(0.75);
  }
  return metrics;
}
