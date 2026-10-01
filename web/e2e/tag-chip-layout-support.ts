import { expect, type Page } from "@playwright/test";

export async function expectTagChipTextVisible(page: Page) {
  const chips = await page.locator(".tag-chip-removable").evaluateAll((elements) => elements.map((chip) => {
    const label = chip.querySelector<HTMLElement>(".tag-chip-label")!;
    const range = document.createRange();
    range.selectNodeContents(label);
    const context = document.createElement("canvas").getContext("2d")!;
    context.font = getComputedStyle(label).font;
    const text = context.measureText(label.textContent ?? "");
    const baseline = range.getBoundingClientRect().top + text.fontBoundingBoxAscent;
    const inkTop = baseline - text.actualBoundingBoxAscent;
    const inkBottom = baseline + text.actualBoundingBoxDescent;
    const bounds = label.getBoundingClientRect();
    const capsule = chip.getBoundingClientRect();
    const button = chip.querySelector("button")!.getBoundingClientRect();
    return { topSpace: inkTop - bounds.top, bottomSpace: bounds.bottom - inkBottom,
      textOffset: (inkTop + inkBottom - capsule.top - capsule.bottom) / 2,
      buttonOffset: (button.top + button.bottom - capsule.top - capsule.bottom) / 2 };
  }));
  expect(chips.length).toBeGreaterThan(0);
  for (const chip of chips) {
    expect(chip.topSpace, "glyph ascenders must not be clipped").toBeGreaterThanOrEqual(0);
    expect(chip.bottomSpace, "glyph descenders must not be clipped").toBeGreaterThanOrEqual(0);
    expect(Math.abs(chip.textOffset)).toBeLessThanOrEqual(1);
    expect(Math.abs(chip.buttonOffset)).toBeLessThanOrEqual(0.5);
  }
}
