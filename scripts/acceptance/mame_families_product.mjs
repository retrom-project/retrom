import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {px68kLocalProxy} from "./px68k_product_support.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, launchCart} from "./fantasy_product_client.mjs";
import {observeAudio, checkConsole} from "./mame_product_support.mjs";
import {prepareFamily, openFamily, familyCases} from "./mame_family_client.mjs";
import {playFamily} from "./mame_family_play.mjs";

const env = process.env, baseUrl = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/mame-families-product");
const source = env.RETROM_MAME_FAMILY_INPUT_DIR;
const platforms = process.argv[2] ? [process.argv[2]] : ["atom", "pv1000"];
assert.ok(baseUrl && source && env.RETROM_CHROME_EXECUTABLE, "MAME_ACCEPTANCE_INPUT_REQUIRED");
assert.ok(platforms.every(platform => familyCases[platform]), "MAME_ACCEPTANCE_PLATFORM_INVALID");
mkdirSync(directory, {recursive: true});
const result = {status: "FAIL", cases: [], requests: [], contentRequests: []}, pending = [];
let browser, proxy;
try {
  proxy = await px68kLocalProxy(baseUrl);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  result.browser = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context); await observeAudio(context);
  context.on("response", response => {
    const path = new URL(response.url()).pathname;
    if (path.startsWith("/runtime/content/") && response.request().method() === "GET") {
      result.contentRequests.push({path, status: response.status()});
    }
    if (path.includes("/assets/mame/")) {pending.push(recordAsset(response, path));}
  });
  const client = await fantasyClient(context, baseUrl);
  for (const platform of platforms) {await runCase(client, context, platform);}
  if (platforms.length > 1) {
    const first = result.cases[0], input = caseInput(first.platform);
    const opened = await openFamily(context, await launchCart(client, first.gameId), input, first, "cross-family-repeat");
    await opened.canvas.screenshot({path: join(input.directory, "cross-family-repeat.png")});
    await opened.page.close();
    result.sequence = ["atom", "pv1000", "atom"];
  }
  await Promise.all(pending);
  for (const evidence of result.cases) {
    assert.deepEqual(evidence.errors, []); checkConsole(evidence.warnings);
    writeFileSync(join(directory, evidence.platform, "product.json"), JSON.stringify(evidence, null, 2) + "\n");
  }
  for (const name of ["mame-common.wasm", ...platforms.map(platform => `mame-${familyCases[platform].family}.wasm`)]) {
    const requests = result.requests.filter(request => request.path.endsWith(name));
    assert.equal(requests.length, 1, `MAME_CODE_DOWNLOADED_AGAIN:${name}`);
    assert.equal(requests[0].encoding, "br");
  }
  const expectedContent = platforms.reduce((total, platform) => total + familyCases[platform].firmware + 1, 0);
  assert.equal(result.contentRequests.length, expectedContent, "MAME_CONTENT_DOWNLOADED_AGAIN");
  assert.equal(new Set(result.contentRequests.map(request => request.path)).size, expectedContent);
  assert.ok(result.contentRequests.every(request => request.status === 200));
  result.status = "AUTOMATED_PASS_REQUIRES_VISUAL_REVIEW";
} catch (error) {result.errorCode = error.message; process.exitCode = 1;}
finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "product.json"), JSON.stringify(result, null, 2) + "\n");
  console.log(JSON.stringify({status: result.status, error: result.errorCode}));
}
function caseInput(platform) {
  return {baseUrl, platform, profile: familyCases[platform], source, directory: join(directory, platform)};
}
async function runCase(client, context, platform) {
  const input = caseInput(platform); mkdirSync(input.directory, {recursive: true});
  const evidence = {platform, caseId: input.profile.caseId, status: "FAIL", stages: [], errors: [], warnings: []};
  result.cases.push(evidence);
  const timer = setTimeout(() => {
    evidence.errorCode = "MAME_CASE_DEADLINE";
    writeFileSync(join(input.directory, "product.json"), JSON.stringify(evidence, null, 2));
    process.exit(124);
  }, 300000);
  try {
    const gameId = await prepareFamily(client, context, input, evidence);
    await playFamily(client, context, input, evidence, gameId);
    assert.deepEqual(evidence.errors, []); checkConsole(evidence.warnings);
    evidence.status = "AUTOMATED_PASS_REQUIRES_VISUAL_REVIEW";
  } catch (error) {evidence.errorCode = error.message; throw error;}
  finally {clearTimeout(timer); writeFileSync(join(input.directory, "product.json"), JSON.stringify(evidence, null, 2) + "\n");}
}
async function recordAsset(response, path) {
  const headers = await response.allHeaders();
  result.requests.push({path, status: response.status(), encoding: headers["content-encoding"] ?? "identity", wireBytes: headers["content-length"] ?? null});
  if (path.endsWith("mame-build.json")) {result.nativeBuild = (await response.json()).buildId;}
}
