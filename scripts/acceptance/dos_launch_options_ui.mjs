import assert from "node:assert/strict";
import {mkdir, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium, expect} from "../../web/node_modules/@playwright/test/index.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.cjs";
import {fantasyClient, approveCart} from "./fantasy_product_client.mjs";
import {importComputer} from "./computer_product_client.mjs";

const base = process.env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/dos-launch-options-ui");
await mkdir(directory, {recursive: true});
const report = {schemaVersion: 1, caseId: "ACC-UI-003", status: "FAIL"};
const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
const context = await browser.newContext({viewport: {width: 2560, height: 1440}, deviceScaleFactor: 1.5});
try {
  assert.ok(base, "DOS_UI_BASE_URL_REQUIRED");
  const client = await fantasyClient(context, base);
  const review = await importComputer(client, "dos", "dosbox_pure", resolve("testdata/public-roms/dos-cache/dos-cache.zip"));
  const game = await approveCart(client, review.itemId);
  report.gameId = game.gameId;
  const page = await context.newPage();
  await page.goto(`${base}/games/${game.gameId}`);
  const options = page.locator(".launch-options");
  const loading = page.getByRole("combobox", {name: "内容加载", exact: true});
  const program = page.getByRole("combobox", {name: "启动程序", exact: true});
  await expect(loading).toBeEnabled();
  await expect(program).toHaveValue("CACHE.COM");
  // A fixed desktop viewport with a narrower real middle column proves this
  // follows the container, including pages with or without a media column.
  const middleWidth = width => page.locator(".game-detail-main").evaluate((element, value) => {element.style.maxWidth = `${value}px`;}, width);
  await middleWidth(700);
  await expect(options).toHaveClass(/is-paired/);
  await expect(options).not.toHaveClass(/is-tabbed/);
  const boxes = await Promise.all([loading.boundingBox(), program.boundingBox(), page.locator(".launch-actions button").last().boundingBox()]);
  assert.equal(boxes[0].y, boxes[1].y, "DOS_SELECTORS_MUST_SHARE_ROW");
  assert.ok(boxes[1].x > boxes[0].x + boxes[0].width, "DOS_PROGRAM_MUST_BE_BESIDE_LOADING");
  assert.ok(Math.abs(boxes[0].x + boxes[0].width - boxes[2].x - boxes[2].width) < 1, "DOS_LOADING_BUTTON_RIGHT_ALIGNMENT");
  await page.screenshot({path: join(directory, "dos-options-wide-4k.png")});
  await middleWidth(400);
  await expect(options).toHaveClass(/is-tabbed/);
  const loadingTab = page.getByRole("tab", {name: /^内容加载/});
  const programTab = page.getByRole("tab", {name: /^启动程序/});
  await expect(loadingTab).toHaveAccessibleName(/按需加载/);
  await expect(programTab).toHaveAccessibleName(/CACHE.COM/);
  await expect(loadingTab).toHaveAttribute("aria-selected", "true");
  await loading.selectOption("PRELOAD");
  await expect(loadingTab).toHaveAccessibleName(/下载完成后开始/);
  const before = await options.boundingBox();
  await programTab.click();
  await expect(loading).toHaveCount(0);
  await expect(page.locator(".launch-option-panel.is-inactive")).toHaveAttribute("inert", "");
  await program.selectOption("");
  await expect(programTab).toHaveAccessibleName(/程序菜单/);
  const after = await options.boundingBox();
  assert.equal(before.height, after.height, "DOS_TAB_SWITCH_MUST_PRESERVE_HEIGHT");
  await programTab.focus(); await page.keyboard.press("ArrowLeft");
  await expect(loadingTab).toBeFocused();
  await expect(loading).toHaveValue("PRELOAD");
  await page.keyboard.press("End"); await expect(programTab).toBeFocused();
  await page.keyboard.press("Home"); await expect(loadingTab).toBeFocused();
  await page.keyboard.press("ArrowRight"); await expect(programTab).toBeFocused();
  await page.keyboard.press("Tab"); await expect(program).toBeFocused();
  await page.screenshot({path: join(directory, "dos-options-tabs-4k.png")});
  await middleWidth(700);
  await expect(page.getByRole("tablist")).toHaveCount(0);
  await expect(program).toHaveValue(""); await expect(loading).toHaveValue("PRELOAD");
  await middleWidth(400);
  await loadingTab.click(); await expect(loading).toHaveValue("PRELOAD");
  await page.reload();
  await expect(program).toHaveValue("CACHE.COM"); // Changing a field does not persist a DOS launch choice.
  await expect(loading).toHaveValue("PRELOAD");
  await page.setViewportSize({width: 390, height: 844});
  await page.getByRole("button", {name: "启动选项", exact: true}).click();
  await expect(page.getByRole("tablist")).toHaveCount(0);
  await expect(loading).toHaveValue("PRELOAD"); await expect(program).toHaveValue("CACHE.COM");
  await page.screenshot({path: join(directory, "dos-options-mobile.png")});
  const metadata = await sharp(join(directory, "dos-options-tabs-4k.png")).metadata();
  assert.equal(metadata.width, 3840); assert.equal(metadata.height, 2160);
  report.dimensions = {width: metadata.width, height: metadata.height, dpr: 1.5};
  report.wide = boxes; report.compact = {before, after};
  report.status = "PASS";
} catch (error) {report.error = error.message; throw error;}
finally {
  await writeFile(join(directory, "dos-launch-options-ui.json"), JSON.stringify(report, null, 2) + "\n");
  await context.close(); await browser.close();
}
