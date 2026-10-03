import {expect, type Page, type TestInfo} from "@playwright/test";
import {evidencePath} from "./acceptance-support";

export async function verifyStartupLoadingLayout(page: Page, testInfo: TestInfo) {
  const viewport = page.viewportSize()!;
  const loading = page.locator(".player-loading");
  await expect(loading.locator(":scope > strong")).toHaveText("游戏启动中");
  const pending = loading.locator('[data-task-kind="PROVIDER_MODULE"]');
  await expect(pending.getByRole("progressbar")).not.toHaveAttribute("aria-valuenow");
  await expect(pending.getByRole("img", {name: "运行时模块进行中"})).toBeVisible();
  for (const size of [viewport, {width: 390, height: 320}, {width: 320, height: 300}]) {
    await page.setViewportSize(size);
    await expect(loading).toBeInViewport();
    await expect(pending.locator(".player-startup-label")).toHaveAttribute("data-overflow", "true");
    const rows = await loading.locator(".player-startup-row").evaluateAll(elements => elements.map(row => {
      const label = row.querySelector(".player-startup-label")!;
      const progress = row.querySelector(".player-startup-progress")!;
      const status = row.querySelector(".player-startup-indicator")!;
      return {label: label.getBoundingClientRect().toJSON(), progress: progress.getBoundingClientRect().toJSON(),
        status: status.getBoundingClientRect().toJSON(), size: parseFloat(getComputedStyle(row).fontSize),
        cells: progress.querySelectorAll(".player-startup-cell").length,
        overflow: row.scrollWidth > row.clientWidth};
    }));
    expect(rows.length).toBeLessThanOrEqual(3);
    for (const row of rows) {
      expect(row.cells).toBe(10);
      expect(row.overflow).toBe(false);
      expect(Math.abs(row.label.width - row.size * 4)).toBeLessThan(1);
      expect(row.label.right).toBeLessThan(row.progress.left);
      expect(row.progress.right).toBeLessThan(row.status.left);
      expect(Math.abs(row.progress.left - rows[0].progress.left)).toBeLessThan(1);
    }
    await page.screenshot({path: evidencePath(testInfo, `startup-${size.width}.png`)});
  }
  const label = pending.locator(".player-startup-label > span");
  await expect(label).toHaveCSS("animation-name", "player-startup-label-scroll");
  await expect.poll(() => label.evaluate(element => {
    const transform = getComputedStyle(element).transform;
    return transform !== "none" && new DOMMatrixReadOnly(transform).m41 < -1;
  })).toBe(true);
  await page.emulateMedia({reducedMotion: "reduce"});
  for (const locator of [label, pending.locator(".player-startup-spinner"), pending.locator(".player-startup-cell").first()]) {
    await expect(locator).toHaveCSS("animation-name", "none");
  }
  await page.emulateMedia({reducedMotion: "no-preference"});
  await page.setViewportSize(viewport);
}

type StartupLayoutEvidence = {samples: number; overlaps: number};

export async function observeStartupHistory(page: Page) {
  await page.addInitScript(() => {
    const scope = window as Window & {startupLayoutEvidence?: StartupLayoutEvidence};
    const evidence = {samples: 0, overlaps: 0};
    scope.startupLayoutEvidence = evidence;
    const sample = () => {
      const rows = [...document.querySelectorAll(".player-startup-row")]
        .map(row => row.getBoundingClientRect()).sort((a, b) => a.top - b.top);
      if (rows.length > 1) {
        evidence.samples++;
        if (rows.some((row, index) => index > 0 && row.top < rows[index - 1].bottom - 1)) {evidence.overlaps++;}
      }
      if (!document.querySelector(".player-loading") && evidence.samples > 0) {return;}
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
}

export async function expectStartupHistorySeparated(page: Page) {
  const evidence = await page.evaluate(() => (window as Window & {startupLayoutEvidence?: StartupLayoutEvidence}).startupLayoutEvidence);
  expect(evidence?.samples).toBeGreaterThan(0);
  expect(evidence?.overlaps, "startup rows must remain separate throughout scrolling").toBe(0);
}
