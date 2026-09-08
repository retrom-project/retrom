#!/usr/bin/env node
import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium, expect} from "../../web/node_modules/@playwright/test/index.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {isLocalAcceptanceHostname} from "./rpgmaker_url.mjs";
import {approveScummvm, assertScummvmTraffic, importScummvm, scummvmClient, trackScummvmTraffic} from "./scummvm_product_api.mjs";
import {captureScummvm, exitScummvm, readyScummvm, scummvmFrame, skyGamepadProof, skyScene} from "./scummvm_product_controls.mjs";
import {immersiveScummvm} from "./scummvm_product_immersive.mjs";
import {readableScummvmImage, resizePausedScummvm} from "./scummvm_product_resize.mjs";

const caseId = "ACC-SCUMMVM-001";
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".cache/acceptance/scummvm");
mkdirSync(directory, {recursive: true});
const required = ["RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME", "RETROM_ACCEPTANCE_PASSWORD",
  "RETROM_CHROME_EXECUTABLE", "RETROM_SCUMMVM_SKY_ARCHIVE", "RETROM_ACCEPTANCE_CASE_DIR"];
const missing = required.filter((name) => !process.env[name]);
if (missing.length) {
  write({schemaVersion: 1, caseId, status: "BLOCKED", errorCode: "SCUMMVM_ACCEPTANCE_INPUT_REQUIRED", missing});
  process.exit(3);
}
const baseUrl = normalizedOrigin(process.env.RETROM_ACCEPTANCE_BASE_URL);
let browser;
try {
  const archive = process.env.RETROM_SCUMMVM_SKY_ARCHIVE;
  const sourceSha256 = createHash("sha256").update(readFileSync(archive)).digest("hex");
  assert.equal(sourceSha256, "d0bac1bd61747a67e885fa44b78c78887bf2b15d3dfa2790c483fad651078818");
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE,
    headless: process.env.RETROM_ACCEPTANCE_HEADED !== "1",
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({baseURL: baseUrl, viewport: {width: 1440, height: 1000}});
  await installVirtualStandardGamepad(context);
  const errors = [];
  context.on("page", (page) => {
    page.on("pageerror", () => errors.push("PAGE_ERROR"));
    page.on("dialog", async (dialog) => {errors.push("UNEXPECTED_DIALOG"); await dialog.dismiss();});
  });
  const client = await scummvmClient(context, baseUrl);
  const review = await importScummvm(client, archive);
  const preview = await previewScummvm(context, review.itemId);
  const published = await approveScummvm(client, review.itemId);
  const product = await ordinaryScummvm(context, published.gameId);
  const cache = assertScummvmTraffic([...preview.traffic, ...product.firstTraffic], product.restoreTraffic, "sky");
  const immersive = await immersiveScummvm(context, published.gameId, directory);
  assert.deepEqual(errors, []);
  write({schemaVersion: 1, caseId, status: "PASS", sourceSha256, browserVersion: browser.version(),
    stages: ["imported", "review-selection", "preview-visible", "published", "native-capture", "fresh-launch-restored",
      "first-stick-confirm-cancel", "selected-plugin-only", "content-cache-reused", "immersive-save-restore",
      "paused-resize-fitted", "saved-screenshot-readable"],
    itemId: review.itemId, gameId: published.gameId, preview: preview.evidence, product: product.evidence, cache, immersive});
  await context.close();
} catch (error) {
  write({schemaVersion: 1, caseId, status: "FAIL", errorCode: "SCUMMVM_ACCEPTANCE_FAILED"});
  // Assertions and product-only selectors are safe diagnostics; never print HTTP bodies or credentials.
  process.stderr.write(`${error.name}: ${error.code ?? "PRODUCT_ASSERTION"}\n${String(error.message).slice(0, 1500)}\n${String(error.stack).split("\n").filter((line) => line.trim().startsWith("at ")).join("\n")}\n`);
  process.exitCode = 1;
} finally {await browser?.close();}

async function previewScummvm(context, itemId) {
  const reviewPage = await context.newPage();
  await reviewPage.goto(`/admin/reviews/${itemId}`);
  const selection = reviewPage.getByRole("combobox", {name: /^运行版本/u});
  await expect(selection).toHaveValue(/^[0-9a-f]{64}$/u);
  const candidateId = await selection.inputValue();
  const popup = reviewPage.waitForEvent("popup");
  await reviewPage.getByRole("button", {name: "运行游戏", exact: true}).click();
  const page = await popup;
  const traffic = trackScummvmTraffic(page);
  await page.bringToFront(); await readyScummvm(page); await skyScene(page);
  const frame = await scummvmFrame(page, directory, "preview");
  const input = await skyGamepadProof(page, directory, "preview");
  const receipt = await captureScummvm(page);
  assert.equal(receipt.resourceKind, "REVIEW_PREVIEW_CHECKPOINT");
  await exitScummvm(page);
  await page.waitForEvent("close").catch(() => assert(page.isClosed()));
  await reviewPage.close();
  return {traffic, evidence: {candidateId, frame, input, captured: true}};
}

async function ordinaryScummvm(context, gameId) {
  const page = await context.newPage();
  const firstTraffic = trackScummvmTraffic(page);
  await page.goto(`/games/${gameId}`);
  await page.getByRole("button", {name: "开始游戏", exact: true}).click();
  await page.waitForURL(/\/play\//u); await readyScummvm(page); await skyScene(page);
  const originalLaunchId = new URL(page.url()).pathname.split("/").at(-1);
  const input = await skyGamepadProof(page, directory, "ordinary-original");
  const pausedResize = await resizePausedScummvm(page, directory);
  const saved = await captureScummvm(page);
  const screenshot = await readableScummvmImage(page, `/content/save-states/${saved.saveStateId}/screenshot`);
  await exitScummvm(page); await page.waitForURL(`/games/${gameId}`);
  const first = [...firstTraffic];
  const restoreTraffic = trackScummvmTraffic(page);
  await page.getByRole("button", {name: "▶ 从这里继续", exact: true}).click();
  await page.waitForURL(/\/play\//u); await readyScummvm(page);
  const restoredLaunchId = new URL(page.url()).pathname.split("/").at(-1);
  assert.notEqual(restoredLaunchId, originalLaunchId);
  const config = await (await context.request.get(`/runtime/launches/${restoredLaunchId}/config`)).json();
  assert.equal(config.restore?.format, "scummvm-save-bundle-v1");
  assert(config.restore.sizeBytes > 0 && /^[0-9a-f]{64}$/u.test(config.restore.sha256));
  const restoredInput = await skyGamepadProof(page, directory, "ordinary-restored");
  await exitScummvm(page); await page.waitForURL(`/games/${gameId}`); await page.close();
  return {firstTraffic: first, restoreTraffic, evidence: {originalLaunchId, restoredLaunchId,
    saveStateId: saved.saveStateId, restore: {format: config.restore.format, sizeBytes: config.restore.sizeBytes,
      sha256: config.restore.sha256}, input, restoredInput, pausedResize, screenshot}};
}

function normalizedOrigin(value) {
  const url = new URL(value);
  assert((url.protocol === "https:" || url.protocol === "http:" && isLocalAcceptanceHostname(url.hostname)) &&
    !url.username && !url.password && url.pathname === "/" && !url.search && !url.hash);
  return url.origin;
}
function write(value) {
  writeFileSync(join(directory, "scummvm-product.json"), `${JSON.stringify(value, null, 2)}\n`, {mode: 0o600});
}
