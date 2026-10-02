import {execFileSync} from "node:child_process";
import path from "node:path";
import {expect, test} from "@playwright/test";
import {evidencePath, noPageOverflow} from "./acceptance-support";

// Playwright's headless default hides native scrollbars even with custom CSS.
test.use({launchOptions: {
  executablePath: process.env.RETROM_CHROME_EXECUTABLE ?? path.resolve("../.cache/tools/retrom-chrome-for-testing"),
  ignoreDefaultArgs: ["--hide-scrollbars"],
}});

test("ACC-UI-001 overflowing platform scrollbar shows only on hover without moving the layout", async ({page}, testInfo) => {
  const database = process.env.RETROM_E2E_DATABASE;
  expect(database).toBeTruthy();
  execFileSync("python3", [path.resolve("../scripts/acceptance/seed-library-platforms.py"), database!]);
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  expect((await page.request.post("/api/v1/auth/login", {headers: {Origin: origin}, data: {username: "test", password: "test"}})).ok()).toBe(true);
  await page.setViewportSize({width: 1440, height: 900});
  await page.goto("/library");
  const row = page.locator(".library-platform-row");
  await expect(row).toBeVisible();
  expect(await row.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true);
  await page.mouse.move(1, 1);
  const thumb = () => row.evaluate(element => getComputedStyle(element, "::-webkit-scrollbar-thumb").backgroundColor);
  expect(await row.evaluate(element => getComputedStyle(element, "::-webkit-scrollbar").height)).toBe("6px");
  expect(await row.evaluate(element => (element as HTMLElement).offsetHeight - element.clientHeight)).toBe(6);
  await expect.poll(thumb).toBe("rgba(0, 0, 0, 0)");
  const before = await row.boundingBox();
  const grid = await page.locator(".library-game-grid").boundingBox();
  await row.hover();
  await expect.poll(thumb).not.toBe("rgba(0, 0, 0, 0)");
  expect(await row.boundingBox()).toEqual(before);
  expect(await page.locator(".library-game-grid").boundingBox()).toEqual(grid);
  await row.evaluate(element => {element.scrollLeft = element.scrollWidth;});
  expect(await row.evaluate(element => element.scrollLeft)).toBeGreaterThan(0);
  await page.screenshot({path: evidencePath(testInfo, "platform-scrollbar-hover-1440.png")});
  await page.mouse.move(1, 1);
  await expect.poll(thumb).toBe("rgba(0, 0, 0, 0)");
  expect(await row.boundingBox()).toEqual(before);
  expect(await page.locator(".library-game-grid").boundingBox()).toEqual(grid);
  await noPageOverflow(page);
  await page.screenshot({path: evidencePath(testInfo, "platform-scrollbar-hidden-1440.png")});
});
