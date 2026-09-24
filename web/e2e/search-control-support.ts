import { expect, type Page } from "@playwright/test";

export async function expectSearchComposition(page: Page) {
  const inputs = page.locator("input[type=search], input:not([type]), input[type=text], input[type=password]");
  for (const input of await inputs.all()) {
    if (!await input.isVisible() || !await input.evaluate((element) => Boolean(element.parentElement?.querySelector(":scope > svg") || element.parentElement?.matches(".password-control")))) { continue; }
    const name = await input.getAttribute("placeholder") ?? "composite input";
    const measure = () => input.evaluate((element) => {
      const parent = element.parentElement!, box = parent.getBoundingClientRect(), field = element.getBoundingClientRect();
      const icon = parent.querySelector(":scope > svg, :scope > button")!.getBoundingClientRect(), style = getComputedStyle(element);
      return { border: style.borderWidth, background: style.backgroundColor, height: box.height,
        fieldCenter: (field.top + field.bottom - box.top - box.bottom) / 2,
        iconCenter: (icon.top + icon.bottom - box.top - box.bottom) / 2,
        overflow: Math.max(box.top - field.top, field.bottom - box.bottom, field.right - box.right),
        focusOutline: getComputedStyle(parent).outlineStyle };
    });
    for (const focused of [false, true]) {
      if (focused) { await input.focus(); }
      const result = await measure();
      expect(result.border, name).toBe("0px");
      expect(result.background).toBe("rgba(0, 0, 0, 0)");
      expect(result.height).toBe(44);
      expect(Math.abs(result.fieldCenter)).toBeLessThanOrEqual(1);
      expect(Math.abs(result.iconCenter)).toBeLessThanOrEqual(1);
      expect(result.overflow).toBeLessThanOrEqual(0);
      if (focused) { expect(result.focusOutline).toBe("solid"); }
    }
    await input.blur();
  }
  await page.evaluate(() => scrollTo(0, 0));
}
