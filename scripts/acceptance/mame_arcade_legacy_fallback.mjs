import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {existsSync, mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {basename, join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {fantasyClient, approveCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {px68kLocalProxy, canvasDigest} from "./px68k_product_support.mjs";

const env = process.env, baseUrl = env.RETROM_ACCEPTANCE_BASE_URL, source = env.RETROM_MAME_ARCADE_ROM;
const machine = basename(source ?? "", ".zip").toLowerCase();
assert.ok(baseUrl && source && env.RETROM_CHROME_EXECUTABLE && /^[a-z0-9_]{1,32}$/u.test(machine),
  "MAME_ARCADE_FALLBACK_INPUT_REQUIRED");
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/mame-arcade-legacy-fallback");
mkdirSync(directory, {recursive: true});
const progressPath = join(directory, "progress.json");
const evidence = {caseId: "ACC-MAME-004-FALLBACK", status: "FAIL", stages: [], errors: []};
let browser, proxy;
try {
  proxy = await px68kLocalProxy(baseUrl);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  const client = await fantasyClient(context, baseUrl);
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=arcade&limit=100");
  const old = instances.items.find(item => item.enabled && item.defaultCoreId === "mame2003_plus");
  assert.ok(old, "MAME2003_PLUS_DIRECTORY_MISSING");
  const digest = createHash("sha256").update(readFileSync(source)).digest("hex");
  const progress = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath, "utf8")) : {};
  if (progress.digest) {assert.equal(progress.digest, digest);}
  let {itemId, gameId} = progress;
  if (!itemId) {
    const uploadId = await client.upload(singleFile(source), "FILES", "GENERAL");
    const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
      data: {uploadId, targetPlatformInstanceId: old.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
    itemId = (await reviewForImport(client, imported.importJobId)).itemId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId}));
  }
  if (!gameId) {
    gameId = (await approveCart(client, itemId)).gameId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId, gameId}));
  }
  evidence.gameId = gameId;
  evidence.stages.push("published-in-mame2003-plus-directory");
  const capabilities = {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true};
  const body = {gameId, coreId: "mame_arcade", saveStateId: null, dosEntry: null,
    returnTo: `/games/${gameId}`, clientCapabilities: capabilities};
  if (env.RETROM_MAME_ARCADE_EXPECT_INCOMPATIBLE === "1") {
    const response = await client.raw("POST", "/api/v1/launches", {headers: client.writeHeaders(), data: body});
    const blocked = await response.json();
    evidence.blocked = blocked;
    assert.equal(response.status(), 422, `MAME_ARCADE_INCOMPATIBLE_STATUS_${response.status()}`);
    assert.equal(blocked.error?.code, "LAUNCH_BLOCKED");
    evidence.stages.push("current-dat-rejected-old-zip");
    evidence.status = "PASS";
  } else {
    let launch;
    for (let attempt = 0; attempt < 3; attempt++) {
      const response = await client.raw("POST", "/api/v1/launches", {headers: client.writeHeaders(), data: body});
      if (response.status() === 201) {launch = await response.json(); break;}
      assert.equal(response.status(), 202, `MAME_ARCADE_FALLBACK_LAUNCH_${response.status()}`);
      const pending = await response.json();
      assert.equal(pending.status, "VALIDATION_PENDING");
      evidence.stages.push("current-variant-validation");
      const deadline = Date.now() + 120000;
      while (Date.now() < deadline) {
        const job = await client.json("GET", `/api/v1/admin/jobs/${pending.jobId}`);
        if (job.state === "SUCCEEDED") {break;}
        assert.ok(!["FAILED", "CANCELLED"].includes(job.state), `MAME_ARCADE_FALLBACK_VALIDATION_${job.errorCode ?? job.state}`);
        await new Promise(resolve => setTimeout(resolve, 500));
      }
    }
    assert.ok(launch, "MAME_ARCADE_FALLBACK_VALIDATION_TIMEOUT");
    evidence.stages.push("current-launch-from-mame2003-plus-directory");
    const page = await context.newPage();
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
    assert.ok(canvas, "MAME_ARCADE_FALLBACK_CANVAS_MISSING");
    await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 30000});
    const config = await page.evaluate(async id => (await fetch(`/runtime/launches/${id}/config`)).json(), launch.launchId);
    assert.equal(config.runtime.targetId, "mame-arcade");
    assert.equal(config.targetOptions.machine, machine);
    await page.waitForTimeout(8000);
    evidence.frame = await canvasDigest(canvas);
    await canvas.screenshot({path: join(directory, "fallback.png")});
    assert.deepEqual(evidence.errors, []);
    evidence.status = "AUTOMATED_PASS_REQUIRES_VISUAL_REVIEW";
  }
} catch (error) {evidence.errorCode = error.message; process.exitCode = 1;}
finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "fallback.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify({status: evidence.status, error: evidence.errorCode}));
}
