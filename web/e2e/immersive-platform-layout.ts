import { expect, type Page, type TestInfo } from "@playwright/test";

export async function verifyPlatformCardLayout(page: Page, info: TestInfo) {
  const original = page.viewportSize()!;
  const sizes = [original, { width: 960, height: 540 }, { width: 1920, height: 800 }];
  const cards = page.getByRole("region", { name: "游戏平台", exact: true }).locator("button");
  try {
    for (const size of sizes) {
      await page.setViewportSize(size);
      await expect(cards).toHaveCount(3);
      const bounds = await cards.evaluateAll((nodes) => nodes.map((node) => {
        const card = node as HTMLElement;
        return { width: card.offsetWidth, height: card.offsetHeight };
      }));
      for (const direction of ["ArrowRight", "ArrowLeft"]) {
        const current = await cards.nth(1).getAttribute("aria-label");
        await page.keyboard.press(direction);
        await expect(cards.nth(1)).not.toHaveAttribute("aria-label", current!);
        const layout = await cards.evaluateAll((nodes) => nodes.map((node) => {
          const card = node as HTMLElement;
          const title = card.querySelector("h2")!;
          const copy = title.parentElement!;
          return {
            name: card.getAttribute("aria-label"),
            width: card.offsetWidth, height: card.offsetHeight,
            titleHeight: title.clientHeight, titleScrollHeight: title.scrollHeight,
            lineHeight: parseFloat(getComputedStyle(title).lineHeight),
            copyFits: copy.scrollHeight <= copy.clientHeight + 1,
            cardFits: card.scrollHeight <= card.clientHeight + 1,
          };
        }));
        for (const [index, card] of layout.entries()) {
          expect({ width: card.width, height: card.height }, card.name!).toEqual(bounds[index]);
          expect(card.titleHeight, card.name!).toBeGreaterThanOrEqual(card.lineHeight - 1);
          expect(card.titleScrollHeight, card.name!).toBeLessThanOrEqual(card.titleHeight + 1);
          expect(card.copyFits, card.name!).toBe(true);
          expect(card.cardFits, card.name!).toBe(true);
        }
      }
      await page.screenshot({ path: info.outputPath(`platform-cards-${size.width}x${size.height}.png`) });
    }
  } finally {
    await page.setViewportSize(original);
  }
}
