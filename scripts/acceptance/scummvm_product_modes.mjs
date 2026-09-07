#!/usr/bin/env node
import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/@playwright/test/index.mjs";
import {isLocalAcceptanceHostname} from "./rpgmaker_url.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {scummvmClient} from "./scummvm_product_api.mjs";
import {deferredScummvm} from "./scummvm_product_deferred.mjs";
import {manualScummvm} from "./scummvm_product_manual.mjs";

const caseId = "ACC-SCUMMVM-002";
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".cache/acceptance/scummvm-modes");
mkdirSync(directory, {recursive: true});
const required = ["RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME", "RETROM_ACCEPTANCE_PASSWORD",
  "RETROM_CHROME_EXECUTABLE", "RETROM_SCUMMVM_SKY_ARCHIVE", "RETROM_SCUMMVM_COMI_ARCHIVE", "RETROM_ACCEPTANCE_CASE_DIR"];
const missing = required.filter((name) => !process.env[name]);
if (missing.length) {
  write({schemaVersion: 1, caseId, status: "BLOCKED", errorCode: "SCUMMVM_ACCEPTANCE_INPUT_REQUIRED", missing});
  process.exit(3);
}
let browser;
const completed = {};
try {
  const url = new URL(process.env.RETROM_ACCEPTANCE_BASE_URL);
  assert((url.protocol === "https:" || url.protocol === "http:" && isLocalAcceptanceHostname(url.hostname)) &&
    !url.username && !url.password && url.pathname === "/" && !url.search && !url.hash);
  const skyArchive = process.env.RETROM_SCUMMVM_SKY_ARCHIVE, comiArchive = process.env.RETROM_SCUMMVM_COMI_ARCHIVE;
  const sha256 = (path) => createHash("sha256").update(readFileSync(path)).digest("hex");
  const sources = {sky: sha256(skyArchive), comi: sha256(comiArchive)};
  assert.equal(sources.sky, "d0bac1bd61747a67e885fa44b78c78887bf2b15d3dfa2790c483fad651078818");
  assert.equal(sources.comi, "d944e2e6b00bd180466c1aa7bc236e763f23b4733f996b943e0976ef3078a7fa");
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE,
    headless: process.env.RETROM_ACCEPTANCE_HEADED !== "1",
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({baseURL: url.origin, viewport: {width: 1440, height: 1000}});
  await installVirtualStandardGamepad(context);
  const errors = [];
  context.on("page", (page) => {
    page.on("pageerror", () => errors.push("PAGE_ERROR"));
    page.on("dialog", async (dialog) => {errors.push("UNEXPECTED_DIALOG"); await dialog.dismiss();});
  });
  const client = await scummvmClient(context, url.origin);
  completed.deferred = await deferredScummvm(context, client, comiArchive, directory);
  completed.manual = await manualScummvm(context, client, skyArchive, directory);
  assert.deepEqual(errors, []);
  write({schemaVersion: 1, caseId, status: "PASS", sources, browserVersion: browser.version(),
    stages: ["deferred-native-capture", "preview-native-restore", "fresh-launch-native-restore", "restored-gamepad",
      "in-game-native-save", "native-exit-final-export", "unknown-slot-kept-manual", "in-game-load"], ...completed});
  await context.close();
} catch (error) {
  write({schemaVersion: 1, caseId, status: "FAIL", errorCode: "SCUMMVM_ACCEPTANCE_FAILED", completed});
  process.stderr.write(`${error.name}: ${error.code ?? "PRODUCT_ASSERTION"}\n${String(error.message).slice(0, 1500)}\n${String(error.stack).split("\n").filter((line) => line.trim().startsWith("at ")).join("\n")}\n`);
  process.exitCode = 1;
} finally {await browser?.close();}

function write(value) {writeFileSync(join(directory, "scummvm-product.json"), `${JSON.stringify(value, null, 2)}\n`, {mode: 0o600});}
