import {execFileSync} from "node:child_process";
import path from "node:path";
import {expect, test} from "@playwright/test";
import {evidencePath, noPageOverflow} from "./acceptance-support";
import {expectDetailPreviewTabsInFrame} from "./game-detail-alignment";
import {uiLayoutState} from "./ui-layout-state";

test.beforeEach(() => uiLayoutState("isolate"));
test.afterEach(() => uiLayoutState("restore"));

for (const mode of ["save", "video"] as const) {
test(`ACC-UI-003 ${mode} only keeps inset tabs and sound changes preserve manual playback`, async ({browser}, testInfo) => {
  test.setTimeout(90_000);
  const database = process.env.RETROM_E2E_DATABASE;
  expect(database).toBeTruthy();
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
    const gameId = execFileSync("python3", [path.resolve("../scripts/acceptance/seed-ui-detail.py"), database!, mode], {encoding: "utf8"}).trim();
    for (const [width, height, dpr] of [[390, 844, 1], [1440, 900, 1], [2560, 1440, 1.5]]) {
      const context = await browser.newContext({baseURL: origin, viewport: {width, height}, deviceScaleFactor: dpr, reducedMotion: "reduce"});
      try {
        expect((await context.request.post("/api/v1/auth/login", {headers: {Origin: origin}, data: {username: "test", password: "test"}})).ok()).toBe(true);
        const page = await context.newPage();
        await page.goto(`/games/${gameId}`);
        const tab = page.getByRole("tab", {name: mode === "save" ? "最近存档" : "视频预览", exact: true});
        await expect(page.getByRole("tab")).toHaveCount(1);
        await expect(tab).toHaveAttribute("aria-selected", "true");
        await tab.focus();
        await tab.press("ArrowRight");
        await expect(tab).toBeFocused();
        await expectDetailPreviewTabsInFrame(page);
        await noPageOverflow(page);
        if (mode === "video") {
          const video = page.locator(".game-detail-feature-preview video");
          await expect(page.getByRole("button", {name: "播放视频预览", exact: true})).toBeVisible();
          await page.getByRole("button", {name: "播放视频预览", exact: true}).click();
          await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(false);
          const original = await video.elementHandle();
          await video.evaluate((element: HTMLVideoElement) => {
            element.dataset.pauseCount = "0";
            element.addEventListener("pause", () => {element.dataset.pauseCount = String(Number(element.dataset.pauseCount) + 1);});
          });
          for (const name of ["已静音", "开启声音"]) {
            await page.getByRole("button", {name, exact: true}).click();
            await expect(page.getByRole("button", {name: "暂停预览", exact: true})).toBeVisible();
            await expect(video).toHaveAttribute("data-pause-count", "0");
            expect(await video.evaluate((element, first) => element === first, original)).toBe(true);
            expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(false);
          }
          const current = await video.evaluate((element: HTMLVideoElement) => element.currentTime);
          await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.currentTime)).not.toBe(current);
        }
        await page.screenshot({path: evidencePath(testInfo, `detail-${mode}-only-${width}.png`)});
      } finally {await context.close();}
    }
});
}
