import {expect, type Page, type TestInfo} from "@playwright/test";
import {evidencePath} from "./acceptance-support";
import {runtimeFrameCount} from "./runtime-provider-support";

export async function expectMobileSettings(page: Page, testInfo: TestInfo, width: number) {
  await page.getByRole("button", {name: "更多操作", exact: true}).click();
  await page.getByRole("menuitem", {name: "模拟器设置"}).click();
  const settings = page.getByRole("region", {name: "模拟器设置工具栏"});
  await expect(settings).toBeVisible();
  const bounds = await settings.boundingBox();
  expect(bounds?.width).toBeLessThanOrEqual(320);
  expect(await settings.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await expect(settings.getByRole("button", {name: "显示"})).toHaveCount(0);
  const volume = settings.getByRole("slider", {name: "模拟器音量"});
  await expect(volume).toBeInViewport();
  await settings.getByRole("combobox", {name: "画面模式"}).selectOption("pixel");
  await settings.getByRole("button", {name: "静音", exact: true}).click();
  await expect(settings.getByRole("button", {name: "取消静音"})).toHaveAttribute("aria-pressed", "true");
  await settings.getByRole("button", {name: "取消静音"}).click();
  await settings.getByRole("button", {name: "高级设置"}).click();
  await expect(settings.getByRole("button", {name: "控制", exact: true})).toHaveCount(0);
  await page.screenshot({path: evidencePath(testInfo, `settings-${width}.png`)});
  const pausedAt = await runtimeFrameCount(page);
  const frame = page.frameLocator("iframe.player-frame");
  const panels = [
    {name: "Core 设置", heading: /Backend Core Options|Core Options|核心选项|核心设置/},
    {name: "显示", heading: /Graphics Settings|图形设置|显示设置/},
  ];
  for (const panel of panels) {
    await settings.getByRole("button", {name: panel.name, exact: true}).click();
    await expect(settings).toHaveCount(0);
    await expect(page.getByRole("region", {name: "原生设置导航"})).toBeVisible();
    const heading = frame.getByRole("button", {name: panel.heading});
    await expect(heading).toBeVisible();
    await expect(heading).toBeInViewport();
    await expect(frame.locator(".ejs_virtualGamepad_left")).toBeHidden();
    await expect(frame.locator(".ejs_virtualGamepad_right")).toBeHidden();
    await expect.poll(() => heading.evaluate((element) => {
      const box = element.closest(".ejs_settings_parent")!.getBoundingClientRect();
      const viewport = element.ownerDocument.defaultView!;
      return box.top >= 0 && box.left >= 0 && box.bottom <= viewport.innerHeight && box.right <= viewport.innerWidth;
    })).toBe(true);

    const target = await heading.boundingBox();
    expect(target).not.toBeNull();
    // Check the host stacking context too: visibility inside the iframe alone
    // does not prove the native setting can be touched through host overlays.
    expect(await page.evaluate(({x, y}) => document.elementFromPoint(x, y)?.matches("iframe.player-frame"), {
      x: target!.x + target!.width / 2, y: target!.y + target!.height / 2,
    })).toBe(true);
    await page.screenshot({path: evidencePath(testInfo, `settings-${panel.name === "显示" ? "display" : "core"}-${width}.png`)});
    await heading.click();
    await expect(frame.locator(".ejs_settings_main_bar").filter({hasText: /Graphics Settings|图形设置|显示设置/})).toBeVisible();
    expect(await runtimeFrameCount(page)).toBeLessThanOrEqual(pausedAt + 1);
    await page.getByRole("button", {name: "返回设置"}).click();
    await expect(settings).toBeVisible();
    await expect(settings.getByRole("button", {name: panel.name, exact: true})).toBeFocused();
  }
  await settings.getByRole("button", {name: "显示", exact: true}).click();
  await page.getByRole("button", {name: "关闭模拟器设置", exact: true}).click();
  await expect(page.getByRole("region", {name: "原生设置导航"})).toHaveCount(0);
  await expect(frame.getByRole("button", {name: /Graphics Settings|图形设置|显示设置/})).toBeHidden();
  await expect(page.getByRole("button", {name: "更多操作", exact: true})).toBeFocused();
  expect(await runtimeFrameCount(page)).toBeLessThanOrEqual(pausedAt + 1);
  await page.getByRole("button", {name: "继续游戏", exact: true}).click();
  await expect.poll(() => runtimeFrameCount(page)).toBeGreaterThan(pausedAt + 5);
  await expect(frame.locator(".ejs_virtualGamepad_left")).toBeVisible();
  await expect(frame.locator(".ejs_virtualGamepad_right")).toBeVisible();
}
