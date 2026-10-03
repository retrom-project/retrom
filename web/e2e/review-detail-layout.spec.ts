import { expect, test, type Page } from "@playwright/test";
import { evidencePath, noPageOverflow, pngDimensions } from "./acceptance-support";

async function measureReview(page: Page) {
  return page.locator(".review-workflow-columns").evaluate((element) => {
    const rect = (selector: string) => {
      const bounds = element.querySelector<HTMLElement>(selector)!.getBoundingClientRect();
      return { ...bounds.toJSON(), top: bounds.top + scrollY, bottom: bounds.bottom + scrollY };
    };
    const description = element.querySelector<HTMLTextAreaElement>("textarea")!;
    return {
      runtime: rect(".review-workflow-left"),
      metadata: rect(".review-workflow-metadata"),
      capability: rect(".review-workflow-capability"),
      checks: rect(".review-workflow-checks"),
      source: rect(".review-workflow-files"),
      fields: rect(".review-workflow-metadata-fields"),
      cover: rect(".review-media-stage"),
      media: rect(".review-workflow-cover-side"),
      layout: rect(".review-workflow-publish-layout"),
      tags: rect(".review-tag-editor"),
      description: description.getBoundingClientRect().toJSON(),
      resize: getComputedStyle(description).resize,
    };
  });
}

async function expectArchiveUsesSourcePanel(page: Page) {
  const panel = page.locator(".review-workflow-files .panel-body");
  // Exercise an expanded archive's layout without changing imported source data.
  await panel.evaluate((element) => {
    const group = document.createElement("div");
    group.dataset.archiveLayoutRegression = "true";
    group.className = "review-source-packages";
    const source = document.createElement("details");
    source.className = "review-source-package";
    source.open = true;
    const summary = document.createElement("summary");
    summary.textContent = "Archive layout fixture.zip";
    const entries = document.createElement("div");
    entries.className = "review-archive-entries";
    for (let index = 0; index < 40; index += 1) {
      const row = document.createElement("div");
      row.textContent = `archive-entry-${index}.bin`;
      entries.append(row);
    }
    source.append(summary, entries);
    group.append(source);
    element.append(group);
  });
  const entries = page.locator("[data-archive-layout-regression] .review-archive-entries");
  const inner = await entries.evaluate((element) => ({ height: element.clientHeight, scrollHeight: element.scrollHeight }));
  expect(inner.height).toBe(inner.scrollHeight);
  const outer = await panel.evaluate((element) => ({ height: element.clientHeight, scrollHeight: element.scrollHeight }));
  expect(inner.height).toBeGreaterThan(outer.height);
  expect(outer.scrollHeight).toBeGreaterThan(outer.height);
  await entries.locator("div").last().scrollIntoViewIfNeeded();
  await expect(entries.locator("div").last()).toBeInViewport();
  expect(await panel.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  expect(await entries.evaluate((element) => element.scrollTop)).toBe(0);
  await page.locator("[data-archive-layout-regression]").evaluate((element) => element.remove());
}

async function expectNaturalReviewLayout(page: Page) {
  const before = await measureReview(page);
  expect(before.metadata.top).toBeGreaterThanOrEqual(before.runtime.bottom + 12);
  expect(before.metadata.width).toBeCloseTo(before.runtime.width, 0);
  expect(before.source.left).toBeGreaterThan(before.capability.right);
  expect(Math.abs(before.source.top - before.capability.top)).toBeLessThanOrEqual(1);
  expect(Math.abs(before.source.bottom - before.checks.bottom)).toBeLessThanOrEqual(1);
  expect(before.cover.left).toBeGreaterThan(before.fields.right);
  expect(before.cover.height).toBeGreaterThanOrEqual(320);
  expect(before.cover.height).toBeLessThanOrEqual(360);
  expect(before.tags.top).toBeGreaterThanOrEqual(Math.max(before.fields.bottom, before.media.bottom) + 16);
  expect(before.tags.width).toBeCloseTo(before.layout.width, 0);
  expect(before.description.height).toBeGreaterThanOrEqual(120);
  expect(before.description.height).toBeLessThanOrEqual(240);
  expect(before.resize).toBe("vertical");
  await page.getByRole("tab", { name: "视频", exact: true }).click();
  await expect(page.getByText("暂无视频")).toBeVisible();
  const video = await measureReview(page);
  expect(video.cover.height).toBeCloseTo(before.cover.height, 0);
  expect(video.metadata.height).toBeCloseTo(before.metadata.height, 0);
  expect(video.tags.top).toBeCloseTo(before.tags.top, 0);
  await page.keyboard.press("Home");
  await expect(page.getByRole("tab", { name: "封面", exact: true })).toBeFocused();
  await expect(page.getByText("暂无封面", { exact: true })).toBeVisible();

  // Exercise changing runtime evidence without modifying the draft or backend.
  await page.locator(".review-workflow-capability .panel-body").evaluate((element) => {
    const details = document.createElement("div");
    details.dataset.layoutRegression = "true";
    details.className = "review-validation-guidance";
    details.tabIndex = 0;
    for (let index = 0; index < 80; index += 1) {
      const item = document.createElement("p");
      item.textContent = `missing-entry-${index}.bin 缺失`;
      details.append(item);
    }
    element.prepend(details);
  });
  const after = await measureReview(page);
  expect(Math.abs(after.source.bottom - after.checks.bottom)).toBeLessThanOrEqual(1);
  expect(after.runtime.height).toBeGreaterThan(before.runtime.height);
  expect(after.metadata.top).toBeGreaterThan(before.metadata.top);
  expect(after.metadata.height).toBeCloseTo(before.metadata.height, 0);
  expect(after.description.height).toBeCloseTo(before.description.height, 0);
  expect(after.cover.height).toBeCloseTo(before.cover.height, 0);
  const overflow = await page.locator("[data-layout-regression]").evaluate((element) => ({
    height: element.clientHeight, scrollHeight: element.scrollHeight, overflow: getComputedStyle(element).overflowY,
  }));
  expect(overflow.height).toBeLessThanOrEqual(360);
  expect(overflow.scrollHeight).toBeGreaterThan(overflow.height);
  expect(overflow.overflow).toBe("auto");
  await page.locator("[data-layout-regression]").focus();
  await page.keyboard.press("End");
  await expect.poll(() => page.locator("[data-layout-regression]").evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  await expectArchiveUsesSourcePanel(page);
  await page.locator("[data-layout-regression]").evaluate((element) => element.remove());
  await noPageOverflow(page);
}

test("ACC-UI-008 review sections and media keep independent natural heights", async ({ page }, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => { if (message.type() === "error") { errors.push(message.text()); } });
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  expect((await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" }, headers: { Origin: origin },
  })).ok()).toBe(true);
  await page.goto("/admin/reviews/30000000-0000-7000-8001-000000000057");
  await expect(page.getByRole("heading", { name: "② 发布成什么？" })).toBeVisible();
  await page.getByRole("textbox", { name: "标题", exact: true }).focus();
  await page.getByRole("textbox", { name: "标题", exact: true }).blur();
  const sizes = testInfo.project.name === "chrome-1280"
    ? [{ width: 1920, height: 1080 }, { width: 1440, height: 1000 }, { width: 1280, height: 800 }]
    : [page.viewportSize()!];
  for (const viewport of sizes) {
    await page.setViewportSize(viewport);
    await expectNaturalReviewLayout(page);
    await page.evaluate(() => scrollTo(0, 0));
    const screenshot = await page.screenshot({ caret: "initial", path: evidencePath(testInfo, `review-layout-${viewport.width}.png`) });
    if (testInfo.project.name === "chrome-4k-150") {
      expect(await page.evaluate(() => devicePixelRatio)).toBe(1.5);
      expect(pngDimensions(screenshot)).toEqual({ width: 3840, height: 2160 });
    }
    await page.screenshot({ caret: "initial", path: evidencePath(testInfo, `review-layout-${viewport.width}-full.png`), fullPage: true });
  }
  expect(errors).toEqual([]);
});
