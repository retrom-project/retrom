import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {resolve, join} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {fantasyClient, launchCart} from "./fantasy_product_client.mjs";
import {px68kLocalProxy} from "./px68k_product_support.mjs";

const env = process.env;
const baseUrl = env.RETROM_ACCEPTANCE_BASE_URL;
const games = [
  ["mspacman", env.RETROM_MAME_ARCADE_MSPACMAN_GAME_ID],
  ["dkong", env.RETROM_MAME_ARCADE_DKONG_GAME_ID],
  ["mspacman", env.RETROM_MAME_ARCADE_MSPACMAN_GAME_ID],
];
assert.ok(baseUrl && env.RETROM_CHROME_EXECUTABLE && games.every(([, id]) => id),
  "MAME_ARCADE_CACHE_INPUT_REQUIRED");
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/mame-arcade-cache");
mkdirSync(directory, {recursive: true});
const evidence = {caseId: "ACC-MAME-004-CACHE", status: "FAIL", launches: [], requests: [], errors: []};
let browser, proxy;
try {
  proxy = await px68kLocalProxy(baseUrl);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  context.on("response", response => {
    const name = new URL(response.url()).pathname.split("/").at(-1);
    if (name?.startsWith("mame-") && (name.endsWith(".wasm") || name === "mame-build.json")) {
      evidence.requests.push({name, status: response.status()});
    }
  });
  const client = await fantasyClient(context, baseUrl);
  for (const [machine, gameId] of games) {
    const launch = await launchCart(client, gameId);
    const page = await context.newPage();
    const cdp = await context.newCDPSession(page);
    await cdp.send("Network.enable");
    await cdp.send("Network.setCacheDisabled", {cacheDisabled: true});
    page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 300)));
    await page.goto(baseUrl + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
    const deadline = Date.now() + 120000;
    let canvas;
    while (Date.now() < deadline && !canvas) {
      for (const frame of page.frames()) {
        const candidate = frame.locator(`canvas[aria-label="MAME Arcade (${machine})"]`);
        if (await candidate.isVisible()) {canvas = candidate; break;}
      }
      if (!canvas) {await page.waitForTimeout(100);}
    }
    assert.ok(canvas, `MAME_ARCADE_CACHE_LAUNCH_FAILED:${machine}`);
    await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 30000});
    evidence.launches.push({machine, dimensions: await canvas.evaluate(element => ({width: element.width, height: element.height}))});
    await page.close();
  }
  assert.deepEqual(evidence.errors, []);
  const count = name => evidence.requests.filter(entry => entry.name === name && entry.status === 200).length;
  assert.equal(count("mame-common.wasm"), 1, "MAME_ARCADE_COMMON_REDOWNLOAD");
  assert.equal(count("mame-pacman.wasm"), 1, "MAME_ARCADE_PACMAN_REDOWNLOAD");
  assert.equal(count("mame-arcade_nintendo.wasm"), 1, "MAME_ARCADE_NINTENDO_REDOWNLOAD");
  evidence.status = "PASS";
} catch (error) {evidence.errorCode = error.message; process.exitCode = 1;}
finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "cache.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify({status: evidence.status, error: evidence.errorCode}));
}
